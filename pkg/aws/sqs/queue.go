package sqs

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// heldState is where a held message is in its life inside the worker. It only moves forward:
// waiting → running → settled, or straight from waiting to settled.
type heldState int

const (
	// waiting: received from SQS and sitting in line, but no handler has taken it yet.
	waiting heldState = iota
	// running: a handler has it and has not returned yet.
	running
	// settled: the worker is finished with it. That happens when the handler marks it, the heartbeat drops it,
	// shutdown lets go of it while it was waiting, or its handler returns. A settled message is no longer
	// extended by the heartbeat. If it was still waiting, it also leaves the line, which frees room; a running
	// message's room is counted through its handler, so it frees up only when the handler calls done.
	settled
)

// heldMessage is one message the worker has received and not yet finished with. It carries the state the
// handlers and the heartbeat need to agree on. Its own mutex guards state, cancel and visibleAt; take
// messageQueue.mu first when holding both.
type heldMessage struct {
	raw types.Message

	mu    sync.Mutex
	state heldState
	// cancel cancels the handler context of the batch running this message. It is set when a handler takes
	// the message, so the heartbeat can stop that handler if the message is lost.
	cancel context.CancelCauseFunc
	// visibleAt is when SQS will show the message to other consumers unless it is extended. It starts at the
	// time the receive request was sent plus the heartbeat extension, which errs early since a long-polling
	// receive can wait up to 20 seconds, and each successful heartbeat pushes it later.
	visibleAt time.Time
}

// extendTo records that SQS now keeps the message hidden until t. It never moves the deadline earlier, so an
// out-of-order or slow heartbeat result cannot shorten it.
func (h *heldMessage) extendTo(t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if t.After(h.visibleAt) {
		h.visibleAt = t
	}
}

// visibleBy reports whether the message's visibility may have ended by t, meaning another consumer could
// receive it. The heartbeat asks this with t set to now plus its call timeout: if the answer is yes, an
// extension sent now might arrive too late, so the message is treated as lost instead.
func (h *heldMessage) visibleBy(t time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return !t.Before(h.visibleAt)
}

// isSettled reports whether the worker is already finished with the message. The heartbeat checks it so it
// does not extend a message that was marked or dropped after the heartbeat took its snapshot.
func (h *heldMessage) isSettled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.state == settled
}

// messageQueue is the shared state between the poller, the handlers and the heartbeat. It does two jobs:
//
//   - It holds every received message until the worker is finished with it: the line of messages waiting for
//     a handler, plus the set of all held messages, waiting or running, that the heartbeat keeps hidden.
//   - It decides how many messages the poller may receive next: a batch for every idle handler, plus up to
//     maxPrefetched waiting for a busy one.
//
// The life of a message through it: the poller calls reserve to learn how many it may receive, receives
// them, and calls add to put them in line. A handler calls take to get a batch, runs it, settles each message
// (by Mark, or when it returns), then calls done to become idle again. The heartbeat reads snapshot and calls
// settle on messages it cannot keep hidden. Shutdown calls stop.
//
// Only idle handlers and waiting messages count toward free space. Room opens when a handler calls done,
// when a waiting message is dropped, or when a handler takes waiting messages into its batch. Marking a
// running message does not open room by itself.
//
// All fields below mu are guarded by it. Goroutines that need to wait for a change (the poller in reserve,
// handlers in take) sleep on cond, and the functions that change the state wake them with cond.Broadcast
// so they recheck. The one exception is reserve raising reserved, which only ever reduces free space, so
// nobody needs waking.
//
// Lock order: messageQueue.mu, then heldMessage.mu.
type messageQueue struct {
	batchSize     int
	maxPrefetched int

	mu   sync.Mutex
	cond *sync.Cond

	idle     int                       // handlers not running a batch; starts at WorkerCount
	reserved int                       // messages a receive still in flight may return
	waiting  []*heldMessage            // received, not yet taken by a handler, oldest first
	held     map[*heldMessage]struct{} // every message the worker holds, waiting or running
	stopped  bool                      // no more messages will be added
}

// newMessageQueue returns an empty queue for a worker with workerCount handlers, all idle.
func newMessageQueue(workerCount, batchSize, maxPrefetched int) *messageQueue {
	q := &messageQueue{
		batchSize:     batchSize,
		maxPrefetched: maxPrefetched,
		idle:          workerCount,
		held:          make(map[*heldMessage]struct{}),
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// free is how many more messages the worker may receive right now. Callers hold q.mu.
//
// Messages already waiting (or about to arrive from a receive in flight) go to idle handlers first, one batch
// per handler. A handler starts as soon as it has any messages and does not wait to fill its batch, so a
// handler that will get even one message counts as full. Each idle handler left over has room for a whole
// batch; beyond that, up to maxPrefetched messages may wait for a busy handler.
//
// For example, with batchSize 3 and maxPrefetched 4:
//
//   - 2 idle handlers, nothing waiting: 2×3 + 4 = 10.
//   - 2 idle handlers, 4 waiting: the 4 fill both handlers (3 + 1), so only the maxPrefetched places (4) are left.
//   - 0 idle handlers, 1 waiting: 4 − 1 = 3.
func (q *messageQueue) free() int {
	pending := len(q.waiting) + q.reserved
	occupied := (pending + q.batchSize - 1) / q.batchSize
	if occupied <= q.idle {
		return (q.idle-occupied)*q.batchSize + q.maxPrefetched
	}
	return q.maxPrefetched - (pending - q.idle*q.batchSize)
}

// reserve is called by the poller before each receive. It answers "how many messages may I ask SQS for?".
// If the answer is zero, it sleeps until a handler finishes, a waiting message is dropped or taken, or ctx is
// cancelled, and then checks again. It returns between 1 and limit (the most one receive may ask for), and
// counts that many as reserved until add is called with the result. It returns ctx's error once ctx is
// cancelled.
func (q *messageQueue) reserve(ctx context.Context, limit int) (int, error) {
	// cond.Wait cannot watch ctx, so wake the sleepers when ctx is cancelled. Without this, a full worker would
	// not notice shutdown until one of its handlers finished.
	stop := context.AfterFunc(ctx, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})
	defer stop()

	q.mu.Lock()
	defer q.mu.Unlock()

	// Sleep until there is room or ctx is cancelled. Wait unlocks q.mu while asleep and locks it again
	// before returning; a wake-up only means something changed, so recheck.
	for q.free() <= 0 && ctx.Err() == nil {
		q.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Count the request as reserved, so the room it may fill is not offered twice.
	n := min(q.free(), limit)
	q.reserved += n
	return n, nil
}

// add is called by the poller after each receive, whether it succeeded or not. It releases the reservation
// of n made by reserve, records each received message as held with SQS hiding it until visibleAt, and puts
// it at the back of the line. It wakes idle handlers so they can take the new messages.
func (q *messageQueue) add(n int, msgs []types.Message, visibleAt time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.reserved -= n
	for _, m := range msgs {
		h := &heldMessage{raw: m, visibleAt: visibleAt}
		q.held[h] = struct{}{}
		q.waiting = append(q.waiting, h)
	}
	q.cond.Broadcast()
}

// take is called by an idle handler to get its next batch. It sleeps until a message is waiting, then takes
// up to batchSize of the oldest waiting messages, without waiting for a full batch. The messages become
// running, the handler counts as busy, and they share a new handler context derived from ctx, so the
// heartbeat can cancel that handler if one of them is lost.
//
// It returns false, and the handler should exit, once the queue is stopped with nothing waiting, or when ctx
// is cancelled: after shutdown starts, waiting messages are never started. take does not watch ctx while it
// sleeps; it notices cancellation when it is woken, which after shutdown happens through stop, called as soon
// as the poller returns.
func (q *messageQueue) take(ctx context.Context) (context.Context, context.CancelCauseFunc, []*heldMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.waiting) == 0 && !q.stopped {
		q.cond.Wait()
	}
	// Once ctx is cancelled, waiting messages are not started.
	if len(q.waiting) == 0 || ctx.Err() != nil {
		return nil, nil, nil, false
	}

	// Take the oldest messages, up to a batch, and remove them from the line.
	n := min(len(q.waiting), q.batchSize)
	batch := slices.Clone(q.waiting[:n])
	q.waiting = slices.Delete(q.waiting, 0, n)

	batchCtx, cancel := context.WithCancelCause(ctx)
	for _, h := range batch {
		h.mu.Lock()
		h.state = running
		h.cancel = cancel
		h.mu.Unlock()
	}

	q.idle--
	q.cond.Broadcast()
	return batchCtx, cancel, batch, true
}

// done is called by a handler after its batch returns and its messages are settled. It makes the handler
// idle again, which frees room for a whole batch, and wakes the poller so it can receive more.
func (q *messageQueue) done() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.idle++
	q.cond.Broadcast()
}

// stop is called when the poller has stopped for good. It tells handlers that no more messages will arrive,
// so they exit once nothing is left for them.
//
// On shutdown (dropWaiting true), the worker also lets go of every waiting message: none of them is started,
// the heartbeat stops extending them, and SQS shows them to other consumers when their visibility ends.
// After a receive error (dropWaiting false), handlers still take and run what is already waiting.
func (q *messageQueue) stop(dropWaiting bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.stopped = true
	if dropWaiting {
		for _, h := range q.waiting {
			h.mu.Lock()
			h.state = settled
			h.mu.Unlock()
			delete(q.held, h)
		}
		q.waiting = nil
	}
	q.cond.Broadcast()
}

// settle marks the worker as finished with a message. Mark, the heartbeat (when it drops a message), and the
// handler loop (after the handler returns) all call it, and only the first call takes effect, so a message
// is never deleted after it was dropped, or dropped after it was marked.
//
// The message leaves the held set, so the heartbeat stops extending it, and leaves the line if it was still
// waiting, which frees room for the poller. settle returns the state the message was in, the cancel func of
// its batch if it was running (so the heartbeat can stop that handler), and whether this call settled it.
func (q *messageQueue) settle(h *heldMessage) (heldState, context.CancelCauseFunc, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	h.mu.Lock()
	prev, cancel := h.state, h.cancel
	if prev == settled {
		h.mu.Unlock()
		return prev, nil, false
	}
	h.state = settled
	h.mu.Unlock()

	delete(q.held, h)
	if prev == waiting {
		q.waiting = slices.DeleteFunc(q.waiting, func(w *heldMessage) bool { return w == h })
	}
	q.cond.Broadcast()
	return prev, cancel, true
}

// snapshot returns every message the worker holds, waiting or running, in no particular order. The heartbeat
// uses it each tick to know which messages to extend. Messages may be settled after the snapshot is taken,
// which the heartbeat checks with isSettled before extending.
func (q *messageQueue) snapshot() []*heldMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]*heldMessage, 0, len(q.held))
	for h := range q.held {
		out = append(out, h)
	}
	return out
}

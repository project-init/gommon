package sqs

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type heldState int

const (
	// waiting: received, not yet given to a handler.
	waiting heldState = iota
	// running: given to a handler that has not returned.
	running
	// settled: marked, dropped, let go on shutdown, or its handler returned. The worker no longer holds it.
	settled
)

// heldMessage is a message the worker has received and not yet settled.
type heldMessage struct {
	raw types.Message

	mu    sync.Mutex
	state heldState
	// cancel cancels the handler context of the batch running this message.
	cancel context.CancelCauseFunc
	// visibleAt is when SQS will show the message to other consumers unless it is extended.
	visibleAt time.Time
}

func (h *heldMessage) extendTo(t time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if t.After(h.visibleAt) {
		h.visibleAt = t
	}
}

// visibleBy reports whether SQS may show the message to other consumers by t.
func (h *heldMessage) visibleBy(t time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return !t.Before(h.visibleAt)
}

func (h *heldMessage) isSettled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.state == settled
}

// messageQueue is the worker's in-memory line of received messages and its count of idle handlers.
// It decides how many messages the poller may receive: enough to fill every idle handler's batch,
// plus up to maxPrefetched waiting for a busy handler.
//
// Lock order: messageQueue.mu, then heldMessage.mu.
type messageQueue struct {
	batchSize     int
	maxPrefetched int

	mu   sync.Mutex
	cond *sync.Cond

	idle     int            // handlers not running a batch
	reserved int            // messages requested by a receive still in flight
	waiting  []*heldMessage // received, not yet taken by a handler, oldest first
	held     map[*heldMessage]struct{}
	stopped  bool // no more messages will be added
}

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

// free is how many more messages the worker may receive. Callers hold q.mu.
//
// Waiting messages, and those a receive in flight may return, go to idle handlers first, one batch per
// handler. A handler starts with whatever is waiting and does not wait to fill its batch, so a handler that
// will be occupied counts as full. Each remaining idle handler has room for a batch; beyond that, messages
// wait, up to maxPrefetched.
func (q *messageQueue) free() int {
	pending := len(q.waiting) + q.reserved
	occupied := (pending + q.batchSize - 1) / q.batchSize
	if occupied <= q.idle {
		return (q.idle-occupied)*q.batchSize + q.maxPrefetched
	}
	return q.maxPrefetched - (pending - q.idle*q.batchSize)
}

// reserve blocks until the worker may receive at least one message, then reserves up to limit.
func (q *messageQueue) reserve(ctx context.Context, limit int) (int, error) {
	stop := context.AfterFunc(ctx, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})
	defer stop()

	q.mu.Lock()
	defer q.mu.Unlock()

	for q.free() <= 0 && ctx.Err() == nil {
		q.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	n := min(q.free(), limit)
	q.reserved += n
	return n, nil
}

// add ends a reservation of n and puts the received messages at the back of the line.
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

// take blocks until a message is waiting, then marks up to batchSize of the oldest as running under a new
// handler context. It returns false once the queue is stopped and empty, or ctx is cancelled.
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

// done returns a handler to the idle pool.
func (q *messageQueue) done() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.idle++
	q.cond.Broadcast()
}

// stop tells handlers no more messages will arrive. When dropWaiting is true, the worker also stops holding
// the waiting messages: they are never started, the heartbeat no longer extends them, and they reappear when
// their visibility ends. Otherwise handlers take them before exiting.
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

// settle ends a message's hold. Only the first call succeeds; it returns the state the message was in and,
// if it was running, the cancel func of its batch.
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

// snapshot returns every held message, waiting or running.
func (q *messageQueue) snapshot() []*heldMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]*heldMessage, 0, len(q.held))
	for h := range q.held {
		out = append(out, h)
	}
	return out
}

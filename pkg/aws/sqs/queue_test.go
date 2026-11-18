package sqs

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeVisSQS records the heartbeat calls fakeSQS accepts, and can make them fail.
type fakeVisSQS struct {
	*fakeSQS

	visMu      sync.Mutex
	heartbeats [][]string // receipt handles per heartbeat call
	extensions []int32    // VisibilityTimeout per heartbeat call

	// heartbeatResult, when set, decides each heartbeat call's result. Default: every entry succeeds.
	heartbeatResult func(in *sqs.ChangeMessageVisibilityBatchInput) (*sqs.ChangeMessageVisibilityBatchOutput, error)
}

func newFakeVisSQS(t *testing.T) *fakeVisSQS {
	t.Helper()
	return &fakeVisSQS{fakeSQS: newFakeSQS(t)}
}

func (f *fakeVisSQS) ChangeMessageVisibilityBatch(ctx context.Context, in *sqs.ChangeMessageVisibilityBatchInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityBatchOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	handles := make([]string, len(in.Entries))
	for i, e := range in.Entries {
		handles[i] = aws.ToString(e.ReceiptHandle)
	}

	f.visMu.Lock()
	f.heartbeats = append(f.heartbeats, handles)
	if len(in.Entries) > 0 {
		f.extensions = append(f.extensions, in.Entries[0].VisibilityTimeout)
	}
	result := f.heartbeatResult
	f.visMu.Unlock()

	if result != nil {
		return result(in)
	}

	out := &sqs.ChangeMessageVisibilityBatchOutput{}
	for _, e := range in.Entries {
		out.Successful = append(out.Successful, types.ChangeMessageVisibilityBatchResultEntry{Id: e.Id})
	}
	return out, nil
}

func (f *fakeVisSQS) getHeartbeats() [][]string {
	f.visMu.Lock()
	defer f.visMu.Unlock()
	return slices.Clone(f.heartbeats)
}

// failHeartbeatFor makes heartbeat entries for the given receipt handle fail as the caller's fault.
func failHeartbeatFor(handle string) func(in *sqs.ChangeMessageVisibilityBatchInput) (*sqs.ChangeMessageVisibilityBatchOutput, error) {
	return func(in *sqs.ChangeMessageVisibilityBatchInput) (*sqs.ChangeMessageVisibilityBatchOutput, error) {
		out := &sqs.ChangeMessageVisibilityBatchOutput{}
		for _, e := range in.Entries {
			if aws.ToString(e.ReceiptHandle) == handle {
				out.Failed = append(out.Failed, types.BatchResultErrorEntry{
					Id:          e.Id,
					Code:        aws.String("ReceiptHandleIsInvalid"),
					SenderFault: true,
				})
				continue
			}
			out.Successful = append(out.Successful, types.ChangeMessageVisibilityBatchResultEntry{Id: e.Id})
		}
		return out, nil
	}
}

func msg(id string) types.Message {
	return types.Message{MessageId: aws.String(id), ReceiptHandle: aws.String("rh-" + id)}
}

func receive(ids ...string) *sqs.ReceiveMessageOutput {
	out := &sqs.ReceiveMessageOutput{}
	for _, id := range ids {
		out.Messages = append(out.Messages, msg(id))
	}
	return out
}

func contains(handles []string, h string) bool { return slices.Contains(handles, h) }

// startWorker runs w until the test ends and returns a cancel func and Start's result.
func startWorker(t *testing.T, w *Worker) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	t.Cleanup(cancel)
	return cancel, done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stays asserts cond holds for the whole of d.
func stays(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !cond() {
			t.Fatalf("expected %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitDone(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for worker shutdown")
	}
}

// gate blocks handlers per message ID until opened.
type gate struct {
	mu      sync.Mutex
	started []string
	chans   map[string]chan struct{}
}

func newGate() *gate { return &gate{chans: map[string]chan struct{}{}} }

func (g *gate) ch(id string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.chans[id] == nil {
		g.chans[id] = make(chan struct{})
	}
	return g.chans[id]
}

func (g *gate) wait(id string) {
	g.mu.Lock()
	g.started = append(g.started, id)
	g.mu.Unlock()
	<-g.ch(id)
}

func (g *gate) open(id string) { close(g.ch(id)) }

func (g *gate) getStarted() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.started)
}

func TestWorker_ReceivesOnlyForFreeSlots_AndALongJobDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	g := newGate()

	w, err := New(
		func(_ context.Context, batch []Message) {
			for _, m := range batch {
				g.wait(aws.ToString(m.Raw().MessageId))
				m.Mark()
			}
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(2),
		WithBatchSize(1),
		WithMaxPrefetched(0),
		WithDeleteBatchWindow(10*time.Millisecond),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	fc.enqueueReceive(receive("b"), nil)
	fc.enqueueReceive(receive("c"), nil)

	cancel, done := startWorker(t, w)

	waitFor(t, "a and b to start", func() bool { return len(g.getStarted()) == 2 })

	// Both handlers are busy, so the worker must not receive c.
	stays(t, "no receive while every slot is held", 100*time.Millisecond, func() bool { return len(fc.getReceiveCalls()) == 2 })

	// Finishing b frees one slot: c is received and runs while a is still running.
	g.open("b")
	waitFor(t, "c to start", func() bool { return contains(g.getStarted(), "c") })

	// Two idle handlers, then one, then one again after b finishes.
	var sizes []int32
	for _, call := range fc.getReceiveCalls() {
		sizes = append(sizes, call.MaxNumberOfMessages)
	}
	assert.Equal(t, []int32{2, 1, 1}, sizes)

	g.open("a")
	g.open("c")
	cancel()
	waitDone(t, done)
}

func TestWorker_ReceiveAsksForFreeSpace(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)

	var batchesMu sync.Mutex
	var batches [][]string
	block := make(chan struct{})

	w, err := New(
		func(_ context.Context, batch []Message) {
			var got []string
			for _, m := range batch {
				got = append(got, aws.ToString(m.Raw().MessageId))
			}
			batchesMu.Lock()
			batches = append(batches, got)
			batchesMu.Unlock()

			<-block
			for _, m := range batch {
				m.Mark()
			}
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(2),
		WithBatchSize(3),
		WithMaxPrefetched(4),
	)
	require.NoError(t, err)

	maxSizes := func() []int32 {
		var out []int32
		for _, call := range fc.getReceiveCalls() {
			out = append(out, call.MaxNumberOfMessages)
		}
		return out
	}

	// Both handlers idle, nothing waiting: 2*3 + 4 = 10.
	fc.enqueueReceive(receive("a", "b", "c", "d"), nil)
	cancel, done := startWorker(t, w)

	// a, b, c and d start in two batches; both handlers are busy, so only the 4 waiting places are free.
	waitFor(t, "second receive", func() bool { return len(fc.getReceiveCalls()) == 2 })
	assert.Equal(t, []int32{10, 4}, maxSizes())
	waitFor(t, "both handlers to start", func() bool {
		batchesMu.Lock()
		defer batchesMu.Unlock()
		return len(batches) == 2
	})

	// e waits for a handler, leaving 3 places.
	fc.enqueueReceive(receive("e"), nil)
	waitFor(t, "third receive", func() bool { return len(fc.getReceiveCalls()) == 3 })
	assert.Equal(t, []int32{10, 4, 3}, maxSizes())

	batchesMu.Lock()
	assert.ElementsMatch(t, [][]string{{"a", "b", "c"}, {"d"}}, batches)
	batchesMu.Unlock()

	close(block)
	cancel()
	waitDone(t, done)
}

func TestWorker_ReceiveAsksForAtMostTenMessages(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)

	w, err := New(
		func(context.Context, []Message) {},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(4),
		WithBatchSize(5),
		WithMaxPrefetched(3),
	)
	require.NoError(t, err)

	cancel, done := startWorker(t, w)
	waitFor(t, "a receive", func() bool { return len(fc.getReceiveCalls()) == 1 })
	assert.Equal(t, int32(10), fc.getReceiveCalls()[0].MaxNumberOfMessages)

	cancel()
	waitDone(t, done)
}

func TestWorker_HandlerTakesWaitingMessagesFromSeveralReceives(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	batches := make(chan []string, 10)
	first := make(chan struct{})
	var once sync.Once

	w, err := New(
		func(_ context.Context, batch []Message) {
			var got []string
			for _, m := range batch {
				got = append(got, aws.ToString(m.Raw().MessageId))
				m.Mark()
			}
			batches <- got
			once.Do(func() { <-first })
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(3),
		WithMaxPrefetched(4),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	cancel, done := startWorker(t, w)

	assert.Equal(t, []string{"a"}, <-batches)

	// b and c, then d, arrive in separate receives while the handler is busy. Once free, it takes a full
	// batch across both receives.
	fc.enqueueReceive(receive("b", "c"), nil)
	fc.enqueueReceive(receive("d"), nil)
	waitFor(t, "b, c and d to be received", func() bool { return len(fc.getReceiveCalls()) >= 4 })
	close(first)
	assert.Equal(t, []string{"b", "c", "d"}, <-batches)

	cancel()
	waitDone(t, done)
}

func TestWorker_PrefetchHoldsAtMostItsLimit(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	g := newGate()

	w, err := New(
		func(_ context.Context, batch []Message) {
			for _, m := range batch {
				g.wait(aws.ToString(m.Raw().MessageId))
				m.Mark()
			}
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(2),
	)
	require.NoError(t, err)

	for _, id := range []string{"a", "b", "c", "d"} {
		fc.enqueueReceive(receive(id), nil)
	}
	cancel, done := startWorker(t, w)

	// One running plus two waiting fills all three slots.
	waitFor(t, "three receives", func() bool { return len(fc.getReceiveCalls()) == 3 })
	stays(t, "no fourth receive while three messages are held", 100*time.Millisecond, func() bool { return len(fc.getReceiveCalls()) == 3 })
	assert.Equal(t, []string{"a"}, g.getStarted())

	// Finishing a starts a waiting message and frees a slot for d.
	g.open("a")
	waitFor(t, "fourth receive", func() bool { return len(fc.getReceiveCalls()) == 4 })
	waitFor(t, "b to start", func() bool { return contains(g.getStarted(), "b") })

	for _, id := range []string{"b", "c", "d"} {
		g.open(id)
	}
	cancel()
	waitDone(t, done)
}

func TestWorker_HeartbeatExtendsWaitingAndRunningMessages(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	g := newGate()

	w, err := New(
		func(_ context.Context, batch []Message) {
			for _, m := range batch {
				g.wait(aws.ToString(m.Raw().MessageId))
				m.Mark()
			}
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(1),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	fc.enqueueReceive(receive("b"), nil)
	cancel, done := startWorker(t, w)

	waitFor(t, "a heartbeat covering running a and waiting b", func() bool {
		for _, hb := range fc.getHeartbeats() {
			if contains(hb, "rh-a") && contains(hb, "rh-b") {
				return true
			}
		}
		return false
	})

	fc.visMu.Lock()
	assert.Equal(t, int32(4), fc.extensions[0])
	fc.visMu.Unlock()

	// Receives hide messages for the heartbeat extension.
	assert.Equal(t, int32(4), fc.getReceiveCalls()[0].VisibilityTimeout)

	g.open("a")
	g.open("b")
	cancel()
	waitDone(t, done)
}

func TestWorker_HeartbeatFailureDropsWaitingMessage(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	fc.heartbeatResult = failHeartbeatFor("rh-b")
	g := newGate()

	w, err := New(
		func(_ context.Context, batch []Message) {
			for _, m := range batch {
				g.wait(aws.ToString(m.Raw().MessageId))
				m.Mark()
			}
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(1),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	fc.enqueueReceive(receive("b"), nil)
	cancel, done := startWorker(t, w)

	// Dropping b frees its slot, so the worker receives again while a still runs.
	waitFor(t, "a receive after b is dropped", func() bool { return len(fc.getReceiveCalls()) == 3 })

	g.open("a")
	stays(t, "b never reaches the handler", 100*time.Millisecond, func() bool { return !contains(g.getStarted(), "b") })

	cancel()
	waitDone(t, done)
}

func TestWorker_HeartbeatFailureCancelsRunningHandler(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	fc.heartbeatResult = failHeartbeatFor("rh-a")
	causes := make(chan error, 1)

	w, err := New(
		func(ctx context.Context, batch []Message) {
			<-ctx.Done()
			causes <- context.Cause(ctx)
			// The message is lost, so marking it does nothing.
			batch[0].Mark()
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(0),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	cancel, done := startWorker(t, w)

	select {
	case cause := <-causes:
		assert.ErrorIs(t, cause, ErrVisibilityLost)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the handler to be cancelled")
	}

	stays(t, "no delete for a lost message", 50*time.Millisecond, func() bool {
		return len(fc.getDeleteCalls()) == 0
	})

	cancel()
	waitDone(t, done)
}

func TestWorker_HeartbeatDropsMessageWhoseVisibilityEnded(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	fc.heartbeatResult = func(*sqs.ChangeMessageVisibilityBatchInput) (*sqs.ChangeMessageVisibilityBatchOutput, error) {
		return nil, errors.New("network down")
	}
	causes := make(chan error, 1)

	w, err := New(
		func(ctx context.Context, _ []Message) {
			<-ctx.Done()
			causes <- context.Cause(ctx)
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(0),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)
	// Shorter than validation allows, so the test does not wait four seconds for the message to be lost.
	w.heartbeatExtension = time.Second

	fc.enqueueReceive(receive("a"), nil)
	cancel, done := startWorker(t, w)

	// Every heartbeat fails, so once the one-second visibility from the receive ends the message is lost.
	select {
	case cause := <-causes:
		assert.ErrorIs(t, cause, ErrVisibilityLost)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the handler to be cancelled")
	}

	cancel()
	waitDone(t, done)
}

func TestWorker_MarkDeletesTheMessageAndStopsItsHeartbeat(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	marked := make(chan struct{})
	finish := make(chan struct{})

	w, err := New(
		func(_ context.Context, batch []Message) {
			for len(fc.getHeartbeats()) == 0 {
				time.Sleep(5 * time.Millisecond)
			}

			batch[0].Mark()
			close(marked)
			<-finish
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(0),
		WithDeleteBatchSize(1),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	cancel, done := startWorker(t, w)

	<-marked
	waitFor(t, "a to be deleted", func() bool { return len(fc.getDeleteCalls()) == 1 })
	afterMark := len(fc.getHeartbeats())

	stays(t, "no heartbeat for a marked message", 100*time.Millisecond, func() bool {
		for _, hb := range fc.getHeartbeats()[afterMark:] {
			if contains(hb, "rh-a") {
				return false
			}
		}
		return true
	})

	close(finish)
	cancel()
	waitDone(t, done)
}

func TestWorker_UnsettledMessageFreesItsSlotWhenTheHandlerReturns(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	handled := make(chan struct{}, 1)

	w, err := New(
		func(_ context.Context, _ []Message) {
			// Leave the message for redelivery.
			handled <- struct{}{}
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(0),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	cancel, done := startWorker(t, w)

	<-handled
	waitFor(t, "the next receive", func() bool { return len(fc.getReceiveCalls()) == 2 })

	// Nothing is held, so the heartbeat makes no more calls once any in-flight one finishes.
	time.Sleep(30 * time.Millisecond)
	before := len(fc.getHeartbeats())
	stays(t, "no heartbeats once nothing is held", 100*time.Millisecond, func() bool { return len(fc.getHeartbeats()) == before })

	assert.Empty(t, fc.getDeleteCalls())

	cancel()
	waitDone(t, done)
}

func TestWorker_ShutdownLetsGoOfWaitingMessagesAndKeepsRunningOnesHidden(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	causes := make(chan error, 1)
	finish := make(chan struct{})
	var started []string
	var startedMu sync.Mutex

	w, err := New(
		func(ctx context.Context, batch []Message) {
			startedMu.Lock()
			started = append(started, aws.ToString(batch[0].Raw().MessageId))
			startedMu.Unlock()

			<-ctx.Done()
			causes <- context.Cause(ctx)
			<-finish
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(1),
		WithDeleteBatchWindow(10*time.Millisecond),
		WithHeartbeat(20*time.Millisecond, 4*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a"), nil)
	fc.enqueueReceive(receive("b"), nil)
	cancel, done := startWorker(t, w)

	waitFor(t, "a running and b waiting", func() bool {
		for _, hb := range fc.getHeartbeats() {
			if contains(hb, "rh-a") && contains(hb, "rh-b") {
				return true
			}
		}
		return false
	})
	cancel()
	assert.ErrorIs(t, <-causes, context.Canceled)

	// The running message stays hidden while its handler finishes; the waiting one is no longer extended.
	afterCancel := len(fc.getHeartbeats())
	waitFor(t, "a heartbeat for a after shutdown began", func() bool {
		for _, hb := range fc.getHeartbeats()[afterCancel:] {
			if contains(hb, "rh-a") {
				return true
			}
		}
		return false
	})
	for _, hb := range fc.getHeartbeats()[afterCancel:] {
		assert.NotContains(t, hb, "rh-b")
	}

	select {
	case err := <-done:
		t.Fatalf("worker returned before the running handler finished: %v", err)
	default:
	}

	close(finish)
	waitDone(t, done)

	startedMu.Lock()
	defer startedMu.Unlock()
	assert.Equal(t, []string{"a"}, started, "b was waiting, so it is never started")
	assert.Empty(t, fc.getDeleteCalls())
}

func TestMessageQueue_Free(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                           string
		idle, batchSize, maxPrefetched int
		waiting, reserved              int
		want                           int
	}{
		{name: "all idle, nothing waiting", idle: 2, batchSize: 3, maxPrefetched: 4, want: 10},
		{name: "waiting messages occupy idle handlers as full batches", idle: 2, batchSize: 3, maxPrefetched: 4, waiting: 4, want: 4},
		{name: "a partial batch leaves no room in its handler", idle: 2, batchSize: 3, maxPrefetched: 0, waiting: 1, want: 3},
		{name: "all busy, nothing waiting", idle: 0, batchSize: 3, maxPrefetched: 4, want: 4},
		{name: "all busy, some waiting", idle: 0, batchSize: 3, maxPrefetched: 4, waiting: 1, want: 3},
		{name: "a receive in flight counts as waiting", idle: 0, batchSize: 3, maxPrefetched: 4, waiting: 1, reserved: 3, want: 0},
		{name: "no prefetching, one idle handler", idle: 1, batchSize: 1, maxPrefetched: 0, want: 1},
		{name: "no prefetching, busy", idle: 0, batchSize: 1, maxPrefetched: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := newMessageQueue(tt.idle, tt.batchSize, tt.maxPrefetched)
			q.reserved = tt.reserved
			for range tt.waiting {
				q.waiting = append(q.waiting, &heldMessage{})
			}

			assert.Equal(t, tt.want, q.free())
		})
	}
}

func TestWorker_ReceiveErrorThenShutdownLetsGoOfWhatIsStillWaiting(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)
	var started []string
	var startedMu sync.Mutex
	running := make(chan struct{})

	w, err := New(
		func(ctx context.Context, batch []Message) {
			startedMu.Lock()
			started = append(started, aws.ToString(batch[0].Raw().MessageId))
			startedMu.Unlock()
			close(running)
			<-ctx.Done()
		},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithMaxPrefetched(2), // room for one more receive while a runs and b waits
	)
	require.NoError(t, err)

	fc.enqueueReceive(receive("a", "b"), nil)
	fc.enqueueReceive(nil, errors.New("boom"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	// The receive error stops polling while a runs and b waits. Cancelling then stops a, and b is never started.
	<-running
	waitFor(t, "polling to stop after the failed receive", func() bool {
		w.queue.mu.Lock()
		defer w.queue.mu.Unlock()
		return w.queue.stopped
	})
	cancel()

	select {
	case err := <-done:
		require.ErrorContains(t, err, "boom")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for worker shutdown")
	}

	startedMu.Lock()
	defer startedMu.Unlock()
	assert.Equal(t, []string{"a"}, started)
	assert.Empty(t, w.queue.snapshot(), "the worker no longer holds b")
}

func TestWorker_HeartbeatDropsMessageAboutToBecomeVisible(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)

	w, err := New(
		func(context.Context, []Message) {},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithHeartbeat(time.Second, 20*time.Second),
	)
	require.NoError(t, err)

	now := time.Now()
	w.queue = newMessageQueue(1, 1, 1)
	w.queue.add(0, []types.Message{msg("soon")}, now.Add(500*time.Millisecond)) // less than the 1s call timeout left
	w.queue.add(0, []types.Message{msg("later")}, now.Add(10*time.Second))

	w.heartbeatOnce(context.Background())

	require.Len(t, fc.getHeartbeats(), 1)
	assert.Equal(t, []string{"rh-later"}, fc.getHeartbeats()[0])

	held := w.queue.snapshot()
	require.Len(t, held, 1)
	assert.Equal(t, "later", aws.ToString(held[0].raw.MessageId))
}

func TestWorker_HeartbeatExtendsTenPerCall(t *testing.T) {
	t.Parallel()

	fc := newFakeVisSQS(t)

	w, err := New(
		func(context.Context, []Message) {},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithHeartbeat(time.Second, 20*time.Second),
	)
	require.NoError(t, err)

	w.queue = newMessageQueue(1, 1, 25)
	for i := range 25 {
		w.queue.add(0, []types.Message{msg(strconv.Itoa(i))}, time.Now().Add(time.Minute))
	}

	w.heartbeatOnce(context.Background())

	var sizes []int
	for _, hb := range fc.getHeartbeats() {
		sizes = append(sizes, len(hb))
	}
	assert.ElementsMatch(t, []int{10, 10, 5}, sizes)
}

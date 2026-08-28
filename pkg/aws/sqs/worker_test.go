package sqs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSQS struct {
	t *testing.T

	mu sync.Mutex

	receiveCalls []*sqs.ReceiveMessageInput
	deleteCalls  []*sqs.DeleteMessageBatchInput
	deleteOutput *sqs.DeleteMessageBatchOutput
	deleteErr    error

	receiveQueue chan receiveResult

	// Optional hooks to coordinate tests
	onReceive func(in *sqs.ReceiveMessageInput)
	onDelete  func(in *sqs.DeleteMessageBatchInput)
}

type receiveResult struct {
	out *sqs.ReceiveMessageOutput
	err error
}

func newFakeSQS(t *testing.T) *fakeSQS {
	t.Helper()
	return &fakeSQS{
		t:            t,
		receiveQueue: make(chan receiveResult, 100),
	}
}

func (f *fakeSQS) ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	f.mu.Lock()
	f.receiveCalls = append(f.receiveCalls, in)
	cb := f.onReceive
	f.mu.Unlock()

	if cb != nil {
		cb(in)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case rr := <-f.receiveQueue:
		return rr.out, rr.err
	}
}

func (f *fakeSQS) DeleteMessageBatch(
	ctx context.Context, in *sqs.DeleteMessageBatchInput, _ ...func(*sqs.Options),
) (*sqs.DeleteMessageBatchOutput, error) {
	f.mu.Lock()
	f.deleteCalls = append(f.deleteCalls, in)
	cb := f.onDelete
	output := f.deleteOutput
	err := f.deleteErr
	f.mu.Unlock()

	if cb != nil {
		cb(in)
	}

	select {
	case <-ctx.Done():
		// Mirror typical SDK behavior: call returns ctx error if context is cancelled.
		return nil, ctx.Err()
	default:
	}

	if output == nil {
		output = &sqs.DeleteMessageBatchOutput{}
	}

	return output, err
}

func (f *fakeSQS) setDeleteResult(output *sqs.DeleteMessageBatchOutput, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleteOutput = output
	f.deleteErr = err
}

func (f *fakeSQS) enqueueReceive(out *sqs.ReceiveMessageOutput, err error) {
	f.receiveQueue <- receiveResult{out: out, err: err}
}

func (f *fakeSQS) getReceiveCalls() []*sqs.ReceiveMessageInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*sqs.ReceiveMessageInput, len(f.receiveCalls))
	copy(cp, f.receiveCalls)
	return cp
}

func (f *fakeSQS) getDeleteCalls() []*sqs.DeleteMessageBatchInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*sqs.DeleteMessageBatchInput, len(f.deleteCalls))
	copy(cp, f.deleteCalls)
	return cp
}

func TestWorker_PollSetsVisibilityTimeout_WhenConfigured(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)
	vt := int32(42)

	w, err := New(
		func(ctx context.Context, batch []Message) {},
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(3),
		WithWaitTimeSeconds(5),
		WithVisibilityTimeoutSeconds(vt),
		WithDeleteBatchSize(10),
		WithDeleteBatchWindow(50*time.Millisecond),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Observe ReceiveMessage being called, then cancel to stop the poll loop.
	receivedCall := make(chan struct{}, 1)
	fc.onReceive = func(_ *sqs.ReceiveMessageInput) {
		select {
		case receivedCall <- struct{}{}:
		default:
		}
	}

	pollDone := make(chan error, 1)
	out := make(chan []types.Message, 1)

	go func() {
		pollDone <- w.poll(ctx, out)
	}()

	// Allow poll to return from its first ReceiveMessage call.
	fc.enqueueReceive(&sqs.ReceiveMessageOutput{Messages: []types.Message{}}, nil)

	select {
	case <-receivedCall:
		// Now we know the ReceiveMessageInput was constructed and passed to the client.
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for ReceiveMessage to be called")
	}

	cancel()

	<-pollDone

	calls := fc.getReceiveCalls()
	require.GreaterOrEqual(t, len(calls), 1)

	last := calls[len(calls)-1]
	require.NotNil(t, last.VisibilityTimeout)
	assert.Equal(t, vt, last.VisibilityTimeout)
	assert.Equal(t, int32(5), last.WaitTimeSeconds)
	assert.Equal(t, int32(3), last.MaxNumberOfMessages)
	assert.Equal(t, "https://example.com/queue", aws.ToString(last.QueueUrl))
}

func TestWorker_RunWorker_MarkEnqueuesDeletes_AndDeleterFlushesBySize(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)

	var deletedMu sync.Mutex
	var deleted []types.DeleteMessageBatchRequestEntry

	flushDone := make(chan struct{}, 1)
	fc.onDelete = func(in *sqs.DeleteMessageBatchInput) {
		deletedMu.Lock()
		deleted = append(deleted, in.Entries...)
		deletedMu.Unlock()
		flushDone <- struct{}{}
	}

	w := &Worker{
		client:   fc,
		queueURL: "https://example.com/queue",
		handler: func(ctx context.Context, batch []Message) {
			for _, m := range batch {
				m.Mark()
			}
		},
		workerCount:         1,
		batchSize:           10,
		waitTimeSeconds:     0,
		maxReceiveBatchSize: 10,

		deleteBatchSize:   2,                // flush by size
		deleteBatchWindow: 10 * time.Second, // avoid time-based flush
		deleteChan:        make(chan deleteEntry, 100),
		jobs:              make(chan []types.Message, 1),
	}

	// Start deleter
	var deleterWG sync.WaitGroup
	deleterWG.Go(func() {
		w.runBatchDeleter(t.Context(), w.deleteChan)
	})

	// Start worker
	var workerWG sync.WaitGroup
	workerWG.Go(func() {
		w.runWorker(t.Context(), w.jobs)
	})

	// Send 3 messages: deletions should flush as 2 + 1 (final flush happens on channel close)
	w.jobs <- []types.Message{
		{MessageId: aws.String("m1"), ReceiptHandle: aws.String("r1")},
		{MessageId: aws.String("m2"), ReceiptHandle: aws.String("r2")},
		{MessageId: aws.String("m3"), ReceiptHandle: aws.String("r3")},
	}

	// Wait for first flush (size-based).
	select {
	case <-flushDone:
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timed out waiting for first delete flush")
	}

	// Stop worker and close delete channel to force final flush.
	close(w.jobs)
	workerWG.Wait()
	close(w.deleteChan)

	// Wait for deleter to finish (it will flush remaining buffer on close).
	deleterWG.Wait()

	// We expect 3 total entries deleted; dedupe shouldn't drop unique receipt handles.
	deletedMu.Lock()
	defer deletedMu.Unlock()

	require.Len(t, deleted, 3)

	receiptHandles := make([]string, 0, len(deleted))
	for _, entry := range deleted {
		receiptHandles = append(receiptHandles, aws.ToString(entry.ReceiptHandle))
	}
	assert.ElementsMatch(t, []string{"r1", "r2", "r3"}, receiptHandles)
}

func TestWorker_RunWorker_MarkBestEffort_NoIDOrReceiptHandle(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)

	w := &Worker{
		client:            fc,
		queueURL:          "https://example.com/queue",
		deleteBatchSize:   1,
		deleteBatchWindow: 10 * time.Second,
		deleteChan:        make(chan deleteEntry, 10),
		jobs:              make(chan []types.Message, 1),
		handler: func(ctx context.Context, batch []Message) {
			for _, m := range batch {
				// These should be no-ops and not enqueue anything.
				m.Mark()
			}
		},
	}

	// Start deleter and worker
	var deleterWG sync.WaitGroup
	deleterWG.Go(func() {
		w.runBatchDeleter(t.Context(), w.deleteChan)
	})

	var workerWG sync.WaitGroup
	workerWG.Go(func() {
		w.runWorker(t.Context(), w.jobs)
	})

	// Missing MessageId
	w.jobs <- []types.Message{
		{ReceiptHandle: aws.String("r1")},
		{MessageId: aws.String("m2")}, // missing receipt
		{},                            // both missing
	}

	close(w.jobs)
	workerWG.Wait()
	close(w.deleteChan)
	deleterWG.Wait()

	// No deletes should have been issued.
	assert.Len(t, fc.getDeleteCalls(), 0)
}

func TestWorker_RunBatchDeleter_DedupesByReceiptHandleWithinFlushWindow(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)

	var (
		mu      sync.Mutex
		entries []types.DeleteMessageBatchRequestEntry
	)
	done := make(chan struct{}, 1)

	fc.onDelete = func(in *sqs.DeleteMessageBatchInput) {
		mu.Lock()
		entries = append(entries, in.Entries...)
		mu.Unlock()
		done <- struct{}{}
	}

	w := &Worker{
		client:            fc,
		queueURL:          "https://example.com/queue",
		deleteBatchSize:   10,               // won't flush by size
		deleteBatchWindow: 10 * time.Second, // avoid ticker flush
	}

	in := make(chan deleteEntry, 10)
	go w.runBatchDeleter(t.Context(), in)

	// Same receipt handle twice (should dedupe), plus a different one.
	in <- deleteEntry{id: "a", receiptHandle: "rh-1"}
	in <- deleteEntry{id: "a-dup", receiptHandle: "rh-1"}
	in <- deleteEntry{id: "b", receiptHandle: "rh-2"}

	// Close channel to force flush.
	close(in)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout waiting for delete flush")
	}

	// There should be exactly 2 entries flushed (rh-1 only once).
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, entries, 2)
	assert.Equal(t, aws.ToString(entries[0].Id), "0")
	assert.Equal(t, aws.ToString(entries[0].ReceiptHandle), "rh-1")
	assert.Equal(t, aws.ToString(entries[1].Id), "1")
	assert.Equal(t, aws.ToString(entries[1].ReceiptHandle), "rh-2")
}

func TestWorker_RunBatchDeleter_LogsPartialDeleteFailures(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)
	fc.setDeleteResult(
		&sqs.DeleteMessageBatchOutput{
			Failed: []types.BatchResultErrorEntry{
				{
					Id:          aws.String("0"),
					Code:        aws.String("ReceiptHandleIsInvalid"),
					Message:     aws.String("invalid receipt handle"),
					SenderFault: true,
				},
			},
		},
		nil,
	)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	w := &Worker{
		client:            fc,
		logger:            logger,
		queueURL:          "https://example.com/queue",
		deleteBatchSize:   10,
		deleteBatchWindow: 10 * time.Second,
	}

	in := make(chan deleteEntry, 1)
	deleterDone := make(chan struct{})

	go func() {
		defer close(deleterDone)
		w.runBatchDeleter(t.Context(), in)
	}()

	in <- deleteEntry{
		id:            "message-1",
		receiptHandle: "receipt-1",
	}
	close(in)

	select {
	case <-deleterDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for batch deleter")
	}

	output := logs.String()
	assert.Contains(t, output, "sqs delete batch entry failed")
	assert.Contains(t, output, "id=0")
	assert.Contains(t, output, "code=ReceiptHandleIsInvalid")
	assert.Contains(t, output, "invalid receipt handle")
	assert.Contains(t, output, "sender_fault=true")
}

func TestWorker_RunBatchDeleter_UsesUniqueIDsForRepeatedMessage(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)
	deleted := make(chan []types.DeleteMessageBatchRequestEntry, 1)

	fc.onDelete = func(in *sqs.DeleteMessageBatchInput) {
		deleted <- in.Entries
	}

	w := &Worker{
		client:            fc,
		queueURL:          "https://example.com/queue",
		deleteBatchSize:   10,
		deleteBatchWindow: 10 * time.Second,
	}

	in := make(chan deleteEntry, 2)
	deleterDone := make(chan struct{})

	go func() {
		defer close(deleterDone)
		w.runBatchDeleter(t.Context(), in)
	}()

	// SQS keeps the same MessageId when redelivering a message,
	// but each delivery has a different receipt handle.
	in <- deleteEntry{id: "same-message", receiptHandle: "receipt-1"}
	in <- deleteEntry{id: "same-message", receiptHandle: "receipt-2"}
	close(in)

	var entries []types.DeleteMessageBatchRequestEntry
	select {
	case entries = <-deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete batch")
	}

	<-deleterDone

	require.Len(t, entries, 2)

	assert.Equal(t, "receipt-1", aws.ToString(entries[0].ReceiptHandle))
	assert.Equal(t, "receipt-2", aws.ToString(entries[1].ReceiptHandle))

	assert.NotEqual(
		t,
		aws.ToString(entries[0].Id),
		aws.ToString(entries[1].Id),
		"delete entry IDs must be unique within one AWS batch",
	)
}

func TestWorker_Start_DrainsMarkedMessagesDuringShutdown(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	deleted := make(chan []types.DeleteMessageBatchRequestEntry, 1)

	fc.onDelete = func(in *sqs.DeleteMessageBatchInput) {
		deleted <- in.Entries
	}

	handler := func(ctx context.Context, batch []Message) {
		close(handlerStarted)

		// Simulate an active database operation during shutdown.
		<-releaseHandler

		batch[0].Mark()
	}

	w, err := New(
		handler,
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(1),
		WithDeleteBatchSize(10),
		WithDeleteBatchWindow(10*time.Second),
	)
	require.NoError(t, err)

	fc.enqueueReceive(
		&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					MessageId:     aws.String("message-1"),
					ReceiptHandle: aws.String("receipt-1"),
				},
			},
		},
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	startDone := make(chan error, 1)

	go func() {
		startDone <- w.Start(ctx)
	}()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	// Stop polling while the handler is still processing.
	cancel()

	// Start must wait for the active handler.
	select {
	case err := <-startDone:
		t.Fatalf("worker returned before handler finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	// The handler finishes successfully after shutdown has started.
	close(releaseHandler)

	var entries []types.DeleteMessageBatchRequestEntry
	select {
	case entries = <-deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for final delete")
	}

	require.Len(t, entries, 1)
	assert.Equal(t, "receipt-1", aws.ToString(entries[0].ReceiptHandle))

	select {
	case err := <-startDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker shutdown")
	}
}

func TestWorker_Start_ReceivesBatch_HandlerMarks_DeletesHappen(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)

	// We want to ensure Start() wires everything up:
	// poll -> jobs -> worker -> mark -> deleteChan -> batch deleter -> DeleteMessageBatch.
	deleted := make(chan []types.DeleteMessageBatchRequestEntry, 10)
	fc.onDelete = func(in *sqs.DeleteMessageBatchInput) {
		deleted <- in.Entries
	}

	handlerCalled := make(chan struct{}, 1)
	handler := func(ctx context.Context, batch []Message) {
		for _, m := range batch {
			m.Mark()
		}
		handlerCalled <- struct{}{}
	}

	w, err := New(
		handler,
		WithQueueURL("https://example.com/queue"),
		WithSQSClient(fc),
		WithWorkerCount(1),
		WithBatchSize(2),
		WithDeleteBatchSize(1), // delete immediately to make test deterministic
		WithDeleteBatchWindow(5*time.Second),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enqueue a receive with 2 messages, then force the next receive to terminate the poll loop.
	fc.enqueueReceive(&sqs.ReceiveMessageOutput{
		Messages: []types.Message{
			{MessageId: aws.String("a"), ReceiptHandle: aws.String("rh-1")},
			{MessageId: aws.String("b"), ReceiptHandle: aws.String("rh-2")},
		},
	}, nil)

	// After first receive, return a context cancellation error to stop promptly.
	// poll() will see ctx.Err() and return it.
	fc.enqueueReceive(nil, context.Canceled)

	startErrCh := make(chan error, 1)
	go func() { startErrCh <- w.Start(ctx) }()

	// Wait handler called.
	select {
	case <-handlerCalled:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for handler to be called")
	}

	// Wait for deletes (2 entries; since DeleteBatchSize=1 we expect two calls).
	var got []types.DeleteMessageBatchRequestEntry
	timeout := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case ents := <-deleted:
			got = append(got, ents...)
		case <-timeout:
			assert.Fail(t, "timed out waiting for deletes")
		}
	}

	// Stop worker. Cancel context and let Start() drain.
	cancel()
	err = <-startErrCh

	// Start() treats context cancellation/deadline as graceful shutdown.
	require.NoError(t, err)

	// Ensure the receipt handles we saw were deleted.
	require.Len(t, got, 2)
	assert.Equal(t, aws.ToString(got[0].Id), "0")
	assert.Equal(t, aws.ToString(got[0].ReceiptHandle), "rh-1")
	assert.Equal(t, aws.ToString(got[1].Id), "0")
	assert.Equal(t, aws.ToString(got[1].ReceiptHandle), "rh-2")
}

// Ensure we keep error wrapping semantics in poll for non-context errors.
func TestWorker_Poll_ReturnsWrappedError_OnReceiveError(t *testing.T) {
	t.Parallel()

	fc := newFakeSQS(t)
	fc.enqueueReceive(nil, errors.New("boom"))

	w := &Worker{
		client:              fc,
		queueURL:            "https://example.com/queue",
		waitTimeSeconds:     0,
		maxReceiveBatchSize: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := w.poll(ctx, make(chan []types.Message, 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sqs worker: receive message:")
}

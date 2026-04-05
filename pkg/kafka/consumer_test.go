package kafka

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ---------------------------------------------------------------------------
// fakeKafka — test double for consumerClient
// ---------------------------------------------------------------------------

type fakeKafka struct {
	t *testing.T

	mu sync.Mutex

	pollQueue chan kgo.Fetches

	markedRecords []*kgo.Record
	commitCalls   int
	closed        bool

	// Optional hooks
	onMark   func(records []*kgo.Record)
	onCommit func()
}

func newFakeKafka(t *testing.T) *fakeKafka {
	t.Helper()
	return &fakeKafka{
		t:         t,
		pollQueue: make(chan kgo.Fetches, 100),
	}
}

func (f *fakeKafka) PollRecords(ctx context.Context, _ int) kgo.Fetches {
	select {
	case <-ctx.Done():
		return nil
	case fetches := <-f.pollQueue:
		return fetches
	}
}

func (f *fakeKafka) MarkCommitRecords(records ...*kgo.Record) {
	f.mu.Lock()
	f.markedRecords = append(f.markedRecords, records...)
	cb := f.onMark
	f.mu.Unlock()

	if cb != nil {
		cb(records)
	}
}

func (f *fakeKafka) CommitMarkedOffsets(_ context.Context) error {
	f.mu.Lock()
	f.commitCalls++
	cb := f.onCommit
	f.mu.Unlock()

	if cb != nil {
		cb()
	}
	return nil
}

func (f *fakeKafka) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

func (f *fakeKafka) getMarkedRecords() []*kgo.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*kgo.Record, len(f.markedRecords))
	copy(cp, f.markedRecords)
	return cp
}

func (f *fakeKafka) getCommitCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commitCalls
}

func (f *fakeKafka) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeKafka) enqueueFetches(fetches kgo.Fetches) {
	f.pollQueue <- fetches
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeFetches builds a kgo.Fetches containing a single topic+partition batch.
func makeFetches(topic string, partition int32, records ...*kgo.Record) kgo.Fetches {
	for _, r := range records {
		r.Topic = topic
		r.Partition = partition
	}
	return kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic: topic,
			Partitions: []kgo.FetchPartition{{
				Partition: partition,
				Records:   records,
			}},
		}},
	}}
}

// makeMultiPartitionFetches builds fetches spanning multiple partitions in one poll.
func makeMultiPartitionFetches(topic string, batches map[int32][]*kgo.Record) kgo.Fetches {
	var partitions []kgo.FetchPartition
	for p, records := range batches {
		for _, r := range records {
			r.Topic = topic
			r.Partition = p
		}
		partitions = append(partitions, kgo.FetchPartition{
			Partition: p,
			Records:   records,
		})
	}
	return kgo.Fetches{{
		Topics: []kgo.FetchTopic{{
			Topic:      topic,
			Partitions: partitions,
		}},
	}}
}

// newTestConsumer creates a Consumer wired to the given fakeKafka.
func newTestConsumer(t *testing.T, handler HandlerFunc, fc *fakeKafka) *Consumer {
	t.Helper()

	c, err := NewConsumer(
		handler,
		WithConsumerBrokers([]string{"localhost:9092"}),
		WithConsumerGroupID("test-group"),
		WithConsumerTopics([]string{"test-topic"}),
		WithConsumerClient(fc),
		WithConsumerRevocationTimeout(2*time.Second),
	)
	require.NoError(t, err)
	return c
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestConsumer_PerPartitionDispatch(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)

	var mu sync.Mutex
	dispatched := map[int32][]int64{} // partition -> offsets

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {
		mu.Lock()
		defer mu.Unlock()
		for _, r := range records {
			dispatched[partition] = append(dispatched[partition], r.Offset())
			r.Mark()
		}
	}

	c := newTestConsumer(t, handler, fc)

	// Simulate partition assignment.
	c.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"test-topic": {0, 1, 2},
	})

	// Enqueue fetches with records for different partitions.
	fc.enqueueFetches(makeMultiPartitionFetches("test-topic", map[int32][]*kgo.Record{
		0: {{Offset: 10}, {Offset: 11}},
		1: {{Offset: 20}},
		2: {{Offset: 30}, {Offset: 31}, {Offset: 32}},
	}))

	ctx, cancel := context.WithCancel(context.Background())

	startDone := make(chan error, 1)
	go func() { startDone <- c.Start(ctx) }()

	// Wait for records to be processed.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(dispatched[0]) == 2 && len(dispatched[1]) == 1 && len(dispatched[2]) == 3
	}, 2*time.Second, 10*time.Millisecond)

	// Verify partitions got the correct offsets.
	mu.Lock()
	assert.Equal(t, []int64{10, 11}, dispatched[0])
	assert.Equal(t, []int64{20}, dispatched[1])
	assert.Equal(t, []int64{30, 31, 32}, dispatched[2])
	mu.Unlock()

	// Verify Mark() called MarkCommitRecords.
	marked := fc.getMarkedRecords()
	assert.Len(t, marked, 6)

	cancel()
	err := <-startDone
	require.NoError(t, err)
	assert.True(t, fc.isClosed())
}

func TestConsumer_MarkPerRecord(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)

	markDone := make(chan struct{}, 1)

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {
		// Only mark even offsets — simulates partial success.
		for _, r := range records {
			if r.Offset()%2 == 0 {
				r.Mark()
			}
		}
		select {
		case markDone <- struct{}{}:
		default:
		}
	}

	c := newTestConsumer(t, handler, fc)

	c.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"test-topic": {0},
	})

	fc.enqueueFetches(makeFetches("test-topic", 0,
		&kgo.Record{Offset: 10},
		&kgo.Record{Offset: 11},
		&kgo.Record{Offset: 12},
		&kgo.Record{Offset: 13},
	))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go c.Start(ctx) //nolint:errcheck

	select {
	case <-markDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler")
	}

	// Only offsets 10 and 12 should be marked.
	marked := fc.getMarkedRecords()
	require.Len(t, marked, 2)
	assert.Equal(t, int64(10), marked[0].Offset)
	assert.Equal(t, int64(12), marked[1].Offset)

	cancel()
}

func TestConsumer_GracefulShutdown_DrainsPartitions(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)

	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {
		close(handlerStarted)
		// Simulate work that takes a moment.
		time.Sleep(100 * time.Millisecond)
		for _, r := range records {
			r.Mark()
		}
		close(handlerDone)
	}

	c := newTestConsumer(t, handler, fc)

	c.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"test-topic": {0},
	})

	fc.enqueueFetches(makeFetches("test-topic", 0,
		&kgo.Record{Offset: 1},
	))

	ctx, cancel := context.WithCancel(context.Background())

	startDone := make(chan error, 1)
	go func() { startDone <- c.Start(ctx) }()

	// Wait for handler to start processing.
	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	// Cancel context — should trigger graceful shutdown.
	cancel()

	err := <-startDone
	require.NoError(t, err)

	// Handler should have completed (drain).
	select {
	case <-handlerDone:
	default:
		t.Fatal("handler was not drained before shutdown completed")
	}

	// Record should have been marked.
	marked := fc.getMarkedRecords()
	require.Len(t, marked, 1)
	assert.Equal(t, int64(1), marked[0].Offset)

	assert.True(t, fc.isClosed())
}

func TestConsumer_Revocation_DrainsAndCommits(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)

	handlerCalled := make(chan struct{}, 1)

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {
		for _, r := range records {
			r.Mark()
		}
		select {
		case handlerCalled <- struct{}{}:
		default:
		}
	}

	c := newTestConsumer(t, handler, fc)

	// Assign P0 and P1.
	c.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"test-topic": {0, 1},
	})

	// Send records to P0.
	c.mu.Lock()
	pw := c.partitions[topicPartition{"test-topic", 0}]
	c.mu.Unlock()

	pw.ch <- []*kgo.Record{{Topic: "test-topic", Partition: 0, Offset: 5}}

	select {
	case <-handlerCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler")
	}

	// Revoke P0 — this is what the rebalance callback does.
	// We pass the fakeKafka as a stand-in; onPartitionsRevoked calls cl.CommitMarkedOffsets.
	// Since we can't pass a real *kgo.Client, test drainPartitions directly.
	c.drainPartitions(map[string][]int32{"test-topic": {0}})

	// P0 should be removed from the map, P1 should remain.
	c.mu.Lock()
	_, p0Exists := c.partitions[topicPartition{"test-topic", 0}]
	_, p1Exists := c.partitions[topicPartition{"test-topic", 1}]
	c.mu.Unlock()

	assert.False(t, p0Exists, "P0 should be removed after revocation")
	assert.True(t, p1Exists, "P1 should still be assigned")

	// Record should have been marked.
	marked := fc.getMarkedRecords()
	require.Len(t, marked, 1)
	assert.Equal(t, int64(5), marked[0].Offset)

	// Clean up P1.
	c.drainPartitions(map[string][]int32{"test-topic": {1}})
}

func TestConsumer_Revocation_ZombieTimeout(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)

	handlerStarted := make(chan struct{})

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {
		close(handlerStarted)
		// Simulate a stuck handler that ignores context.
		select {
		case <-ctx.Done():
			// Handler eventually respects cancellation.
		case <-time.After(30 * time.Second):
		}
	}

	c, err := NewConsumer(
		handler,
		WithConsumerBrokers([]string{"localhost:9092"}),
		WithConsumerGroupID("test-group"),
		WithConsumerTopics([]string{"test-topic"}),
		WithConsumerClient(fc),
		WithConsumerRevocationTimeout(200*time.Millisecond), // short timeout for test
	)
	require.NoError(t, err)

	c.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"test-topic": {0},
	})

	// Send a record to the partition goroutine.
	c.mu.Lock()
	pw := c.partitions[topicPartition{"test-topic", 0}]
	c.mu.Unlock()

	pw.ch <- []*kgo.Record{{Topic: "test-topic", Partition: 0, Offset: 1}}

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	// Drain should return after revocationTimeout, not block forever.
	start := time.Now()
	c.drainPartitions(map[string][]int32{"test-topic": {0}})
	elapsed := time.Since(start)

	// Should have taken roughly revocationTimeout (200ms), not 30s.
	assert.Less(t, elapsed, 2*time.Second, "drainPartitions should not block longer than revocationTimeout")

	// Partition should be removed from the map.
	c.mu.Lock()
	_, exists := c.partitions[topicPartition{"test-topic", 0}]
	c.mu.Unlock()
	assert.False(t, exists)
}

func TestConsumer_OnPartitionsLost_CancelsImmediately(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)

	handlerStarted := make(chan struct{})
	handlerCtxDone := make(chan struct{})

	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {
		close(handlerStarted)
		<-ctx.Done()
		close(handlerCtxDone)
	}

	c := newTestConsumer(t, handler, fc)

	c.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"test-topic": {0},
	})

	// Send a record to get the handler blocked.
	c.mu.Lock()
	pw := c.partitions[topicPartition{"test-topic", 0}]
	c.mu.Unlock()

	pw.ch <- []*kgo.Record{{Topic: "test-topic", Partition: 0, Offset: 1}}

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	// Lost should cancel the context and return immediately (no drain wait).
	c.onPartitionsLost(context.Background(), nil, map[string][]int32{"test-topic": {0}})

	// Handler's context should have been cancelled.
	select {
	case <-handlerCtxDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handler context cancellation")
	}

	// Partition should be removed.
	c.mu.Lock()
	_, exists := c.partitions[topicPartition{"test-topic", 0}]
	c.mu.Unlock()
	assert.False(t, exists)
}

func TestConsumer_RecordAccessors(t *testing.T) {
	t.Parallel()

	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	raw := &kgo.Record{
		Key:       []byte("key"),
		Value:     []byte("value"),
		Topic:     "my-topic",
		Partition: 3,
		Offset:    42,
		Timestamp: ts,
		Headers: []kgo.RecordHeader{
			{Key: "h1", Value: []byte("v1")},
		},
	}

	var marked bool
	r := ConsumerRecord{
		raw:  raw,
		mark: func(_ *kgo.Record) { marked = true },
	}

	assert.Equal(t, []byte("key"), r.Key())
	assert.Equal(t, []byte("value"), r.Value())
	assert.Equal(t, "my-topic", r.Topic())
	assert.Equal(t, int32(3), r.Partition())
	assert.Equal(t, int64(42), r.Offset())
	assert.Equal(t, ts, r.Timestamp())
	require.Len(t, r.Headers(), 1)
	assert.Equal(t, "h1", r.Headers()[0].Key)
	assert.Equal(t, raw, r.Raw())

	r.Mark()
	assert.True(t, marked)
}

func TestConsumerRecord_Mark_NilMarkFunc(t *testing.T) {
	t.Parallel()

	r := ConsumerRecord{raw: &kgo.Record{}}
	// Should not panic.
	r.Mark()
}

func TestConsumer_Start_ContextCancelled_ReturnsNil(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)
	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {}

	c := newTestConsumer(t, handler, fc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := c.Start(ctx)
	require.NoError(t, err, "context cancellation should be treated as graceful shutdown")
	assert.True(t, fc.isClosed())
}

func TestConsumer_UntrackedPartition_DoesNotPanic(t *testing.T) {
	t.Parallel()

	fc := newFakeKafka(t)
	handler := func(ctx context.Context, topic string, partition int32, records []ConsumerRecord) {}
	c := newTestConsumer(t, handler, fc)

	// Don't assign any partitions. Enqueue fetches for a partition we don't own.
	fc.enqueueFetches(makeFetches("test-topic", 99, &kgo.Record{Offset: 1}))

	ctx, cancel := context.WithCancel(context.Background())

	startDone := make(chan error, 1)
	go func() { startDone <- c.Start(ctx) }()

	// Give poll loop time to process the fetch.
	time.Sleep(100 * time.Millisecond)

	cancel()
	err := <-startDone
	require.NoError(t, err)
}

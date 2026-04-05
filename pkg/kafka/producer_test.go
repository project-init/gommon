package kafka

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ---------------------------------------------------------------------------
// fakeProducer — test double for producerClient
// ---------------------------------------------------------------------------

type fakeProducer struct {
	mu sync.Mutex

	produced []*kgo.Record
	closed   bool

	// If set, ProduceSync returns this error for every record.
	err error
}

func (f *fakeProducer) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	f.mu.Lock()
	defer f.mu.Unlock()

	var results kgo.ProduceResults
	for _, r := range rs {
		f.produced = append(f.produced, r)
		results = append(results, kgo.ProduceResult{Record: r, Err: f.err})
	}
	return results
}

func (f *fakeProducer) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}

func (f *fakeProducer) getProduced() []*kgo.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*kgo.Record, len(f.produced))
	copy(cp, f.produced)
	return cp
}

func (f *fakeProducer) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestProducer_Produce_SingleRecord(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	rec := NewProducerRecord("test-topic", []byte("key"), []byte("value"))

	err = p.Produce(context.Background(), rec)
	require.NoError(t, err)

	produced := fp.getProduced()
	require.Len(t, produced, 1)
	assert.Equal(t, "test-topic", produced[0].Topic)
	assert.Equal(t, []byte("key"), produced[0].Key)
	assert.Equal(t, []byte("value"), produced[0].Value)
	assert.Equal(t, int32(-1), produced[0].Partition)
}

func TestProducer_Produce_MultipleRecords(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	records := []*ProducerRecord{
		NewProducerRecord("topic-a", []byte("k1"), []byte("v1")),
		NewProducerRecord("topic-b", []byte("k2"), []byte("v2")),
		NewProducerRecord("topic-a", []byte("k3"), []byte("v3")),
	}

	err = p.Produce(context.Background(), records...)
	require.NoError(t, err)

	produced := fp.getProduced()
	require.Len(t, produced, 3)
	assert.Equal(t, "topic-a", produced[0].Topic)
	assert.Equal(t, "topic-b", produced[1].Topic)
	assert.Equal(t, "topic-a", produced[2].Topic)
}

func TestProducer_Produce_WithHeaders(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	rec := NewProducerRecord("test-topic", []byte("key"), []byte("value")).
		WithHeader("trace-id", []byte("abc123")).
		WithHeader("content-type", []byte("application/json"))

	err = p.Produce(context.Background(), rec)
	require.NoError(t, err)

	produced := fp.getProduced()
	require.Len(t, produced, 1)
	require.Len(t, produced[0].Headers, 2)
	assert.Equal(t, "trace-id", produced[0].Headers[0].Key)
	assert.Equal(t, []byte("abc123"), produced[0].Headers[0].Value)
	assert.Equal(t, "content-type", produced[0].Headers[1].Key)
	assert.Equal(t, []byte("application/json"), produced[0].Headers[1].Value)
}

func TestProducer_Produce_WithPartition(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	rec := NewProducerRecord("test-topic", []byte("key"), []byte("value")).
		WithPartition(7)

	err = p.Produce(context.Background(), rec)
	require.NoError(t, err)

	produced := fp.getProduced()
	require.Len(t, produced, 1)
	assert.Equal(t, int32(7), produced[0].Partition)
}

func TestProducer_Produce_EmptyRecords(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	// No records — should be a no-op.
	err = p.Produce(context.Background())
	require.NoError(t, err)
	assert.Empty(t, fp.getProduced())
}

func TestProducer_Produce_Error(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{err: fmt.Errorf("broker unavailable")}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	rec := NewProducerRecord("test-topic", []byte("key"), []byte("value"))

	err = p.Produce(context.Background(), rec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker unavailable")
	assert.Contains(t, err.Error(), "kafka producer")
}

func TestProducer_Produce_PartialError(t *testing.T) {
	t.Parallel()

	// Fake that alternates success/failure per record.
	fp := &fakeProducerAlternating{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	records := []*ProducerRecord{
		NewProducerRecord("topic-a", []byte("k1"), []byte("v1")),
		NewProducerRecord("topic-b", []byte("k2"), []byte("v2")),
		NewProducerRecord("topic-a", []byte("k3"), []byte("v3")),
	}

	err = p.Produce(context.Background(), records...)
	require.Error(t, err)
	// Should contain the error for the second record.
	assert.Contains(t, err.Error(), "topic-b")
}

func TestProducer_Close(t *testing.T) {
	t.Parallel()

	fp := &fakeProducer{}
	p, err := NewProducer(
		WithProducerBrokers([]string{"localhost:9092"}),
		WithProducerClient(fp),
	)
	require.NoError(t, err)

	p.Close()
	assert.True(t, fp.isClosed())
}

func TestProducerRecord_Builder(t *testing.T) {
	t.Parallel()

	rec := NewProducerRecord("my-topic", []byte("k"), []byte("v")).
		WithHeader("h1", []byte("val1")).
		WithHeader("h2", []byte("val2")).
		WithPartition(3)

	assert.Equal(t, "my-topic", rec.raw.Topic)
	assert.Equal(t, []byte("k"), rec.raw.Key)
	assert.Equal(t, []byte("v"), rec.raw.Value)
	assert.Equal(t, int32(3), rec.raw.Partition)
	require.Len(t, rec.raw.Headers, 2)
	assert.Equal(t, "h1", rec.raw.Headers[0].Key)
	assert.Equal(t, "h2", rec.raw.Headers[1].Key)
}

func TestProducerRecord_DefaultPartition(t *testing.T) {
	t.Parallel()

	rec := NewProducerRecord("my-topic", []byte("k"), []byte("v"))
	assert.Equal(t, int32(-1), rec.raw.Partition, "default partition should be -1")
}

func TestProducerRecord_NoHeaders(t *testing.T) {
	t.Parallel()

	rec := NewProducerRecord("my-topic", nil, []byte("v"))
	assert.Nil(t, rec.raw.Headers)
}

// ---------------------------------------------------------------------------
// fakeProducerAlternating — fails every other record
// ---------------------------------------------------------------------------

type fakeProducerAlternating struct {
	mu sync.Mutex
	n  int
}

func (f *fakeProducerAlternating) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	f.mu.Lock()
	defer f.mu.Unlock()

	var results kgo.ProduceResults
	for _, r := range rs {
		var err error
		if f.n%2 == 1 {
			err = fmt.Errorf("simulated failure")
		}
		results = append(results, kgo.ProduceResult{Record: r, Err: err})
		f.n++
	}
	return results
}

func (f *fakeProducerAlternating) Close() {}

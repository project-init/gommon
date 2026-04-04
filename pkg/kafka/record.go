package kafka

import (
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// RecordHeader is a key-value pair attached to a Kafka record.
// This is our own type so callers never need to import kgo.
type RecordHeader struct {
	Key   string
	Value []byte
}

// ---------------------------------------------------------------------------
// Record — consumer-side wrapper
// ---------------------------------------------------------------------------

// Record wraps a franz-go record and provides a Mark() API for offset commit control.
type Record struct {
	raw  *kgo.Record
	mark func(*kgo.Record)
}

// Raw returns the underlying franz-go record.
// This keeps the wrapper non-leaky while still allowing full access when needed.
func (r Record) Raw() *kgo.Record { return r.raw }

// Mark signals that this record has been successfully processed and its offset
// should be committed. It is a no-op if the mark function is not configured.
//
// Only marked records have their offsets committed. If your handler does not call
// Mark(), the record's offset will not advance and the record will be redelivered.
func (r Record) Mark() {
	if r.mark == nil {
		return
	}
	r.mark(r.raw)
}

// Key returns the record key.
func (r Record) Key() []byte { return r.raw.Key }

// Value returns the record value.
func (r Record) Value() []byte { return r.raw.Value }

// Topic returns the topic this record was consumed from.
func (r Record) Topic() string { return r.raw.Topic }

// Partition returns the partition this record was consumed from.
func (r Record) Partition() int32 { return r.raw.Partition }

// Offset returns the offset of this record within its partition.
func (r Record) Offset() int64 { return r.raw.Offset }

// Headers returns the record headers converted to our RecordHeader type.
func (r Record) Headers() []RecordHeader {
	if len(r.raw.Headers) == 0 {
		return nil
	}
	out := make([]RecordHeader, len(r.raw.Headers))
	for i, h := range r.raw.Headers {
		out[i] = RecordHeader{Key: h.Key, Value: h.Value}
	}
	return out
}

// Timestamp returns the record timestamp.
func (r Record) Timestamp() time.Time { return r.raw.Timestamp }

// ---------------------------------------------------------------------------
// ProduceRecord — producer-side record with builder methods
// ---------------------------------------------------------------------------

// ProduceRecord represents a record to be sent to Kafka.
// Use NewProduceRecord to create one, then chain builder methods.
type ProduceRecord struct {
	Topic     string
	Key       []byte
	Value     []byte
	Headers   []RecordHeader
	Partition int32 // -1 = use default partitioner
}

// NewProduceRecord creates a ProduceRecord for the given topic.
// Partition defaults to -1 (use the client's default partitioner).
func NewProduceRecord(topic string, key, value []byte) *ProduceRecord {
	return &ProduceRecord{
		Topic:     topic,
		Key:       key,
		Value:     value,
		Partition: -1,
	}
}

// WithHeader appends a header to the record and returns the record for chaining.
func (r *ProduceRecord) WithHeader(key string, value []byte) *ProduceRecord {
	r.Headers = append(r.Headers, RecordHeader{Key: key, Value: value})
	return r
}

// WithPartition sets an explicit partition and returns the record for chaining.
// Use -1 to revert to the default partitioner.
func (r *ProduceRecord) WithPartition(p int32) *ProduceRecord {
	r.Partition = p
	return r
}

// toKgo converts a ProduceRecord to a franz-go *kgo.Record.
func (r *ProduceRecord) toKgo() *kgo.Record {
	rec := &kgo.Record{
		Topic:     r.Topic,
		Key:       r.Key,
		Value:     r.Value,
		Partition: r.Partition,
	}
	if len(r.Headers) > 0 {
		rec.Headers = make([]kgo.RecordHeader, len(r.Headers))
		for i, h := range r.Headers {
			rec.Headers[i] = kgo.RecordHeader{Key: h.Key, Value: h.Value}
		}
	}
	return rec
}

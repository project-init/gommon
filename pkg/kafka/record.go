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
// ConsumerRecord — consumer-side wrapper
// ---------------------------------------------------------------------------

// ConsumerRecord wraps a franz-go record and provides a Mark() API for offset commit control.
type ConsumerRecord struct {
	raw  *kgo.Record
	mark func(*kgo.Record)
}

// Raw returns the underlying franz-go record.
// This keeps the wrapper non-leaky while still allowing full access when needed.
func (r ConsumerRecord) Raw() *kgo.Record { return r.raw }

// Mark signals that this record has been successfully processed and its offset
// should be committed. It is a no-op if the mark function is not configured.
//
// Only marked records have their offsets committed. If your handler does not call
// Mark(), the record's offset will not advance and the record will be redelivered.
func (r ConsumerRecord) Mark() {
	if r.mark == nil {
		return
	}
	r.mark(r.raw)
}

// Key returns the record key.
func (r ConsumerRecord) Key() []byte { return r.raw.Key }

// Value returns the record value.
func (r ConsumerRecord) Value() []byte { return r.raw.Value }

// Topic returns the topic this record was consumed from.
func (r ConsumerRecord) Topic() string { return r.raw.Topic }

// Partition returns the partition this record was consumed from.
func (r ConsumerRecord) Partition() int32 { return r.raw.Partition }

// Offset returns the offset of this record within its partition.
func (r ConsumerRecord) Offset() int64 { return r.raw.Offset }

// Headers returns the record headers converted to our RecordHeader type.
func (r ConsumerRecord) Headers() []RecordHeader {
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
func (r ConsumerRecord) Timestamp() time.Time { return r.raw.Timestamp }

// ---------------------------------------------------------------------------
// ProducerRecord — producer-side record with builder methods
// ---------------------------------------------------------------------------

// ProducerRecord represents a record to be sent to Kafka.
// Use NewProducerRecord to create one, then chain builder methods.
type ProducerRecord struct {
	raw *kgo.Record
}

// NewProducerRecord creates a ProducerRecord for the given topic.
// Partition defaults to -1 (use the client's default partitioner).
func NewProducerRecord(topic string, key, value []byte) *ProducerRecord {
	return &ProducerRecord{
		raw: &kgo.Record{
			Topic:     topic,
			Key:       key,
			Value:     value,
			Partition: -1,
		},
	}
}

// WithHeader appends a header to the record and returns the record for chaining.
func (r *ProducerRecord) WithHeader(key string, value []byte) *ProducerRecord {
	r.raw.Headers = append(r.raw.Headers, kgo.RecordHeader{Key: key, Value: value})
	return r
}

// WithPartition sets an explicit partition and returns the record for chaining.
// Use -1 to revert to the default partitioner.
func (r *ProducerRecord) WithPartition(p int32) *ProducerRecord {
	r.raw.Partition = p
	return r
}

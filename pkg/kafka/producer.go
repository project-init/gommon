package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"

	"github.com/twmb/franz-go/pkg/kgo"
)

// producerClient is the subset of *kgo.Client methods used by Producer.
// Unexported to allow test fakes.
type producerClient interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
	Close()
}

// Producer sends records to Kafka synchronously.
type Producer struct {
	client producerClient
	logger *slog.Logger
}

// NewProducer constructs a Producer using DefaultProducerOptions(), applies any
// variadic options, then validates.
func NewProducer(optFns ...ProducerOption) (*Producer, error) {
	opts := DefaultProducerOptions()
	for _, fn := range optFns {
		if fn == nil {
			continue
		}
		if err := fn(&opts); err != nil {
			return nil, err
		}
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	p := &Producer{
		client: opts.Client,
		logger: opts.Logger,
	}

	if p.logger == nil {
		p.logger = slog.Default()
	}

	// Build a real franz-go client if none was injected.
	if p.client == nil {
		cl, err := buildProducerClient(opts)
		if err != nil {
			return nil, fmt.Errorf("kafka producer: create client: %w", err)
		}
		p.client = cl
	}

	return p, nil
}

func buildProducerClient(opts ProducerOptions) (*kgo.Client, error) {
	kgoOpts := []kgo.Opt{
		kgo.SeedBrokers(opts.Brokers...),

		// Idempotent production is enabled by default in franz-go.
		// We only configure delivery timeout and retry limits.
		kgo.RecordDeliveryTimeout(opts.DeliveryTimeout),
		kgo.RecordRetries(opts.MaxRetries),
	}

	if opts.SASLMechanism != nil {
		kgoOpts = append(kgoOpts, kgo.SASL(opts.SASLMechanism))
	}

	if opts.TLSConfig != nil {
		kgoOpts = append(kgoOpts, kgo.DialTLSConfig(opts.TLSConfig))
	} else if opts.SASLMechanism != nil {
		// If no TLS config but SASL is set, use a default TLS config
		// (most SASL brokers require TLS).
		kgoOpts = append(kgoOpts, kgo.DialTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
		}))
	}

	return kgo.NewClient(kgoOpts...)
}

// Produce sends one or more records to Kafka synchronously.
// It returns the first error encountered across all records, if any.
func (p *Producer) Produce(ctx context.Context, records ...*ProduceRecord) error {
	if len(records) == 0 {
		return nil
	}

	kgoRecords := make([]*kgo.Record, len(records))
	for i, r := range records {
		kgoRecords[i] = r.toKgo()
	}

	results := p.client.ProduceSync(ctx, kgoRecords...)

	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("kafka producer: topic=%s partition=%d: %w",
				r.Record.Topic, r.Record.Partition, r.Err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Close shuts down the underlying Kafka client, flushing any buffered records.
func (p *Producer) Close() {
	p.client.Close()
}

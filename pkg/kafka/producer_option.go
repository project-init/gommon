package kafka

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/sasl"
)

// ProducerOptions holds all configurable settings for Producer construction.
type ProducerOptions struct {
	// Required
	Brokers []string

	// Optional. If nil, NewProducer() creates a real franz-go client.
	Client producerClient

	// Optional. If nil, slog.Default() is used.
	Logger *slog.Logger

	// DeliveryTimeout is the total time allowed for a record to be delivered,
	// including all retries. Default: 30s.
	DeliveryTimeout time.Duration

	// MaxRetries caps the number of produce retries per record.
	// Default: 5. franz-go defaults to unlimited; we cap it for safety.
	MaxRetries int

	// SASL / TLS
	SASLMechanism sasl.Mechanism
	TLSConfig     *tls.Config
}

// DefaultProducerOptions returns ProducerOptions with sane defaults.
// Brokers must still be set before validation passes.
func DefaultProducerOptions() ProducerOptions {
	return ProducerOptions{
		Logger:          slog.Default(),
		DeliveryTimeout: 30 * time.Second,
		MaxRetries:      5,
	}
}

// Validate checks ProducerOptions for correctness.
func (o ProducerOptions) Validate() error {
	if len(o.Brokers) == 0 {
		return fmt.Errorf("kafka producer: options.Brokers is required")
	}
	if o.DeliveryTimeout <= 0 {
		return fmt.Errorf("kafka producer: options.DeliveryTimeout must be > 0 (got %s)", o.DeliveryTimeout)
	}
	if o.MaxRetries < 0 {
		return fmt.Errorf("kafka producer: options.MaxRetries must be >= 0 (got %d)", o.MaxRetries)
	}
	return nil
}

// ProducerOption mutates ProducerOptions (functional options pattern).
type ProducerOption func(*ProducerOptions) error

func WithProducerBrokers(brokers []string) ProducerOption {
	return func(o *ProducerOptions) error {
		o.Brokers = brokers
		return nil
	}
}

func WithProducerLogger(l *slog.Logger) ProducerOption {
	return func(o *ProducerOptions) error {
		o.Logger = l
		return nil
	}
}

func WithProducerDeliveryTimeout(d time.Duration) ProducerOption {
	return func(o *ProducerOptions) error {
		o.DeliveryTimeout = d
		return nil
	}
}

func WithProducerMaxRetries(n int) ProducerOption {
	return func(o *ProducerOptions) error {
		o.MaxRetries = n
		return nil
	}
}

func WithProducerSASLMechanism(m sasl.Mechanism) ProducerOption {
	return func(o *ProducerOptions) error {
		o.SASLMechanism = m
		return nil
	}
}

func WithProducerTLSConfig(c *tls.Config) ProducerOption {
	return func(o *ProducerOptions) error {
		o.TLSConfig = c
		return nil
	}
}

func WithProducerClient(c producerClient) ProducerOption {
	return func(o *ProducerOptions) error {
		o.Client = c
		return nil
	}
}

package kafka

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/sasl"
)

// ConsumerOptions holds all configurable settings for Consumer construction.
type ConsumerOptions struct {
	// Required
	Brokers []string
	GroupID string
	Topics  []string

	// Optional. If nil, New() creates a real franz-go client.
	Client consumerClient

	// Optional. If nil, slog.Default() is used.
	Logger *slog.Logger

	// MaxPollRecords caps records returned per PollRecords call.
	MaxPollRecords int

	// SessionTimeout controls how long the broker waits for heartbeats
	// before considering this consumer dead.
	SessionTimeout time.Duration

	// RevocationTimeout is the maximum time to wait for a partition's
	// in-flight handler to finish during rebalance before abandoning it.
	RevocationTimeout time.Duration

	// SASL / TLS
	SASLMechanism sasl.Mechanism
	TLSConfig     *tls.Config
}

// DefaultConsumerOptions returns ConsumerOptions with sane defaults.
// Brokers, GroupID, and Topics must still be set before validation passes.
func DefaultConsumerOptions() ConsumerOptions {
	return ConsumerOptions{
		Logger:            slog.Default(),
		MaxPollRecords:    100,
		SessionTimeout:    45 * time.Second,
		RevocationTimeout: 10 * time.Second,
	}
}

// Validate checks ConsumerOptions for correctness.
func (o ConsumerOptions) Validate() error {
	if len(o.Brokers) == 0 {
		return fmt.Errorf("kafka consumer: options.Brokers is required")
	}
	if o.GroupID == "" {
		return fmt.Errorf("kafka consumer: options.GroupID is required")
	}
	if len(o.Topics) == 0 {
		return fmt.Errorf("kafka consumer: options.Topics is required")
	}
	if o.MaxPollRecords <= 0 {
		return fmt.Errorf("kafka consumer: options.MaxPollRecords must be > 0 (got %d)", o.MaxPollRecords)
	}
	if o.SessionTimeout <= 0 {
		return fmt.Errorf("kafka consumer: options.SessionTimeout must be > 0 (got %s)", o.SessionTimeout)
	}
	if o.RevocationTimeout <= 0 {
		return fmt.Errorf("kafka consumer: options.RevocationTimeout must be > 0 (got %s)", o.RevocationTimeout)
	}
	if o.RevocationTimeout >= o.SessionTimeout {
		return fmt.Errorf("kafka consumer: options.RevocationTimeout (%s) must be less than SessionTimeout (%s)", o.RevocationTimeout, o.SessionTimeout)
	}
	return nil
}

// ConsumerOption mutates ConsumerOptions (functional options pattern).
type ConsumerOption func(*ConsumerOptions) error

func WithConsumerBrokers(brokers []string) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.Brokers = brokers
		return nil
	}
}

func WithConsumerGroupID(id string) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.GroupID = id
		return nil
	}
}

func WithConsumerTopics(topics []string) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.Topics = topics
		return nil
	}
}

func WithConsumerLogger(l *slog.Logger) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.Logger = l
		return nil
	}
}

func WithConsumerMaxPollRecords(n int) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.MaxPollRecords = n
		return nil
	}
}

func WithConsumerSessionTimeout(d time.Duration) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.SessionTimeout = d
		return nil
	}
}

func WithConsumerRevocationTimeout(d time.Duration) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.RevocationTimeout = d
		return nil
	}
}

func WithConsumerSASLMechanism(m sasl.Mechanism) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.SASLMechanism = m
		return nil
	}
}

func WithConsumerTLSConfig(c *tls.Config) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.TLSConfig = c
		return nil
	}
}

func WithConsumerClient(c consumerClient) ConsumerOption {
	return func(o *ConsumerOptions) error {
		o.Client = c
		return nil
	}
}

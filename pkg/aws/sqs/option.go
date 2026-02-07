package sqs

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// Options holds all configurable settings for Worker construction.
type Options struct {
	// Required
	QueueURL string

	// Optional. If nil, New() should create a client using gommon/pkg/aws GetConfig().
	Client *sqs.Client

	// Concurrency
	WorkerCount int

	// Batching
	// BatchSize controls how many messages are passed to your handler per job.
	// Note: SQS ReceiveMessage MaxNumberOfMessages is <= 10, and the worker currently
	// maps receive batch == handler batch, so BatchSize must be 1..10.
	BatchSize int

	// ReceiveMessage tuning
	// WaitTimeSeconds controls SQS long-polling (0..20).
	WaitTimeSeconds int32

	// VisibilityTimeoutSeconds overrides the per-receive visibility timeout.
	// If nil, the queue default visibility timeout applies.
	VisibilityTimeoutSeconds *int32

	// Delete batching
	// DeleteBatchSize controls max entries per DeleteMessageBatch call (1..10).
	DeleteBatchSize int
	// DeleteBatchWindow controls periodic flushing even if the delete batch isn't full.
	DeleteBatchWindow time.Duration
}

// DefaultOptions returns a fully-populated Options struct with sane defaults.
// QueueURL is still required and must be set via WithQueueURL (or directly) before validation.
func DefaultOptions() Options {
	return Options{
		WorkerCount: 1,

		BatchSize: 10,

		WaitTimeSeconds: 20,

		VisibilityTimeoutSeconds: nil,

		DeleteBatchSize:   10,
		DeleteBatchWindow: 1 * time.Second,
	}
}

// Validate checks the Options for correctness.
func (o Options) Validate() error {
	if o.QueueURL == "" {
		return fmt.Errorf("sqs worker: options.QueueURL is required")
	}

	if o.WorkerCount <= 0 {
		return fmt.Errorf("sqs worker: options.WorkerCount must be > 0 (got %d)", o.WorkerCount)
	}

	if o.BatchSize <= 0 {
		return fmt.Errorf("sqs worker: options.BatchSize must be > 0 (got %d)", o.BatchSize)
	}
	if o.BatchSize > 10 {
		return fmt.Errorf("sqs worker: options.BatchSize must be <= 10 (got %d)", o.BatchSize)
	}

	if o.WaitTimeSeconds < 0 || o.WaitTimeSeconds > 20 {
		return fmt.Errorf("sqs worker: options.WaitTimeSeconds must be 0..20 (got %d)", o.WaitTimeSeconds)
	}

	if o.VisibilityTimeoutSeconds != nil && *o.VisibilityTimeoutSeconds < 0 {
		return fmt.Errorf("sqs worker: options.VisibilityTimeoutSeconds must be >= 0 (got %d)", *o.VisibilityTimeoutSeconds)
	}

	if o.DeleteBatchSize <= 0 {
		return fmt.Errorf("sqs worker: options.DeleteBatchSize must be > 0 (got %d)", o.DeleteBatchSize)
	}
	if o.DeleteBatchSize > 10 {
		return fmt.Errorf("sqs worker: options.DeleteBatchSize must be <= 10 (got %d)", o.DeleteBatchSize)
	}

	if o.DeleteBatchWindow <= 0 {
		return fmt.Errorf("sqs worker: options.DeleteBatchWindow must be > 0 (got %s)", o.DeleteBatchWindow)
	}

	return nil
}

// Option mutates Options (variadic functional options style).
type Option func(*Options) error

func WithQueueURL(url string) Option {
	return func(o *Options) error {
		o.QueueURL = url
		return nil
	}
}

func WithWorkerCount(n int) Option {
	return func(o *Options) error {
		o.WorkerCount = n
		return nil
	}
}

func WithBatchSize(n int) Option {
	return func(o *Options) error {
		o.BatchSize = n
		return nil
	}
}

func WithSQSClient(c *sqs.Client) Option {
	return func(o *Options) error {
		o.Client = c
		return nil
	}
}

func WithWaitTimeSeconds(n int32) Option {
	return func(o *Options) error {
		o.WaitTimeSeconds = n
		return nil
	}
}

func WithVisibilityTimeoutSeconds(n int32) Option {
	return func(o *Options) error {
		o.VisibilityTimeoutSeconds = &n
		return nil
	}
}

func WithDeleteBatchWindow(d time.Duration) Option {
	return func(o *Options) error {
		o.DeleteBatchWindow = d
		return nil
	}
}

func WithDeleteBatchSize(n int) Option {
	return func(o *Options) error {
		o.DeleteBatchSize = n
		return nil
	}
}

package sqs

import (
	"fmt"
	"log/slog"
	"time"
)

// Options holds all configurable settings for Worker construction.
type Options struct {
	// Required
	QueueURL string

	// Optional. If nil, New() should create a client using gommon/pkg/aws GetConfig().
	Client sqsClient

	// Optional. If nil, the worker should use slog.Default().
	Logger *slog.Logger

	// Concurrency
	WorkerCount int

	// Batching
	// BatchSize is the most messages passed to your handler at once (1..10). A free handler takes up to
	// BatchSize of the oldest waiting messages and does not wait for a full batch.
	BatchSize int

	// ReceiveMessage tuning
	// WaitTimeSeconds controls SQS long-polling (0..20).
	WaitTimeSeconds int32

	// Delete batching
	// DeleteBatchSize controls max entries per DeleteMessageBatch call (1..10).
	DeleteBatchSize int
	// DeleteBatchWindow controls periodic flushing even if the delete batch isn't full.
	DeleteBatchWindow time.Duration

	// MaxPrefetched is the most received messages that may wait in memory for a busy handler. The worker
	// receives only as many messages as it can hold: a full batch for every idle handler plus up to
	// MaxPrefetched waiting. The default is 10. Use 0 for long jobs, so the worker receives only for idle
	// handlers and waiting messages stay in the queue for other workers.
	MaxPrefetched int

	// HeartbeatInterval and HeartbeatExtension control how the worker keeps the messages it holds hidden.
	// Each receive hides messages for HeartbeatExtension, and every HeartbeatInterval the worker resets
	// every held message, waiting or running, to stay hidden for HeartbeatExtension from then. The
	// defaults are 3 and 6 minutes: longer values make fewer SQS calls, but a crashed worker's messages
	// take longer to reappear. A message whose visibility cannot be kept is dropped: a waiting message
	// never reaches the handler, and a running message's handler context is cancelled with ErrVisibilityLost.
	//
	// HeartbeatExtension must be whole seconds. SQS keeps a message hidden for at most 12 hours after it
	// was received; after that it rejects extensions, so a job still running then loses its message.
	HeartbeatInterval  time.Duration
	HeartbeatExtension time.Duration
}

const (
	// maxVisibilityTimeout is the largest visibility timeout SQS accepts.
	maxVisibilityTimeout = 12 * time.Hour

	// minHeartbeatInterval is the shortest heartbeat interval New accepts.
	minHeartbeatInterval = time.Second
)

// DefaultOptions returns a fully-populated Options struct with sane defaults.
// QueueURL is still required and must be set via WithQueueURL (or directly) before validation.
func DefaultOptions() Options {
	return Options{
		WorkerCount:        1,
		BatchSize:          10,
		MaxPrefetched:      10,
		WaitTimeSeconds:    20,
		HeartbeatInterval:  3 * time.Minute,
		HeartbeatExtension: 6 * time.Minute,
		DeleteBatchSize:    10,
		DeleteBatchWindow:  10 * time.Second,
		Logger:             slog.Default(),
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
	if o.BatchSize <= 0 || o.BatchSize > 10 {
		return fmt.Errorf("sqs worker: options.BatchSize must be > 0 and <= 10 (got %d)", o.BatchSize)
	}
	if o.WaitTimeSeconds < 0 || o.WaitTimeSeconds > 20 {
		return fmt.Errorf("sqs worker: options.WaitTimeSeconds must be 0..20 (got %d)", o.WaitTimeSeconds)
	}
	if o.DeleteBatchSize <= 0 || o.DeleteBatchSize > 10 {
		return fmt.Errorf("sqs worker: options.DeleteBatchSize must be > 0 and <= 10 (got %d)", o.DeleteBatchSize)
	}
	if o.DeleteBatchWindow <= 0 || o.DeleteBatchWindow > 2*time.Minute {
		return fmt.Errorf("sqs worker: options.DeleteBatchWindow must be > 0 and <= 2m (got %s)", o.DeleteBatchWindow)
	}
	if o.MaxPrefetched < 0 {
		return fmt.Errorf("sqs worker: options.MaxPrefetched must be >= 0 (got %d)", o.MaxPrefetched)
	}
	// Each heartbeat call gets at most one interval to finish, so a tiny interval would make every call time out.
	if o.HeartbeatInterval < minHeartbeatInterval || o.HeartbeatInterval >= o.HeartbeatExtension {
		return fmt.Errorf("sqs worker: options.HeartbeatInterval must be >= %s and < HeartbeatExtension (got %s and %s)", minHeartbeatInterval, o.HeartbeatInterval, o.HeartbeatExtension)
	}
	if o.HeartbeatExtension > maxVisibilityTimeout {
		return fmt.Errorf("sqs worker: options.HeartbeatExtension must be <= %s (got %s)", maxVisibilityTimeout, o.HeartbeatExtension)
	}
	if o.HeartbeatExtension%time.Second != 0 {
		return fmt.Errorf("sqs worker: options.HeartbeatExtension must be whole seconds (got %s)", o.HeartbeatExtension)
	}
	// A marked message stops being extended. Its last extension may be a whole interval old, so what is
	// left must still cover the delete.
	if left, need := o.HeartbeatExtension-o.HeartbeatInterval, o.DeleteBatchWindow+deleteCallTimeout; left <= need {
		return fmt.Errorf("sqs worker: options.HeartbeatExtension - HeartbeatInterval must be > DeleteBatchWindow + %s (got %s, need > %s)", deleteCallTimeout, left, need)
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

func WithLogger(l *slog.Logger) Option {
	return func(o *Options) error {
		o.Logger = l
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

func WithSQSClient(c sqsClient) Option {
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

func WithDeleteBatchWindow(d time.Duration) Option {
	return func(o *Options) error {
		o.DeleteBatchWindow = d
		return nil
	}
}

// WithMaxPrefetched sets the most received messages that may wait in memory for a busy handler.
// The default is 10. Use 0 for long jobs, so the worker receives only for idle handlers.
func WithMaxPrefetched(n int) Option {
	return func(o *Options) error {
		o.MaxPrefetched = n
		return nil
	}
}

// WithHeartbeat sets how long each receive and extension hides a message, and how often the worker extends
// the messages it holds. The defaults are 3 and 6 minutes. extension must be whole seconds, and SQS stops
// extending a message 12 hours after it was received.
func WithHeartbeat(interval, extension time.Duration) Option {
	return func(o *Options) error {
		o.HeartbeatInterval = interval
		o.HeartbeatExtension = extension
		return nil
	}
}

func WithDeleteBatchSize(n int) Option {
	return func(o *Options) error {
		o.DeleteBatchSize = n
		return nil
	}
}

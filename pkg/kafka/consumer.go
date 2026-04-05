package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// HandlerFunc is invoked for each batch of records from a single partition.
//
// The handler MUST respect ctx.Done() to allow timely shutdown during rebalances.
// The handler MUST be idempotent — Kafka provides at-least-once delivery.
// Records are only committed if your handler calls ConsumerRecord.Mark() on them.
type HandlerFunc func(ctx context.Context, topic string, partition int32, records []ConsumerRecord)

// consumerClient is the subset of *kgo.Client methods used by Consumer.
// Unexported to allow test fakes.
type consumerClient interface {
	PollRecords(ctx context.Context, maxRecords int) kgo.Fetches
	MarkCommitRecords(records ...*kgo.Record)
	CommitMarkedOffsets(ctx context.Context) error
	Close()
}

type topicPartition struct {
	topic     string
	partition int32
}

type partitionWorker struct {
	ch     chan []*kgo.Record
	done   chan struct{}
	cancel context.CancelFunc
}

// Consumer reads from Kafka topics using a consumer group with per-partition
// goroutines for concurrent, order-preserving processing.
type Consumer struct {
	client  consumerClient
	handler HandlerFunc
	logger  *slog.Logger

	topics         []string
	maxPollRecords int

	revocationTimeout time.Duration

	mu         sync.Mutex
	partitions map[topicPartition]*partitionWorker
}

// NewConsumer constructs a Consumer using DefaultConsumerOptions(), applies any
// variadic options, then validates. Handler is required.
func NewConsumer(handler HandlerFunc, optFns ...ConsumerOption) (*Consumer, error) {
	if handler == nil {
		return nil, errors.New("kafka consumer: handler is required")
	}

	opts := DefaultConsumerOptions()
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

	c := &Consumer{
		handler:           handler,
		client:            opts.Client,
		logger:            opts.Logger,
		topics:            opts.Topics,
		maxPollRecords:    opts.MaxPollRecords,
		revocationTimeout: opts.RevocationTimeout,
		partitions:        make(map[topicPartition]*partitionWorker),
	}

	if c.logger == nil {
		c.logger = slog.Default()
	}

	// Build a real franz-go client if none was injected.
	if c.client == nil {
		cl, err := buildConsumerClient(c, opts)
		if err != nil {
			return nil, fmt.Errorf("kafka consumer: create client: %w", err)
		}
		c.client = cl
	}

	return c, nil
}

func buildConsumerClient(c *Consumer, opts ConsumerOptions) (*kgo.Client, error) {
	kgoOpts := []kgo.Opt{
		kgo.SeedBrokers(opts.Brokers...),
		kgo.ConsumerGroup(opts.GroupID),
		kgo.ConsumeTopics(opts.Topics...),

		// Use AutoCommitMarks: only records where Mark() was called get committed.
		kgo.AutoCommitMarks(),

		// Cooperative sticky balancing — no stop-the-world rebalances.
		kgo.Balancers(kgo.CooperativeStickyBalancer()),

		kgo.SessionTimeout(opts.SessionTimeout),

		// Rebalance callbacks — wired to partition goroutine lifecycle.
		kgo.OnPartitionsAssigned(c.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(c.onPartitionsRevoked),
		kgo.OnPartitionsLost(c.onPartitionsLost),
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

// Start runs the poll loop until ctx is cancelled, then performs graceful shutdown.
// It blocks until shutdown is complete.
func (c *Consumer) Start(ctx context.Context) error {
	pollErr := c.poll(ctx)

	// Shutdown: stop all partition goroutines, then close the client.
	c.stopAllPartitions()
	c.client.Close()

	if errors.Is(pollErr, context.Canceled) || errors.Is(pollErr, context.DeadlineExceeded) {
		return nil
	}
	return pollErr
}

func (c *Consumer) poll(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fetches := c.client.PollRecords(ctx, c.maxPollRecords)

		// Check for fetch-level errors (auth failures, offset out of range, etc.)
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				// Context cancellation is expected during shutdown.
				if errors.Is(e.Err, context.Canceled) || errors.Is(e.Err, context.DeadlineExceeded) {
					return e.Err
				}

				// Network-level errors (connection refused, DNS, etc.) — log and
				// let franz-go retry on the next poll.
				var netErr net.Error
				if errors.As(e.Err, &netErr) {
					c.logger.Error("kafka consumer: network error",
						"topic", e.Topic,
						"partition", e.Partition,
						"err", e.Err,
					)
					continue
				}

				// Unknown / fatal error.
				return fmt.Errorf("kafka consumer: fetch error on %s/%d: %w", e.Topic, e.Partition, e.Err)
			}
		}

		// Dispatch records to per-partition goroutines.
		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			if len(ftp.Records) == 0 {
				return
			}

			tp := topicPartition{
				topic:     ftp.Topic,
				partition: ftp.Partition,
			}

			c.mu.Lock()
			pw, ok := c.partitions[tp]
			c.mu.Unlock()

			if !ok {
				// Partition not in our map — could happen briefly during rebalance.
				// franz-go will reassign it, so just drop these records.
				c.logger.Warn("kafka consumer: received records for untracked partition",
					"topic", ftp.Topic,
					"partition", ftp.Partition,
					"count", len(ftp.Records),
				)
				return
			}

			// Send to the partition goroutine's channel.
			// This blocks if the channel buffer is full (backpressure).
			pw.ch <- ftp.Records
		})
	}
}

// --- Rebalance callbacks ---

func (c *Consumer) onPartitionsAssigned(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for topic, partitions := range assigned {
		for _, partition := range partitions {
			tp := topicPartition{topic, partition}

			if _, exists := c.partitions[tp]; exists {
				// Already tracked — shouldn't happen with cooperative balancing, but be safe.
				continue
			}

			ctx, cancel := context.WithCancel(context.Background())
			pw := &partitionWorker{
				ch:     make(chan []*kgo.Record, 1), // buffer = 1
				done:   make(chan struct{}),
				cancel: cancel,
			}

			c.partitions[tp] = pw

			go c.runPartition(ctx, tp, pw)

			c.logger.Info("kafka consumer: partition assigned",
				"topic", topic,
				"partition", partition,
			)
		}
	}
}

func (c *Consumer) onPartitionsRevoked(ctx context.Context, cl *kgo.Client, revoked map[string][]int32) {
	c.drainPartitions(revoked)

	if err := cl.CommitMarkedOffsets(ctx); err != nil {
		c.logger.Error("kafka consumer: commit marked offsets on revoke failed", "err", err)
	}
}

func (c *Consumer) onPartitionsLost(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
	// Partitions lost — broker already reassigned them. No point committing.
	// Just kill the goroutines immediately.
	c.mu.Lock()
	defer c.mu.Unlock()

	for topic, partitions := range lost {
		for _, partition := range partitions {
			tp := topicPartition{topic, partition}
			pw, ok := c.partitions[tp]
			if !ok {
				continue
			}
			delete(c.partitions, tp)

			pw.cancel()
			close(pw.ch)
			// Don't wait — these partitions are gone, no commit possible.

			c.logger.Warn("kafka consumer: partition lost",
				"topic", topic,
				"partition", partition,
			)
		}
	}
}

// drainPartitions gracefully stops goroutines for the given partitions,
// waiting up to revocationTimeout for each to finish.
func (c *Consumer) drainPartitions(partitions map[string][]int32) {
	type entry struct {
		tp topicPartition
		pw *partitionWorker
	}

	var toStop []entry

	c.mu.Lock()
	for topic, parts := range partitions {
		for _, partition := range parts {
			tp := topicPartition{topic, partition}
			pw, ok := c.partitions[tp]
			if !ok {
				continue
			}
			delete(c.partitions, tp)
			toStop = append(toStop, entry{tp, pw})
		}
	}
	c.mu.Unlock()

	var wg sync.WaitGroup
	for _, e := range toStop {
		wg.Add(1)
		go func(tp topicPartition, pw *partitionWorker) {
			defer wg.Done()

			close(pw.ch) // signal: no more batches

			select {
			case <-pw.done: // goroutine drained naturally
				c.logger.Info("kafka consumer: partition drained",
					"topic", tp.topic,
					"partition", tp.partition,
				)

			case <-time.After(c.revocationTimeout):
				// Zombie — cancel context and abandon.
				pw.cancel()
				c.logger.Warn("kafka consumer: partition handler did not finish within revocation timeout, abandoning",
					"topic", tp.topic,
					"partition", tp.partition,
					"timeout", c.revocationTimeout,
				)
				// Don't wait for pw.done — the goroutine may be truly stuck.
				// Its marks won't be committed (stale generation).
			}
		}(e.tp, e.pw)
	}
	wg.Wait()
}

// stopAllPartitions is called during final shutdown (after poll loop exits).
func (c *Consumer) stopAllPartitions() {
	all := make(map[string][]int32)

	c.mu.Lock()
	for tp := range c.partitions {
		all[tp.topic] = append(all[tp.topic], tp.partition)
	}
	c.mu.Unlock()

	c.drainPartitions(all)
}

// runPartition is the per-partition goroutine. It reads batches from the channel,
// wraps them into ConsumerRecords, and calls the handler.
func (c *Consumer) runPartition(ctx context.Context, tp topicPartition, pw *partitionWorker) {
	defer close(pw.done)

	for batch := range pw.ch {
		if ctx.Err() != nil {
			return // partition revoked or consumer shutting down
		}

		records := make([]ConsumerRecord, 0, len(batch))
		for _, raw := range batch {
			r := raw // capture loop var
			records = append(records, ConsumerRecord{
				raw: r,
				mark: func(rec *kgo.Record) {
					c.client.MarkCommitRecords(rec)
				},
			})
		}

		c.handler(ctx, tp.topic, tp.partition, records)
	}
}

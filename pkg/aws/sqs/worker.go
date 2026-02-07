package sqs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	gaws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	pkgaws "github.com/project-init/gommon/pkg/aws"
)

// HandlerFunc is invoked for each received batch.
type HandlerFunc func(ctx context.Context, batch []Message) error

type deleteEntry struct {
	id            string
	receiptHandle string
}

type Worker struct {
	client *sqs.Client

	queueURL string

	handler HandlerFunc

	workerCount int
	batchSize   int

	// ReceiveMessage tuning
	waitTimeSeconds          int32
	visibilityTimeoutSeconds *int32
	maxReceiveBatchSize      int32 // sent to ReceiveMessage (<= 10)

	// Delete batching
	deleteBatchSize   int
	deleteBatchWindow time.Duration
	deleteChan        chan deleteEntry

	// Internal worker scheduling
	jobs chan []types.Message
}

// New constructs a Worker using DefaultOptions(), applies any variadic options, then validates.
// Handler is required. QueueURL must be provided via WithQueueURL(...).
func New(handler HandlerFunc, optFns ...Option) (*Worker, error) {
	if handler == nil {
		return nil, errors.New("sqs worker: handler is required")
	}

	opts := DefaultOptions()
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

	w := &Worker{
		handler: handler,

		client:   opts.Client,
		queueURL: opts.QueueURL,

		workerCount: opts.WorkerCount,
		batchSize:   opts.BatchSize,

		waitTimeSeconds:          opts.WaitTimeSeconds,
		visibilityTimeoutSeconds: opts.VisibilityTimeoutSeconds,
		maxReceiveBatchSize:      int32(opts.BatchSize),

		deleteBatchSize:   opts.DeleteBatchSize,
		deleteBatchWindow: opts.DeleteBatchWindow,
	}

	if w.client == nil {
		w.client = sqs.NewFromConfig(pkgaws.GetConfig())
	}

	return w, nil
}

// Start runs the poller, workers, and background deleter until ctx is cancelled.
// It returns when shutdown is complete. Any poll error is returned.
func (w *Worker) Start(ctx context.Context) error {
	w.jobs = make(chan []types.Message, w.workerCount*2)
	w.deleteChan = make(chan deleteEntry, 1000)

	var allWG sync.WaitGroup
	var workersWG sync.WaitGroup

	// Start background batch deleter
	allWG.Go(func() {
		w.runBatchDeleter(ctx, w.deleteChan)
	})

	// 2) Start workers
	for i := 0; i < w.workerCount; i++ {
		allWG.Add(1)
		workersWG.Add(1)
		go func(id int) {
			defer allWG.Done()
			defer workersWG.Done()
			w.runWorker(ctx, id, w.jobs)
		}(i)
	}

	// 3) Run poller (blocks)
	pollErr := w.poll(ctx, w.jobs)

	// Shutdown sequence:
	// - stop giving jobs to workers
	close(w.jobs)
	workersWG.Wait()

	// - stop deleter and flush
	close(w.deleteChan)
	allWG.Wait()

	return pollErr
}

func (w *Worker) runWorker(ctx context.Context, id int, jobs <-chan []types.Message) {
	for rawBatch := range jobs {
		if len(rawBatch) == 0 {
			continue
		}

		// Wrap raw messages so handler can call Mark() directly.
		batch := make([]Message, 0, len(rawBatch))
		for i := range rawBatch {
			msg := rawBatch[i]
			batch = append(batch, Message{
				raw: msg,
				mark: func(m types.Message) {
					// Only enqueue if it has the fields needed for DeleteMessageBatch.
					if m.MessageId == nil || m.ReceiptHandle == nil {
						return
					}
					w.deleteChan <- deleteEntry{
						id:            *m.MessageId,
						receiptHandle: *m.ReceiptHandle,
					}
				},
			})
		}

		if err := w.handler(ctx, batch); err != nil {
			log.Printf("[sqs worker %d] handler error (batch size=%d): %v", id, len(batch), err)
			// On handler error we do not delete; messages will become visible again after visibility timeout.
			continue
		}
	}
}

func (w *Worker) poll(ctx context.Context, out chan<- []types.Message) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		in := &sqs.ReceiveMessageInput{
			QueueUrl:            gaws.String(w.queueURL),
			MaxNumberOfMessages: w.maxReceiveBatchSize,
			WaitTimeSeconds:     w.waitTimeSeconds,
		}
		if w.visibilityTimeoutSeconds != nil {
			in.VisibilityTimeout = *w.visibilityTimeoutSeconds
		}

		resp, err := w.client.ReceiveMessage(ctx, in)
		if err != nil {
			// If context cancelled, bubble it; else return error to allow caller to restart/backoff.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("sqs worker: receive message: %w", err)
		}
		if len(resp.Messages) == 0 {
			continue
		}

		// Send the received batch as a single unit of work.
		select {
		case out <- resp.Messages:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (w *Worker) runBatchDeleter(ctx context.Context, in <-chan deleteEntry) {
	ticker := time.NewTicker(w.deleteBatchWindow)
	defer ticker.Stop()

	buffer := make([]types.DeleteMessageBatchRequestEntry, 0, w.deleteBatchSize)
	seen := make(map[string]struct{}, w.deleteBatchSize*2) // receiptHandle dedupe per flush window

	flush := func() {
		if len(buffer) == 0 {
			return
		}

		entries := make([]types.DeleteMessageBatchRequestEntry, len(buffer))
		copy(entries, buffer)

		// reset
		buffer = buffer[:0]
		for k := range seen {
			delete(seen, k)
		}

		_, err := w.client.DeleteMessageBatch(ctx, &sqs.DeleteMessageBatchInput{
			QueueUrl: gaws.String(w.queueURL),
			Entries:  entries,
		})
		if err != nil {
			// If deletion fails, we rely on visibility timeout and re-processing.
			log.Printf("[sqs deleter] delete batch failed (n=%d): %v", len(entries), err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			// Best-effort flush on shutdown using same ctx.
			flush()
			return

		case <-ticker.C:
			flush()

		case e, ok := <-in:
			if !ok {
				flush()
				return
			}
			if e.id == "" || e.receiptHandle == "" {
				continue
			}
			if _, exists := seen[e.receiptHandle]; exists {
				continue
			}
			seen[e.receiptHandle] = struct{}{}

			buffer = append(buffer, types.DeleteMessageBatchRequestEntry{
				Id:            gaws.String(e.id),
				ReceiptHandle: gaws.String(e.receiptHandle),
			})

			if len(buffer) >= w.deleteBatchSize {
				flush()
			}
		}
	}
}

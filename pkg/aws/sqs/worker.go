package sqs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	gaws "github.com/project-init/gommon/pkg/aws"
)

// HandlerFunc is invoked for each received batch.
//
// The handler should handle errors internally. Messages are only deleted if
// your handler calls `Message.Mark()` on them.
type HandlerFunc func(ctx context.Context, batch []Message)

type deleteEntry struct {
	id            string
	receiptHandle string
}

type sqsClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessageBatch(ctx context.Context, params *sqs.DeleteMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error)
}

type Worker struct {
	client sqsClient
	logger *slog.Logger

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
		logger:   opts.Logger,
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
		w.client = sqs.NewFromConfig(gaws.GetConfig())
	}

	if w.logger == nil {
		w.logger = slog.Default()
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

	// Start workers
	for range w.workerCount {
		allWG.Add(1)
		workersWG.Add(1)
		go func() {
			defer allWG.Done()
			defer workersWG.Done()
			w.runWorker(ctx, w.jobs)
		}()
	}

	// Run poller (blocks)
	pollErr := w.poll(ctx, w.jobs)

	// Shutdown sequence:
	// 1) Stop dispatching new jobs and wait for workers to exit.
	//    Workers may still call Message.Mark(), so we must not close deleteChan until they are done.
	close(w.jobs)
	workersWG.Wait()

	// 2) Close deleteChan to signal the deleter to flush any buffered deletes and exit.
	close(w.deleteChan)
	allWG.Wait()

	// Treat context cancellation/deadline as graceful shutdown.
	if errors.Is(pollErr, context.Canceled) || errors.Is(pollErr, context.DeadlineExceeded) {
		return nil
	}

	return pollErr
}

func (w *Worker) runWorker(ctx context.Context, jobs <-chan []types.Message) {
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

		w.handler(ctx, batch)
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
			QueueUrl:            aws.String(w.queueURL),
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
	deleteCtx := context.WithoutCancel(ctx)
	ticker := time.NewTicker(w.deleteBatchWindow)
	defer ticker.Stop()

	buffer := make([]types.DeleteMessageBatchRequestEntry, 0, w.deleteBatchSize)
	seen := make(map[string]struct{}, w.deleteBatchSize*2) // receiptHandle dedupe per flush window

	flush := func(flushCtx context.Context) {
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

		output, err := w.client.DeleteMessageBatch(flushCtx, &sqs.DeleteMessageBatchInput{
			QueueUrl: aws.String(w.queueURL),
			Entries:  entries,
		})
		if err != nil {
			// Avoid noisy logs during shutdown/cancellation.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || flushCtx.Err() != nil {
				return
			}
			// If deletion fails, we rely on visibility timeout and re-processing.
			w.logger.Error("sqs delete batch failed", "count", len(entries), "err", err)
			return
		}
		if output == nil {
			return
		}

		for _, failed := range output.Failed {
			w.logger.Error(
				"sqs delete batch entry failed",
				"id", aws.ToString(failed.Id),
				"code", aws.ToString(failed.Code),
				"message", aws.ToString(failed.Message),
				"sender_fault", failed.SenderFault,
			)
		}
	}

	flushWithTimeout := func() {
		flushCtx, cancel := context.WithTimeout(deleteCtx, 3*time.Second)
		defer cancel()
		flush(flushCtx)
	}

	for {
		select {

		case <-ticker.C:
			flushWithTimeout()

		case e, ok := <-in:
			if !ok {
				// Channel closed: handlers are finished, so flush remaining deletes
				// with a cancellation-independent timeout before exiting
				flushWithTimeout()
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
				Id:            aws.String(strconv.Itoa(len(buffer))),
				ReceiptHandle: aws.String(e.receiptHandle),
			})

			if len(buffer) >= w.deleteBatchSize {
				flushWithTimeout()
			}
		}
	}
}

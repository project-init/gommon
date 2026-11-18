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

// HandlerFunc is invoked for each batch of up to BatchSize messages.
//
// The handler decides whether each message is done: Mark deletes it, and a message not marked is redelivered
// once its visibility timeout ends. Mark before returning; once the handler returns, Mark does nothing.
type HandlerFunc func(ctx context.Context, batch []Message)

type deleteEntry struct {
	id            string
	receiptHandle string
}

type sqsClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessageBatch(ctx context.Context, params *sqs.DeleteMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error)
	ChangeMessageVisibilityBatch(ctx context.Context, params *sqs.ChangeMessageVisibilityBatchInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityBatchOutput, error)
}

var (
	// ErrVisibilityLost is the cause of a handler's context cancellation when the worker could not keep one of
	// the batch's messages hidden. Another worker may already have received the message, so the handler
	// should stop without writing anything. Mark is a no-op for a lost message. This also happens to a job
	// still running 12 hours after its message was received, the most SQS keeps a message hidden.
	ErrVisibilityLost = errors.New("sqs worker: message visibility lost")
)

const (
	// maxReceiveMessages is the most messages one ReceiveMessage call returns.
	maxReceiveMessages = 10

	// deleteCallTimeout bounds each DeleteMessageBatch call.
	deleteCallTimeout = 3 * time.Second
)

type Worker struct {
	client sqsClient
	logger *slog.Logger

	queueURL string

	handler HandlerFunc

	workerCount   int
	batchSize     int
	maxPrefetched int

	// ReceiveMessage tuning
	waitTimeSeconds int32

	// Heartbeat
	heartbeatInterval  time.Duration
	heartbeatExtension time.Duration

	// Delete batching
	deleteBatchSize   int
	deleteBatchWindow time.Duration
	deleteChan        chan deleteEntry

	// deleteMu guards deleteChan, so a late Mark cannot send on a closed channel.
	deleteMu      sync.RWMutex
	deletesClosed bool

	queue *messageQueue
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

		workerCount:   opts.WorkerCount,
		batchSize:     opts.BatchSize,
		maxPrefetched: opts.MaxPrefetched,

		waitTimeSeconds: opts.WaitTimeSeconds,

		heartbeatInterval:  opts.HeartbeatInterval,
		heartbeatExtension: opts.HeartbeatExtension,

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

// Start runs the poller, handlers, heartbeat, and background deleter until ctx is cancelled.
// It returns when shutdown is complete. Any poll error is returned.
//
// The poller receives only as many messages as the worker can hold: a full batch for every idle handler,
// plus up to MaxPrefetched waiting for a busy one. A free handler takes up to BatchSize of the oldest
// waiting messages at once.
//
// On shutdown, waiting messages are not started; the worker stops holding them, and they reappear when
// their visibility ends. Running handlers see ctx cancelled, and Start waits for them, keeping their
// messages hidden.
func (w *Worker) Start(ctx context.Context) error {
	w.queue = newMessageQueue(w.workerCount, w.batchSize, w.maxPrefetched)
	w.deleteChan = make(chan deleteEntry, 1000)

	var deleterWG, handlersWG, heartbeatWG sync.WaitGroup

	deleterWG.Go(func() {
		w.runBatchDeleter(ctx, w.deleteChan)
	})

	// The heartbeat outlives ctx: running handlers keep their messages hidden until they return.
	stopHeartbeat := make(chan struct{})
	heartbeatWG.Go(func() {
		w.runHeartbeat(context.WithoutCancel(ctx), stopHeartbeat)
	})

	for range w.workerCount {
		handlersWG.Go(func() {
			w.runHandler(ctx)
		})
	}

	pollErr := w.poll(ctx)

	// Shutdown: on cancellation, stop holding waiting messages now, so the heartbeat no longer extends them.
	// After a receive error, handlers take what is already waiting.
	w.queue.stop(ctx.Err() != nil)
	handlersWG.Wait()

	// Handlers stop taking messages once ctx is cancelled, which can leave some waiting after a receive error.
	w.queue.stop(true)

	close(stopHeartbeat)
	heartbeatWG.Wait()

	w.closeDeletes()
	deleterWG.Wait()

	if errors.Is(pollErr, context.Canceled) || errors.Is(pollErr, context.DeadlineExceeded) {
		return nil
	}
	return pollErr
}

// poll receives as many messages as the queue has room for, up to the ReceiveMessage limit.
func (w *Worker) poll(ctx context.Context) error {
	visibility := int32(w.heartbeatExtension / time.Second)

	for {
		n, err := w.queue.reserve(ctx, maxReceiveMessages)
		if err != nil {
			return err
		}

		in := &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(w.queueURL),
			MaxNumberOfMessages: int32(n),
			WaitTimeSeconds:     w.waitTimeSeconds,
			VisibilityTimeout:   visibility,
		}

		// Measured before the call, so the visibility deadline errs early.
		sentAt := time.Now()
		resp, err := w.client.ReceiveMessage(ctx, in)
		if err != nil {
			w.queue.add(n, nil, time.Time{})
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("sqs worker: receive message: %w", err)
		}

		var msgs []types.Message
		if resp != nil {
			msgs = resp.Messages
		}

		w.queue.add(n, msgs, sentAt.Add(w.heartbeatExtension))
	}
}

func (w *Worker) runHandler(ctx context.Context) {
	for {
		batchCtx, cancel, batch, ok := w.queue.take(ctx)
		if !ok {
			return
		}

		msgs := make([]Message, len(batch))
		for i, h := range batch {
			msgs[i] = w.message(h)
		}
		w.handler(batchCtx, msgs)

		// Messages the handler left unsettled stop being extended and reappear when their visibility ends.
		for _, h := range batch {
			w.queue.settle(h)
		}
		cancel(nil)
		w.queue.done()
	}
}

func (w *Worker) message(h *heldMessage) Message {
	return Message{
		raw: h.raw,
		mark: func(m types.Message) {
			if _, _, ok := w.queue.settle(h); !ok {
				return
			}
			if m.MessageId == nil || m.ReceiptHandle == nil {
				return
			}
			w.enqueueDelete(deleteEntry{id: *m.MessageId, receiptHandle: *m.ReceiptHandle})
		},
	}
}

// enqueueDelete sends a delete to the batch deleter, unless it has already shut down.
func (w *Worker) enqueueDelete(e deleteEntry) {
	w.deleteMu.RLock()
	defer w.deleteMu.RUnlock()

	if w.deletesClosed {
		return
	}
	w.deleteChan <- e
}

func (w *Worker) closeDeletes() {
	w.deleteMu.Lock()
	defer w.deleteMu.Unlock()

	w.deletesClosed = true
	close(w.deleteChan)
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
		flushCtx, cancel := context.WithTimeout(deleteCtx, deleteCallTimeout)
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

package sqs

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type Worker struct {
	client      *sqs.Client
	queueURL    string
	handler     HandlerFunc
	workerCount int
	// Configuration for batching
	batchSize   int
	batchWindow time.Duration
}

func New(client *sqs.Client, queueURL string, handler HandlerFunc, workerCount int) *Worker {
	return &Worker{
		client:      client,
		queueURL:    queueURL,
		handler:     handler,
		workerCount: workerCount,
		batchSize:   10,              // SQS limit is 10
		batchWindow: 1 * time.Second, // Flush at least every second
	}
}

func (w *Worker) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	var workerWg sync.WaitGroup // Separate WG to know when workers are done

	msgChan := make(chan types.Message, w.workerCount)

	// Channel for sending delete requests to the batcher
	// The struct contains the ReceiptHandle and the MessageID (required for batch reqs)
	deleteChan := make(chan types.DeleteMessageBatchRequestEntry, 100)

	// 1. Start Batch Deleter
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.runBatchDeleter(ctx, deleteChan)
	}()

	// 2. Start Workers
	for i := 0; i < w.workerCount; i++ {
		wg.Add(1)
		workerWg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer workerWg.Done()
			w.processMessages(ctx, id, msgChan, deleteChan)
		}(i)
	}

	// 3. Start Poller (Blocks until context cancellation)
	pollErr := w.poll(ctx, msgChan)

	// 4. Shutdown Sequence
	close(msgChan)    // Stop workers from receiving new messages
	workerWg.Wait()   // Wait for all workers to finish processing
	close(deleteChan) // Signal batch deleter that no more deletes are coming
	wg.Wait()         // Wait for batch deleter to flush final messages

	return pollErr
}

// processMessages now sends to deleteChan instead of calling SQS directly
func (w *Worker) processMessages(ctx context.Context, id int, messages <-chan types.Message, deleteChan chan<- types.DeleteMessageBatchRequestEntry) {
	for msg := range messages {
		// Execute Handler
		if err := w.handler(ctx, &msg); err != nil {
			log.Printf("[Worker %d] Handler failed: %v", id, err)
			continue
		}

		// Queue for deletion
		// We use MessageId as the batch 'Id' identifier
		deleteChan <- types.DeleteMessageBatchRequestEntry{
			Id:            msg.MessageId,
			ReceiptHandle: msg.ReceiptHandle,
		}
	}
}

// runBatchDeleter aggregates deletes and sends them in batches
func (w *Worker) runBatchDeleter(ctx context.Context, deleteChan <-chan types.DeleteMessageBatchRequestEntry) {
	buffer := make([]types.DeleteMessageBatchRequestEntry, 0, w.batchSize)
	ticker := time.NewTicker(w.batchWindow)
	defer ticker.Stop()

	// Helper to perform the actual network call
	flush := func() {
		if len(buffer) == 0 {
			return
		}

		// Copy buffer to avoid race conditions if we were reusing it immediately (though here we reset)
		entries := make([]types.DeleteMessageBatchRequestEntry, len(buffer))
		copy(entries, buffer)

		// Clear buffer for next round
		buffer = buffer[:0]

		_, err := w.client.DeleteMessageBatch(ctx, &sqs.DeleteMessageBatchInput{
			QueueUrl: aws.String(w.queueURL),
			Entries:  entries,
		})

		if err != nil {
			log.Printf("[BatchDeleter] Failed to delete batch: %v", err)
			// In a production system, you might want to retry specific failed entries
			// or rely on visibility timeout to re-process them.
		}
	}

	for {
		select {
		case entry, ok := <-deleteChan:
			if !ok {
				// Channel closed, flush remaining buffer and exit
				flush()
				return
			}
			buffer = append(buffer, entry)

			// If buffer is full, flush immediately
			if len(buffer) >= w.batchSize {
				flush()
			}

		case <-ticker.C:
			// Time window expired, flush whatever we have
			flush()
		}
	}
}

// poll remains the same as previous example...
func (w *Worker) poll(ctx context.Context, msgChan chan<- types.Message) error {
	// (Same implementation as previous response)
	// ...
	return nil
}

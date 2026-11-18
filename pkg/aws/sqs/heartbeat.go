package sqs

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
	// maxVisibilityBatch is the most entries ChangeMessageVisibilityBatch accepts.
	maxVisibilityBatch = 10

	// maxHeartbeatCallTimeout caps each heartbeat call when the interval is longer.
	maxHeartbeatCallTimeout = 5 * time.Second
)

func (w *Worker) runHeartbeat(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.heartbeatOnce(ctx)
		}
	}
}

// heartbeatCallTimeout bounds each heartbeat call, so one slow call cannot hold up the next tick.
func (w *Worker) heartbeatCallTimeout() time.Duration {
	return min(w.heartbeatInterval, maxHeartbeatCallTimeout)
}

// heartbeatOnce extends every held message, waiting or running, ten per call. A message is dropped when its
// visibility may end before an extension could land, or when SQS rejects the extension as the caller's
// fault (for example, an invalid receipt handle). Other failures are retried on the next tick while the
// message is still hidden.
func (w *Worker) heartbeatOnce(ctx context.Context) {
	for chunk := range slices.Chunk(w.queue.snapshot(), maxVisibilityBatch) {
		w.heartbeatChunk(ctx, chunk)
	}
}

func (w *Worker) heartbeatChunk(ctx context.Context, chunk []*heldMessage) {
	callTimeout := w.heartbeatCallTimeout()
	deadline := time.Now().Add(callTimeout)

	live := make([]*heldMessage, 0, len(chunk))
	for _, h := range chunk {
		if h.raw.ReceiptHandle == nil || h.isSettled() {
			continue
		}
		if h.visibleBy(deadline) {
			w.drop(h, "visibility may end before it could be extended")
			continue
		}
		live = append(live, h)
	}
	if len(live) == 0 {
		return
	}

	extension := int32(w.heartbeatExtension / time.Second)
	entries := make([]types.ChangeMessageVisibilityBatchRequestEntry, len(live))
	for i, h := range live {
		entries[i] = types.ChangeMessageVisibilityBatchRequestEntry{
			Id:                aws.String(strconv.Itoa(i)),
			ReceiptHandle:     h.raw.ReceiptHandle,
			VisibilityTimeout: extension,
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	sentAt := time.Now()
	out, err := w.client.ChangeMessageVisibilityBatch(callCtx, &sqs.ChangeMessageVisibilityBatchInput{
		QueueUrl: aws.String(w.queueURL),
		Entries:  entries,
	})
	if err != nil {
		w.logger.Warn("sqs heartbeat failed; retrying next interval", "count", len(entries), "err", err)
		return
	}
	if out == nil {
		return
	}

	for _, ok := range out.Successful {
		if h := entryMessage(live, ok.Id); h != nil {
			h.extendTo(sentAt.Add(w.heartbeatExtension))
		}
	}

	for _, failed := range out.Failed {
		h := entryMessage(live, failed.Id)
		if h == nil {
			continue
		}
		if failed.SenderFault {
			w.drop(h, fmt.Sprintf("%s: %s", aws.ToString(failed.Code), aws.ToString(failed.Message)))
			continue
		}
		w.logger.Warn("sqs heartbeat entry failed; retrying next interval", "id", aws.ToString(h.raw.MessageId), "code", aws.ToString(failed.Code))
	}
}

// drop settles a message whose visibility could not be kept, and cancels its handler if it is running.
func (w *Worker) drop(h *heldMessage, reason string) {
	prev, cancel, ok := w.queue.settle(h)
	if !ok {
		return
	}

	w.logger.Warn("sqs message visibility lost; dropping it", "id", aws.ToString(h.raw.MessageId), "running", prev == running, "reason", reason)

	if prev == running && cancel != nil {
		cancel(ErrVisibilityLost)
	}
}

// entryMessage maps a batch entry ID, the entry's index, back to its message.
func entryMessage(chunk []*heldMessage, id *string) *heldMessage {
	i, err := strconv.Atoi(aws.ToString(id))
	if err != nil || i < 0 || i >= len(chunk) {
		return nil
	}
	return chunk[i]
}

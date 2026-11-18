package sqs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	gaws "github.com/project-init/gommon/pkg/aws"
	gerror "github.com/project-init/gommon/pkg/errors"
)

const (
	defaultSendTimeout          = 5 * time.Second
	defaultSendMaxRetryAttempts = 3

	// maxSendBatch is the most entries SQS accepts in one SendMessageBatch call.
	maxSendBatch = 10
)

type sendAPI interface {
	SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
}

// Sender sends messages to one queue. It is the producing side of Worker.
type Sender struct {
	api      sendAPI
	queueURL string
	timeout  time.Duration
}

// NewSender builds a sender for queueURL. timeout is the deadline for each SendMessageBatch call.
// maxRetryAttempts is passed to the SDK as MaxAttempts, which counts the first attempt.
func NewSender(queueURL string, timeout time.Duration, maxRetryAttempts int) (*Sender, error) {
	if queueURL == "" {
		return nil, fmt.Errorf("%w: queueURL is required", gerror.ErrBadRequest)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout must be greater than 0", gerror.ErrBadRequest)
	}
	if maxRetryAttempts < 1 {
		return nil, fmt.Errorf("%w: maxRetryAttempts must be greater than 0", gerror.ErrBadRequest)
	}

	api := sqs.NewFromConfig(gaws.GetConfig(), func(opts *sqs.Options) {
		opts.Retryer = retry.NewStandard(func(standardOptions *retry.StandardOptions) {
			standardOptions.MaxAttempts = maxRetryAttempts
		})
	})
	return &Sender{api: api, queueURL: queueURL, timeout: timeout}, nil
}

// DefaultSender builds a sender with a 5 second timeout and 3 total attempts.
func DefaultSender(queueURL string) (*Sender, error) {
	return NewSender(queueURL, defaultSendTimeout, defaultSendMaxRetryAttempts)
}

// FailedMessage is one body SQS rejected.
type FailedMessage struct {
	// Index is the body's position in the slice passed to SendBatch.
	Index       int
	Code        string
	Message     string
	SenderFault bool
}

// SendError lists the bodies SQS rejected while accepting the rest of their batch.
type SendError struct {
	Failed []FailedMessage
}

func (e *SendError) Error() string {
	parts := make([]string, len(e.Failed))
	for i, failed := range e.Failed {
		parts[i] = fmt.Sprintf("message %d (%s: %s)", failed.Index, failed.Code, failed.Message)
	}
	return fmt.Sprintf("sqs sender: %d messages rejected: %s", len(e.Failed), strings.Join(parts, ", "))
}

// SendBatch sends every body, ten per SendMessageBatch call, and returns how many SQS accepted.
//
// Bodies SQS rejects one at a time do not stop the send: the remaining batches still go, and the
// returned error is a *SendError naming each rejected body by its index in bodies. A failed call
// stops the send, and the error wraps the SDK error along with any *SendError from earlier batches.
// An empty body is rejected before anything is sent.
func (s *Sender) SendBatch(ctx context.Context, bodies []string) (int, error) {
	for i, body := range bodies {
		if body == "" {
			return 0, fmt.Errorf("%w: body %d is empty", gerror.ErrBadRequest, i)
		}
	}

	sent := 0
	var rejected []FailedMessage
	for start := 0; start < len(bodies); start += maxSendBatch {
		batch := bodies[start:min(start+maxSendBatch, len(bodies))]

		accepted, failed, err := s.sendOne(ctx, start, batch)
		sent += accepted
		rejected = append(rejected, failed...)
		if err != nil {
			if len(rejected) > 0 {
				return sent, fmt.Errorf("%w (after %w)", err, &SendError{Failed: rejected})
			}
			return sent, err
		}
	}

	if len(rejected) > 0 {
		return sent, &SendError{Failed: rejected}
	}
	return sent, nil
}

// sendOne sends one batch whose first body sits at offset in the caller's slice. Entry IDs are the
// bodies' indexes in that slice, so a failed entry maps straight back to its body.
func (s *Sender) sendOne(ctx context.Context, offset int, batch []string) (int, []FailedMessage, error) {
	entries := make([]types.SendMessageBatchRequestEntry, len(batch))
	for i, body := range batch {
		entries[i] = types.SendMessageBatchRequestEntry{
			Id:          aws.String(strconv.Itoa(offset + i)),
			MessageBody: aws.String(body),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	output, err := s.api.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{
		QueueUrl: aws.String(s.queueURL),
		Entries:  entries,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("%w: send %d messages to %s", err, len(batch), s.queueURL)
	}
	if output == nil {
		return 0, nil, fmt.Errorf("send %d messages to %s: empty response", len(batch), s.queueURL)
	}

	failed := make([]FailedMessage, 0, len(output.Failed))
	for _, entry := range output.Failed {
		index, err := strconv.Atoi(aws.ToString(entry.Id))
		if err != nil {
			index = -1
		}
		failed = append(failed, FailedMessage{
			Index:       index,
			Code:        aws.ToString(entry.Code),
			Message:     aws.ToString(entry.Message),
			SenderFault: entry.SenderFault,
		})
	}
	return len(output.Successful), failed, nil
}

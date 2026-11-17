//go:build integration

package sqs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	gaws "github.com/project-init/gommon/pkg/aws"
	gerror "github.com/project-init/gommon/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const senderQueueName = "gommon-sqs-dev"

func TestSendBatchDeliversEveryBodyAcrossBatches(t *testing.T) {
	ctx, api, queueURL := localQueue(t)

	sender, err := NewSender(queueURL, 5*time.Second, 3)
	require.NoError(t, err)

	run := uuid.NewString()
	bodies := make([]string, 23)
	want := map[string]bool{}
	for i := range bodies {
		bodies[i] = fmt.Sprintf(`{"run":%q,"n":%d}`, run, i)
		want[bodies[i]] = true
	}

	sent, err := sender.SendBatch(ctx, bodies)
	require.NoError(t, err)
	assert.Equal(t, len(bodies), sent)

	got := receiveBodies(t, ctx, api, queueURL, len(bodies))
	assert.Equal(t, want, got)
}

func TestSendBatchNamesRejectedBodiesAndSendsTheRest(t *testing.T) {
	ctx, api, queueURL := localQueue(t)

	sender, err := NewSender(queueURL, 5*time.Second, 3)
	require.NoError(t, err)

	run := uuid.NewString()
	bodies := make([]string, 12)
	for i := range bodies {
		bodies[i] = fmt.Sprintf(`{"run":%q,"n":%d}`, run, i)
	}
	// U+0001 is outside the characters SQS allows in a message body.
	bodies[3] = "bad\u0001body"
	bodies[11] = "bad\u0001body"

	sent, err := sender.SendBatch(ctx, bodies)
	require.Error(t, err)
	var sendErr *SendError
	require.ErrorAs(t, err, &sendErr)
	indexes := make([]int, len(sendErr.Failed))
	for i, failed := range sendErr.Failed {
		indexes[i] = failed.Index
		assert.NotEmpty(t, failed.Code)
	}
	assert.ElementsMatch(t, []int{3, 11}, indexes)
	assert.Equal(t, 10, sent)

	got := receiveBodies(t, ctx, api, queueURL, 10)
	assert.Len(t, got, 10)
}

func TestSendBatchRejectsEmptyBodyBeforeSending(t *testing.T) {
	ctx, _, queueURL := localQueue(t)

	sender, err := NewSender(queueURL, 5*time.Second, 3)
	require.NoError(t, err)

	sent, err := sender.SendBatch(ctx, []string{"first", ""})
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Zero(t, sent)
}

func TestSendBatchToMissingQueueFails(t *testing.T) {
	ctx, _, queueURL := localQueue(t)

	sender, err := NewSender(queueURL+"-missing-"+uuid.NewString(), 5*time.Second, 1)
	require.NoError(t, err)

	sent, err := sender.SendBatch(ctx, []string{"first"})
	require.Error(t, err)
	var sendErr *SendError
	assert.False(t, errors.As(err, &sendErr))
	assert.Zero(t, sent)
}

// localQueue returns the LocalStack sender queue, purged so each test reads only its own messages.
func localQueue(t *testing.T) (context.Context, *sqs.Client, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)

	api := sqs.NewFromConfig(gaws.GetConfig())
	queue, err := api.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(senderQueueName)})
	require.NoError(t, err)
	queueURL := aws.ToString(queue.QueueUrl)

	_, err = api.PurgeQueue(ctx, &sqs.PurgeQueueInput{QueueUrl: aws.String(queueURL)})
	require.NoError(t, err)
	return ctx, api, queueURL
}

// receiveBodies reads and deletes messages until it has want of them or the queue stays empty.
func receiveBodies(t *testing.T, ctx context.Context, api *sqs.Client, queueURL string, want int) map[string]bool {
	t.Helper()

	got := map[string]bool{}
	deadline := time.Now().Add(15 * time.Second)
	for len(got) < want && time.Now().Before(deadline) {
		output, err := api.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     1,
		})
		require.NoError(t, err)
		for _, message := range output.Messages {
			got[aws.ToString(message.Body)] = true
			_, err := api.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: message.ReceiptHandle,
			})
			require.NoError(t, err)
		}
	}
	return got
}

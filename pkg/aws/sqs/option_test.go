package sqs

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	o := DefaultOptions()

	assert.Equal(t, 1, o.WorkerCount, "WorkerCount")
	assert.Equal(t, 10, o.BatchSize, "BatchSize")
	assert.Equal(t, int32(20), o.WaitTimeSeconds, "WaitTimeSeconds")
	assert.Nil(t, o.VisibilityTimeoutSeconds, "VisibilityTimeoutSeconds")
	assert.Equal(t, 10, o.DeleteBatchSize, "DeleteBatchSize")
	assert.Equal(t, 10*time.Second, o.DeleteBatchWindow, "DeleteBatchWindow")
	assert.NotNil(t, o.Logger, "Logger")

	// QueueURL is intentionally empty by default and should fail validation until set.
	require.Error(t, o.Validate())
}

func TestOptionsValidate_Success(t *testing.T) {
	t.Parallel()

	o := DefaultOptions()
	o.QueueURL = "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue"

	require.NoError(t, o.Validate())
}

func TestOptionsValidate_Errors(t *testing.T) {
	t.Parallel()

	valid := func() Options {
		o := DefaultOptions()
		o.QueueURL = "https://example.com/queue"
		return o
	}

	t.Run("missing QueueURL", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.QueueURL = ""

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.QueueURL is required")
	})

	t.Run("WorkerCount <= 0", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.WorkerCount = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.WorkerCount must be > 0")
	})

	t.Run("BatchSize out of range", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.BatchSize = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.BatchSize must be > 0 and <= 10")

		o = valid()
		o.BatchSize = 11

		err = o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.BatchSize must be > 0 and <= 10")
	})

	t.Run("WaitTimeSeconds out of range", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.WaitTimeSeconds = -1

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.WaitTimeSeconds must be 0..20")

		o = valid()
		o.WaitTimeSeconds = 21

		err = o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.WaitTimeSeconds must be 0..20")
	})

	t.Run("VisibilityTimeoutSeconds negative", func(t *testing.T) {
		t.Parallel()

		o := valid()
		v := int32(-1)
		o.VisibilityTimeoutSeconds = &v

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.VisibilityTimeoutSeconds must be >= 0")
	})

	t.Run("DeleteBatchSize out of range", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.DeleteBatchSize = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.DeleteBatchSize must be > 0 and <= 10")

		o = valid()
		o.DeleteBatchSize = 11

		err = o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.DeleteBatchSize must be > 0 and <= 10")
	})

	t.Run("DeleteBatchWindow out of range", func(t *testing.T) {
		t.Parallel()

		o := valid()
		o.DeleteBatchWindow = 0

		err := o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.DeleteBatchWindow must be > 0 and <= 2m")

		o = valid()
		o.DeleteBatchWindow = 2*time.Minute + time.Millisecond

		err = o.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "options.DeleteBatchWindow must be > 0 and <= 2m")
	})
}

type fakeSQSClient struct{}

func (fakeSQSClient) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{Messages: []types.Message{}}, nil
}

func (fakeSQSClient) DeleteMessageBatch(ctx context.Context, params *sqs.DeleteMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	return &sqs.DeleteMessageBatchOutput{}, nil
}

func TestFunctionalOptions_SetFields(t *testing.T) {
	t.Parallel()

	o := DefaultOptions()

	qURL := "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue"
	l := slog.New(slog.NewTextHandler(nil, nil))
	vt := int32(45)

	var client sqsClient = fakeSQSClient{}

	optFns := []Option{
		WithQueueURL(qURL),
		WithWorkerCount(7),
		WithBatchSize(3),
		WithSQSClient(client),
		WithWaitTimeSeconds(10),
		WithVisibilityTimeoutSeconds(vt),
		WithDeleteBatchWindow(30 * time.Second),
		WithDeleteBatchSize(9),
		WithLogger(l),
	}

	for _, fn := range optFns {
		require.NoError(t, fn(&o))
	}

	assert.Equal(t, qURL, o.QueueURL, "QueueURL")
	assert.Equal(t, 7, o.WorkerCount, "WorkerCount")
	assert.Equal(t, 3, o.BatchSize, "BatchSize")
	assert.Equal(t, client, o.Client, "Client")
	assert.Equal(t, int32(10), o.WaitTimeSeconds, "WaitTimeSeconds")

	if assert.NotNil(t, o.VisibilityTimeoutSeconds, "VisibilityTimeoutSeconds") {
		assert.Equal(t, vt, *o.VisibilityTimeoutSeconds, "VisibilityTimeoutSeconds value")
	}

	assert.Equal(t, 30*time.Second, o.DeleteBatchWindow, "DeleteBatchWindow")
	assert.Equal(t, 9, o.DeleteBatchSize, "DeleteBatchSize")
	assert.Equal(t, l, o.Logger, "Logger")

	// Ensure final options validate.
	require.NoError(t, o.Validate())
}

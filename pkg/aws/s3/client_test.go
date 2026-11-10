//go:build unit

package s3

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdks3 "github.com/aws/aws-sdk-go-v2/service/s3"
	gerror "github.com/project-init/gommon/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bucket   string
		timeout  time.Duration
		attempts int
		message  string
	}{
		{name: "empty bucket", timeout: time.Second, attempts: 3, message: "bucket is required"},
		{name: "zero timeout", bucket: "records", attempts: 3, message: "timeout must be greater than 0"},
		{name: "negative timeout", bucket: "records", timeout: -time.Second, attempts: 3, message: "timeout must be greater than 0"},
		{name: "zero attempts", bucket: "records", timeout: time.Second, message: "maxRetryAttempts must be greater than 0"},
		{name: "negative attempts", bucket: "records", timeout: time.Second, attempts: -1, message: "maxRetryAttempts must be greater than 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := New(tt.bucket, tt.timeout, tt.attempts)
			require.ErrorIs(t, err, gerror.ErrBadRequest)
			require.ErrorContains(t, err, tt.message)
			assert.Nil(t, client)
		})
	}
}

func TestApplyClientOptions(t *testing.T) {
	t.Parallel()

	withEndpoint := sdks3.Options{}
	applyClientOptions(aws.Config{BaseEndpoint: aws.String("http://localhost:4566")}, 3)(&withEndpoint)
	assert.True(t, withEndpoint.UsePathStyle)
	require.NotNil(t, withEndpoint.Retryer)
	assert.Equal(t, 3, withEndpoint.Retryer.MaxAttempts())

	production := sdks3.Options{}
	applyClientOptions(aws.Config{}, 3)(&production)
	assert.False(t, production.UsePathStyle)

	remote := sdks3.Options{}
	applyClientOptions(aws.Config{BaseEndpoint: aws.String("https://s3.us-east-1.amazonaws.com")}, 3)(&remote)
	assert.False(t, remote.UsePathStyle)
}

func TestDefaultRejectsEmptyBucket(t *testing.T) {
	t.Parallel()

	client, err := Default("")
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Nil(t, client)
}

func TestGetRequiresKey(t *testing.T) {
	t.Parallel()

	client, err := New("records", time.Second, 3)
	require.NoError(t, err)

	got, err := client.Get(context.Background(), "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Nil(t, got)
}

func TestPutRequiresKeyAndBody(t *testing.T) {
	t.Parallel()

	client, err := New("records", time.Second, 3)
	require.NoError(t, err)

	err = client.Put(context.Background(), "", bytes.NewReader([]byte("record")), "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)

	err = client.Put(context.Background(), "failed/record-1", nil, "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)
}

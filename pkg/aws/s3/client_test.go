//go:build unit

package s3

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	sdks3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
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

	_, err = client.Put(context.Background(), "", bytes.NewReader([]byte("record")), "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)

	_, err = client.Put(context.Background(), "failed/record-1", nil, "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)
}

func TestPutRejectsEmptyIfMatch(t *testing.T) {
	t.Parallel()

	client, err := New("records", time.Second, 3)
	require.NoError(t, err)

	got, err := client.Put(context.Background(), "sessions/SAL-1", bytes.NewReader([]byte("record")), "", WithIfMatch(""))
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Nil(t, got)
}

func TestPutRejectsIfMatchWithIfNoneMatch(t *testing.T) {
	t.Parallel()

	client, err := New("records", time.Second, 3)
	require.NoError(t, err)

	got, err := client.Put(context.Background(), "sessions/SAL-1", bytes.NewReader([]byte("record")), "", WithIfMatch(`"9b2cf535f27731c974343645a3985328"`), WithIfNoneMatch())
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Nil(t, got)
}

func TestConditionalOptionsSetHeaders(t *testing.T) {
	t.Parallel()

	input := &sdks3.PutObjectInput{}
	WithIfMatch(`"abc"`)(input)
	WithIfNoneMatch()(input)
	assert.Equal(t, `"abc"`, aws.ToString(input.IfMatch))
	assert.Equal(t, "*", aws.ToString(input.IfNoneMatch))
}

func TestPutErrorSentinel(t *testing.T) {
	t.Parallel()

	// responseError builds the SDK's operation and HTTP response error chain, with only the status
	// code set, as Put does not read error codes. It leaves out S3's internal s3shared wrapper, which
	// cannot be built here; the integration tests cover that layer.
	responseError := func(status int) error {
		return &smithy.OperationError{ServiceID: "S3", OperationName: "PutObject", Err: &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
			Err:      errors.New("api error"),
		}}}
	}

	plain := &sdks3.PutObjectInput{}
	conditional := &sdks3.PutObjectInput{IfMatch: aws.String(`"9b2cf535f27731c974343645a3985328"`)}

	tests := []struct {
		name     string
		err      error
		input    *sdks3.PutObjectInput
		expected error
	}{
		{name: "412", err: responseError(http.StatusPreconditionFailed), input: conditional, expected: gerror.ErrPreconditionFailed},
		{name: "409", err: responseError(http.StatusConflict), input: plain, expected: gerror.ErrConflict},
		{name: "404 with If-Match", err: responseError(http.StatusNotFound), input: conditional, expected: gerror.ErrNotFound},
		{name: "404 without If-Match", err: responseError(http.StatusNotFound), input: plain},
		{name: "500", err: responseError(http.StatusInternalServerError), input: conditional},
		{name: "plain error", err: errors.New("connection reset"), input: conditional},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, putErrorSentinel(tt.err, tt.input))
		})
	}
}

package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdks3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
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

func TestDefaultConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5*time.Second, defaultTimeout)
	assert.Equal(t, 3, defaultMaxRetryAttempts)
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

func TestGet(t *testing.T) {
	t.Parallel()

	modified := time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	fake := &fakeAPI{
		getOut: &sdks3.GetObjectOutput{
			Body:          io.NopCloser(bytes.NewReader([]byte("record"))),
			ContentType:   aws.String("application/json"),
			ContentLength: aws.Int64(6),
			LastModified:  aws.Time(modified),
		},
	}
	client := newClient("records", time.Second, 3, fake)

	got, err := client.Get(context.Background(), "failed/record-1")
	require.NoError(t, err)
	require.NoError(t, fake.contexts[0].Err())

	payload, err := io.ReadAll(got.Body)
	require.NoError(t, err)
	require.NoError(t, fake.contexts[0].Err())
	require.NoError(t, got.Body.Close())
	require.ErrorIs(t, fake.contexts[0].Err(), context.Canceled)
	assert.Equal(t, []byte("record"), payload)
	assert.Equal(t, "application/json", got.ContentType)
	assert.Equal(t, int64(6), got.Size)
	assert.Equal(t, modified, got.LastModified)
	require.Len(t, fake.getInputs, 1)
	assert.Equal(t, "records", aws.ToString(fake.getInputs[0].Bucket))
	assert.Equal(t, "failed/record-1", aws.ToString(fake.getInputs[0].Key))
	_, hasDeadline := fake.contexts[0].Deadline()
	assert.True(t, hasDeadline)
}

func TestGetMissingKey(t *testing.T) {
	t.Parallel()

	fake := &fakeAPI{getErr: &s3types.NoSuchKey{Message: aws.String("missing")}}
	client := newClient("records", time.Second, 3, fake)

	got, err := client.Get(context.Background(), "failed/missing")
	require.ErrorIs(t, err, gerror.ErrNotFound)
	assert.Nil(t, got)
	require.ErrorIs(t, fake.contexts[0].Err(), context.Canceled)
}

func TestGetMissingBucketIsNotNotFound(t *testing.T) {
	t.Parallel()

	fake := &fakeAPI{getErr: &s3types.NoSuchBucket{Message: aws.String("missing")}}
	client := newClient("records", time.Second, 3, fake)

	got, err := client.Get(context.Background(), "failed/record-1")
	require.Error(t, err)
	require.NotErrorIs(t, err, gerror.ErrNotFound)
	assert.Nil(t, got)
}

func TestGetRequiresKey(t *testing.T) {
	t.Parallel()

	client := newClient("records", time.Second, 3, &fakeAPI{})

	got, err := client.Get(context.Background(), "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Nil(t, got)
	assert.Empty(t, clientAPI(client).getInputs)
}

func TestPut(t *testing.T) {
	t.Parallel()

	fake := &fakeAPI{}
	client := newClient("records", time.Second, 3, fake)

	err := client.Put(context.Background(), "failed/record-1", bytes.NewReader([]byte("record")), "application/json")
	require.NoError(t, err)
	require.Len(t, fake.putInputs, 1)
	assert.Equal(t, "records", aws.ToString(fake.putInputs[0].Bucket))
	assert.Equal(t, "failed/record-1", aws.ToString(fake.putInputs[0].Key))
	assert.Equal(t, "application/json", aws.ToString(fake.putInputs[0].ContentType))
	body, err := io.ReadAll(fake.putInputs[0].Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("record"), body)
}

func TestPutRequiresKeyAndBody(t *testing.T) {
	t.Parallel()

	client := newClient("records", time.Second, 3, &fakeAPI{})

	err := client.Put(context.Background(), "", bytes.NewReader([]byte("record")), "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)

	err = client.Put(context.Background(), "failed/record-1", nil, "")
	require.ErrorIs(t, err, gerror.ErrBadRequest)
	assert.Empty(t, clientAPI(client).putInputs)
}

func TestListFailedPrefix(t *testing.T) {
	t.Parallel()

	modified := time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC)
	fake := &fakeAPI{
		listFn: func(input *sdks3.ListObjectsV2Input) (*sdks3.ListObjectsV2Output, error) {
			if aws.ToString(input.ContinuationToken) == "" {
				token := "next"
				return &sdks3.ListObjectsV2Output{
					Contents: []s3types.Object{{
						Key:          aws.String("failed/record-1"),
						Size:         aws.Int64(6),
						LastModified: aws.Time(modified),
					}},
					NextContinuationToken: aws.String(token),
				}, nil
			}
			return &sdks3.ListObjectsV2Output{}, nil
		},
	}
	client := newClient("records", time.Second, 3, fake)

	page, err := client.List(context.Background(), "failed/", "")
	require.NoError(t, err)
	require.Equal(t, "next", page.NextContinuationToken)
	require.Equal(t, []ObjectInfo{{
		Key:          "failed/record-1",
		Size:         6,
		LastModified: modified,
	}}, page.Objects)

	page, err = client.List(context.Background(), "failed/", page.NextContinuationToken)
	require.NoError(t, err)
	assert.Empty(t, page.Objects)
	assert.Empty(t, page.NextContinuationToken)

	require.Len(t, fake.listInputs, 2)
	assert.Equal(t, "records", aws.ToString(fake.listInputs[0].Bucket))
	assert.Equal(t, "failed/", aws.ToString(fake.listInputs[0].Prefix))
	assert.Nil(t, fake.listInputs[0].Delimiter)
	assert.Nil(t, fake.listInputs[0].ContinuationToken)
	assert.Equal(t, "next", aws.ToString(fake.listInputs[1].ContinuationToken))
	assert.Equal(t, "failed/", aws.ToString(fake.listInputs[1].Prefix))
}

func TestListEmptyPageIsSuccess(t *testing.T) {
	t.Parallel()

	fake := &fakeAPI{listOut: &sdks3.ListObjectsV2Output{}}
	client := newClient("records", time.Second, 3, fake)

	page, err := client.List(context.Background(), "failed/", "")
	require.NoError(t, err)
	assert.Empty(t, page.Objects)
	assert.Empty(t, page.NextContinuationToken)
	require.Len(t, fake.listInputs, 1)
	assert.Equal(t, "failed/", aws.ToString(fake.listInputs[0].Prefix))
}

func TestListWholeBucketOmitsPrefix(t *testing.T) {
	t.Parallel()

	fake := &fakeAPI{listOut: &sdks3.ListObjectsV2Output{}}
	client := newClient("records", time.Second, 3, fake)

	_, err := client.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Len(t, fake.listInputs, 1)
	assert.Nil(t, fake.listInputs[0].Prefix)
}

type fakeAPI struct {
	getInputs  []*sdks3.GetObjectInput
	putInputs  []*sdks3.PutObjectInput
	listInputs []*sdks3.ListObjectsV2Input
	contexts   []context.Context

	getOut  *sdks3.GetObjectOutput
	getErr  error
	putErr  error
	listOut *sdks3.ListObjectsV2Output
	listErr error
	listFn  func(*sdks3.ListObjectsV2Input) (*sdks3.ListObjectsV2Output, error)
}

func (f *fakeAPI) GetObject(ctx context.Context, params *sdks3.GetObjectInput, _ ...func(*sdks3.Options)) (*sdks3.GetObjectOutput, error) {
	f.contexts = append(f.contexts, ctx)
	f.getInputs = append(f.getInputs, params)
	return f.getOut, f.getErr
}

func (f *fakeAPI) PutObject(ctx context.Context, params *sdks3.PutObjectInput, _ ...func(*sdks3.Options)) (*sdks3.PutObjectOutput, error) {
	f.contexts = append(f.contexts, ctx)
	f.putInputs = append(f.putInputs, params)
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &sdks3.PutObjectOutput{}, nil
}

func (f *fakeAPI) ListObjectsV2(ctx context.Context, params *sdks3.ListObjectsV2Input, _ ...func(*sdks3.Options)) (*sdks3.ListObjectsV2Output, error) {
	f.contexts = append(f.contexts, ctx)
	f.listInputs = append(f.listInputs, params)
	if f.listFn != nil {
		return f.listFn(params)
	}
	return f.listOut, f.listErr
}

func clientAPI(client *Client) *fakeAPI {
	api, ok := client.api.(*fakeAPI)
	if !ok {
		panic("client API is not the fake")
	}
	return api
}

var _ s3API = (*fakeAPI)(nil)

func TestPutWrapsSDKError(t *testing.T) {
	t.Parallel()

	fake := &fakeAPI{putErr: errors.New("throttled")}
	client := newClient("records", time.Second, 3, fake)

	err := client.Put(context.Background(), "failed/record-1", bytes.NewReader([]byte("record")), "")
	require.ErrorContains(t, err, "put object failed/record-1")
	require.ErrorContains(t, err, "throttled")
}

package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	sdks3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	gaws "github.com/project-init/gommon/pkg/aws"
	gerror "github.com/project-init/gommon/pkg/errors"
)

const (
	defaultTimeout          = 5 * time.Second
	defaultMaxRetryAttempts = 3
)

// ObjectInfo is one key returned by List.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// ListResult is one page of objects. NextContinuationToken is empty on the last page.
type ListResult struct {
	Objects               []ObjectInfo
	NextContinuationToken string
}

// GetResult is the body and metadata for one key. The caller closes Body.
// The Get deadline stays in effect until Body is closed.
type GetResult struct {
	Body         io.ReadCloser
	ContentType  string
	Size         int64
	LastModified time.Time
}

// cancelOnClose cancels the Get deadline after the caller finishes the body.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *cancelOnClose) Close() error {
	defer b.once.Do(b.cancel)
	return b.ReadCloser.Close()
}

type s3API interface {
	GetObject(ctx context.Context, params *sdks3.GetObjectInput, optFns ...func(*sdks3.Options)) (*sdks3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *sdks3.PutObjectInput, optFns ...func(*sdks3.Options)) (*sdks3.PutObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *sdks3.ListObjectsV2Input, optFns ...func(*sdks3.Options)) (*sdks3.ListObjectsV2Output, error)
}

// Client reads and writes objects in one bucket.
type Client struct {
	api     s3API
	bucket  string
	timeout time.Duration
}

// New builds a client for bucket. timeout is the deadline for each call.
// maxRetryAttempts is passed to the SDK as MaxAttempts, which counts the first attempt.
func New(bucket string, timeout time.Duration, maxRetryAttempts int) (*Client, error) {
	if err := validate(bucket, timeout, maxRetryAttempts); err != nil {
		return nil, err
	}

	return newClient(bucket, timeout, maxRetryAttempts, nil), nil
}

// Default builds a client with a 5 second timeout and 3 total attempts.
func Default(bucket string) (*Client, error) {
	return New(bucket, defaultTimeout, defaultMaxRetryAttempts)
}

func newClient(bucket string, timeout time.Duration, maxRetryAttempts int, api s3API) *Client {
	if api == nil {
		cfg := gaws.GetConfig()
		api = sdks3.NewFromConfig(cfg, applyClientOptions(cfg, maxRetryAttempts))
	}

	return &Client{
		api:     api,
		bucket:  bucket,
		timeout: timeout,
	}
}

func validate(bucket string, timeout time.Duration, maxRetryAttempts int) error {
	if bucket == "" {
		return fmt.Errorf("%w: bucket is required", gerror.ErrBadRequest)
	}
	if timeout <= 0 {
		return fmt.Errorf("%w: timeout must be greater than 0", gerror.ErrBadRequest)
	}
	if maxRetryAttempts < 1 {
		return fmt.Errorf("%w: maxRetryAttempts must be greater than 0", gerror.ErrBadRequest)
	}
	return nil
}

func applyClientOptions(cfg aws.Config, maxRetryAttempts int) func(*sdks3.Options) {
	return func(opts *sdks3.Options) {
		opts.Retryer = retry.NewStandard(func(standardOptions *retry.StandardOptions) {
			standardOptions.MaxAttempts = maxRetryAttempts
		})
		// LocalStack is served on localhost and needs path-style keys.
		// Any other endpoint keeps virtual-hosted addressing.
		if localEndpoint(aws.ToString(cfg.BaseEndpoint)) {
			opts.UsePathStyle = true
		}
	}
}

func localEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

// Get returns the object at key. A missing key wraps ErrNotFound.
// The caller closes Body. Closing it ends the Get deadline.
func (c *Client) Get(ctx context.Context, key string) (*GetResult, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: key is required", gerror.ErrBadRequest)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	output, err := c.api.GetObject(ctx, &sdks3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		releaseGet(cancel, output)
		var noSuchKey *s3types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("%w: %s", gerror.ErrNotFound, noSuchKey.ErrorMessage())
		}
		return nil, fmt.Errorf("%w: get object %s", err, key)
	}
	if output == nil || output.Body == nil {
		releaseGet(cancel, output)
		return nil, fmt.Errorf("get object %s: empty response", key)
	}

	return &GetResult{
		Body:         &cancelOnClose{ReadCloser: output.Body, cancel: cancel},
		ContentType:  aws.ToString(output.ContentType),
		Size:         aws.ToInt64(output.ContentLength),
		LastModified: aws.ToTime(output.LastModified),
	}, nil
}

func releaseGet(cancel context.CancelFunc, output *sdks3.GetObjectOutput) {
	if output != nil && output.Body != nil {
		_ = output.Body.Close()
	}
	cancel()
}

// Put writes body to key, replacing any existing object. A seekable body lets the SDK set the content length.
func (c *Client) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	if key == "" {
		return fmt.Errorf("%w: key is required", gerror.ErrBadRequest)
	}
	if body == nil {
		return fmt.Errorf("%w: body is required", gerror.ErrBadRequest)
	}

	input := &sdks3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if _, err := c.api.PutObject(ctx, input); err != nil {
		return fmt.Errorf("%w: put object %s", err, key)
	}
	return nil
}

// List returns one page of objects under prefix. An empty prefix lists the bucket.
// Pass the previous NextContinuationToken to read the next page. An empty page is success.
func (c *Client) List(ctx context.Context, prefix, continuationToken string) (*ListResult, error) {
	input := &sdks3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	if continuationToken != "" {
		input.ContinuationToken = aws.String(continuationToken)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := c.api.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("%w: list objects %s", err, prefix)
	}
	if output == nil {
		return nil, fmt.Errorf("list objects %s: empty response", prefix)
	}

	objects := make([]ObjectInfo, 0, len(output.Contents))
	for _, item := range output.Contents {
		objects = append(objects, ObjectInfo{
			Key:          aws.ToString(item.Key),
			Size:         aws.ToInt64(item.Size),
			LastModified: aws.ToTime(item.LastModified),
		})
	}

	return &ListResult{
		Objects:               objects,
		NextContinuationToken: aws.ToString(output.NextContinuationToken),
	}, nil
}

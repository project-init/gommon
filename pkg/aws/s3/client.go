package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
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
// Metadata holds the user metadata set on the object, keyed without the x-amz-meta- prefix.
// ETag is the object's entity tag in the form S3 sends it, surrounding double quotes included,
// for example "9b2cf535f27731c974343645a3985328". Pass it unchanged to WithIfMatch.
type GetResult struct {
	Body         io.ReadCloser
	ContentType  string
	Size         int64
	LastModified time.Time
	Metadata     map[string]string
	ETag         string
}

// PutResult describes the object a Put wrote. ETag has the same quoted form as GetResult.ETag and
// can be passed unchanged to WithIfMatch on the next Put.
type PutResult struct {
	ETag string
}

// PutOption sets an optional field on a Put.
type PutOption func(*sdks3.PutObjectInput)

// WithMetadata stores metadata as the object's user metadata, served back as x-amz-meta-<key>
// headers and returned by Get and Head. S3 lowercases the keys. It replaces any metadata the object
// had before, as Put replaces the whole object.
func WithMetadata(metadata map[string]string) PutOption {
	return func(input *sdks3.PutObjectInput) {
		if len(metadata) == 0 {
			return
		}
		input.Metadata = make(map[string]string, len(metadata))
		for key, value := range metadata {
			input.Metadata[key] = value
		}
	}
}

// WithIfMatch makes the Put write only if the object at the key currently has this ETag, so a writer
// holding an old copy cannot overwrite a newer one. Pass an ETag exactly as returned by Get, Head or
// Put. A mismatch makes Put return an error wrapping ErrPreconditionFailed. If no object exists at
// the key, S3 answers 404 instead and Put returns an error wrapping ErrNotFound. S3 answers 404 for
// a missing bucket as well, so with WithIfMatch a missing bucket also wraps ErrNotFound. Call
// CheckBucket first when the two must be told apart.
// An empty etag makes Put fail with ErrBadRequest instead of writing unconditionally.
func WithIfMatch(etag string) PutOption {
	return func(input *sdks3.PutObjectInput) {
		input.IfMatch = aws.String(etag)
	}
}

// WithIfNoneMatch makes the Put write only if no object exists at the key (If-None-Match: *).
// If one does, Put returns an error wrapping ErrPreconditionFailed.
func WithIfNoneMatch() PutOption {
	return func(input *sdks3.PutObjectInput) {
		input.IfNoneMatch = aws.String("*")
	}
}

// HeadResult is the metadata for one key, read without its body.
// Metadata holds the user metadata set on the object, keyed without the x-amz-meta- prefix.
// ETag has the same quoted form as GetResult.ETag.
type HeadResult struct {
	ContentType  string
	Size         int64
	LastModified time.Time
	Metadata     map[string]string
	ETag         string
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
	HeadObject(ctx context.Context, params *sdks3.HeadObjectInput, optFns ...func(*sdks3.Options)) (*sdks3.HeadObjectOutput, error)
	HeadBucket(ctx context.Context, params *sdks3.HeadBucketInput, optFns ...func(*sdks3.Options)) (*sdks3.HeadBucketOutput, error)
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
		Metadata:     output.Metadata,
		ETag:         aws.ToString(output.ETag),
	}, nil
}

// Head returns the metadata for key without downloading its body. A missing key wraps ErrNotFound.
//
// S3 answers 404 for a missing bucket as well as a missing key, so a missing bucket also wraps
// ErrNotFound here. Call CheckBucket first when the two must be told apart.
func (c *Client) Head(ctx context.Context, key string) (*HeadResult, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: key is required", gerror.ErrBadRequest)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := c.api.HeadObject(ctx, &sdks3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: head object %s", gerror.ErrNotFound, key)
		}
		return nil, fmt.Errorf("%w: head object %s", err, key)
	}
	if output == nil {
		return nil, fmt.Errorf("head object %s: empty response", key)
	}

	return &HeadResult{
		ContentType:  aws.ToString(output.ContentType),
		Size:         aws.ToInt64(output.ContentLength),
		LastModified: aws.ToTime(output.LastModified),
		Metadata:     output.Metadata,
		ETag:         aws.ToString(output.ETag),
	}, nil
}

// CheckBucket confirms the bucket exists and the caller can reach it. A missing bucket wraps
// ErrNotFound.
func (c *Client) CheckBucket(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if _, err := c.api.HeadBucket(ctx, &sdks3.HeadBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: head bucket %s", gerror.ErrNotFound, c.bucket)
		}
		return fmt.Errorf("%w: head bucket %s", err, c.bucket)
	}
	return nil
}

// isNotFound reports a 404. HEAD responses carry no body, so the SDK cannot always build a typed
// NotFound error and the status code is the fallback.
func isNotFound(err error) bool {
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var status interface{ HTTPStatusCode() int }
	return errors.As(err, &status) && status.HTTPStatusCode() == http.StatusNotFound
}

// putErrorSentinel returns the gerror sentinel for the S3 statuses Put maps, or nil for
// any other error. input is the request that failed. It reads the HTTP status from the SDK's response
// error, because the SDK has no typed errors for these refusals on PutObject.
//   - 412: the If-Match or If-None-Match condition does not hold.
//   - 409: S3 reports a conflict, usually another conditional write to the same key in progress.
//   - 404 with If-Match: no object exists at the key, or the bucket is missing; the status cannot tell
//     them apart. Without If-Match, Put creates a missing key, so a 404 can only mean a missing
//     bucket and is left unmapped.
func putErrorSentinel(err error, input *sdks3.PutObjectInput) error {
	var responseErr *awshttp.ResponseError
	if !errors.As(err, &responseErr) {
		return nil
	}
	switch responseErr.HTTPStatusCode() {
	case http.StatusPreconditionFailed:
		return gerror.ErrPreconditionFailed
	case http.StatusConflict:
		return gerror.ErrConflict
	case http.StatusNotFound:
		if input.IfMatch != nil {
			return gerror.ErrNotFound
		}
	}
	return nil
}

func releaseGet(cancel context.CancelFunc, output *sdks3.GetObjectOutput) {
	if output != nil && output.Body != nil {
		_ = output.Body.Close()
	}
	cancel()
}

// Put writes body to key, replacing any existing object, and returns the new object's ETag.
// A seekable body lets the SDK set the content length.
// Pass WithMetadata to store user metadata alongside the object.
//
// Pass WithIfMatch or WithIfNoneMatch, not both, to make the write conditional. Passing both fails
// with ErrBadRequest, as S3 does not support the pair. When S3 refuses a conditional write, Put
// returns an error wrapping one of:
//   - ErrPreconditionFailed (412): the condition does not hold. Another writer changed or created
//     the object, so retrying the same write fails again.
//   - ErrNotFound (404): WithIfMatch was used and no object, or no bucket, exists at the key.
//
// Any 409, S3's conflict status, wraps ErrConflict, whether or not the Put is conditional. On a
// conditional Put it usually means another conditional write to the same key was in progress. For
// WithIfMatch, AWS advises reading the current ETag and trying again; for WithIfNoneMatch, trying
// again.
//
// The client does not retry any of these.
//
// A conditional Put can report ErrPreconditionFailed even though its own write landed. The client
// retries an attempt that failed with a connection error or a transient server error. If S3 stored
// the object on the first attempt but the response was lost, the retry finds the object already
// changed and gets 412. Put cannot tell this apart from a write by someone else. Callers that need
// to know can write a unique token with WithMetadata and read it back with Head.
func (c *Client) Put(ctx context.Context, key string, body io.Reader, contentType string, opts ...PutOption) (*PutResult, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: key is required", gerror.ErrBadRequest)
	}
	if body == nil {
		return nil, fmt.Errorf("%w: body is required", gerror.ErrBadRequest)
	}

	input := &sdks3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	for _, opt := range opts {
		if opt != nil {
			opt(input)
		}
	}
	if input.IfMatch != nil && *input.IfMatch == "" {
		return nil, fmt.Errorf("%w: if-match etag is required", gerror.ErrBadRequest)
	}
	if input.IfMatch != nil && input.IfNoneMatch != nil {
		return nil, fmt.Errorf("%w: if-match and if-none-match cannot be combined", gerror.ErrBadRequest)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := c.api.PutObject(ctx, input)
	if err != nil {
		if sentinel := putErrorSentinel(err, input); sentinel != nil {
			return nil, fmt.Errorf("%w: put object %s: %w", sentinel, key, err)
		}
		return nil, fmt.Errorf("%w: put object %s", err, key)
	}
	if output == nil {
		return nil, fmt.Errorf("put object %s: empty response", key)
	}
	return &PutResult{ETag: aws.ToString(output.ETag)}, nil
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

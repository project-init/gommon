//go:build integration

package s3

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	gerror "github.com/project-init/gommon/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedPrefixRoundTrip(t *testing.T) {
	client, ctx := localClient(t)

	key := "failed/" + uuid.NewString()
	body := []byte(`{"reason":"parquet conversion failed"}`)
	put, err := client.Put(ctx, key, bytes.NewReader(body), "application/json")
	require.NoError(t, err)
	assert.Regexp(t, `^".+"$`, put.ETag, "ETag keeps the quotes S3 sends")

	var found bool
	var size int64
	for _, object := range listAll(t, ctx, client, "failed/") {
		if object.Key == key {
			found = true
			size = object.Size
			assert.False(t, object.LastModified.IsZero())
		}
	}
	require.True(t, found, "listed objects under failed/")
	assert.Equal(t, int64(len(body)), size)

	got, err := client.Get(ctx, key)
	require.NoError(t, err)
	payload, err := io.ReadAll(got.Body)
	require.NoError(t, err)
	require.NoError(t, got.Body.Close())
	assert.Equal(t, body, payload)
	assert.Equal(t, "application/json", got.ContentType)
	assert.Equal(t, int64(len(body)), got.Size)
	assert.False(t, got.LastModified.IsZero())
	assert.Equal(t, put.ETag, got.ETag)

	_, err = client.Get(ctx, "failed/does-not-exist-"+uuid.NewString())
	require.ErrorIs(t, err, gerror.ErrNotFound)
}

func TestListEmptyPageIsSuccess(t *testing.T) {
	client, ctx := localClient(t)

	page, err := client.List(ctx, "missing/"+uuid.NewString()+"/", "")
	require.NoError(t, err)
	assert.Empty(t, page.Objects)
	assert.Empty(t, page.NextContinuationToken)
}

func TestListWholeBucketIncludesObjectsOutsideThePrefix(t *testing.T) {
	client, ctx := localClient(t)

	inside := "failed/" + uuid.NewString()
	outside := "other/" + uuid.NewString()
	body := []byte("record")
	put(t, ctx, client, inside, body)
	put(t, ctx, client, outside, body)

	var sawInside, sawOutside bool
	for _, object := range listAll(t, ctx, client, "") {
		switch object.Key {
		case inside:
			sawInside = true
		case outside:
			sawOutside = true
		}
	}
	assert.True(t, sawInside)
	assert.True(t, sawOutside)

	sawInside, sawOutside = false, false
	for _, object := range listAll(t, ctx, client, "failed/") {
		switch object.Key {
		case inside:
			sawInside = true
		case outside:
			sawOutside = true
		}
	}
	assert.True(t, sawInside)
	assert.False(t, sawOutside)
}

func TestMissingBucketIsNotNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	client, err := New("gommon-s3-missing-"+uuid.NewString(), 5*time.Second, 3)
	require.NoError(t, err)

	key := "failed/" + uuid.NewString()
	_, err = client.Get(ctx, key)
	require.Error(t, err)
	require.NotErrorIs(t, err, gerror.ErrNotFound)

	_, err = client.Put(ctx, key, bytes.NewReader([]byte("record")), "")
	require.Error(t, err)
	require.NotErrorIs(t, err, gerror.ErrNotFound)
	require.ErrorContains(t, err, "put object "+key)
}

func TestHeadReturnsMetadataWithoutBody(t *testing.T) {
	client, ctx := localClient(t)

	key := "head/" + uuid.NewString()
	body := []byte(`{"id":"SAL-1"}`)
	written, err := client.Put(ctx, key, bytes.NewReader(body), "application/json")
	require.NoError(t, err)

	head, err := client.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, written.ETag, head.ETag)
	assert.Equal(t, "application/json", head.ContentType)
	assert.Equal(t, int64(len(body)), head.Size)
	assert.False(t, head.LastModified.IsZero())

	_, err = client.Head(ctx, "head/does-not-exist-"+uuid.NewString())
	require.ErrorIs(t, err, gerror.ErrNotFound)
}

func TestPutMetadataRoundTrip(t *testing.T) {
	client, ctx := localClient(t)

	key := "metadata/" + uuid.NewString()
	updated := "2026-11-17T14:30:00Z"
	_, err := client.Put(ctx, key, bytes.NewReader([]byte(`{}`)), "application/json",
		WithMetadata(map[string]string{"Jira-Updated": updated}))
	require.NoError(t, err)

	head, err := client.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"jira-updated": updated}, head.Metadata)

	got, err := client.Get(ctx, key)
	require.NoError(t, err)
	require.NoError(t, got.Body.Close())
	assert.Equal(t, map[string]string{"jira-updated": updated}, got.Metadata)

	// A second Put without metadata replaces the object and drops the old metadata.
	_, err = client.Put(ctx, key, bytes.NewReader([]byte(`{}`)), "application/json")
	require.NoError(t, err)
	head, err = client.Head(ctx, key)
	require.NoError(t, err)
	assert.Empty(t, head.Metadata)
}

func TestPutIfNoneMatchOnlyCreates(t *testing.T) {
	client, ctx := localClient(t)

	key := "conditional/" + uuid.NewString()
	first, err := client.Put(ctx, key, bytes.NewReader([]byte("first")), "", WithIfNoneMatch())
	require.NoError(t, err)

	_, err = client.Put(ctx, key, bytes.NewReader([]byte("second")), "", WithIfNoneMatch())
	require.ErrorIs(t, err, gerror.ErrPreconditionFailed)
	require.ErrorContains(t, err, "put object "+key)

	head, err := client.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, first.ETag, head.ETag)
}

func TestPutIfMatchRejectsStaleETag(t *testing.T) {
	client, ctx := localClient(t)

	key := "conditional/" + uuid.NewString()
	stale := put(t, ctx, client, key, []byte("v1")).ETag
	current := put(t, ctx, client, key, []byte("v2")).ETag
	require.NotEqual(t, stale, current)

	_, err := client.Put(ctx, key, bytes.NewReader([]byte("v3")), "", WithIfMatch(stale))
	require.ErrorIs(t, err, gerror.ErrPreconditionFailed)

	head, err := client.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, current, head.ETag)
}

func TestPutIfMatchOnMissingKeyFails(t *testing.T) {
	client, ctx := localClient(t)

	key := "conditional/" + uuid.NewString()
	_, err := client.Put(ctx, key, bytes.NewReader([]byte("v1")), "", WithIfMatch(`"9b2cf535f27731c974343645a3985328"`))
	// S3 answers 404 NoSuchKey here, not 412.
	require.ErrorIs(t, err, gerror.ErrNotFound)
	require.NotErrorIs(t, err, gerror.ErrPreconditionFailed)

	_, err = client.Head(ctx, key)
	require.ErrorIs(t, err, gerror.ErrNotFound)
}

func TestETagsFromPutGetAndHeadWorkWithIfMatch(t *testing.T) {
	client, ctx := localClient(t)

	key := "conditional/" + uuid.NewString()
	fromPut := put(t, ctx, client, key, []byte("v1")).ETag

	fromPut2, err := client.Put(ctx, key, bytes.NewReader([]byte("v2")), "", WithIfMatch(fromPut))
	require.NoError(t, err)

	got, err := client.Get(ctx, key)
	require.NoError(t, err)
	require.NoError(t, got.Body.Close())
	assert.Equal(t, fromPut2.ETag, got.ETag)

	fromGet, err := client.Put(ctx, key, bytes.NewReader([]byte("v3")), "", WithIfMatch(got.ETag))
	require.NoError(t, err)

	head, err := client.Head(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, fromGet.ETag, head.ETag)

	_, err = client.Put(ctx, key, bytes.NewReader([]byte("v4")), "", WithIfMatch(head.ETag))
	require.NoError(t, err)

	got, err = client.Get(ctx, key)
	require.NoError(t, err)
	payload, err := io.ReadAll(got.Body)
	require.NoError(t, err)
	require.NoError(t, got.Body.Close())
	assert.Equal(t, []byte("v4"), payload)
}

func TestCheckBucketTellsMissingBucketFromMissingKey(t *testing.T) {
	client, ctx := localClient(t)
	require.NoError(t, client.CheckBucket(ctx))

	missing, err := New("gommon-s3-missing-"+uuid.NewString(), 5*time.Second, 1)
	require.NoError(t, err)
	require.ErrorIs(t, missing.CheckBucket(ctx), gerror.ErrNotFound)
}

func localClient(t *testing.T) (*Client, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	client, err := New("gommon-s3-dev", 5*time.Second, 3)
	require.NoError(t, err)
	return client, ctx
}

func put(t *testing.T, ctx context.Context, client *Client, key string, body []byte) *PutResult {
	t.Helper()

	result, err := client.Put(ctx, key, bytes.NewReader(body), "")
	require.NoError(t, err)
	return result
}

func listAll(t *testing.T, ctx context.Context, client *Client, prefix string) []ObjectInfo {
	t.Helper()

	var objects []ObjectInfo
	token := ""
	seen := map[string]struct{}{"": {}}
	for {
		page, err := client.List(ctx, prefix, token)
		require.NoError(t, err)
		objects = append(objects, page.Objects...)
		if page.NextContinuationToken == "" {
			return objects
		}
		if _, ok := seen[page.NextContinuationToken]; ok {
			return objects
		}
		seen[page.NextContinuationToken] = struct{}{}
		token = page.NextContinuationToken
	}
}

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
	err := client.Put(ctx, key, bytes.NewReader(body), "application/json")
	require.NoError(t, err)

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
	require.NoError(t, client.Put(ctx, inside, bytes.NewReader(body), ""))
	require.NoError(t, client.Put(ctx, outside, bytes.NewReader(body), ""))

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

	err = client.Put(ctx, key, bytes.NewReader([]byte("record")), "")
	require.Error(t, err)
	require.NotErrorIs(t, err, gerror.ErrNotFound)
	require.ErrorContains(t, err, "put object "+key)
}

func localClient(t *testing.T) (*Client, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	client, err := New("gommon-s3-dev", 5*time.Second, 3)
	require.NoError(t, err)
	return client, ctx
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

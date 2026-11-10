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
	const bucket = "gommon-s3-dev"

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	client, err := New(bucket, 5*time.Second, 3)
	require.NoError(t, err)

	key := "failed/" + uuid.NewString()
	body := []byte(`{"reason":"parquet conversion failed"}`)
	err = client.Put(ctx, key, bytes.NewReader(body), "application/json")
	require.NoError(t, err)

	var found bool
	var size int64
	token := ""
	seen := map[string]struct{}{"": {}}
	for {
		page, err := client.List(ctx, "failed/", token)
		require.NoError(t, err)
		for _, object := range page.Objects {
			if object.Key == key {
				found = true
				size = object.Size
			}
		}
		if found || page.NextContinuationToken == "" {
			break
		}
		if _, ok := seen[page.NextContinuationToken]; ok {
			break
		}
		seen[page.NextContinuationToken] = struct{}{}
		token = page.NextContinuationToken
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

	_, err = client.Get(ctx, "failed/does-not-exist-"+uuid.NewString())
	require.ErrorIs(t, err, gerror.ErrNotFound)
}

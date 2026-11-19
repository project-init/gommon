package jiraclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrAttachmentTooLarge means an attachment's content is longer than the limit the caller passed.
var ErrAttachmentTooLarge = errors.New("attachment too large")

// AttachmentContent is a downloaded attachment.
type AttachmentContent struct {
	Body        []byte
	ContentType string
}

// DownloadAttachment returns the content of the attachment with attachmentID, retrying as Do does.
// It asks Jira to serve the content itself rather than redirect to the media service, so the
// client's credentials are only ever sent to Jira. Content longer than maxBytes is not read past
// the limit and returns ErrAttachmentTooLarge.
func (c *Client) DownloadAttachment(ctx context.Context, attachmentID string, maxBytes int64) (*AttachmentContent, error) {
	id, err := pathSegment(attachmentID)
	if err != nil {
		return nil, err
	}
	request, err := c.NewRequest(ctx, http.MethodGet, "/rest/api/3/attachment/content/"+id+"?redirect=false", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "*/*")

	response, err := c.send(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, finish(response, nil)
	}
	defer func() { _ = response.Body.Close() }()

	if response.ContentLength > maxBytes {
		return nil, fmt.Errorf("%w: attachment %s is %d bytes, over %d", ErrAttachmentTooLarge, attachmentID, response.ContentLength, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment %s: %w", attachmentID, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: attachment %s is over %d bytes", ErrAttachmentTooLarge, attachmentID, maxBytes)
	}
	return &AttachmentContent{Body: body, ContentType: response.Header.Get("Content-Type")}, nil
}

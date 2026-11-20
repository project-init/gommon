package jiraclient

import (
	"context"
	"fmt"
	"net/http"
)

// Comment is a comment Jira created on an issue.
type Comment struct {
	ID string `json:"id"`
}

// AddSimpleComment declares the comment as an ADFDescription with no acceptance criteria or document url. Basically the
// simplest wrapper a client can have to add a comment to a ticket.
func (c *Client) AddSimpleComment(ctx context.Context, issueKey string, simpleComment string) (*Comment, error) {
	if len(simpleComment) == 0 {
		return nil, fmt.Errorf("comment on issue %s can't be empty", issueKey)
	}
	return c.AddComment(ctx, issueKey, ADFDescription(simpleComment, nil, ""))
}

// AddComment posts a comment on an issue. The body is an Atlassian Document Format document, such
// as ADFDescription(markdown, nil, "") returns for Markdown text.
func (c *Client) AddComment(ctx context.Context, issueKey string, body map[string]any) (*Comment, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("comment on issue %s needs a body", issueKey)
	}
	key, err := pathSegment(issueKey)
	if err != nil {
		return nil, err
	}
	req, err := c.NewRequest(ctx, http.MethodPost, "/rest/api/3/issue/"+key+"/comment", map[string]any{"body": body})
	if err != nil {
		return nil, err
	}

	var comment Comment
	if err := c.Do(req, &comment); err != nil {
		return nil, err
	}

	return &comment, nil
}

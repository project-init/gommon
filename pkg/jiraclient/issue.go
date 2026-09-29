package jiraclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// IssueResponse is a basic representation of a created / fetched Jira issue
type IssueResponse struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// SearchResult is a response from JQL search
type SearchResult struct {
	Issues        []IssueResponse `json:"issues"`
	NextPageToken string
	IsLast        bool
}

// SearchJQL executes a JQL query and returns SearchResult
func (c *Client) SearchJQL(ctx context.Context, jql string, maxResults int, nextPageToken string) (*SearchResult, error) {
	if maxResults > 0 {
		// 100 matches the old behavior.
		maxResults = 100
	}
	page, err := c.SearchIssues(ctx, IssueSearch{
		JQL:           jql,
		Fields:        []string{"key"},
		MaxResults:    maxResults,
		NextPageToken: nextPageToken,
	})
	if err != nil {
		return nil, err
	}
	result := &SearchResult{NextPageToken: page.NextPageToken, IsLast: page.IsLast}
	for _, issue := range page.Issues {
		result.Issues = append(result.Issues, IssueResponse{ID: issue.ID, Key: issue.Key})
	}

	return result, nil
}

// GetIssueProperty retrieves the id stored in an issue property's value.
func (c *Client) GetIssueProperty(ctx context.Context, issueKey, propertyKey string) (string, error) {
	path, err := propertyPath(issueKey, propertyKey)
	if err != nil {
		return "", err
	}
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}

	var property struct {
		Value struct {
			ID string `json:"id"`
		} `json:"value"`
	}
	if err := c.Do(req, &property); err != nil {
		return "", err
	}

	return property.Value.ID, nil
}

// CreateIssue creates a new issue in Jira
func (c *Client) CreateIssue(ctx context.Context, body map[string]any) (*IssueResponse, error) {
	req, err := c.NewRequest(ctx, "POST", "/rest/api/3/issue", body)
	if err != nil {
		return nil, err
	}

	var response IssueResponse
	if err := c.Do(req, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CreateIssueLink creates a link between two issues
func (c *Client) CreateIssueLink(ctx context.Context, linkType, inwardIssueKey, outwardIssueKey string) error {
	body := map[string]any{
		"type":         map[string]string{"name": linkType},
		"inwardIssue":  map[string]string{"key": inwardIssueKey},
		"outwardIssue": map[string]string{"key": outwardIssueKey},
	}

	req, err := c.NewRequest(ctx, "POST", "/rest/api/3/issueLink", body)
	if err != nil {
		return err
	}

	return c.Do(req, nil)
}

// IssueLink is an issue link seen from the issue it was listed on. Exactly one of InwardIssue and
// OutwardIssue names the other issue; for a "Blocks" link listed on the blocked issue, InwardIssue
// is the blocker.
type IssueLink struct {
	ID           string
	Type         string
	InwardIssue  string
	OutwardIssue string
}

// GetIssueLinks returns the links on an issue whose other end is inward, such as its blockers.
func (c *Client) GetIssueLinks(ctx context.Context, issueKey string) ([]IssueLink, error) {
	all, err := c.ListIssueLinks(ctx, issueKey)
	if err != nil {
		return nil, err
	}

	var links []IssueLink
	for _, link := range all {
		if link.InwardIssue != "" {
			links = append(links, link)
		}
	}

	return links, nil
}

// GetIssueLinkTypes fetches all valid link types on the Jira instance
func (c *Client) GetIssueLinkTypes(ctx context.Context) ([]string, error) {
	req, err := c.NewRequest(ctx, "GET", "/rest/api/3/issueLinkType", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		IssueLinkTypes []struct {
			Name string `json:"name"`
		} `json:"issueLinkTypes"`
	}
	if err := c.Do(req, &response); err != nil {
		return nil, err
	}
	var names []string
	for _, t := range response.IssueLinkTypes {
		names = append(names, t.Name)
	}
	return names, nil
}

// Issue is a fetched Jira issue. Fields and Properties hold each returned value undecoded, so
// callers decode only what they asked for.
type Issue struct {
	ID         string                     `json:"id"`
	Key        string                     `json:"key"`
	Fields     map[string]json.RawMessage `json:"fields"`
	Properties map[string]json.RawMessage `json:"properties"`
}

// IssueSearch selects the issues SearchIssues returns and what each carries.
type IssueSearch struct {
	JQL           string
	Fields        []string
	Properties    []string
	MaxResults    int
	NextPageToken string
}

// IssueSearchResult is one page of SearchIssues results.
type IssueSearchResult struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"nextPageToken"`
	IsLast        bool    `json:"isLast"`
}

// SearchIssues runs a JQL search and returns each issue with the requested fields and issue
// properties, so a caller reads a page of issues in one request.
func (c *Client) SearchIssues(ctx context.Context, search IssueSearch) (*IssueSearchResult, error) {
	query := url.Values{}
	query.Set("jql", search.JQL)
	if len(search.Fields) > 0 {
		query.Set("fields", strings.Join(search.Fields, ","))
	}
	if len(search.Properties) > 0 {
		query.Set("properties", strings.Join(search.Properties, ","))
	}
	if search.MaxResults > 0 {
		query.Set("maxResults", strconv.Itoa(search.MaxResults))
	}
	if search.NextPageToken != "" {
		query.Set("nextPageToken", search.NextPageToken)
	}
	req, err := c.NewRequest(ctx, http.MethodGet, "/rest/api/3/search/jql?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var result IssueSearchResult
	if err := c.Do(req, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Links decodes the issue's issuelinks field, which must have been requested.
func (i *Issue) Links() ([]IssueLink, error) {
	var raw []struct {
		ID   string `json:"id"`
		Type struct {
			Name string `json:"name"`
		} `json:"type"`
		InwardIssue struct {
			Key string `json:"key"`
		} `json:"inwardIssue"`
		OutwardIssue struct {
			Key string `json:"key"`
		} `json:"outwardIssue"`
	}
	if encoded, exists := i.Fields["issuelinks"]; exists {
		if err := json.Unmarshal(encoded, &raw); err != nil {
			return nil, fmt.Errorf("decode links of %s: %w", i.Key, err)
		}
	}

	links := make([]IssueLink, 0, len(raw))
	for _, link := range raw {
		links = append(links, IssueLink{
			ID:           link.ID,
			Type:         link.Type.Name,
			InwardIssue:  link.InwardIssue.Key,
			OutwardIssue: link.OutwardIssue.Key,
		})
	}

	return links, nil
}

// GetIssue fetches an issue with the named fields and issue properties, or with every navigable
// field when no fields are named.
func (c *Client) GetIssue(ctx context.Context, issueKey string, fields []string, properties []string) (*Issue, error) {
	key, err := pathSegment(issueKey)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if len(fields) > 0 {
		query.Set("fields", strings.Join(fields, ","))
	}
	if len(properties) > 0 {
		query.Set("properties", strings.Join(properties, ","))
	}
	path := "/rest/api/3/issue/" + key
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var issue Issue
	if err := c.Do(req, &issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

// IssueUpdate describes an edit to one issue.
type IssueUpdate struct {
	// Fields replaces each named field's value.
	Fields map[string]any
	// Update applies Jira's per-field operations, such as {"labels": [{"add": "x"}]}.
	Update map[string]any
	// Properties sets each named issue property in the same request as the edit.
	Properties map[string]any
}

// UpdateIssue edits an issue. An empty update sends nothing.
func (c *Client) UpdateIssue(ctx context.Context, issueKey string, edit IssueUpdate) error {
	if len(edit.Fields) == 0 && len(edit.Update) == 0 && len(edit.Properties) == 0 {
		return nil
	}
	key, err := pathSegment(issueKey)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if len(edit.Fields) > 0 {
		body["fields"] = edit.Fields
	}
	if len(edit.Update) > 0 {
		body["update"] = edit.Update
	}
	if len(edit.Properties) > 0 {
		properties := make([]map[string]any, 0, len(edit.Properties))
		for name, value := range edit.Properties {
			properties = append(properties, map[string]any{"key": name, "value": value})
		}
		sort.Slice(properties, func(i, j int) bool {
			return properties[i]["key"].(string) < properties[j]["key"].(string)
		})
		body["properties"] = properties
	}
	req, err := c.NewRequest(ctx, http.MethodPut, "/rest/api/3/issue/"+key, body)
	if err != nil {
		return err
	}

	return c.Do(req, nil)
}

// SetIssueProperty stores value under propertyKey on an issue, replacing any existing value.
func (c *Client) SetIssueProperty(ctx context.Context, issueKey, propertyKey string, value any) error {
	if value == nil {
		return fmt.Errorf("issue %s property %s needs a value", issueKey, propertyKey)
	}
	path, err := propertyPath(issueKey, propertyKey)
	if err != nil {
		return err
	}
	req, err := c.NewRequest(ctx, http.MethodPut, path, value)
	if err != nil {
		return err
	}

	return c.Do(req, nil)
}

// ListIssueLinks returns every link on an issue, inward and outward, with the ID DeleteIssueLink
// needs.
func (c *Client) ListIssueLinks(ctx context.Context, issueKey string) ([]IssueLink, error) {
	issue, err := c.GetIssue(ctx, issueKey, []string{"issuelinks"}, nil)
	if err != nil {
		return nil, err
	}

	return issue.Links()
}

// DeleteIssueLink removes an issue link by ID.
func (c *Client) DeleteIssueLink(ctx context.Context, linkID string) error {
	id, err := pathSegment(linkID)
	if err != nil {
		return err
	}
	req, err := c.NewRequest(ctx, http.MethodDelete, "/rest/api/3/issueLink/"+id, nil)
	if err != nil {
		return err
	}

	return c.Do(req, nil)
}

func propertyPath(issueKey, propertyKey string) (string, error) {
	key, err := pathSegment(issueKey)
	if err != nil {
		return "", err
	}
	property, err := pathSegment(propertyKey)
	if err != nil {
		return "", err
	}

	return "/rest/api/3/issue/" + key + "/properties/" + property, nil
}

// pathSegment escapes value for one URL path segment. PathEscape leaves dots alone, and a proxy
// that normalizes dot segments would turn "." or ".." into a different endpoint.
func pathSegment(value string) (string, error) {
	if value == "" || value == "." || value == ".." {
		return "", fmt.Errorf("invalid Jira path segment %q", value)
	}

	return url.PathEscape(value), nil
}

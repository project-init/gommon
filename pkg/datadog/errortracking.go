package datadog

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// GitMetadataBias is the sample bias whose ErrorStack carries parsed frames.
// Asking for any other bias returns a sample with no stack trace, and Datadog
// ignores a bias it does not recognise rather than rejecting the request, so a
// typo here reads as an error with no stack rather than as a bad request.
const GitMetadataBias = "git_metadata"

// SampleResourceType is the type of an included error sample. Note it is not
// "issue_sample": the API returns error_tracking_issue_sample.
const SampleResourceType = "error_tracking_issue_sample"

// IssueResponse is a GET of one Error Tracking issue. The payload is JSON:API
// shaped: Data holds the issue, and Included holds the resources asked for with
// the include parameter, matched back by type.
type IssueResponse struct {
	Data     Issue              `json:"data"`
	Included []IncludedResource `json:"included"`
}

// Issue is a group of similar errors, fingerprinted by Datadog from the error
// type, message and stack.
type Issue struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Attributes IssueAttributes `json:"attributes"`
}

// IssueAttributes describes the group. It carries no environment and no line
// number; both of those come from a sample.
type IssueAttributes struct {
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`
	Service      string `json:"service"`
	State        string `json:"state"`
	Platform     string `json:"platform"`
	IsCrash      bool   `json:"is_crash"`

	Languages []string `json:"languages"`

	// FilePath is where the error was recorded as the build saw it, so it can
	// carry a container prefix such as /app rather than a repository path. A
	// frame's GitFileLocation is repository-relative and more useful.
	FilePath     string `json:"file_path"`
	FunctionName string `json:"function_name"`

	// FirstSeen and LastSeen are epoch milliseconds. A sample's Timestamp is
	// RFC 3339 instead, so the two do not decode the same way.
	FirstSeen int64 `json:"first_seen"`
	LastSeen  int64 `json:"last_seen"`

	// The versions are whatever the service tags builds with, which can be a
	// release tag such as v1.1.14 rather than a commit. Only a frame's
	// GitFileLocation.Ref or SampleAttributes.GitCommitSHA pins a commit.
	FirstSeenVersion string `json:"first_seen_version"`
	LastSeenVersion  string `json:"last_seen_version"`
}

// IncludedResource is one resource from the included array. Match on Type
// before reading Attributes: the array is heterogeneous.
type IncludedResource struct {
	Type       string           `json:"type"`
	ID         string           `json:"id"`
	Attributes SampleAttributes `json:"attributes"`
}

// SampleAttributes is one error occurrence picked out of the group.
type SampleAttributes struct {
	Biases     []string `json:"biases"`
	DataSource string   `json:"data_source"`
	Language   string   `json:"language"`
	Service    string   `json:"service"`
	Version    string   `json:"version"`

	// TraceID is the decimal low half of the trace id. Event.TraceID carries
	// the full hex form.
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`

	Timestamp time.Time `json:"timestamp"`

	// GitCommitSHA and GitRepositoryURL arrive only when Source Code
	// Integration is configured for the service. Without it they are absent
	// and ErrorStack is an empty object, so no source link can be built.
	GitCommitSHA     string `json:"git_commit_sha"`
	GitRepositoryURL string `json:"git_repository_url"`
	SessionReplayURL string `json:"session_replay_url"`

	ErrorStack ErrorStack  `json:"error_stack"`
	Event      SampleEvent `json:"event"`
}

// HasBias reports whether this sample was picked by the named bias.
func (s SampleAttributes) HasBias(bias string) bool {
	return slices.Contains(s.Biases, bias)
}

// ErrorStack is the parsed stack of a sample. Frames is empty unless the sample
// was picked by GitMetadataBias and Source Code Integration is configured.
type ErrorStack struct {
	Language string       `json:"language"`
	Frames   []StackFrame `json:"frames"`
}

// StackFrame is one frame of a parsed stack, outermost last.
type StackFrame struct {
	Text            string           `json:"text"`
	CodeSnippet     *CodeSnippet     `json:"code_snippet"`
	GitFileLocation *GitFileLocation `json:"git_file_location"`
}

// CodeSnippet is source around a frame. Offset is the line number of Lines[0],
// so a renderer can number the block correctly.
type CodeSnippet struct {
	Lines  []string `json:"lines"`
	Offset int      `json:"offset"`
}

// GitFileLocation is resolved per frame, so a frame inside a vendored
// dependency reports that dependency's repository and ref rather than the
// service's.
type GitFileLocation struct {
	FilePath  string `json:"filepath"`
	FirstLine *int   `json:"first_line"`
	LastLine  *int   `json:"last_line"`
	Column    *int   `json:"column"`
	Ref       string `json:"ref"`

	// RepoID is host and path with no scheme, e.g. github.com/org/repo.
	RepoID string `json:"repo_id"`
}

// SampleEvent is the raw event behind a sample. It holds things the typed
// attributes leave out, the environment and the endpoint among them.
type SampleEvent struct {
	Env          string `json:"env"`
	Service      string `json:"service"`
	ResourceName string `json:"resource_name"`
	Status       string `json:"status"`
	Version      string `json:"version"`

	// TraceID is the full hex id, unlike SampleAttributes.TraceID.
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`

	// Root is the entry point of the trace the error occurred in, which names
	// the endpoint rather than the failing call.
	Root struct {
		ResourceName string `json:"resource_name"`
		Service      string `json:"service"`
	} `json:"root"`

	Error struct {
		File string `json:"file"`
		Type string `json:"type"`

		// Stack is the throwing stack and HandlingStack is where the error was
		// recorded. dd-trace-go moved the handling stack to its own field in
		// v2.7.0, so a service on a newer tracer populates HandlingStack and
		// may leave Stack empty. Neither is parsed into frames.
		Stack         string `json:"stack"`
		HandlingStack string `json:"handling_stack"`

		Message string `json:"message"`
	} `json:"error"`
}

// GetIssue reads one Error Tracking issue.
//
// Each bias asks for the sample that bias picks; passing none asks for the
// issue alone, which is a smaller response when the stack is not wanted. Only
// GitMetadataBias carries a parsed stack, so that is usually the one to pass.
func (c *Client) GetIssue(ctx context.Context, issueID string, biases ...string) (IssueResponse, error) {
	req, err := c.NewRequest(ctx, http.MethodGet, issuePath(issueID, biases), nil)
	if err != nil {
		return IssueResponse{}, err
	}

	var resp IssueResponse
	if err := c.Do(req, &resp); err != nil {
		return IssueResponse{}, err
	}

	return resp, nil
}

// issuePath builds the endpoint and its query.
//
// url.Values rather than concatenation for two reasons: the issue id usually
// comes from a URL a person pasted, and the bias filter's key contains
// brackets. Encode percent-encodes them to filter%5Bsamples.biases%5D, which
// Datadog honours.
func issuePath(issueID string, biases []string) string {
	path := "/api/v2/error-tracking/issues/" + url.PathEscape(issueID)
	if len(biases) == 0 {
		return path
	}

	q := url.Values{
		"include":                {"samples"},
		"filter[samples.biases]": {strings.Join(biases, ",")},
	}

	return path + "?" + q.Encode()
}

// Sample returns the sample to read an occurrence from: the one picked by
// preferBias, falling back to any sample so an issue whose service has no
// Source Code Integration still yields an environment and a trace. The bool
// reports whether a sample was found at all.
func (r IssueResponse) Sample(preferBias string) (SampleAttributes, bool) {
	var fallback SampleAttributes
	found := false

	for _, inc := range r.Included {
		if inc.Type != SampleResourceType {
			continue
		}
		if inc.Attributes.HasBias(preferBias) {
			return inc.Attributes, true
		}
		if !found {
			fallback, found = inc.Attributes, true
		}
	}

	return fallback, found
}

package datadog

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// issueBody is a trimmed real response: an issue whose service has no Source
// Code Integration, so ErrorStack is an empty object and there are no git
// fields. That is what the API returns until the integration is configured, so
// it is the shape worth decoding in a test.
const issueBody = `{
  "data": {
    "id": "aae10c1c-dacd-11f1-a39f-da7ad0900002",
    "type": "issue",
    "attributes": {
      "error_type": "*errors.errorString",
      "error_message": "HTTP 503: undefined response type",
      "service": "login-ui-app",
      "state": "OPEN",
      "platform": "BACKEND",
      "languages": ["GO"],
      "file_path": "/app/pkg/kratos/admin.go",
      "function_name": "pkg/kratos.endSpan",
      "first_seen": 1794071227024,
      "last_seen": 1794071227024,
      "first_seen_version": "v1.1.14",
      "last_seen_version": "v1.1.14"
    }
  },
  "included": [
    {
      "id": "sample-id",
      "type": "error_tracking_issue_sample",
      "attributes": {
        "biases": ["git_metadata"],
        "data_source": "APM",
        "language": "go",
        "service": "login-ui-app",
        "version": "v1.1.14",
        "trace_id": "7034400712117581761",
        "timestamp": "2026-11-07T17:07:07.024Z",
        "error_stack": {},
        "event": {
          "env": "production",
          "trace_id": "1039f7bfdfc3e342619f362d8585f7c1",
          "resource_name": "get_identity",
          "root": { "resource_name": "POST /webhook/registration" },
          "error": { "handling_stack": "pkg/kratos.endSpan\n\t/app/pkg/kratos/admin.go:118" }
        }
      }
    }
  ]
}`

// TestGetIssueRequest pins the query, which is the part that fails quietly.
// Datadog ignores a bias it does not recognise rather than rejecting the
// request, so a wrong filter returns samples with no frames — which reads as an
// error with no stack trace rather than as a malformed request.
func TestGetIssueRequest(t *testing.T) {
	const issueID = "aae10c1c-dacd-11f1-a39f-da7ad0900002"

	var gotPath, gotQuery string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(issueBody))
	})

	if _, err := client.GetIssue(context.Background(), issueID, GitMetadataBias); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	if want := "/api/v2/error-tracking/issues/" + issueID; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}

	q, err := url.ParseQuery(gotQuery)
	if err != nil {
		t.Fatalf("query does not parse: %v", err)
	}
	if q.Get("include") != "samples" {
		t.Errorf("include = %q, want samples", q.Get("include"))
	}
	if q.Get("filter[samples.biases]") != GitMetadataBias {
		t.Errorf("bias filter = %q, want %q", q.Get("filter[samples.biases]"), GitMetadataBias)
	}
	// Percent-encoded brackets are what Datadog receives, and it honours them.
	if !strings.Contains(gotQuery, "filter%5Bsamples.biases%5D") {
		t.Errorf("query = %q, want the brackets percent-encoded", gotQuery)
	}
}

// TestGetIssueWithoutBiasesAsksForNoSamples keeps the cheap call cheap: with no
// bias there is nothing to include, so the query is omitted entirely.
func TestGetIssueWithoutBiasesAsksForNoSamples(t *testing.T) {
	var gotQuery string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(issueBody))
	})

	if _, err := client.GetIssue(context.Background(), "an-id"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want none", gotQuery)
	}
}

// TestGetIssueEscapesTheID matters because the id is usually pulled out of a
// URL a person pasted.
func TestGetIssueEscapesTheID(t *testing.T) {
	var gotPath string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(issueBody))
	})

	if _, err := client.GetIssue(context.Background(), "../../admin/secrets"); err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if !strings.HasPrefix(gotPath, "/api/v2/error-tracking/issues/") {
		t.Errorf("path = %q, want the endpoint prefix intact", gotPath)
	}
}

func TestGetIssueDecodes(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(issueBody))
	})

	resp, err := client.GetIssue(context.Background(), "an-id", GitMetadataBias)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}

	attrs := resp.Data.Attributes
	if attrs.Service != "login-ui-app" || attrs.State != "OPEN" {
		t.Errorf("attributes = %+v", attrs)
	}
	// Epoch milliseconds on the issue, RFC 3339 on the sample.
	if attrs.FirstSeen != 1794071227024 {
		t.Errorf("FirstSeen = %d", attrs.FirstSeen)
	}

	sample, ok := resp.Sample(GitMetadataBias)
	if !ok {
		t.Fatal("Sample: no sample found")
	}
	if sample.Timestamp.IsZero() {
		t.Error("sample Timestamp did not decode")
	}
	// The environment lives only on the raw event; the typed attributes have
	// no such field on either the issue or the sample.
	if sample.Event.Env != "production" {
		t.Errorf("Event.Env = %q, want production", sample.Event.Env)
	}
	// No Source Code Integration, so no frames and no git fields.
	if len(sample.ErrorStack.Frames) != 0 || sample.GitCommitSHA != "" {
		t.Errorf("expected no frames and no commit: %+v", sample.ErrorStack)
	}
}

// TestSample covers which sample an occurrence is read from. Only the
// git_metadata one can carry frames, so it has to win even when another comes
// back first — and any sample is better than none, since the environment and
// the trace are on all of them.
func TestSample(t *testing.T) {
	sample := func(bias, service string) IncludedResource {
		return IncludedResource{
			Type:       SampleResourceType,
			Attributes: SampleAttributes{Biases: []string{bias}, Service: service},
		}
	}

	tests := map[string]struct {
		included    []IncludedResource
		wantService string
		wantFound   bool
	}{
		"prefers the biased sample even when it is not first": {
			included:    []IncludedResource{sample("latest", "other"), sample(GitMetadataBias, "wanted")},
			wantService: "wanted", wantFound: true,
		},
		"falls back to any sample": {
			included:    []IncludedResource{sample("latest", "other")},
			wantService: "other", wantFound: true,
		},
		"ignores resources that are not samples": {
			included: []IncludedResource{{Type: "error_tracking_issue_case"}},
		},
		"nothing included": {},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, found := IssueResponse{Included: tt.included}.Sample(GitMetadataBias)

			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if got.Service != tt.wantService {
				t.Errorf("Service = %q, want %q", got.Service, tt.wantService)
			}
		})
	}
}

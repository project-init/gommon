package jiraclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gerror "github.com/project-init/gommon/pkg/errors"
)

type recordedRequest struct {
	method string
	path   string
	query  string
	body   string
}

// newTestClient serves every request with respond and records what arrived.
func newTestClient(t *testing.T, respond func(http.ResponseWriter, *http.Request)) (*Client, *[]recordedRequest) {
	t.Helper()
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, recordedRequest{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.RawQuery, body: string(body)})
		respond(w, r)
	}))
	t.Cleanup(server.Close)

	return NewClient(server.Client(), server.URL, "user@example.com", "token"), &requests
}

func TestGetIssueRequestsOnlyNamedFieldsAndProperties(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"10","key":"INIT-1","fields":{"summary":"Title"}}`))
	})

	issue, err := client.GetIssue(context.Background(), "INIT-1", []string{"summary", "description"}, []string{"devex"})
	if err != nil {
		t.Fatal(err)
	}

	if got := (*requests)[0]; got.method != http.MethodGet || got.path != "/rest/api/3/issue/INIT-1" || got.query != "fields=summary%2Cdescription&properties=devex" {
		t.Fatalf("request = %+v", got)
	}
	var summary string
	if err := json.Unmarshal(issue.Fields["summary"], &summary); err != nil || summary != "Title" || issue.Key != "INIT-1" {
		t.Fatalf("issue = %+v, summary %q, err %v", issue, summary, err)
	}
}

func TestUpdateIssueSendsOnlyNonEmptySections(t *testing.T) {
	tests := []struct {
		name string
		edit IssueUpdate
		want string
	}{
		{
			name: "fields and update operations",
			edit: IssueUpdate{
				Fields: map[string]any{"summary": "New"},
				Update: map[string]any{"labels": []map[string]string{{"add": "triaged"}}},
			},
			want: `{"fields":{"summary":"New"},"update":{"labels":[{"add":"triaged"}]}}`,
		},
		{
			name: "fields and properties",
			edit: IssueUpdate{
				Fields:     map[string]any{"summary": "New"},
				Properties: map[string]any{"b": 2, "a": map[string]string{"id": "x"}},
			},
			want: `{"fields":{"summary":"New"},"properties":[{"key":"a","value":{"id":"x"}},{"key":"b","value":2}]}`,
		},
		{name: "nothing to change"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})

			if err := client.UpdateIssue(context.Background(), "INIT-1", test.edit); err != nil {
				t.Fatal(err)
			}

			if test.want == "" {
				if len(*requests) != 0 {
					t.Fatalf("requests = %d, want none", len(*requests))
				}
				return
			}
			got := (*requests)[0]
			if got.method != http.MethodPut || got.path != "/rest/api/3/issue/INIT-1" || got.body != test.want {
				t.Fatalf("request = %+v", got)
			}
		})
	}
}

func TestSearchIssuesRequestsFieldsAndProperties(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"id":"10","key":"INIT-1","fields":{"summary":"Title"},` +
			`"properties":{"devex":{"id":"disc/WI-001"}}}],"nextPageToken":"next","isLast":false}`))
	})

	result, err := client.SearchIssues(context.Background(), IssueSearch{
		JQL: `project = "INIT"`, Fields: []string{"summary", "issuelinks"}, Properties: []string{"devex"}, MaxResults: 50,
	})
	if err != nil {
		t.Fatal(err)
	}

	query := (*requests)[0].query
	for _, want := range []string{"fields=summary%2Cissuelinks", "properties=devex", "maxResults=50", "jql=project"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query = %s, want %s", query, want)
		}
	}
	if len(result.Issues) != 1 || string(result.Issues[0].Properties["devex"]) != `{"id":"disc/WI-001"}` ||
		result.NextPageToken != "next" || result.IsLast {
		t.Fatalf("result = %+v", result)
	}
}

func TestIssuePropertyRoundTrip(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(`{"key":"devex","value":{"id":"disc/WI-001","live":"abc"}}`))
	})

	value := map[string]string{"id": "disc/WI-001", "live": "abc"}
	if err := client.SetIssueProperty(context.Background(), "INIT-1", "devex", value); err != nil {
		t.Fatal(err)
	}
	id, err := client.GetIssueProperty(context.Background(), "INIT-1", "devex")
	if err != nil {
		t.Fatal(err)
	}

	put, get := (*requests)[0], (*requests)[1]
	if put.method != http.MethodPut || put.path != "/rest/api/3/issue/INIT-1/properties/devex" || put.body != `{"id":"disc/WI-001","live":"abc"}` {
		t.Fatalf("put = %+v", put)
	}
	if get.method != http.MethodGet || get.path != "/rest/api/3/issue/INIT-1/properties/devex" {
		t.Fatalf("get = %+v", get)
	}
	if id != "disc/WI-001" {
		t.Fatalf("id = %q", id)
	}
}

func TestMissingPropertyUnwrapsToErrNotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := client.GetIssueProperty(context.Background(), "INIT-1", "devex")
	if !errors.Is(err, gerror.ErrNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
	if !errors.Is(fmt.Errorf("read stamp: %w", err), gerror.ErrNotFound) {
		t.Fatal("a wrapped 404 must still read as not found")
	}
	unprocessable := &httpStatusError{StatusCode: http.StatusUnprocessableEntity}
	if errors.Is(unprocessable, gerror.ErrInternalServerError) {
		t.Fatal("a client error must not read as a server error")
	}
}

func TestListIssueLinksKeepsIDsAndBothDirections(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"10","key":"INIT-2","fields":{"issuelinks":[` +
			`{"id":"501","type":{"name":"Blocks"},"inwardIssue":{"key":"INIT-1"}},` +
			`{"id":"502","type":{"name":"Relates"},"outwardIssue":{"key":"INIT-9"}}]}}`))
	})

	links, err := client.ListIssueLinks(context.Background(), "INIT-2")
	if err != nil {
		t.Fatal(err)
	}

	if got := (*requests)[0].query; got != "fields=issuelinks" {
		t.Fatalf("query = %s", got)
	}
	want := []IssueLink{
		{ID: "501", Type: "Blocks", InwardIssue: "INIT-1"},
		{ID: "502", Type: "Relates", OutwardIssue: "INIT-9"},
	}
	if len(links) != len(want) || links[0] != want[0] || links[1] != want[1] {
		t.Fatalf("links = %+v", links)
	}
}

func TestListIssueLinksWithoutLinks(t *testing.T) {
	for _, fields := range []string{`{}`, `{"issuelinks":null}`} {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":"10","key":"INIT-2","fields":` + fields + `}`))
		})

		links, err := client.ListIssueLinks(context.Background(), "INIT-2")
		if err != nil || len(links) != 0 {
			t.Fatalf("fields %s: links = %v, err = %v", fields, links, err)
		}
	}
}

func TestGetIssueLinksKeepsOnlyInwardLinks(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"10","key":"INIT-2","fields":{"issuelinks":[` +
			`{"id":"501","type":{"name":"Blocks"},"inwardIssue":{"key":"INIT-1"}},` +
			`{"id":"502","type":{"name":"Blocks"},"outwardIssue":{"key":"INIT-3"}}]}}`))
	})

	links, err := client.GetIssueLinks(context.Background(), "INIT-2")
	if err != nil || len(links) != 1 || links[0].InwardIssue != "INIT-1" || links[0].ID != "501" {
		t.Fatalf("links = %+v, err = %v", links, err)
	}
}

func TestInvalidPathSegmentsAndValuesSendNothing(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.SetIssueProperty(context.Background(), "INIT-1", "..", map[string]string{}); err == nil {
		t.Fatal("a dot-segment property key must be rejected")
	}
	if err := client.DeleteIssueLink(context.Background(), "."); err == nil {
		t.Fatal("a dot-segment link ID must be rejected")
	}
	if err := client.SetIssueProperty(context.Background(), "INIT-1", "devex", nil); err == nil {
		t.Fatal("a nil property value must be rejected")
	}
	if len(*requests) != 0 {
		t.Fatalf("requests = %d, want none", len(*requests))
	}
}

// A property without a value reads as unmarked, as it always has.
func TestPropertyWithoutAValueHasNoID(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"key":"devex"}`))
	})

	if id, err := client.GetIssueProperty(context.Background(), "INIT-1", "devex"); err != nil || id != "" {
		t.Fatalf("id = %q, err = %v, want no ID and no error", id, err)
	}
}

func TestDeleteIssueLink(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.DeleteIssueLink(context.Background(), "501"); err != nil {
		t.Fatal(err)
	}

	if got := (*requests)[0]; got.method != http.MethodDelete || got.path != "/rest/api/3/issueLink/501" {
		t.Fatalf("request = %+v", got)
	}
}

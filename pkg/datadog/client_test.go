package datadog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	gerror "github.com/project-init/gommon/pkg/errors"
)

// newTestClient serves every request with respond, and records each retry wait instead of sleeping.
func newTestClient(t *testing.T, respond http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	server := httptest.NewServer(respond)
	t.Cleanup(server.Close)

	client := NewClient(server.Client(), server.URL, "api-key", "app-key")
	var slept []time.Duration
	client.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return client, &slept
}

// TestDoDoesNotFollowARedirect keeps the keys from following a redirect to another host, and leaves the caller's
// http.Client as it was.
func TestDoDoesNotFollowARedirect(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("DD-API-KEY") != "" || r.Header.Get("DD-APPLICATION-KEY") != ""
	}))
	t.Cleanup(other.Close)
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusFound)
	})

	_, err := client.ListActiveMetrics(context.Background(), time.Unix(0, 0))
	if err == nil || !strings.Contains(err.Error(), "302") || leaked {
		t.Errorf("error = %v, keys sent to the redirect target = %v; want a 302 error and no keys sent", err, leaked)
	}
	if http.DefaultClient.CheckRedirect != nil {
		t.Error("NewClient changed http.DefaultClient")
	}
}

// TestQueryMapsSeriesByIndex sends every query in one call and puts each series back on the query that asked for
// it. Datadog pads a batch onto one time grid with nulls; those points are dropped. A query with no data gets none.
func TestQueryMapsSeriesByIndex(t *testing.T) {
	from, to := time.Unix(1000, 0), time.Unix(2000, 0)
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/query" || q.Get("from") != "1000" || q.Get("to") != "2000" || q.Get("query") != "a{x:1,y:2},b{*},c{*}" {
			t.Errorf("request = %s %s, want /api/v1/query from=1000 to=2000 and the three queries joined by commas", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("DD-API-KEY") != "api-key" || r.Header.Get("DD-APPLICATION-KEY") != "app-key" {
			t.Errorf("key headers = %q, %q; want api-key, app-key", r.Header.Get("DD-API-KEY"), r.Header.Get("DD-APPLICATION-KEY"))
		}
		_, _ = w.Write([]byte(`{"status":"ok","series":[
			{"query_index":2,"pointlist":[[1000000,3]]},
			{"query_index":0,"pointlist":[[1000000,1.5],[1900000,null],[2000000,2]]}
		]}`))
	})

	got, err := client.Query(context.Background(), []string{"a{x:1,y:2}", "b{*}", "c{*}"}, from, to)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	want := [][]Point{
		{{time.UnixMilli(1000000), 1.5}, {time.UnixMilli(2000000), 2}},
		nil,
		{{time.UnixMilli(1000000), 3}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Query points\n got %+v\nwant %+v", got, want)
	}
}

func TestQueryReportsAFailedQuery(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"error","error":"Error parsing query"}`))
	})
	if _, err := client.Query(context.Background(), []string{"a{"}, time.Unix(0, 0), time.Unix(1, 0)); err == nil ||
		!strings.Contains(err.Error(), "Error parsing query") {
		t.Errorf("Query error = %v, want Datadog's message", err)
	}
}

// TestDoWaitsOutTheRateLimit retries a 429 after the X-RateLimit-Reset seconds, and after DefaultRetryWait when the
// header is missing.
func TestDoWaitsOutTheRateLimit(t *testing.T) {
	calls := 0
	client, slept := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("X-RateLimit-Reset", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = w.Write([]byte(`{"metrics":["a.b"]}`))
		}
	})
	got, err := client.ListActiveMetrics(context.Background(), time.Unix(0, 0))
	if err != nil || !reflect.DeepEqual(got, []string{"a.b"}) {
		t.Fatalf("ListActiveMetrics after two 429s = %v, %v", got, err)
	}
	if want := []time.Duration{7 * time.Second, DefaultRetryWait}; calls != 3 || !reflect.DeepEqual(*slept, want) {
		t.Errorf("calls = %d, waits = %v; want 3 calls and waits %v", calls, *slept, want)
	}
}

// TestDoStopsRetryingAfterMaxRetries returns the 429 once the retries run out.
func TestDoStopsRetryingAfterMaxRetries(t *testing.T) {
	calls := 0
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.ListActiveMetrics(context.Background(), time.Unix(0, 0))
	if !errors.Is(err, gerror.ErrTooManyRequests) || calls != MaxRetries+1 {
		t.Errorf("error = %v after %d calls, want ErrTooManyRequests after %d", err, calls, MaxRetries+1)
	}
}

// TestDoTimeoutIsErrTimeout turns a client timeout into errors.ErrTimeout, so a caller can split a batch, and keeps
// the long URL out of the message.
func TestDoTimeoutIsErrTimeout(t *testing.T) {
	client, _ := newTestClient(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	client.client.Timeout = 10 * time.Millisecond
	_, err := client.Query(context.Background(), []string{"a{*}"}, time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, gerror.ErrTimeout) || strings.Contains(err.Error(), "a%7B") {
		t.Errorf("Query error = %v, want ErrTimeout without the URL", err)
	}
}

// TestStatusErrorsUnwrapToSentinels tells a 403 (the app key lacks a scope) apart from a 401 (Datadog rejects the
// keys), and never puts a key in the message.
func TestStatusErrorsUnwrapToSentinels(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{status: http.StatusForbidden, want: gerror.ErrForbidden},
		{status: http.StatusUnauthorized, want: gerror.ErrPermissionDenied},
		{status: http.StatusBadRequest, want: gerror.ErrBadRequest},
		{status: http.StatusServiceUnavailable, want: gerror.ErrServiceUnavailable},
	}
	for _, tc := range cases {
		client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"errors":["nope"]}`))
		})
		_, err := client.Query(context.Background(), []string{"a{*}"}, time.Unix(0, 0), time.Unix(1, 0))
		if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), "nope") || strings.Contains(err.Error(), "app-key") {
			t.Errorf("%d: error = %v, want %v with the body and no key", tc.status, err, tc.want)
		}
	}

	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	if _, err := client.Query(context.Background(), []string{"a{*}"}, time.Unix(0, 0), time.Unix(1, 0)); errors.Is(err, gerror.ErrInternalServerError) {
		t.Errorf("418: error = %v unwraps to a server error", err)
	}
}

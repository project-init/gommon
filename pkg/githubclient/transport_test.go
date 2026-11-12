package githubclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v74/github"
	gerror "github.com/project-init/gommon/pkg/errors"
)

// newTestClient serves every request with respond, and records each retry wait instead of sleeping.
func newTestClient(t *testing.T, timeout time.Duration, respond http.HandlerFunc) (*gh.Client, *[]time.Duration) {
	t.Helper()
	server := httptest.NewServer(respond)
	t.Cleanup(server.Close)

	var slept []time.Duration
	client, err := newClient("token", server.URL, &retryTransport{
		timeout: timeout,
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	if http.DefaultClient.CheckRedirect != nil || http.DefaultClient.Timeout != 0 {
		t.Error("New changed http.DefaultClient")
	}
	return client, &slept
}

func TestRoundTripHonorsRetryAfter(t *testing.T) {
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("request = %s %s, authorization = %q", r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"))
		}
		if calls == 1 {
			w.Header().Set("Retry-After", "8")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	})

	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil || user.GetLogin() != "octocat" {
		t.Fatalf("Users.Get = %v, %v", user, err)
	}
	if calls != 2 || !reflect.DeepEqual(*slept, []time.Duration{8 * time.Second}) {
		t.Errorf("calls = %d, waits = %v; want 2 calls and a wait of 8s", calls, *slept)
	}
}

func TestRoundTripOmitsAuthorizationWhenTheTokenIsEmpty(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	}))
	t.Cleanup(server.Close)

	client, err := newClient("", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil || user.GetLogin() != "octocat" || authorization != "" {
		t.Fatalf("Users.Get = %v, %v, authorization = %q", user, err, authorization)
	}
}

func TestRoundTripBacksOffWithoutRetryAfter(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusTooManyRequests)
	})

	if _, _, err := client.Users.Get(context.Background(), ""); err == nil {
		t.Fatal("Users.Get error = nil")
	}
	if calls != MaxRetries+1 || !reflect.DeepEqual(*slept, want) {
		t.Errorf("calls = %d, waits = %v; want %d calls and waits %v", calls, *slept, MaxRetries+1, want)
	}
}

func TestRoundTripRetriesAServiceUnavailable(t *testing.T) {
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	})

	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil || user.GetLogin() != "octocat" {
		t.Fatalf("Users.Get = %v, %v", user, err)
	}
	if calls != 2 || len(*slept) != 1 {
		t.Errorf("calls = %d, waits = %v; want one retry", calls, *slept)
	}
}

func TestRoundTripWaitsForPrimaryReset(t *testing.T) {
	reset := time.Now().Add(30 * time.Second).Unix()
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	})

	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil || user.GetLogin() != "octocat" {
		t.Fatalf("Users.Get = %v, %v", user, err)
	}
	if calls != 2 || len(*slept) != 1 || (*slept)[0] < 20*time.Second || (*slept)[0] > 35*time.Second {
		t.Errorf("calls = %d, waits = %v; want 2 calls and a wait until the reset timestamp", calls, *slept)
	}
}

func TestRoundTripPrefersRetryAfterOverPrimaryReset(t *testing.T) {
	reset := time.Now().Add(30 * time.Second).Unix()
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "8")
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	})

	user, _, err := client.Users.Get(context.Background(), "")
	if err != nil || user.GetLogin() != "octocat" {
		t.Fatalf("Users.Get = %v, %v", user, err)
	}
	if calls != 2 || !reflect.DeepEqual(*slept, []time.Duration{8 * time.Second}) {
		t.Errorf("calls = %d, waits = %v; want 2 calls and a wait of 8s", calls, *slept)
	}
}

func TestRoundTripCapsALongWait(t *testing.T) {
	t.Run("retry-after", func(t *testing.T) {
		calls := 0
		client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("Retry-After", "999999")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = io.WriteString(w, `{"login":"octocat"}`)
		})

		user, _, err := client.Users.Get(context.Background(), "")
		if err != nil || user.GetLogin() != "octocat" {
			t.Fatalf("Users.Get = %v, %v", user, err)
		}
		if calls != 2 || !reflect.DeepEqual(*slept, []time.Duration{time.Hour}) {
			t.Errorf("calls = %d, waits = %v; want 2 calls and a wait of 1h", calls, *slept)
		}
	})

	t.Run("primary reset", func(t *testing.T) {
		reset := time.Now().Add(48 * time.Hour).Unix()
		calls := 0
		client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
			calls++
			if calls == 1 {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = io.WriteString(w, `{"login":"octocat"}`)
		})

		user, _, err := client.Users.Get(context.Background(), "")
		if err != nil || user.GetLogin() != "octocat" {
			t.Fatalf("Users.Get = %v, %v", user, err)
		}
		if calls != 2 || !reflect.DeepEqual(*slept, []time.Duration{time.Hour}) {
			t.Errorf("calls = %d, waits = %v; want 2 calls and a wait of 1h", calls, *slept)
		}
	})
}

func TestRoundTripPastResetUsesBackoff(t *testing.T) {
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "7")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	})

	if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !reflect.DeepEqual(*slept, []time.Duration{time.Second}) {
		t.Errorf("calls = %d, waits = %v; want 2 calls and a 1s backoff", calls, *slept)
	}
}

func TestRoundTripWaitsOnSecondaryLimit(t *testing.T) {
	docs := []string{
		"https://docs.github.com/rest/overview/resources-in-the-rest-api#secondary-rate-limits",
		"https://docs.github.com/rest/overview/resources-in-the-rest-api#abuse-rate-limits",
	}
	for _, doc := range docs {
		t.Run(doc[strings.LastIndex(doc, "#")+1:], func(t *testing.T) {
			calls := 0
			client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `{"message":"slow down","documentation_url":"`+doc+`"}`)
					return
				}
				_, _ = io.WriteString(w, `{"login":"octocat"}`)
			})

			if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || !reflect.DeepEqual(*slept, []time.Duration{time.Minute}) {
				t.Errorf("calls = %d, waits = %v; want 2 calls and a wait of 1m", calls, *slept)
			}
		})
	}
}

func TestRoundTripBacksOffOnSecondaryLimit(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"slow down","documentation_url":"https://docs.github.com/rest/overview/resources-in-the-rest-api#secondary-rate-limits"}`)
	})

	if _, _, err := client.Users.Get(context.Background(), ""); err == nil {
		t.Fatal("Users.Get error = nil")
	}
	if calls != MaxRetries+1 || !reflect.DeepEqual(*slept, want) {
		t.Errorf("calls = %d, waits = %v; want %d calls and waits %v", calls, *slept, MaxRetries+1, want)
	}
}

func TestRoundTripForbiddenRetryAfterMustBeAPositiveNumber(t *testing.T) {
	for _, header := range []string{"0", "abc"} {
		t.Run(header, func(t *testing.T) {
			calls := 0
			client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Retry-After", header)
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"message":"nope"}`)
			})

			_, _, err := client.Users.Get(context.Background(), "")
			if err == nil || calls != 1 || len(*slept) != 0 || !strings.Contains(err.Error(), "nope") {
				t.Errorf("calls = %d, waits = %v, error = %v; want one 403 and no retry", calls, *slept, err)
			}
		})
	}
}

func TestRoundTripLeavesPlainForbidden(t *testing.T) {
	calls := 0
	client, _ := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"nope"}`)
	})

	_, _, err := client.Users.Get(context.Background(), "")
	if err == nil || calls != 1 || !strings.Contains(err.Error(), "nope") || strings.Contains(err.Error(), "token") {
		t.Errorf("calls = %d, error = %v; want one 403 that names nope and omits the token", calls, err)
	}
}

func TestRoundTripReplaysTheRequestBody(t *testing.T) {
	var seen []string
	client, _ := newTestClient(t, 0, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, string(body))
		if len(seen) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "<p>hi</p>")
	})

	got, _, err := client.Markdown.Render(context.Background(), "hello", nil)
	if err != nil || got != "<p>hi</p>" {
		t.Fatalf("Render = %q, %v", got, err)
	}
	if len(seen) != 2 || seen[0] != seen[1] || !strings.Contains(seen[0], `"text":"hello"`) {
		t.Errorf("bodies = %#v; want the JSON body sent twice", seen)
	}
}

func TestRoundTripReturnsABodyItCannotReplay(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "slow down")
	}))
	t.Cleanup(server.Close)

	slept := false
	transport := &retryTransport{sleep: func(context.Context, time.Duration) error {
		slept = true
		return nil
	}}
	req, err := http.NewRequest(http.MethodPost, server.URL, io.NopCloser(strings.NewReader("once")))
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if calls != 1 || slept || response.StatusCode != http.StatusTooManyRequests || string(body) != "slow down" {
		t.Errorf("calls = %d, slept = %v, status = %d, body = %q", calls, slept, response.StatusCode, body)
	}
}

func TestRoundTripTimeoutIsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	transport := &retryTransport{timeout: 30 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/search?q=a%7Bsecret", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(req)
	if !errors.Is(err, gerror.ErrTimeout) || strings.Contains(err.Error(), "a%7B") || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v, want ErrTimeout without the URL", err)
	}

	client, _ := newTestClient(t, 30*time.Millisecond, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	_, _, err = client.Users.Get(context.Background(), "")
	if !errors.Is(err, gerror.ErrTimeout) || strings.Contains(err.Error(), "/user") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("Users.Get error = %v, want ErrTimeout whose message omits the URL", err)
	}
}

func TestRoundTripLeavesCallerDeadlineAlone(t *testing.T) {
	client, _ := newTestClient(t, time.Minute, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, _, err := client.Users.Get(ctx, "")
	if err == nil || errors.Is(err, gerror.ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want the request context deadline", err)
	}
}

func TestRoundTripStopsWhenTheContextEndsDuringTheWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := newClient("token", server.URL, &retryTransport{
		sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = client.Users.Get(ctx, "")
	if !errors.Is(err, context.Canceled) || errors.Is(err, gerror.ErrTimeout) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

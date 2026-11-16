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
	"sync"
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
	// A server error holds only its own request, so the next one goes out at once.
	if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !reflect.DeepEqual(*slept, []time.Duration{time.Second}) {
		t.Errorf("calls = %d, waits = %v; want one retry after 1s and no wait for the next request", calls, *slept)
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

// near reports whether got is within a second of want, for waits measured against the clock.
func near(got, want time.Duration) bool {
	return got <= want && got > want-time.Second
}

// GitHub documents no example body for either rate limit, so these are go-github v74's fixtures,
// verbatim. The primary-limit body's documentation_url points at abuse limits too; only
// X-RateLimit-Remaining 0 marks it as a spent primary limit.
const (
	secondaryLimitBody = `{
   "message": "You have triggered an abuse detection mechanism and have been temporarily blocked from content creation. Please retry your request again later.",
   "documentation_url": "https://docs.github.com/en/rest/overview/resources-in-the-rest-api#abuse-rate-limits"
}`
	primaryLimitBody = `{
   "message": "API rate limit exceeded for xxx.xxx.xxx.xxx. (But here's the good news: Authenticated requests get a higher rate limit. Check out the documentation for more details.)",
   "documentation_url": "https://docs.github.com/en/rest/overview/resources-in-the-rest-api#abuse-rate-limits"
}`
)

func TestRoundTripHoldsTheNextRequestDuringARateLimit(t *testing.T) {
	limits := map[string]struct {
		respond func(http.ResponseWriter)
		wait    time.Duration
	}{
		"429 with Retry-After": {wait: time.Minute, respond: func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		}},
		"429 alone": {wait: time.Second, respond: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusTooManyRequests)
		}},
		"403 with Retry-After": {wait: time.Minute, respond: func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"slow down"}`)
		}},
		"403 secondary limit": {wait: time.Minute, respond: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, secondaryLimitBody)
		}},
		"429 secondary limit": {wait: time.Minute, respond: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, secondaryLimitBody)
		}},
		"503 with Retry-After": {wait: time.Minute, respond: func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusServiceUnavailable)
		}},
	}
	for name, limit := range limits {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					limit.respond(w)
					return
				}
				_, _ = io.WriteString(w, `{"login":"octocat"}`)
			})
			for range 2 { // the second stands in for another goroutine's request
				if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
					t.Fatal(err)
				}
			}
			if len(*slept) != 2 || !near((*slept)[0], limit.wait) || !near((*slept)[1], limit.wait) {
				t.Errorf("waits = %v, want %s for the limited request and about that for the next", *slept, limit.wait)
			}
		})
	}
}

func TestRoundTripLetsOthersPassALimitNoRequestWaitsOut(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	t.Cleanup(server.Close)

	var slept []time.Duration
	transport := &retryTransport{sleep: func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}}
	once, err := http.NewRequest(http.MethodPost, server.URL, io.NopCloser(strings.NewReader("once")))
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(once)
	if err != nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("RoundTrip(unreplayable) = %v, %v; want the 429 returned", response, err)
	}
	_ = response.Body.Close()
	next, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	if response, err = transport.RoundTrip(next); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(slept) != 0 {
		t.Errorf("waits = %v, want none: the limited request returned without waiting", slept)
	}
}

func TestRoundTripLetsOthersPassASpentPrimaryLimit(t *testing.T) {
	// A spent search quota leaves core requests, which have their own quota, free to go.
	future := strconv.FormatInt(time.Now().Add(2*time.Minute).Unix(), 10)
	cases := map[string]struct {
		status int
		reset  string
		body   string
	}{
		"403":                {http.StatusForbidden, future, primaryLimitBody},
		"429":                {http.StatusTooManyRequests, future, primaryLimitBody},
		"reset already past": {http.StatusForbidden, "7", primaryLimitBody}, // a local clock running ahead
		"429 past its reset": {http.StatusTooManyRequests, "7", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 {
					w.Header().Set("X-RateLimit-Remaining", "0")
					w.Header().Set("X-RateLimit-Resource", "search")
					w.Header().Set("X-RateLimit-Reset", tc.reset)
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
					return
				}
				_, _ = io.WriteString(w, `{"login":"octocat"}`)
			})
			for range 2 {
				if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
					t.Fatal(err)
				}
			}
			if len(*slept) != 1 {
				t.Errorf("waits = %v, want only the limited request's wait", *slept)
			}
		})
	}
}

// TestRoundTripHoldIsSafeForConcurrentRequests runs the hold from several goroutines. It proves
// something only under go test -race, which the unit task does not pass; the sequential tests
// above pin what the hold does.
func TestRoundTripHoldIsSafeForConcurrentRequests(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	}))
	t.Cleanup(server.Close)
	var slept []time.Duration
	client, err := newClient("token", server.URL, &retryTransport{sleep: func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		slept = append(slept, d)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if calls != 9 {
		t.Errorf("calls = %d, want every request through after the hold", calls)
	}
}

func TestRoundTripKeepsALongerHold(t *testing.T) {
	calls := 0
	client, slept := newTestClient(t, 0, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		case 3:
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = io.WriteString(w, `{"login":"octocat"}`)
		}
	})
	for range 2 {
		if _, _, err := client.Users.Get(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(*slept) != 3 || !near((*slept)[2], time.Minute) {
		t.Errorf("waits = %v, want the 5s limit to wait out the 1m hold still in force", *slept)
	}
}

// roundTripFunc serves an attempt from a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// brokenBody fails every read, as a connection reset mid-body does.
type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }
func (brokenBody) Close() error             { return nil }

func TestRoundTripRetriesA429WhoseBodyBreaks(t *testing.T) {
	calls := 0
	transport := &retryTransport{
		sleep: func(context.Context, time.Duration) error { return nil },
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: brokenBody{}, Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
		}),
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.test/user", nil)
	response, err := transport.RoundTrip(req)
	if err != nil || response.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("RoundTrip = %v, %v after %d calls; want the 429 retried", response, err, calls)
	}
	_ = response.Body.Close()
}

func TestRoundTripReportsA429BodyItCannotRead(t *testing.T) {
	transport := &retryTransport{
		sleep: func(context.Context, time.Duration) error { return nil },
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: brokenBody{}, Request: req}, nil
		}),
	}
	req, _ := http.NewRequest(http.MethodPost, "https://api.github.test/user", io.NopCloser(strings.NewReader("once")))
	if response, err := transport.RoundTrip(req); err == nil {
		_ = response.Body.Close()
		t.Error("RoundTrip(unreplayable) = nil error, want the failed read reported")
	}
}

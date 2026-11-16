package githubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	gerror "github.com/project-init/gommon/pkg/errors"
)

const (
	// MaxRetries is how many times a retryable response is sent again.
	MaxRetries = 5
	// DefaultTimeout is how long one attempt waits for GitHub to answer.
	// The wait between attempts is outside this deadline.
	DefaultTimeout = 30 * time.Second

	serverBackoff       = time.Second
	maxServerBackoff    = 16 * time.Second
	secondaryBackoff    = time.Minute
	maxSecondaryBackoff = 16 * time.Minute
	maxRetryWait        = time.Hour
	maxErrorBody        = 32 * 1024
)

// retryTransport retries GitHub responses that are safe to send again.
// go-github owns Client.Do, so the loop has to live on the http.Client passed to gh.NewClient.
// While one request waits out a Retry-After, a secondary rate limit, or a 429, every request
// through the transport waits with it: GitHub asks clients to stop sending while limited, and
// counts concurrent requests toward its secondary limits.
type retryTransport struct {
	base    http.RoundTripper
	token   string
	timeout time.Duration
	sleep   func(context.Context, time.Duration) error

	mu       sync.Mutex
	resumeAt time.Time // when a rate limit lets requests go out again
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("githubclient: nil request")
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	timeout := t.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	sleep := t.sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	attemptClient := &http.Client{
		Timeout:   timeout,
		Transport: base,
		// go-github follows redirects on its own client. This attempt stops at the first response.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// Clone so the bearer token is not written onto the caller's request.
	// The clone shares GetBody, so a retry can still replay the body.
	// A timeout redacts the outer request URL: net/http copies that URL into its error text.
	attemptReq := req.Clone(req.Context())
	if t.token != "" {
		attemptReq.Header.Set("Authorization", "Bearer "+t.token)
	}

	var waited time.Time // the latest hold this request has already waited out
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := rewind(attemptReq); err != nil {
				return nil, err
			}
		}
		if err := t.awaitHold(req.Context(), sleep, &waited); err != nil {
			return nil, err
		}

		response, err := attemptClient.Do(attemptReq)
		if ue := (*url.Error)(nil); errors.As(err, &ue) && ue.Timeout() && req.Context().Err() == nil {
			redactRequestURL(req)
			return nil, fmt.Errorf("github did not answer within %s: %w", timeout, gerror.ErrTimeout)
		}
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("githubclient: empty response")
		}

		retry, body, consumed, err := classify(response)
		retrying := retry && attempt < MaxRetries && canReplay(req)
		if err != nil && !retrying {
			return nil, err
		}
		if retrying {
			delay, shared := retryWait(response, body, attempt)
			if shared {
				delay, waited = t.hold(delay)
			}
			if !consumed {
				discard(response)
			}
			if err := sleep(req.Context(), delay); err != nil {
				return nil, err
			}
			continue
		}
		if consumed {
			response.Body = io.NopCloser(bytes.NewReader(body))
		}
		return response, nil
	}
}

// hold keeps every request waiting for delay, unless an earlier hold lasts longer. It returns
// how long this request waits and the hold's end.
func (t *retryTransport) hold(delay time.Duration) (time.Duration, time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if until := time.Now().Add(delay); until.After(t.resumeAt) {
		t.resumeAt = until
		return delay, until
	}
	return time.Until(t.resumeAt), t.resumeAt
}

// awaitHold waits out any hold ending after waited, the end of the last hold this request waited
// out.
func (t *retryTransport) awaitHold(ctx context.Context, sleep func(context.Context, time.Duration) error, waited *time.Time) error {
	for {
		t.mu.Lock()
		resumeAt := t.resumeAt
		t.mu.Unlock()
		delay := time.Until(resumeAt)
		if !resumeAt.After(*waited) || delay <= 0 {
			return nil
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
		*waited = resumeAt
	}
}

func redactRequestURL(req *http.Request) {
	if req == nil || req.URL == nil {
		return
	}
	req.URL = &url.URL{}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// classify reports whether the response should be sent again.
// A 403 or 429 body is read so a secondary rate limit can be told apart from a missing scope or
// a plain 429. consumed is true when Body has been closed and body holds what was read.
func classify(response *http.Response) (retry bool, body []byte, consumed bool, err error) {
	status := response.StatusCode
	if status >= 500 && status <= 599 {
		return true, nil, false, nil
	}
	if status != http.StatusForbidden && status != http.StatusTooManyRequests {
		return false, nil, false, nil
	}

	body, err = readBody(response)
	if err != nil {
		return status == http.StatusTooManyRequests, body, true, err // a 429 is retried whatever its body held
	}
	if status == http.StatusTooManyRequests || retryAfter(response.Header) > 0 {
		return true, body, true, nil
	}
	if remaining, _, ok := rateLimit(response.Header); ok && remaining == 0 {
		return true, body, true, nil
	}
	if secondaryLimit(body) {
		return true, body, true, nil
	}
	return false, body, true, nil
}

// retryWait is how long to pause before the next attempt, and whether every request through the
// transport pauses with it. Retry-After is a number of seconds. X-RateLimit-Reset is the Unix
// time when a primary window with X-RateLimit-Remaining 0 opens again. Either of those waits is
// capped at one hour. A secondary limit with neither signal backs off from one minute, doubling
// up to 16 minutes. A 429 or 5xx with no signal at all backs off from one second, doubling up to
// 16 seconds. A Retry-After holds every request. Otherwise a spent primary limit, marked by
// X-RateLimit-Remaining 0, covers one resource such as search, so it holds only its own request,
// as does a 5xx; a secondary limit or a 429 holds every request.
func retryWait(response *http.Response, body []byte, attempt int) (time.Duration, bool) {
	if delay := retryAfter(response.Header); delay > 0 {
		return capWait(delay), true
	}
	remaining, reset, ok := rateLimit(response.Header)
	primary := ok && remaining == 0
	if delay := time.Until(reset); primary && delay > 0 {
		return capWait(delay), false
	}
	secondary := secondaryLimit(body)
	delay := backoff(attempt, serverBackoff, maxServerBackoff)
	if secondary {
		delay = backoff(attempt, secondaryBackoff, maxSecondaryBackoff)
	}
	return delay, !primary && (secondary || response.StatusCode == http.StatusTooManyRequests)
}

func capWait(delay time.Duration) time.Duration {
	if delay > maxRetryWait {
		return maxRetryWait
	}
	return delay
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func rateLimit(header http.Header) (remaining int, reset time.Time, ok bool) {
	raw := header.Get("X-RateLimit-Remaining")
	if raw == "" {
		return 0, time.Time{}, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, time.Time{}, false
	}
	if seconds, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		reset = time.Unix(seconds, 0)
	}
	return n, reset, true
}

func secondaryLimit(body []byte) bool {
	var payload struct {
		DocumentationURL string `json:"documentation_url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return strings.HasSuffix(payload.DocumentationURL, "#abuse-rate-limits") ||
		strings.HasSuffix(payload.DocumentationURL, "secondary-rate-limits")
}

func backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 4 {
		attempt = 4
	}
	delay := base << attempt
	if delay <= 0 || delay > max {
		return max
	}
	return delay
}

func canReplay(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func rewind(req *http.Request) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return fmt.Errorf("githubclient: replay request body: %w", err)
	}
	req.Body = body
	return nil
}

func readBody(response *http.Response) ([]byte, error) {
	if response.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBody))
	_ = response.Body.Close()
	return body, err
}

func discard(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBody))
	_ = response.Body.Close()
}

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
type retryTransport struct {
	base    http.RoundTripper
	token   string
	timeout time.Duration
	sleep   func(context.Context, time.Duration) error
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

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := rewind(attemptReq); err != nil {
				return nil, err
			}
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
		if err != nil {
			return nil, err
		}
		if retry && attempt < MaxRetries && canReplay(req) {
			delay := retryWait(response, body, attempt)
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
// A 403 body is read so a secondary rate limit can be told apart from a missing scope.
// consumed is true when Body has been closed and body holds what was read.
func classify(response *http.Response) (retry bool, body []byte, consumed bool, err error) {
	status := response.StatusCode
	if status == http.StatusTooManyRequests || (status >= 500 && status <= 599) {
		return true, nil, false, nil
	}
	if status != http.StatusForbidden {
		return false, nil, false, nil
	}

	body, err = readBody(response)
	if err != nil {
		return false, body, true, err
	}
	if retryAfter(response.Header) > 0 {
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

// retryWait is how long to pause before the next attempt.
// Retry-After is a number of seconds. X-RateLimit-Reset is the Unix time when a
// primary window with X-RateLimit-Remaining 0 opens again. Either of those waits
// is capped at one hour. A 429 or 5xx with neither signal backs off from one second,
// doubling up to 16 seconds. A secondary limit with neither signal backs off from
// one minute, doubling up to 16 minutes.
func retryWait(response *http.Response, body []byte, attempt int) time.Duration {
	if delay := retryAfter(response.Header); delay > 0 {
		return capWait(delay)
	}
	if remaining, reset, ok := rateLimit(response.Header); ok && remaining == 0 {
		if delay := time.Until(reset); delay > 0 {
			return capWait(delay)
		}
	}
	if response.StatusCode == http.StatusForbidden && secondaryLimit(body) {
		return backoff(attempt, secondaryBackoff, maxSecondaryBackoff)
	}
	return backoff(attempt, serverBackoff, maxServerBackoff)
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

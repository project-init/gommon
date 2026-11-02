package datadog

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
	gerrhttp "github.com/project-init/gommon/pkg/errors/http"
)

const (
	// MaxRetries is how many times Do retries a request that Datadog rate limits.
	MaxRetries = 5
	// DefaultRetryWait is used when a 429 carries no X-RateLimit-Reset header.
	DefaultRetryWait = 10 * time.Second
)

// Client handles communication with the Datadog REST API
type Client struct {
	client  *http.Client
	baseURL string
	apiKey  string
	appKey  string
	sleep   func(context.Context, time.Duration) error
}

// NewClient creates new Datadog API client for baseURL, the API host of the caller's Datadog site, e.g.
// https://api.datadoghq.com, with an API key and an application key.
// Accepts custom *http.Client, uses a copy of DefaultClient if nil. The client is copied, and the copy does not follow
// redirects: Go sends custom headers such as DD-API-KEY on to the redirect target, even on another host, and the
// Datadog API does not redirect. A redirect comes back as an error instead.
func NewClient(httpClient *http.Client, baseURL, apiKey, appKey string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	noRedirects := *httpClient
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		client:  &noRedirects,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		appKey:  appKey,
		sleep:   sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// NewRequest creates an API request
// A relative path can be provided, e.g. "/api/v1/query?from=..."
func (c *Client) NewRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}

	request.Header.Set("DD-API-KEY", c.apiKey)
	request.Header.Set("DD-APPLICATION-KEY", c.appKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	return request, nil
}

// Do executes an API request and unmarshals the response body into responseTarget. A 429 is retried up to
// MaxRetries times, after the X-RateLimit-Reset seconds. A request the http.Client times out returns an error that
// wraps errors.ErrTimeout and leaves out the URL, which can carry a long query.
func (c *Client) Do(request *http.Request, responseTarget any) error {
	for attempt := 0; ; attempt++ {
		response, err := c.client.Do(request)
		if ue := (*url.Error)(nil); errors.As(err, &ue) && ue.Timeout() && request.Context().Err() == nil {
			return fmt.Errorf("datadog did not answer within %s: %w", c.client.Timeout, gerror.ErrTimeout)
		}
		if err != nil {
			return err
		}

		if response.StatusCode == http.StatusTooManyRequests && attempt < MaxRetries {
			_ = response.Body.Close()
			if err := c.sleep(request.Context(), retryWait(response.Header)); err != nil {
				return err
			}
			continue
		}
		return decode(response, responseTarget)
	}
}

// retryWait reads X-RateLimit-Reset, the number of seconds until the rate limit window resets.
func retryWait(header http.Header) time.Duration {
	if s, err := strconv.Atoi(header.Get("X-RateLimit-Reset")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return DefaultRetryWait
}

func decode(response *http.Response, responseTarget any) error {
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 32*1024))
		return &httpStatusError{StatusCode: response.StatusCode, Body: string(bodyBytes)}
	}

	if responseTarget == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(responseTarget); err != nil {
		return fmt.Errorf("decode Datadog response: %w", err)
	}
	return nil
}

type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("Datadog returned HTTP %d: %s", e.StatusCode, e.Body)
}

// Unwrap maps the status to a gommon error sentinel, such as errors.ErrForbidden for a 403. A client error with no
// sentinel of its own unwraps to nil, never to a server error.
func (e *httpStatusError) Unwrap() error {
	sentinel := gerrhttp.ToErrorFromHTTP(e.StatusCode)
	if e.StatusCode < http.StatusInternalServerError && errors.Is(sentinel, gerror.ErrInternalServerError) {
		return nil
	}

	return sentinel
}

package jiraclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	gerror "github.com/project-init/gommon/pkg/errors"
	gerrhttp "github.com/project-init/gommon/pkg/errors/http"
)

const (
	maxRetries       = 5
	serverBackoff    = time.Second
	maxServerBackoff = 16 * time.Second
)

// Client handles communication with Jira Rest API
type Client struct {
	client  *http.Client
	email   string
	baseURL string
	token   string
	sleep   func(context.Context, time.Duration) error
}

// NewClient creates new Jira API client
// Accepts custom *http.Client, uses DefaultClient if nil [i.e base URL https://my-domain.atlassian.net and auth creds ]
func NewClient(httpClient *http.Client, baseURL string, email string, token string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		client:  httpClient,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		email:   email,
		token:   token,
	}
}

// NewRequest creates an API request
// A relative path can be provided, e.g. "/rest/api/3/issue"
func (c *Client) NewRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	reqURL := c.baseURL + path
	request, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, err
	}

	request.SetBasicAuth(c.email, c.token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	return request, nil
}

// Do executes an API request and unmarshals the response body into responseTarget.
// A 429 or 5xx is sent again up to 5 times. Retry-After, a number of seconds, is the wait
// when Jira sends it. Otherwise the wait starts at one second and doubles up to 16 seconds.
// The wait uses the request context, so a canceled caller stops between attempts.
func (c *Client) Do(request *http.Request, responseTarget any) error {
	sleep := c.sleep
	if sleep == nil {
		sleep = sleepCtx
	}

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := rewind(request); err != nil {
				return err
			}
		}

		response, err := c.client.Do(request)
		if err != nil {
			return err
		}
		if response == nil {
			return errors.New("Jira returned an empty response")
		}

		if retryable(response.StatusCode) && attempt < maxRetries && canReplay(request) {
			delay := retryWait(response.Header, attempt)
			discard(response)
			if err := sleep(request.Context(), delay); err != nil {
				return err
			}
			continue
		}
		return finish(response, responseTarget)
	}
}

func finish(response *http.Response, responseTarget any) error {
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 32*1024))
		return &httpStatusError{StatusCode: response.StatusCode, Body: string(bodyBytes)}
	}

	if responseTarget == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(responseTarget); err != nil {
		return fmt.Errorf("decode Jira response: %w", err)
	}
	return nil
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status <= 599)
}

func retryWait(header http.Header, attempt int) time.Duration {
	if delay := retryAfter(header); delay > 0 {
		return delay
	}
	return backoff(attempt)
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 4 {
		attempt = 4
	}
	delay := serverBackoff << attempt
	if delay <= 0 || delay > maxServerBackoff {
		return maxServerBackoff
	}
	return delay
}

func canReplay(request *http.Request) bool {
	return request.Body == nil || request.Body == http.NoBody || request.GetBody != nil
}

func rewind(request *http.Request) error {
	if request.Body == nil || request.Body == http.NoBody {
		return nil
	}
	body, err := request.GetBody()
	if err != nil {
		return fmt.Errorf("replay Jira request body: %w", err)
	}
	request.Body = body
	return nil
}

func discard(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32*1024))
	_ = response.Body.Close()
}

func sleepCtx(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("Jira returned HTTP %d: %s", e.StatusCode, e.Body)
}

// Unwrap maps the status to a gommon error sentinel, such as errors.ErrNotFound for a 404. A
// client error with no sentinel of its own unwraps to nil, never to a server error.
func (e *httpStatusError) Unwrap() error {
	sentinel := gerrhttp.ToErrorFromHTTP(e.StatusCode)
	if e.StatusCode < http.StatusInternalServerError && errors.Is(sentinel, gerror.ErrInternalServerError) {
		return nil
	}

	return sentinel
}

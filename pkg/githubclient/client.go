package githubclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	gh "github.com/google/go-github/v74/github"
)

// ErrAuthUnavailable is returned when neither GITHUB_TOKEN nor gh CLI auth is available.
var ErrAuthUnavailable = errors.New("github auth unavailable")

// ErrAuthTimedOut is returned when `gh auth token` does not finish within ghAuthTimeout.
var ErrAuthTimedOut = errors.New("github auth timed out")

// ghAuthTimeout bounds `gh auth token`. A keychain prompt that never returns
// would otherwise hold the caller indefinitely.
var ghAuthTimeout = 15 * time.Second

// New returns a GitHub API client. An empty token sends requests without a bearer token.
// An empty baseURL uses https://api.github.com/ and https://uploads.github.com/.
// Any other absolute http or https baseURL keeps uploads on that same host, under /api/uploads/.
// Each attempt waits DefaultTimeout. A 429 or 5xx is retried up to MaxRetries times, as is a
// primary or secondary rate limit. Retry-After is the wait when GitHub sends a positive
// number of seconds. A primary limit waits until X-RateLimit-Reset, a Unix timestamp.
// Either of those waits is capped at one hour.
// A client timeout returns an error that wraps errors.ErrTimeout and does not include the request URL.
func New(token string, baseURL string) (*gh.Client, error) {
	return newClient(token, baseURL, nil)
}

func newClient(token, baseURL string, transport *retryTransport) (*gh.Client, error) {
	if transport == nil {
		transport = &retryTransport{}
	}
	if transport.base == nil {
		transport.base = http.DefaultTransport
	}
	if transport.sleep == nil {
		transport.sleep = sleepCtx
	}
	transport.token = token

	client := gh.NewClient(&http.Client{Transport: transport})
	if err := applyBaseURL(client, baseURL); err != nil {
		return nil, err
	}
	return client, nil
}

func applyBaseURL(client *gh.Client, baseURL string) error {
	if baseURL == "" {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/") + "/")
	if err != nil {
		return fmt.Errorf("parse github base url: %w", err)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("github base url must be an absolute http or https url")
	}
	client.BaseURL = parsed

	upload := *parsed
	upload.RawQuery = ""
	upload.Fragment = ""
	upload.RawPath = ""
	if parsed.Hostname() == "api.github.com" {
		upload.Host = "uploads.github.com"
		upload.Path = "/"
	} else {
		upload.Path = "/api/uploads/"
	}
	client.UploadURL = &upload
	return nil
}

func ResolveToken() (string, error) {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ghAuthTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("gh auth token did not finish within %s: %w", ghAuthTimeout, ErrAuthTimedOut)
		}
		return "", ErrAuthUnavailable
	}
	if token := strings.TrimSpace(string(out)); token != "" {
		return token, nil
	}
	return "", ErrAuthUnavailable
}

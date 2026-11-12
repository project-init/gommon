package githubclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewKeepsPublicGitHubURLs(t *testing.T) {
	client, err := New("", "")
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL.String() != "https://api.github.com/" || client.UploadURL.String() != "https://uploads.github.com/" {
		t.Fatalf("base = %s, upload = %s", client.BaseURL, client.UploadURL)
	}
}

func TestNewPointsUploadsAtTheSameHost(t *testing.T) {
	client, err := New("", "https://ghe.example.com/api/v3")
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL.String() != "https://ghe.example.com/api/v3/" || client.UploadURL.String() != "https://ghe.example.com/api/uploads/" {
		t.Fatalf("base = %s, upload = %s", client.BaseURL, client.UploadURL)
	}

	public, err := New("", "https://api.github.com")
	if err != nil {
		t.Fatal(err)
	}
	if public.UploadURL.String() != "https://uploads.github.com/" {
		t.Fatalf("upload = %s, want https://uploads.github.com/", public.UploadURL)
	}
}

func TestNewRejectsABaseURLThatIsNotAbsoluteHTTP(t *testing.T) {
	for _, baseURL := range []string{"ghe.example.com/api/v3", "ftp://ghe.example.com/api/v3"} {
		t.Run(baseURL, func(t *testing.T) {
			client, err := New("", baseURL)
			if err == nil || client != nil || !strings.Contains(err.Error(), "absolute http or https") {
				t.Fatalf("New(%q) = %v, %v", baseURL, client, err)
			}
		})
	}
}

func TestResolveTokenPrefersEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "  env-token  ")
	writeGH(t, "#!/bin/sh\nexit 2\n")

	token, err := ResolveToken()
	if err != nil || token != "env-token" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestResolveTokenUsesGH(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "   ")
	writeGH(t, "#!/bin/sh\nprintf '  from-gh\\n'\n")

	token, err := ResolveToken()
	if err != nil || token != "from-gh" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestResolveTokenReportsUnavailable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	writeGH(t, "#!/bin/sh\necho no >&2\nexit 1\n")

	token, err := ResolveToken()
	if token != "" || !errors.Is(err, ErrAuthUnavailable) {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestResolveTokenReportsEmptyGHOutput(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	writeGH(t, "#!/bin/sh\nprintf '   \\n'\n")

	token, err := ResolveToken()
	if token != "" || !errors.Is(err, ErrAuthUnavailable) || errors.Is(err, ErrAuthTimedOut) {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestResolveTokenStopsWhenGHHangs(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	writeGH(t, "#!/bin/sh\n/bin/sleep 30\n")
	previous := ghAuthTimeout
	ghAuthTimeout = 50 * time.Millisecond
	t.Cleanup(func() { ghAuthTimeout = previous })

	started := time.Now()
	token, err := ResolveToken()
	if token != "" || !errors.Is(err, ErrAuthTimedOut) || errors.Is(err, ErrAuthUnavailable) || time.Since(started) > 2*time.Second {
		t.Fatalf("token = %q, err = %v, elapsed = %s", token, err, time.Since(started))
	}
	if !strings.Contains(err.Error(), ghAuthTimeout.String()) {
		t.Fatalf("error = %v, want the deadline", err)
	}
}

func writeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	gerror "github.com/project-init/gommon/pkg/errors"
)

// emailUsername is a native user's username, which is their email address and must stay out of errors.
const emailUsername = "user@example.com"

// newTestAdapter points an adapter at a server that answers every Cognito call with respond. It makes one attempt
// per call, so a throttling answer returns without the SDK's retry backoff.
func newTestAdapter(t *testing.T, respond http.HandlerFunc) *CognitoAdapter {
	t.Helper()
	server := httptest.NewServer(respond)
	t.Cleanup(server.Close)

	return &CognitoAdapter{
		cognitoClient: cognitoidentityprovider.New(cognitoidentityprovider.Options{
			BaseEndpoint:     aws.String(server.URL),
			Region:           "us-east-1",
			RetryMaxAttempts: 1,
		}),
		userPoolID: "us-east-1_pool",
	}
}

// writeCognitoError answers as the AdminUserGlobalSignOut API reference lists its errors: HTTP 400, with the error's
// shape name in the __type body field the AWS JSON 1.1 protocol specifies.
func writeCognitoError(w http.ResponseWriter, errorType, message string) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"__type": errorType, "message": message})
}

func TestAdminUserGlobalSignOutSignsOutTheUsernameInThePool(t *testing.T) {
	var target string
	var body map[string]string
	adapter := newTestAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		target = r.Header.Get("X-Amz-Target")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte("{}"))
	})

	if err := adapter.AdminUserGlobalSignOut(context.Background(), "testuser"); err != nil {
		t.Fatalf("AdminUserGlobalSignOut() error = %v", err)
	}
	if target != "AWSCognitoIdentityProviderService.AdminUserGlobalSignOut" {
		t.Errorf("X-Amz-Target = %q, want the AdminUserGlobalSignOut action", target)
	}
	if body["UserPoolId"] != "us-east-1_pool" || body["Username"] != "testuser" {
		t.Errorf("request body = %v, want the adapter's pool and the given username", body)
	}
}

func TestAdminUserGlobalSignOutMapsErrors(t *testing.T) {
	tests := []struct {
		errorType string
		message   string
		want      error
	}{
		{"UserNotFoundException", "This exception is thrown when a user isn't found.", gerror.ErrNotFound},
		{
			"TooManyRequestsException",
			"This exception is thrown when the user has made too many requests for a given operation.",
			gerror.ErrTooManyRequests,
		},
	}
	for _, tt := range tests {
		t.Run(tt.errorType, func(t *testing.T) {
			adapter := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
				writeCognitoError(w, tt.errorType, tt.message)
			})

			err := adapter.AdminUserGlobalSignOut(context.Background(), emailUsername)
			if !errors.Is(err, tt.want) || !strings.Contains(err.Error(), tt.message) {
				t.Errorf("AdminUserGlobalSignOut() error = %v, want one wrapping %v with Cognito's message", err, tt.want)
			}
			if strings.Contains(err.Error(), emailUsername) {
				t.Errorf("AdminUserGlobalSignOut() error = %v, want it without the username", err)
			}
		})
	}
}

// TestAdminUserGlobalSignOutPassesOtherErrorsThrough keeps any other Cognito error matchable as itself, under no
// gerror sentinel.
func TestAdminUserGlobalSignOutPassesOtherErrorsThrough(t *testing.T) {
	adapter := newTestAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
		writeCognitoError(w, "NotAuthorizedException", "This exception is thrown when a user isn't authorized.")
	})

	err := adapter.AdminUserGlobalSignOut(context.Background(), emailUsername)
	if err != nil && strings.Contains(err.Error(), emailUsername) {
		t.Errorf("AdminUserGlobalSignOut() error = %v, want it without the username", err)
	}
	var notAuthorized *types.NotAuthorizedException
	if !errors.As(err, &notAuthorized) || errors.Is(err, gerror.ErrNotFound) || errors.Is(err, gerror.ErrTooManyRequests) {
		t.Errorf("AdminUserGlobalSignOut() error = %v, want the NotAuthorizedException under no gerror sentinel", err)
	}
}

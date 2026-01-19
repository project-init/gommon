package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/project-init/gommon/pkg/env"
	"github.com/stretchr/testify/assert"
)

var urlMap = UrlMap{
	env.Development: []string{"http://localhost:3000", "http://10.0.0.2:8081"},
	env.Staging:     []string{"https://app.staging.project-init.com"},
	env.Production:  []string{"https://app.production.project-init.com"},
}

func TestGetCorsHandlerAllowedOrigins(t *testing.T) {
	tests := []struct {
		name        string
		env         env.Env
		expectError bool
	}{
		{
			name:        "valid URL's in development",
			env:         env.Development,
			expectError: false,
		},
		{
			name:        "staging environment",
			env:         env.Staging,
			expectError: false,
		},

		{
			name:        "production environment",
			env:         env.Production,
			expectError: false,
		},
		{
			name:        "unknown environment",
			env:         env.Env("unknown"),
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := GetCorsHandler(test.env, urlMap, []string{})
			if test.expectError {
				assert.Error(t, err, "Expected error for test case: %s", test.name)
				assert.Nil(t, handler, "Expected nil CORS handler for test case: %s", test.name)
			} else {
				assert.NoError(t, err, "Expected no error for test case: %s", test.name)
				assert.NotNil(t, handler, "Expected non-nil CORS handler for test case: %s", test.name)

				for _, origin := range urlMap[test.env] {
					r := &http.Request{
						Header: http.Header{
							"Origin": []string{origin},
						},
					}
					assert.True(t, handler.OriginAllowed(r), "Origin should be allowed for test case: %s", test.name)
				}
			}

		})
	}
}

func TestGetCorsHandlerAllowedHeaders(t *testing.T) {
	tests := []struct {
		name                string
		extraAllowedHeaders []string
		requestedHeaders    string
		expectedHeaders     []string
	}{
		{
			name:                "base headers only",
			extraAllowedHeaders: []string{},
			requestedHeaders:    "authorization, content-type",
			expectedHeaders:     []string{"authorization", "content-type"},
		},
		{
			name:                "single custom header",
			extraAllowedHeaders: []string{"x-custom-header"},
			requestedHeaders:    "x-custom-header",
			expectedHeaders:     []string{"x-custom-header"},
		},
		{
			name:                "multiple custom headers",
			extraAllowedHeaders: []string{"x-custom-header", "x-api-key"},
			requestedHeaders:    "x-api-key, x-custom-header",
			expectedHeaders:     []string{"x-api-key", "x-custom-header"},
		},
		{
			name:                "multiple custom headers with base headers",
			extraAllowedHeaders: []string{"x-api-key", "x-custom-header"},
			requestedHeaders:    "authorization, content-type, x-api-key, x-custom-header",
			expectedHeaders:     []string{"authorization", "content-type", "x-api-key", "x-custom-header"},
		},
		{
			name:                "headers supplied in any order with mixed case",
			extraAllowedHeaders: []string{"X-Custom-Header", "X-Api-Key"},
			requestedHeaders:    "authorization, content-type, x-api-key, x-custom-header",
			expectedHeaders:     []string{"authorization", "content-type", "x-api-key", "x-custom-header"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := GetCorsHandler(env.Development, urlMap, test.extraAllowedHeaders)
			assert.NoError(t, err)

			// Use the first allowed origin from urlMap for testing
			allowedOrigin := urlMap[env.Development][0]

			// Simulates a preflight request
			req := httptest.NewRequest("OPTIONS", allowedOrigin, nil)
			req.Header.Set("Origin", allowedOrigin)
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			req.Header.Set("Access-Control-Request-Headers", test.requestedHeaders)

			w := httptest.NewRecorder()
			corsMiddleware := handler.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			corsMiddleware.ServeHTTP(w, req)

			allowedHeaders := w.Header().Get("Access-Control-Allow-Headers")
			for _, expected := range test.expectedHeaders {
				assert.Contains(t, allowedHeaders, expected, "Expected header %s to be allowed", expected)
			}
		})
	}
}

func TestGetCorsHandlerDisallowedOrigin(t *testing.T) {
	tests := []struct {
		name             string
		disallowedOrigin string
		requestedHeaders string
	}{
		{
			name:             "completely different origin",
			disallowedOrigin: "http://evil.com",
			requestedHeaders: "authorization, content-type",
		},
		{
			name:             "localhost with wrong port",
			disallowedOrigin: "http://localhost:9999",
			requestedHeaders: "authorization, content-type",
		},
		{
			name:             "different subdomain",
			disallowedOrigin: "http://attacker.localhost:3000",
			requestedHeaders: "authorization, content-type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := GetCorsHandler(env.Development, urlMap, []string{})
			assert.NoError(t, err)

			// Simulates a preflight request from a disallowed origin
			req := httptest.NewRequest("OPTIONS", test.disallowedOrigin, nil)
			req.Header.Set("Origin", test.disallowedOrigin)
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			req.Header.Set("Access-Control-Request-Headers", test.requestedHeaders)

			w := httptest.NewRecorder()
			corsMiddleware := handler.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			corsMiddleware.ServeHTTP(w, req)

			// Should NOT have Access-Control-Allow-Origin header for disallowed origins
			allowOrigin := w.Header().Get("Access-Control-Allow-Origin")
			assert.Empty(t, allowOrigin, "Expected no Access-Control-Allow-Origin header for disallowed origin")

			// Should NOT have Access-Control-Allow-Headers for disallowed origins
			allowedHeaders := w.Header().Get("Access-Control-Allow-Headers")
			assert.Empty(t, allowedHeaders, "Expected no Access-Control-Allow-Headers header for disallowed origin")

			// Should still have Vary header
			vary := w.Header().Get("Vary")
			assert.NotEmpty(t, vary, "Expected Vary header to be present")
		})
	}
}

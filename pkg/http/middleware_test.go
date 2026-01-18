package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/project-init/gommon/pkg/env"
	"github.com/stretchr/testify/assert"
)

func TestGetCorsHandlerAllowedOrigins(t *testing.T) {
	tests := []struct {
		name           string
		env            env.Env
		port           string
		expectError    bool
		allowedOrigins string
		allowedHeaders []string
	}{
		{
			name:           "valid port in development",
			env:            env.Development,
			port:           "8081",
			expectError:    false,
			allowedOrigins: "http://localhost:8081",
		},
		{
			name:        "invalid port string",
			env:         env.Development,
			port:        "invalid",
			expectError: true,
		},
		{
			name:        "port out of range - negative",
			env:         env.Development,
			port:        "-1",
			expectError: true,
		},
		{
			name:        "port out of range - too high",
			env:         env.Development,
			port:        "65536",
			expectError: true,
		},
		{
			name:           "minimum valid port",
			env:            env.Development,
			port:           "1",
			expectError:    false,
			allowedOrigins: "http://localhost:1",
		},
		{
			name:           "maximum valid port",
			env:            env.Development,
			port:           "65535",
			expectError:    false,
			allowedOrigins: "http://localhost:65535",
		},
		{
			name:           "staging environment",
			env:            env.Staging,
			port:           "",
			expectError:    false,
			allowedOrigins: "https://app.staging.project-init.com",
		},
		{
			name:           "staging environment with port -- should ignore port",
			env:            env.Staging,
			port:           "8081",
			expectError:    false,
			allowedOrigins: "https://app.staging.project-init.com",
		},
		{
			name:           "production environment",
			env:            env.Production,
			port:           "",
			expectError:    false,
			allowedOrigins: "https://app.production.project-init.com",
		},
		{
			name:        "production environment with port -- should ignore port",
			env:         env.Production,
			port:        "8081",
			expectError: false,
		},
		{
			name:        "unknown environment",
			env:         env.Env("unknown"),
			port:        "8081",
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := GetCorsHandler(test.env, test.port, []string{})
			if test.expectError {
				assert.Error(t, err, "Expected error for test case: %s", test.name)
				assert.Nil(t, handler, "Expected nil CORS handler for test case: %s", test.name)
			} else {
				assert.NoError(t, err, "Expected no error for test case: %s", test.name)
				assert.NotNil(t, handler, "Expected non-nil CORS handler for test case: %s", test.name)
				if test.allowedOrigins != "" {
					r := &http.Request{
						Header: http.Header{
							"Origin": []string{test.allowedOrigins},
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
			extraAllowedHeaders: []string{"X-Custom-Header"},
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
			extraAllowedHeaders: []string{"x-custom-header", "x-api-key"},
			requestedHeaders:    "x-api-key, x-custom-header, authorization, content-type",
			expectedHeaders:     []string{"x-api-key", "x-custom-header", "authorization", "content-type"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := GetCorsHandler(env.Development, "3000", test.extraAllowedHeaders)
			assert.NoError(t, err)

			// Simulates a preflight request
			req := httptest.NewRequest("OPTIONS", "http://localhost:3000", nil)
			req.Header.Set("Origin", "http://localhost:3000")
			req.Header.Set("Access-Control-Request-Method", "POST")
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

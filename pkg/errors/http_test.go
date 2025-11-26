package errors

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToErrorFromHTTP(t *testing.T) {
	tests := []struct {
		status   int
		expected error
	}{
		{status: http.StatusBadRequest, expected: ErrBadRequest},
		{status: http.StatusUnauthorized, expected: ErrPermissionDenied},
		{status: http.StatusForbidden, expected: ErrForbidden},
		{status: http.StatusNotFound, expected: ErrNotFound},
		{status: http.StatusConflict, expected: ErrConflict},
		{status: http.StatusTooManyRequests, expected: ErrTooManyRequests},
		{status: http.StatusInternalServerError, expected: ErrInternalServerError},
		{status: http.StatusNotImplemented, expected: ErrNotImplemented},
		{status: http.StatusBadGateway, expected: ErrBadGateway},
		{status: http.StatusServiceUnavailable, expected: ErrServiceUnavailable},
		{status: http.StatusGatewayTimeout, expected: ErrTimeout},
		{status: http.StatusPreconditionFailed, expected: ErrPreconditionFailed},
		// Status we haven't mapped
		{status: http.StatusFailedDependency, expected: ErrInternalServerError},
	}

	for _, test := range tests {
		err := ToErrorFromHTTP(test.status)
		assert.Equal(t, test.expected, err)
	}
}

func TestToHTTPFromError(t *testing.T) {
	tests := []struct {
		err      error
		expected int
	}{
		{expected: http.StatusOK, err: nil},
		{expected: http.StatusBadRequest, err: ErrBadRequest},
		{expected: http.StatusUnauthorized, err: ErrPermissionDenied},
		{expected: http.StatusForbidden, err: ErrForbidden},
		{expected: http.StatusNotFound, err: ErrNotFound},
		{expected: http.StatusConflict, err: ErrConflict},
		{expected: http.StatusTooManyRequests, err: ErrTooManyRequests},
		{expected: http.StatusInternalServerError, err: ErrInternalServerError},
		{expected: http.StatusNotImplemented, err: ErrNotImplemented},
		{expected: http.StatusBadGateway, err: ErrBadGateway},
		{expected: http.StatusServiceUnavailable, err: ErrServiceUnavailable},
		{expected: http.StatusGatewayTimeout, err: ErrTimeout},
		{expected: http.StatusPreconditionFailed, err: ErrPreconditionFailed},
	}

	for _, test := range tests {
		status := ToHTTPFromError(test.err)
		assert.Equal(t, test.expected, status)
	}
}

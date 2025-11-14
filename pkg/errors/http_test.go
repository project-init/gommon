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
		{status: http.StatusForbidden, expected: ErrPermissionDenied},
		{status: http.StatusNotFound, expected: ErrNotFound},
		{status: http.StatusConflict, expected: ErrConflict},
		{status: http.StatusTooManyRequests, expected: ErrTooManyRequests},
		{status: http.StatusInternalServerError, expected: ErrInternalServerError},
		{status: http.StatusNotImplemented, expected: ErrNotImplemented},
		{status: http.StatusBadGateway, expected: ErrBadGateway},
		{status: http.StatusServiceUnavailable, expected: ErrServiceUnavailable},
		{status: http.StatusGatewayTimeout, expected: ErrTimeout},
		// Status we haven't mapped
		{status: http.StatusFailedDependency, expected: ErrInternalServerError},
	}

	for _, test := range tests {
		err := ToErrorFromHTTP(test.status)
		assert.Equal(t, test.expected, err)
	}
}

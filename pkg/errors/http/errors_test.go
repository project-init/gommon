package http

import (
	nethttp "net/http"
	"testing"

	gerror "github.com/project-init/gommon/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestToErrorFromHTTP(t *testing.T) {
	tests := []struct {
		status   int
		expected error
	}{
		{status: nethttp.StatusBadRequest, expected: gerror.ErrBadRequest},
		{status: nethttp.StatusUnauthorized, expected: gerror.ErrPermissionDenied},
		{status: nethttp.StatusForbidden, expected: gerror.ErrForbidden},
		{status: nethttp.StatusNotFound, expected: gerror.ErrNotFound},
		{status: nethttp.StatusConflict, expected: gerror.ErrConflict},
		{status: nethttp.StatusTooManyRequests, expected: gerror.ErrTooManyRequests},
		{status: nethttp.StatusInternalServerError, expected: gerror.ErrInternalServerError},
		{status: nethttp.StatusNotImplemented, expected: gerror.ErrNotImplemented},
		{status: nethttp.StatusBadGateway, expected: gerror.ErrBadGateway},
		{status: nethttp.StatusServiceUnavailable, expected: gerror.ErrServiceUnavailable},
		{status: nethttp.StatusGatewayTimeout, expected: gerror.ErrTimeout},
		{status: nethttp.StatusPreconditionFailed, expected: gerror.ErrPreconditionFailed},
		// Status we haven't mapped
		{status: nethttp.StatusFailedDependency, expected: gerror.ErrInternalServerError},
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
		{expected: nethttp.StatusOK, err: nil},
		{expected: nethttp.StatusBadRequest, err: gerror.ErrBadRequest},
		{expected: nethttp.StatusUnauthorized, err: gerror.ErrPermissionDenied},
		{expected: nethttp.StatusForbidden, err: gerror.ErrForbidden},
		{expected: nethttp.StatusNotFound, err: gerror.ErrNotFound},
		{expected: nethttp.StatusConflict, err: gerror.ErrConflict},
		{expected: nethttp.StatusTooManyRequests, err: gerror.ErrTooManyRequests},
		{expected: nethttp.StatusInternalServerError, err: gerror.ErrInternalServerError},
		{expected: nethttp.StatusNotImplemented, err: gerror.ErrNotImplemented},
		{expected: nethttp.StatusBadGateway, err: gerror.ErrBadGateway},
		{expected: nethttp.StatusServiceUnavailable, err: gerror.ErrServiceUnavailable},
		{expected: nethttp.StatusGatewayTimeout, err: gerror.ErrTimeout},
		{expected: nethttp.StatusPreconditionFailed, err: gerror.ErrPreconditionFailed},
	}

	for _, test := range tests {
		status := ToHTTPFromError(test.err)
		assert.Equal(t, test.expected, status)
	}
}

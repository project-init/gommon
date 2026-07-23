package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	gerror "github.com/project-init/gommon/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnectCodeFromError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected connect.Code
	}{
		{name: "not found", err: gerror.ErrNotFound, expected: connect.CodeNotFound},
		{name: "bad request", err: gerror.ErrBadRequest, expected: connect.CodeInvalidArgument},
		{name: "conflict", err: gerror.ErrConflict, expected: connect.CodeAlreadyExists},
		{name: "forbidden", err: gerror.ErrForbidden, expected: connect.CodePermissionDenied},
		{name: "permission denied", err: gerror.ErrPermissionDenied, expected: connect.CodePermissionDenied},
		{name: "too many requests", err: gerror.ErrTooManyRequests, expected: connect.CodeResourceExhausted},
		{name: "not implemented", err: gerror.ErrNotImplemented, expected: connect.CodeUnimplemented},
		{name: "service unavailable", err: gerror.ErrServiceUnavailable, expected: connect.CodeUnavailable},
		{name: "timeout", err: gerror.ErrTimeout, expected: connect.CodeDeadlineExceeded},
		{name: "precondition failed", err: gerror.ErrPreconditionFailed, expected: connect.CodeFailedPrecondition},
		{name: "context canceled", err: context.Canceled, expected: connect.CodeCanceled},
		{name: "cancelled", err: gerror.ErrCancelled, expected: connect.CodeCanceled},
		{name: "internal server error", err: gerror.ErrInternalServerError, expected: connect.CodeInternal},
		{name: "unmapped", err: errors.New("unmapped error"), expected: connect.CodeInternal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, ConnectCodeFromError(test.err))
		})
	}
}

func TestConnectCodeFromWrappedError(t *testing.T) {
	err := fmt.Errorf("lookup failed: %w", gerror.ErrNotFound)

	assert.Equal(t, connect.CodeNotFound, ConnectCodeFromError(err))
}

func TestNewConnectError(t *testing.T) {
	cause := fmt.Errorf("request failed: %w", gerror.ErrBadRequest)

	err := NewConnectError(cause)

	require.NotNil(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, err.Code())
	assert.ErrorIs(t, err, cause)
}

package errors

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
)

func TestToConnectCodeFromError(t *testing.T) {
	tests := []struct {
		error    error
		expected connect.Code
	}{
		{error: ErrNotFound, expected: connect.CodeNotFound},
		{error: ErrBadRequest, expected: connect.CodeInvalidArgument},
		{error: ErrPermissionDenied, expected: connect.CodePermissionDenied},
		{error: ErrNotImplemented, expected: connect.CodeUnimplemented},
		{error: ErrServiceUnavailable, expected: connect.CodeUnavailable},
		{error: ErrConflict, expected: connect.CodeAlreadyExists},
		{error: ErrTimeout, expected: connect.CodeDeadlineExceeded},
		{error: ErrPreconditionFailed, expected: connect.CodeFailedPrecondition},
		{error: context.Canceled, expected: connect.CodeCanceled},
		{error: ErrInternalServerError, expected: connect.CodeInternal},
		// Error we haven't mapped
		{error: fmt.Errorf("unmapped error"), expected: connect.CodeInternal},
	}

	for _, test := range tests {
		code := ConnectCodeFromError(test.error)
		assert.Equal(t, test.expected, code)
	}
}

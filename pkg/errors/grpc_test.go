package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToGrpcFromError(t *testing.T) {
	tests := []struct {
		error    error
		expected codes.Code
	}{
		{error: nil, expected: codes.OK},
		{error: ErrNotFound, expected: codes.NotFound},
		{error: ErrBadRequest, expected: codes.InvalidArgument},
		{error: ErrPermissionDenied, expected: codes.PermissionDenied},
		{error: ErrNotImplemented, expected: codes.Unimplemented},
		{error: ErrServiceUnavailable, expected: codes.Unavailable},
		{error: ErrConflict, expected: codes.AlreadyExists},
		{error: ErrTimeout, expected: codes.DeadlineExceeded},
		{error: ErrPreconditionFailed, expected: codes.FailedPrecondition},
		{error: context.Canceled, expected: codes.Canceled},
		{error: ErrInternalServerError, expected: codes.Internal},
		// Error we haven't mapped
		{error: fmt.Errorf("unmapped error"), expected: codes.Internal},
	}

	for _, test := range tests {
		code := GrpcFromError(test.error)
		assert.Equal(t, test.expected, code)
	}
}

func TestToErrorFromGrpc(t *testing.T) {
	tests := []struct {
		error    error
		expected error
	}{
		{error: status.Error(codes.OK, ""), expected: nil},
		{error: status.Error(codes.NotFound, ""), expected: ErrNotFound},
		{error: status.Error(codes.InvalidArgument, ""), expected: ErrBadRequest},
		{error: status.Error(codes.PermissionDenied, ""), expected: ErrPermissionDenied},
		{error: status.Error(codes.Unimplemented, ""), expected: ErrNotImplemented},
		{error: status.Error(codes.Unavailable, ""), expected: ErrServiceUnavailable},
		{error: status.Error(codes.AlreadyExists, ""), expected: ErrConflict},
		{error: status.Error(codes.DeadlineExceeded, ""), expected: ErrTimeout},
		{error: status.Error(codes.FailedPrecondition, ""), expected: ErrPreconditionFailed},
		{error: status.Error(codes.Canceled, ""), expected: ErrCancelled},
		{error: status.Error(codes.Internal, ""), expected: ErrInternalServerError},
	}

	for _, test := range tests {
		err := ErrFromGrpc(test.error)
		assert.True(t, errors.Is(err, test.expected), err)
	}
}

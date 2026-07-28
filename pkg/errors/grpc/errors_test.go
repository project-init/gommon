package grpc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	gerror "github.com/project-init/gommon/pkg/errors"
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
		{error: gerror.ErrNotFound, expected: codes.NotFound},
		{error: gerror.ErrBadRequest, expected: codes.InvalidArgument},
		{error: gerror.ErrPermissionDenied, expected: codes.PermissionDenied},
		{error: gerror.ErrNotImplemented, expected: codes.Unimplemented},
		{error: gerror.ErrServiceUnavailable, expected: codes.Unavailable},
		{error: gerror.ErrConflict, expected: codes.AlreadyExists},
		{error: gerror.ErrTimeout, expected: codes.DeadlineExceeded},
		{error: gerror.ErrPreconditionFailed, expected: codes.FailedPrecondition},
		{error: context.Canceled, expected: codes.Canceled},
		{error: gerror.ErrInternalServerError, expected: codes.Internal},
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
		{error: status.Error(codes.NotFound, ""), expected: gerror.ErrNotFound},
		{error: status.Error(codes.InvalidArgument, ""), expected: gerror.ErrBadRequest},
		{error: status.Error(codes.PermissionDenied, ""), expected: gerror.ErrPermissionDenied},
		{error: status.Error(codes.Unimplemented, ""), expected: gerror.ErrNotImplemented},
		{error: status.Error(codes.Unavailable, ""), expected: gerror.ErrServiceUnavailable},
		{error: status.Error(codes.AlreadyExists, ""), expected: gerror.ErrConflict},
		{error: status.Error(codes.DeadlineExceeded, ""), expected: gerror.ErrTimeout},
		{error: status.Error(codes.FailedPrecondition, ""), expected: gerror.ErrPreconditionFailed},
		{error: status.Error(codes.Canceled, ""), expected: gerror.ErrCancelled},
		{error: status.Error(codes.Internal, ""), expected: gerror.ErrInternalServerError},
	}

	for _, test := range tests {
		err := ErrFromGrpc(test.error)
		assert.True(t, errors.Is(err, test.expected), err)
	}
}

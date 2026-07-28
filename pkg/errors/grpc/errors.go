package grpc

import (
	"context"
	"errors"

	gerror "github.com/project-init/gommon/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GrpcFromError(err error) codes.Code {
	switch {
	case err == nil:
		return codes.OK
	case errors.Is(err, gerror.ErrNotFound):
		return codes.NotFound
	case errors.Is(err, gerror.ErrBadRequest):
		return codes.InvalidArgument
	case errors.Is(err, gerror.ErrConflict):
		return codes.AlreadyExists
	case errors.Is(err, gerror.ErrPermissionDenied):
		return codes.PermissionDenied
	case errors.Is(err, gerror.ErrNotImplemented):
		return codes.Unimplemented
	case errors.Is(err, gerror.ErrServiceUnavailable):
		return codes.Unavailable
	case errors.Is(err, gerror.ErrTimeout):
		return codes.DeadlineExceeded
	case errors.Is(err, gerror.ErrPreconditionFailed):
		return codes.FailedPrecondition
	case errors.Is(err, context.Canceled):
		return codes.Canceled
	case errors.Is(err, gerror.ErrInternalServerError):
		return codes.Internal
	default:
		return codes.Internal
	}
}

func ErrFromGrpc(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return err
	}

	switch st.Code() {
	case codes.OK:
		return nil
	case codes.NotFound:
		return gerror.ErrNotFound
	case codes.InvalidArgument:
		return gerror.ErrBadRequest
	case codes.AlreadyExists:
		return gerror.ErrConflict
	case codes.PermissionDenied:
		return gerror.ErrPermissionDenied
	case codes.Unimplemented:
		return gerror.ErrNotImplemented
	case codes.Unavailable:
		return gerror.ErrServiceUnavailable
	case codes.DeadlineExceeded:
		return gerror.ErrTimeout
	case codes.FailedPrecondition:
		return gerror.ErrPreconditionFailed
	case codes.Canceled:
		return gerror.ErrCancelled
	case codes.Internal:
		return gerror.ErrInternalServerError
	default:
		return gerror.ErrInternalServerError
	}
}

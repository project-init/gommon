package errors

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GrpcFromError(err error) codes.Code {
	switch {
	case err == nil:
		return codes.OK
	case errors.Is(err, ErrNotFound):
		return codes.NotFound
	case errors.Is(err, ErrBadRequest):
		return codes.InvalidArgument
	case errors.Is(err, ErrConflict):
		return codes.AlreadyExists
	case errors.Is(err, ErrPermissionDenied):
		return codes.PermissionDenied
	case errors.Is(err, ErrNotImplemented):
		return codes.Unimplemented
	case errors.Is(err, ErrServiceUnavailable):
		return codes.Unavailable
	case errors.Is(err, ErrTimeout):
		return codes.DeadlineExceeded
	case errors.Is(err, ErrPreconditionFailed):
		return codes.FailedPrecondition
	case errors.Is(err, context.Canceled):
		return codes.Canceled
	case errors.Is(err, ErrInternalServerError):
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
		return ErrNotFound
	case codes.InvalidArgument:
		return ErrBadRequest
	case codes.AlreadyExists:
		return ErrConflict
	case codes.PermissionDenied:
		return ErrPermissionDenied
	case codes.Unimplemented:
		return ErrNotImplemented
	case codes.Unavailable:
		return ErrServiceUnavailable
	case codes.DeadlineExceeded:
		return ErrTimeout
	case codes.FailedPrecondition:
		return ErrPreconditionFailed
	case codes.Canceled:
		return ErrCancelled
	case codes.Internal:
		return ErrInternalServerError
	default:
		return ErrInternalServerError
	}
}

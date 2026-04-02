package errors

import (
	"context"
	"errors"

	"connectrpc.com/connect"
)

func NewConnectError(err error) *connect.Error {
	return connect.NewError(ConnectCodeFromError(err), err)
}

func ConnectCodeFromError(err error) connect.Code {
	switch {
	case errors.Is(err, ErrNotFound):
		return connect.CodeNotFound
	case errors.Is(err, ErrBadRequest):
		return connect.CodeInvalidArgument
	case errors.Is(err, ErrConflict):
		return connect.CodeAlreadyExists
	case errors.Is(err, ErrPermissionDenied):
		return connect.CodePermissionDenied
	case errors.Is(err, ErrNotImplemented):
		return connect.CodeUnimplemented
	case errors.Is(err, ErrServiceUnavailable):
		return connect.CodeUnavailable
	case errors.Is(err, ErrTimeout):
		return connect.CodeDeadlineExceeded
	case errors.Is(err, ErrPreconditionFailed):
		return connect.CodeFailedPrecondition
	case errors.Is(err, context.Canceled):
		return connect.CodeCanceled
	case errors.Is(err, ErrCancelled):
		return connect.CodeCanceled
	case errors.Is(err, ErrInternalServerError):
		return connect.CodeInternal
	default:
		return connect.CodeInternal
	}
}

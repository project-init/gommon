package errors

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	gerror "github.com/project-init/gommon/pkg/errors"
)

func NewConnectError(err error) *connect.Error {
	return connect.NewError(ConnectCodeFromError(err), err)
}

func ConnectCodeFromError(err error) connect.Code {
	switch {
	case errors.Is(err, gerror.ErrNotFound):
		return connect.CodeNotFound
	case errors.Is(err, gerror.ErrBadRequest):
		return connect.CodeInvalidArgument
	case errors.Is(err, gerror.ErrConflict):
		return connect.CodeAlreadyExists
	case errors.Is(err, gerror.ErrForbidden):
		return connect.CodePermissionDenied
	case errors.Is(err, gerror.ErrPermissionDenied):
		return connect.CodePermissionDenied
	case errors.Is(err, gerror.ErrTooManyRequests):
		return connect.CodeResourceExhausted
	case errors.Is(err, gerror.ErrNotImplemented):
		return connect.CodeUnimplemented
	case errors.Is(err, gerror.ErrServiceUnavailable):
		return connect.CodeUnavailable
	case errors.Is(err, gerror.ErrTimeout):
		return connect.CodeDeadlineExceeded
	case errors.Is(err, gerror.ErrPreconditionFailed):
		return connect.CodeFailedPrecondition
	case errors.Is(err, context.Canceled):
		return connect.CodeCanceled
	case errors.Is(err, gerror.ErrCancelled):
		return connect.CodeCanceled
	case errors.Is(err, gerror.ErrInternalServerError):
		return connect.CodeInternal
	default:
		return connect.CodeInternal
	}
}

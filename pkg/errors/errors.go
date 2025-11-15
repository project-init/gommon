package errors

import "errors"

var (
	ErrRetriable    = errors.New("retriable")
	ErrNonRetriable = errors.New("non retriable")

	ErrExternal  = errors.New("external")
	ErrCancelled = errors.New("cancelled")

	ErrBadGateway          = errors.New("bad gateway")
	ErrTimeout             = errors.New("timeout")
	ErrBadRequest          = errors.New("bad request")
	ErrPermissionDenied    = errors.New("permission denied")
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("conflict")
	ErrTooManyRequests     = errors.New("too many requests")
	ErrInternalServerError = errors.New("internal server error")
	ErrNotImplemented      = errors.New("not implemented")
	ErrServiceUnavailable  = errors.New("service unavailable")
	ErrPreconditionFailed  = errors.New("precondition failed")
)

package http

import (
	"errors"
	nethttp "net/http"

	gerror "github.com/project-init/gommon/pkg/errors"
)

func ToErrorFromHTTP(status int) error {
	switch status {
	case nethttp.StatusBadRequest: // 400
		return gerror.ErrBadRequest
	case nethttp.StatusUnauthorized: // 401
		return gerror.ErrPermissionDenied
	case nethttp.StatusForbidden: // 403
		return gerror.ErrForbidden
	case nethttp.StatusNotFound: // 404
		return gerror.ErrNotFound
	case nethttp.StatusConflict: // 409
		return gerror.ErrConflict
	case nethttp.StatusTooManyRequests: // 429
		return gerror.ErrTooManyRequests
	case nethttp.StatusInternalServerError: // 500
		return gerror.ErrInternalServerError
	case nethttp.StatusNotImplemented: // 501
		return gerror.ErrNotImplemented
	case nethttp.StatusBadGateway: // 502
		return gerror.ErrBadGateway
	case nethttp.StatusServiceUnavailable: // 503
		return gerror.ErrServiceUnavailable
	case nethttp.StatusGatewayTimeout: // 504
		return gerror.ErrTimeout
	case nethttp.StatusPreconditionFailed:
		return gerror.ErrPreconditionFailed
	default:
		return gerror.ErrInternalServerError
	}
}

func ToHTTPFromError(err error) int {
	if err == nil {
		return nethttp.StatusOK
	}
	if errors.Is(err, gerror.ErrBadRequest) {
		return nethttp.StatusBadRequest
	} else if errors.Is(err, gerror.ErrPermissionDenied) {
		return nethttp.StatusUnauthorized
	} else if errors.Is(err, gerror.ErrForbidden) {
		return nethttp.StatusForbidden
	} else if errors.Is(err, gerror.ErrBadRequest) {
		return nethttp.StatusBadRequest
	} else if errors.Is(err, gerror.ErrNotFound) {
		return nethttp.StatusNotFound
	} else if errors.Is(err, gerror.ErrConflict) {
		return nethttp.StatusConflict
	} else if errors.Is(err, gerror.ErrTooManyRequests) {
		return nethttp.StatusTooManyRequests
	} else if errors.Is(err, gerror.ErrInternalServerError) {
		return nethttp.StatusInternalServerError
	} else if errors.Is(err, gerror.ErrNotImplemented) {
		return nethttp.StatusNotImplemented
	} else if errors.Is(err, gerror.ErrBadGateway) {
		return nethttp.StatusBadGateway
	} else if errors.Is(err, gerror.ErrTimeout) {
		return nethttp.StatusGatewayTimeout
	} else if errors.Is(err, gerror.ErrServiceUnavailable) {
		return nethttp.StatusServiceUnavailable
	} else if errors.Is(err, gerror.ErrPreconditionFailed) {
		return nethttp.StatusPreconditionFailed
	} else {
		return nethttp.StatusInternalServerError
	}
}

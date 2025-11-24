package errors

import (
	"errors"
	"net/http"
)

func ToErrorFromHTTP(status int) error {
	switch status {
	case http.StatusBadRequest: // 400
		return ErrBadRequest
	case http.StatusUnauthorized: // 401
		return ErrPermissionDenied
	case http.StatusForbidden: // 403
		return ErrPermissionDenied
	case http.StatusNotFound: // 404
		return ErrNotFound
	case http.StatusConflict: // 409
		return ErrConflict
	case http.StatusTooManyRequests: // 429
		return ErrTooManyRequests
	case http.StatusInternalServerError: // 500
		return ErrInternalServerError
	case http.StatusNotImplemented: // 501
		return ErrNotImplemented
	case http.StatusBadGateway: // 502
		return ErrBadGateway
	case http.StatusServiceUnavailable: // 503
		return ErrServiceUnavailable
	case http.StatusGatewayTimeout: // 504
		return ErrTimeout
	default:
		return ErrInternalServerError
	}
}

func ToHTTPFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, ErrBadRequest) {
		return http.StatusBadRequest
	} else if errors.Is(err, ErrPermissionDenied) {
		return http.StatusUnauthorized
	} else if errors.Is(err, ErrBadRequest) {
		return http.StatusBadRequest
	} else if errors.Is(err, ErrNotFound) {
		return http.StatusNotFound
	} else if errors.Is(err, ErrConflict) {
		return http.StatusConflict
	} else if errors.Is(err, ErrTooManyRequests) {
		return http.StatusTooManyRequests
	} else if errors.Is(err, ErrInternalServerError) {
		return http.StatusInternalServerError
	} else if errors.Is(err, ErrNotImplemented) {
		return http.StatusNotImplemented
	} else if errors.Is(err, ErrBadGateway) {
		return http.StatusBadGateway
	} else if errors.Is(err, ErrTimeout) {
		return http.StatusGatewayTimeout
	} else if errors.Is(err, ErrServiceUnavailable) {
		return http.StatusServiceUnavailable
	} else {
		return http.StatusInternalServerError
	}
}

package errors

import "net/http"

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

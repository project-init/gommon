package http

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/project-init/gommon/pkg/env"
	"github.com/rs/cors"
)

// GetCorsHandler returns a CORS handler configured based on the environment and port. It follows the
// principle of least privilege by restricting allowed origins based on environment, limits headers to a minimum
// of "Content-Type" and "Authorization" plus any additional headers provided, and allows only essential HTTP methods.
// It will return an error if invoked with an invalid port number in the development environment, port being optional
// in higher environments.
//
//	GetCorsHandler(env.Development, "8080", []string{"X-Custom-Header"})
//
// Allows origin "http://localhost:8080" with Allowed-Headers: "Content-Type", "Authorization", "X-Custom-Header"
//
//	GetCorsHandler(env.Staging, "", nil)
//
// Allows origin "https://app.staging.project-init.com" with default headers
//
//	GetCorsHandler(env.Production, "", []string{"X-Another-Header"})
//
// Allows origin "https://app.production.project-init.com" with specified headers
func GetCorsHandler(e env.Env, port string, allowedHeaders []string) (*cors.Cors, error) {
	baseHeaders := []string{"Content-Type", "Authorization"}
	allHeaders := append(baseHeaders, allowedHeaders...)

	var corsOptions = cors.Options{
		AllowCredentials: true,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowedHeaders:   allHeaders,
	}
	switch e {
	case env.Development:
		if portNum, err := strconv.Atoi(port); err == nil {
			if portNum > 0 && portNum <= 65535 {
				corsOptions.AllowedOrigins = []string{fmt.Sprintf("http://localhost:%s", port)}
			} else {
				return nil, fmt.Errorf("port number outside of range 0-65535: %s", port)
			}
		} else {
			return nil, fmt.Errorf("invalid port number: %w", err)
		}
	case env.Staging, env.Production:
		if e == env.Staging {
			corsOptions.AllowedOrigins = []string{"https://app.staging.project-init.com"}
		}
		if e == env.Production {
			corsOptions.AllowedOrigins = []string{"https://app.production.project-init.com"}
		}
	default:
		return nil, fmt.Errorf("unknown environment: %s", e)
	}

	return cors.New(corsOptions), nil
}

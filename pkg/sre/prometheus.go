package sre

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func ServePrometheusEndpoint(port int) error {
	if port < 9002 || port > 65535 {
		return fmt.Errorf("invalid port %d, must be between 9002 and 65535", port)
	}
	return http.ListenAndServe(fmt.Sprintf(":%d", port), promhttp.Handler())
}

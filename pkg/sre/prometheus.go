package sre

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
)

func ServePrometheusEndpoint(port int) error {
	if port < 9002 || port > 65535 {
		return fmt.Errorf("invalid port %d, must be between 9002 and 65535", port)
	}
	return http.ListenAndServe(fmt.Sprintf(":%d", port), promhttp.Handler())
}

func prometheusMeterProvider() (*otelmetric.MeterProvider, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to start prometheus exporter: %w", err)
	}

	meterProvider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	return meterProvider, nil
}

func SetPrometheusMeterProvider() error {
	meterProvider, err := prometheusMeterProvider()
	if err != nil {
		return err
	}
	otel.SetMeterProvider(meterProvider)
	return nil
}

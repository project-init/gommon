package sre

import (
	"fmt"

	"go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats/opentelemetry"
)

func GetOtelGrpcServerOption() (*grpc.ServerOption, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to start prometheus exporter: %w", err)
	}

	meterProvider := otelmetric.NewMeterProvider(otelmetric.WithReader(exporter))
	so := opentelemetry.ServerOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: meterProvider,
		},
	})
	return &so, nil
}

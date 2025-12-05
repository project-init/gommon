package sre

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats/opentelemetry"
)

func GrpcServerOption() (*grpc.ServerOption, error) {
	meterProvider, err := prometheusMeterProvider()
	if err != nil {
		return nil, err
	}

	so := opentelemetry.ServerOption(opentelemetry.Options{
		MetricsOptions: opentelemetry.MetricsOptions{
			MeterProvider: meterProvider,
		},
	})
	return &so, nil
}

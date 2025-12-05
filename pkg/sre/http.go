package sre

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
)

var (
	responseTimeBuckets = []float64{2, 5, 10, 50, 100, 250, 500, 1000, 2500, 5000}
)

const (
	reqsName    = "http_requests_total"
	latencyName = "http_request_duration_milliseconds"
)

// sreMiddleware is a handler that exposes prometheus metrics and otel traces for the number of requests, the latency
// and the response size, partitioned by status code, method and HTTP path.
type sreMiddleware struct {
	appName string
	reqs    *prometheus.CounterVec
	latency *prometheus.HistogramVec
}

// HttpMiddleware returns a new Middleware handler with prometheus metrics, and tracing.
func HttpMiddleware(appName string) func(next http.Handler) http.Handler {
	var m sreMiddleware

	m.reqs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        reqsName,
			Help:        "How many HTTP requests processed, partitioned by status code, method and HTTP path.",
			ConstLabels: prometheus.Labels{"service": appName},
		},
		[]string{"code", "method", "path"},
	)
	prometheus.MustRegister(m.reqs)

	m.latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        latencyName,
		Help:        "How long it took to process the request, partitioned by status code, method and HTTP path.",
		ConstLabels: prometheus.Labels{"service": appName},
		Buckets:     responseTimeBuckets,
	},
		[]string{"code", "method", "path"},
	)
	prometheus.MustRegister(m.latency)
	return m.handler
}

func (m sreMiddleware) handler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Setup tracing
		operation := r.Method + " " + r.URL.Path
		_, span := otel.Tracer(m.appName).Start(r.Context(), operation)
		defer span.End()

		// Prometheus Metrics Handling
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		otelhttp.NewHandler(next, operation).ServeHTTP(ww, r)
		m.reqs.WithLabelValues(http.StatusText(ww.Status()), r.Method, r.URL.Path).Inc()
		m.latency.WithLabelValues(http.StatusText(ww.Status()), r.Method, r.URL.Path).Observe(float64(time.Since(start).Nanoseconds()) / 1000000)
	}
	return http.HandlerFunc(fn)
}

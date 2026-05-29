package gopodder

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type prometheusMetrics struct {
	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
	syncOperationsTotal *prometheus.CounterVec
}

func newPrometheusMetrics() *prometheusMetrics {
	m := &prometheusMetrics{
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gopodder_http_requests_total",
				Help: "Total number of HTTP requests.",
			},
			[]string{"method", "path", "status"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gopodder_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		syncOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gopodder_sync_operations_total",
				Help: "Total number of sync operations.",
			},
			[]string{"type"},
		),
	}
	prometheus.MustRegister(m.httpRequestsTotal)
	prometheus.MustRegister(m.httpRequestDuration)
	prometheus.MustRegister(m.syncOperationsTotal)
	return m
}

func (m *prometheusMetrics) IncHTTPRequest(method, path string, status int) {
	m.httpRequestsTotal.WithLabelValues(method, path, strconv.Itoa(status)).Inc()
}

func (m *prometheusMetrics) ObserveHTTPDuration(method, path string, duration time.Duration) {
	m.httpRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

func (m *prometheusMetrics) IncSyncOperation(typ string) {
	m.syncOperationsTotal.WithLabelValues(typ).Inc()
}

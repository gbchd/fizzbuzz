package metrics

import (
	"bytes"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/expfmt"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests handled.",
	}, []string{"method", "path", "status"})

	httpRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "Duration of HTTP requests, in seconds.",
	}, []string{"method", "path", "status"})

	statsredisRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "statsredis_request_duration_seconds",
		Help: "Duration of statsredis repository calls, in seconds.",
	}, []string{"operation"})

	statsredisErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "statsredis_errors_total",
		Help: "Total number of failed statsredis repository calls.",
	}, []string{"operation"})
)

// RecordHTTPRequest updates the request counter and duration histogram for one HTTP request.
func RecordHTTPRequest(method, path string, status int, start time.Time) {
	statusLabel := strconv.Itoa(status)
	httpRequestsTotal.WithLabelValues(method, path, statusLabel).Inc()
	httpRequestDurationSeconds.WithLabelValues(method, path, statusLabel).Observe(time.Since(start).Seconds())
}

// RecordRedisRequest updates the request duration histogram and error counter for one statsredis repository call.
func RecordRedisRequest(operation string, start time.Time, err error) {
	statsredisRequestDurationSeconds.WithLabelValues(operation).Observe(time.Since(start).Seconds())
	if err != nil {
		statsredisErrorsTotal.WithLabelValues(operation).Inc()
	}
}

// Prometheus returns the current metrics in Prometheus text format.
func Prometheus() (string, error) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	for _, mf := range families {
		if _, err := expfmt.MetricFamilyToText(&buf, mf); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

package metrics_test

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/fizzbuzz/internal/pkg/metrics"
)

// Each case uses a path unique to itself, so the underlying global registry
// never lets one case's assertions see another case's increments.
func TestRecordHTTPRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{name: "a successful request", method: "GET", path: "/test-http-ok", status: 200},
		{name: "a server error", method: "POST", path: "/test-http-fail", status: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metrics.RecordHTTPRequest(tt.method, tt.path, tt.status, time.Now())

			out, err := metrics.Prometheus()
			require.NoError(t, err)

			statusLabel := strconv.Itoa(tt.status)
			assert.Contains(t, out, fmt.Sprintf(`http_requests_total{method=%q,path=%q,status=%q} 1`, tt.method, tt.path, statusLabel))
			assert.Contains(t, out, fmt.Sprintf(`http_request_duration_seconds_bucket{method=%q,path=%q,status=%q,le=`, tt.method, tt.path, statusLabel))
		})
	}
}

func TestRecordRedisRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		err       error
	}{
		{name: "a successful call leaves no error counter", operation: "test-redis-ok", err: nil},
		{name: "a failed call increments the error counter", operation: "test-redis-fail", err: errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metrics.RecordRedisRequest(tt.operation, time.Now(), tt.err)

			out, err := metrics.Prometheus()
			require.NoError(t, err)

			assert.Contains(t, out, fmt.Sprintf(`statsredis_request_duration_seconds_bucket{operation=%q,le=`, tt.operation))

			errLine := fmt.Sprintf(`statsredis_errors_total{operation=%q} 1`, tt.operation)
			if tt.err != nil {
				assert.Contains(t, out, errLine)
				return
			}
			assert.NotContains(t, out, errLine)
		})
	}
}

func TestPrometheusIncludesProcessMetrics(t *testing.T) {
	t.Parallel()

	out, err := metrics.Prometheus()
	require.NoError(t, err)
	assert.Contains(t, out, "go_goroutines")
}

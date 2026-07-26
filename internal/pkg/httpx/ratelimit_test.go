package httpx_test

import (
	"cmp"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gbchd/fizzbuzz/internal/pkg/httpx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var rejected = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte("slow down"))
})

func TestRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		requests   int
		skip       []string
		path       string
		addrs      []string
		wantStatus []int
	}{
		{
			name:       "the allowance passes and what exceeds it is rejected",
			requests:   2,
			addrs:      []string{"10.0.0.1:1111", "10.0.0.1:1111", "10.0.0.1:1111"},
			wantStatus: []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests},
		},
		{
			name:       "each address spends its own allowance",
			requests:   1,
			addrs:      []string{"10.0.0.1:1111", "10.0.0.2:1111", "10.0.0.3:1111"},
			wantStatus: []int{http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			// A new connection from one caller must not buy a new allowance.
			name:       "the port is not part of the identity",
			requests:   1,
			addrs:      []string{"10.0.0.1:1111", "10.0.0.1:2222"},
			wantStatus: []int{http.StatusOK, http.StatusTooManyRequests},
		},
		{
			// One IPv6 allocation holds more addresses than could ever be
			// tracked, so a bucket per address would limit its holder not at all.
			name:       "addresses in one IPv6 block share an allowance",
			requests:   1,
			addrs:      []string{"[2001:db8::1]:1111", "[2001:db8::2]:2222"},
			wantStatus: []int{http.StatusOK, http.StatusTooManyRequests},
		},
		{
			name:       "separate IPv6 blocks are separate clients",
			requests:   1,
			addrs:      []string{"[2001:db8::1]:1111", "[2001:dba::1]:1111"},
			wantStatus: []int{http.StatusOK, http.StatusOK},
		},
		{
			name:       "an address with no port is still a client",
			requests:   1,
			addrs:      []string{"10.0.0.1", "10.0.0.1"},
			wantStatus: []int{http.StatusOK, http.StatusTooManyRequests},
		},
		{
			// An orchestrator polls far above any limit a caller would get, and
			// restarts what it cannot reach.
			name:       "a skipped path is never limited",
			requests:   1,
			skip:       []string{"/healthz", "/readyz"},
			path:       "/readyz",
			addrs:      []string{"10.0.0.1:1111", "10.0.0.1:1111", "10.0.0.1:1111"},
			wantStatus: []int{http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			name:       "a path not in the skip list is still limited",
			requests:   1,
			skip:       []string{"/healthz"},
			path:       "/fizzbuzz",
			addrs:      []string{"10.0.0.1:1111", "10.0.0.1:1111"},
			wantStatus: []int{http.StatusOK, http.StatusTooManyRequests},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A window too long to roll over mid-test, so the case measures the
			// allowance alone.
			h := httpx.RateLimit(tt.requests, time.Hour, rejected, tt.skip...)(okHandler)

			path := cmp.Or(tt.path, "/fizzbuzz")

			got := make([]int, 0, len(tt.addrs))
			for _, addr := range tt.addrs {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
				req.RemoteAddr = addr

				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				got = append(got, rec.Code)
			}

			assert.Equal(t, tt.wantStatus, got)
		})
	}
}

func TestRateLimitRejectionIsTheCallersResponse(t *testing.T) {
	tests := []struct {
		name           string
		window         time.Duration
		wantRetryAfter string
	}{
		{name: "a rejected request is told what to send and when", window: 90 * time.Second, wantRetryAfter: "90"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpx.RateLimit(1, tt.window, rejected)(okHandler)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/fizzbuzz", nil)
			req.RemoteAddr = "10.0.0.1:1111"

			first := httptest.NewRecorder()
			h.ServeHTTP(first, req)
			require.Equal(t, http.StatusOK, first.Code, "the first request must spend the only allowance")

			second := httptest.NewRecorder()
			h.ServeHTTP(second, req)

			require.Equal(t, http.StatusTooManyRequests, second.Code)
			assert.Equal(t, "slow down", second.Body.String())
			assert.Equal(t, tt.wantRetryAfter, second.Header().Get(httpx.RetryAfterHeader))
		})
	}
}

func TestRateLimitUnderConcurrency(t *testing.T) {
	tests := []struct {
		name     string
		requests int
		parallel int
	}{
		{name: "only the allowance is admitted, however hard it is hit", requests: 20, parallel: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpx.RateLimit(tt.requests, time.Hour, rejected)(okHandler)

			var allowed atomic.Int64

			var wg sync.WaitGroup
			for range tt.parallel {
				wg.Add(1)
				go func() {
					defer wg.Done()

					req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/fizzbuzz", nil)
					req.RemoteAddr = "10.0.0.1:1111"

					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)

					if rec.Code == http.StatusOK {
						allowed.Add(1)
					}
				}()
			}
			wg.Wait()

			assert.Equal(t, int64(tt.requests), allowed.Load(), "the allowance is exactly what it says")
		})
	}
}

package httpx_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/fizzbuzz/internal/pkg/httpx"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

// okHandler is the end of a chain, for tests that only care what precedes it.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// logged runs h with a logger on the context and returns the lines it wrote.
func logged(t *testing.T, h http.Handler, req *http.Request) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()

	var buf bytes.Buffer
	ctx := slogx.WithCtx(req.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req.WithContext(ctx))

	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}

		var line map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &line))
		lines = append(lines, line)
	}

	return rec, lines
}

func TestChain(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   []string
	}{
		{
			name:   "the first listed runs outermost",
			labels: []string{"outer", "middle", "inner"},
			want:   []string{"outer", "middle", "inner", "handler"},
		},
		{
			name:   "no middleware leaves the handler untouched",
			labels: nil,
			want:   []string{"handler"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var order []string

			middlewares := make([]httpx.Middleware, 0, len(tt.labels))
			for _, label := range tt.labels {
				middlewares = append(middlewares, func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						order = append(order, label)
						next.ServeHTTP(w, r)
					})
				})
			}

			h := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				order = append(order, "handler")
				w.WriteHeader(http.StatusOK)
			}), middlewares...)

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

			assert.Equal(t, tt.want, order)
		})
	}
}

func TestLimitRequestBody(t *testing.T) {
	const maxBytes = 16

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "a body within the cap is readable",
			body:    strings.Repeat("a", maxBytes),
			wantErr: false,
		},
		{
			name:    "a body over the cap fails the read",
			body:    strings.Repeat("a", maxBytes+1),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var readErr error
			h := httpx.LimitRequestBody(maxBytes)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, readErr = io.ReadAll(r.Body)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body))
			h.ServeHTTP(httptest.NewRecorder(), req)

			if tt.wantErr {
				require.Error(t, readErr, "the cap must stop the read")
				return
			}

			require.NoError(t, readErr)
		})
	}
}

func TestRequestID(t *testing.T) {
	tests := []struct {
		name    string
		inbound string
		// wantGenerated means a fresh UUIDv7 rather than the inbound value.
		wantGenerated bool
	}{
		{
			name:          "generated when absent",
			inbound:       "",
			wantGenerated: true,
		},
		{
			name:          "an upstream identifier is preserved",
			inbound:       "trace-from-the-proxy",
			wantGenerated: false,
		},
		{
			name:          "an over-long identifier is capped",
			inbound:       strings.Repeat("x", 300),
			wantGenerated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tt.inbound != "" {
				req.Header.Set(httpx.RequestIDHeader, tt.inbound)
			}

			rec := httptest.NewRecorder()
			httpx.RequestID(okHandler).ServeHTTP(rec, req)

			echoed := rec.Header().Get(httpx.RequestIDHeader)
			require.NotEmpty(t, echoed, "every response must carry an identifier")
			assert.LessOrEqual(t, len(echoed), 64)

			if !tt.wantGenerated {
				assert.Equal(t, tt.inbound[:min(len(tt.inbound), 64)], echoed)
				return
			}

			parsed, err := uuid.Parse(echoed)
			require.NoError(t, err, "got %q", echoed)
			assert.Equal(t, uuid.Version(7), parsed.Version())
		})
	}
}

// The identifier has to reach the log through the context, since that is the
// only route that carries request-scoped fields. Every line written below the
// middleware carries it, the access log's own included: a line whose
// request_id is empty is the regression this pins down.
func TestRequestIDReachesEveryLogLine(t *testing.T) {
	tests := []struct {
		name    string
		inbound string
	}{
		{name: "an upstream identifier reaches the log", inbound: "known-id"},
		{name: "a generated identifier reaches the log", inbound: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpx.Chain(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					slogx.FromCtx(r.Context()).InfoContext(r.Context(), "from the handler")
					w.WriteHeader(http.StatusOK)
				}),
				httpx.RequestID,
				httpx.AccessLog(),
			)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if tt.inbound != "" {
				req.Header.Set(httpx.RequestIDHeader, tt.inbound)
			}

			rec, lines := logged(t, h, req)

			// The echoed header is what a user would quote, so it is the value
			// the log has to agree with.
			echoed := rec.Header().Get(httpx.RequestIDHeader)
			require.NotEmpty(t, echoed)
			if tt.inbound != "" {
				require.Equal(t, tt.inbound, echoed)
			}

			require.Len(t, lines, 2, "the handler and the access log each write one line")
			assert.Equal(t, "from the handler", lines[0]["msg"])
			assert.Equal(t, "handled request", lines[1]["msg"])

			for _, line := range lines {
				assert.Equal(t, echoed, line["request_id"], "every line must be attributable to the request")
			}
		})
	}
}

func TestAccessLog(t *testing.T) {
	tests := []struct {
		name       string
		skip       []string
		path       string
		status     int
		wantLogged bool
	}{
		{
			name:       "one line per served request",
			path:       "/fizzbuzz",
			status:     http.StatusOK,
			wantLogged: true,
		},
		{
			name:       "the handler's status is recorded, not assumed",
			path:       "/fizzbuzz",
			status:     http.StatusTeapot,
			wantLogged: true,
		},
		{
			name:       "a skipped path produces no line",
			skip:       []string{"/healthz", "/readyz"},
			path:       "/readyz",
			status:     http.StatusOK,
			wantLogged: false,
		},
		{
			name:       "a path not in the skip list is still logged",
			skip:       []string{"/healthz"},
			path:       "/fizzbuzz",
			status:     http.StatusOK,
			wantLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpx.AccessLog(tt.skip...)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, tt.path, nil)
			rec, lines := logged(t, h, req)

			require.Equal(t, tt.status, rec.Code)

			if !tt.wantLogged {
				assert.Empty(t, lines)
				return
			}

			require.Len(t, lines, 1)
			line := lines[0]
			assert.Equal(t, http.MethodPost, line["method"])
			assert.Equal(t, tt.path, line["path"])
			assert.Equal(t, float64(tt.status), line["status"])
			assert.NotEmpty(t, line["duration"])
		})
	}
}

// A wrapped writer must not quietly drop what the real one can do.
func TestAccessLogPreservesResponseWriterBehaviour(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "the body reaches the client through the wrapper", body: "the sequence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := httpx.AccessLog()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// io.Copy, the way a reader-backed response is served.
				_, err := io.Copy(w, strings.NewReader(tt.body))
				require.NoError(t, err)

				// Flush must still reach the real writer, via http.ResponseController.
				require.NoError(t, http.NewResponseController(w).Flush())
			}))

			rec, _ := logged(t, h, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

			assert.Equal(t, http.StatusOK, rec.Code, "an unset status must default to 200")
			assert.Equal(t, tt.body, rec.Body.String())
			assert.True(t, rec.Flushed)
		})
	}
}

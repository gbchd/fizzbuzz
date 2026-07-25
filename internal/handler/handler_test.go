package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/fizzbuzz/internal/handler"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
	"github.com/gbchd/fizzbuzz/internal/service/fizzbuzz"
)

// validBody is for cases that are about something other than the parameters.
const validBody = `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":15}`

// stubService lets a test reach the failure path the real service cannot be
// talked into, since validation only ever passes it legal input.
type stubService struct {
	sequence []string
	err      error
}

func (s stubService) FizzBuzz(_ int, _ string, _ int, _ string, _ int) ([]string, error) {
	return s.sequence, s.err
}

// response is everything a client can observe, which is all these tests assert
// on — never middleware order, generated type names, or log formatting.
type response struct {
	status int
	header http.Header
	body   []byte
}

// serve runs one request against the fully assembled handler.
func serve(t *testing.T, svc handler.FizzBuzzService, req *http.Request) response {
	t.Helper()

	resp, _ := serveCapturingLogs(t, svc, req)

	return resp
}

// serveCapturingLogs puts the logger on the server's base context the way the
// entrypoint does, so this drives the real propagation path, not a stand-in.
func serveCapturingLogs(t *testing.T, svc handler.FizzBuzzService, req *http.Request) (response, []logLine) {
	t.Helper()

	h, err := handler.New(svc)
	require.NoError(t, err)

	logs := &logCapture{}
	server := httptest.NewUnstartedServer(h)
	server.Config.BaseContext = func(net.Listener) context.Context {
		return slogx.WithCtx(context.Background(), slog.New(slog.NewJSONHandler(logs, nil)))
	}
	server.Start()
	t.Cleanup(server.Close)

	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(server.URL, "http://")

	resp, err := server.Client().Do(req)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	// The access log is written after the body reaches the client, so waiting for
	// outstanding requests is what makes the lines below complete.
	server.Close()

	return response{status: resp.StatusCode, header: resp.Header, body: body}, logs.lines(t)
}

type logLine map[string]any

// logCapture collects log output for assertions. Writes come from the server's
// goroutine, hence the lock.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.buf.Write(p)
}

func (c *logCapture) lines(t *testing.T) []logLine {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	var lines []logLine
	for raw := range strings.SplitSeq(strings.TrimSpace(c.buf.String()), "\n") {
		if raw == "" {
			continue
		}

		var line logLine
		require.NoError(t, json.Unmarshal([]byte(raw), &line))
		lines = append(lines, line)
	}

	return lines
}

func get(t *testing.T, path string) *http.Request {
	t.Helper()

	return newRequest(t, http.MethodGet, path, "")
}

// postFizzbuzz takes a raw body, so a test can send what the typed request
// struct could not express.
func postFizzbuzz(t *testing.T, body string) *http.Request {
	t.Helper()

	req := newRequest(t, http.MethodPost, "/fizzbuzz", body)
	req.Header.Set("Content-Type", "application/json")

	return req
}

func newRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	require.NoError(t, err)

	return req
}

func TestPostFizzbuzz(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "substitutes both rules and concatenates a multi-match",
			body: `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":15}`,
			want: []string{
				"1", "2", "fizz", "4", "buzz", "fizz", "7", "8", "fizz", "buzz",
				"11", "fizz", "13", "14", "fizzbuzz",
			},
		},
		{
			name: "an empty replacement still substitutes",
			body: `{"int1":3,"str1":"","int2":5,"str2":"buzz","limit":5}`,
			want: []string{"1", "2", "", "4", "buzz"},
		},
		{
			name: "non-ASCII and punctuation round-trip unchanged",
			body: `{"int1":2,"str1":"a & b = c","int2":3,"str2":"café 🍕","limit":6}`,
			want: []string{"1", "a & b = c", "café 🍕", "a & b = c", "5", "a & b = ccafé 🍕"},
		},
		{
			name: "the ceiling itself is accepted",
			body: `{"int1":400,"str1":"x","int2":399,"str2":"y","limit":400}`,
			want: ceilingSequence(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, fizzbuzz.NewService(), postFizzbuzz(t, tt.body))

			require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
			assert.Equal(t, "application/json", resp.header.Get("Content-Type"))

			var got []string
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPostFizzbuzzRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		// mentions is the field or rule the message must name, so a client can
		// fix the request without bisecting.
		mentions string
	}{
		{
			name:     "empty body",
			body:     ``,
			mentions: "body",
		},
		{
			name:     "absent required field",
			body:     `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz"}`,
			mentions: "limit",
		},
		{
			name:     "limit below the minimum",
			body:     `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":0}`,
			mentions: "limit",
		},
		{
			name:     "limit above the ceiling",
			body:     `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":401}`,
			mentions: "limit",
		},
		{
			name:     "divisor below the minimum",
			body:     `{"int1":0,"str1":"fizz","int2":5,"str2":"buzz","limit":15}`,
			mentions: "int1",
		},
		{
			name:     "replacement over the length cap",
			body:     `{"int1":3,"str1":"` + strings.Repeat("f", 65) + `","int2":5,"str2":"buzz","limit":15}`,
			mentions: "str1",
		},
		{
			name:     "wrong JSON type",
			body:     `{"int1":"three","str1":"fizz","int2":5,"str2":"buzz","limit":15}`,
			mentions: "int1",
		},
		{
			name:     "body over the size limit",
			body:     `{"int1":3,"str1":"` + strings.Repeat("f", 16<<10) + `","int2":5,"str2":"buzz","limit":15}`,
			mentions: "body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, fizzbuzz.NewService(), postFizzbuzz(t, tt.body))

			require.Equal(t, http.StatusBadRequest, resp.status, "body: %s", resp.body)
			assert.Equal(t, "application/json", resp.header.Get("Content-Type"))

			var got handler.Error
			require.NoError(t, json.Unmarshal(resp.body, &got), "body: %s", resp.body)
			assert.Contains(t, got.Message, tt.mentions)
			assert.NotContains(t, got.Message, "Schema:", "the schema must not be echoed back")
			assert.NotContains(t, got.Message, "Value:", "the submitted value must not be echoed back")
		})
	}
}

// A service error means the document and the domain have diverged, which is our
// fault, not the caller's.
func TestPostFizzbuzzReportsServiceErrorsAsServerFaults(t *testing.T) {
	tests := []struct {
		name string
		svc  handler.FizzBuzzService
	}{
		{
			name: "domain error from the service",
			svc:  stubService{err: fizzbuzz.ErrInvalidDivisor},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, tt.svc, postFizzbuzz(t, validBody))

			require.Equal(t, http.StatusInternalServerError, resp.status, "body: %s", resp.body)

			var got handler.Error
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.NotEmpty(t, got.Message)
			assert.NotContains(t, got.Message, fizzbuzz.ErrInvalidDivisor.Error(),
				"internal detail must not leak to the client")
		})
	}
}

func TestRequestID(t *testing.T) {
	const header = "X-Request-ID"

	tests := []struct {
		name    string
		inbound string
		// wantGenerated means the response must carry a fresh UUIDv7 rather than
		// whatever was sent.
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
			req := postFizzbuzz(t, `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":3}`)
			if tt.inbound != "" {
				req.Header.Set(header, tt.inbound)
			}

			resp := serve(t, fizzbuzz.NewService(), req)

			echoed := resp.header.Get(header)
			require.NotEmpty(t, echoed, "every response must carry an identifier")
			assert.LessOrEqual(t, len(echoed), 64)

			if !tt.wantGenerated {
				assert.Equal(t, truncate(tt.inbound, 64), echoed)
				return
			}

			parsed, err := uuid.Parse(echoed)
			require.NoError(t, err, "got %q", echoed)
			assert.Equal(t, uuid.Version(7), parsed.Version())
			assert.Equal(t, uuid.RFC4122, parsed.Variant())
		})
	}
}

// Sorting by identifier should order requests by arrival, which is the whole
// reason for choosing v7 over a random identifier.
func TestGeneratedRequestIDsSortByArrival(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{name: "successive requests", count: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := make([]string, 0, tt.count)
			for range tt.count {
				resp := serve(t, fizzbuzz.NewService(), get(t, "/healthz"))

				id := resp.header.Get("X-Request-ID")
				require.NotEmpty(t, id)
				ids = append(ids, id)

				// v7 orders by millisecond; within one, the remaining bits are random.
				time.Sleep(2 * time.Millisecond)
			}

			assert.True(t, slices.IsSorted(ids), "issued in order but sort to %v", ids)
		})
	}
}

func TestAccessLog(t *testing.T) {
	tests := []struct {
		name    string
		request func(t *testing.T) *http.Request
		// wantLine is the access log line expected for the request, or nil when
		// the request should produce none.
		wantLine logLine
	}{
		{
			name:    "one line per served request",
			request: func(t *testing.T) *http.Request { t.Helper(); return postFizzbuzz(t, validBody) },
			wantLine: logLine{
				"method": http.MethodPost,
				"path":   "/fizzbuzz",
				"status": float64(http.StatusOK),
			},
		},
		{
			name:    "a rejected request is logged with its status",
			request: func(t *testing.T) *http.Request { t.Helper(); return postFizzbuzz(t, `{}`) },
			wantLine: logLine{
				"method": http.MethodPost,
				"path":   "/fizzbuzz",
				"status": float64(http.StatusBadRequest),
			},
		},
		{
			// An orchestrator probes every few seconds; those would bury real traffic.
			name:     "the liveness probe is not logged",
			request:  func(t *testing.T) *http.Request { t.Helper(); return get(t, "/healthz") },
			wantLine: nil,
		},
		{
			name:     "the readiness probe is not logged",
			request:  func(t *testing.T) *http.Request { t.Helper(); return get(t, "/readyz") },
			wantLine: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, lines := serveCapturingLogs(t, fizzbuzz.NewService(), tt.request(t))

			if tt.wantLine == nil {
				assert.Empty(t, lines)
				return
			}

			require.Len(t, lines, 1, "exactly one line per request: %v", lines)
			line := lines[0]

			for key, want := range tt.wantLine {
				assert.Equal(t, want, line[key], "field %q", key)
			}
			assert.NotEmpty(t, line["duration"])
			assert.NotEmpty(t, line["request_id"], "handlers must be able to correlate their own lines")
		})
	}
}

func TestMetaEndpoints(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		contentType string
		assertBody  func(t *testing.T, body []byte)
	}{
		{
			name:        "healthz reports that the process is serving",
			path:        "/healthz",
			contentType: "application/json",
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()

				var got handler.Health
				require.NoError(t, json.Unmarshal(body, &got))
				assert.NotEmpty(t, got.Status)
			},
		},
		{
			name:        "definition serves the contract as parseable JSON",
			path:        "/definition",
			contentType: "application/json",
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()

				var doc struct {
					OpenAPI string                    `json:"openapi"`
					Paths   map[string]map[string]any `json:"paths"`
				}
				require.NoError(t, json.Unmarshal(body, &doc))

				assert.True(t, strings.HasPrefix(doc.OpenAPI, "3.0"), "got %q", doc.OpenAPI)
				for _, path := range []string{"/fizzbuzz", "/healthz", "/readyz", "/definition", "/documentation"} {
					assert.Contains(t, doc.Paths, path)
				}
			},
		},
		{
			name:        "documentation serves a browsable page",
			path:        "/documentation",
			contentType: "text/html",
			assertBody: func(t *testing.T, body []byte) {
				t.Helper()

				assert.Contains(t, string(body), "<html")
				// The renderer comes from the CDN, so its absence means the
				// generated page is no longer a Redoc page at all.
				assert.Contains(t, string(body), "cdn.redocly.com")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, fizzbuzz.NewService(), get(t, tt.path))

			require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
			assert.Equal(t, tt.contentType, resp.header.Get("Content-Type"))
			tt.assertBody(t, resp.body)
		})
	}
}

// Nothing is registered outside the generated router, so a path the document
// does not declare is refused rather than reaching a handler.
func TestUndeclaredRequestsAreRefused(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{
			name:   "undeclared path",
			method: http.MethodGet,
			path:   "/docs",
			want:   http.StatusNotFound,
		},
		{
			name:   "undeclared method on a declared path",
			method: http.MethodGet,
			path:   "/fizzbuzz",
			want:   http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, fizzbuzz.NewService(), newRequest(t, tt.method, tt.path, ""))

			require.Equal(t, tt.want, resp.status, "body: %s", resp.body)

			var got handler.Error
			require.NoError(t, json.Unmarshal(resp.body, &got), "every failure uses one shape")
			assert.NotEmpty(t, got.Message)
		})
	}
}

// Readiness must turn 503 the moment shutdown starts, so a load balancer stops
// routing before the drain, while liveness stays 200 so nothing restarts us.
func TestReadiness(t *testing.T) {
	tests := []struct {
		name         string
		shuttingDown bool
		wantReadyz   int
		wantHealthz  int
	}{
		{
			name:         "ready while serving",
			shuttingDown: false,
			wantReadyz:   http.StatusOK,
			wantHealthz:  http.StatusOK,
		},
		{
			name:         "not ready once shutdown has begun",
			shuttingDown: true,
			wantReadyz:   http.StatusServiceUnavailable,
			wantHealthz:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(handler.ResetShutdown)
			if tt.shuttingDown {
				handler.BeginShutdown()
			}

			readyz := serve(t, fizzbuzz.NewService(), get(t, "/readyz"))
			require.Equal(t, tt.wantReadyz, readyz.status, "body: %s", readyz.body)
			assert.Equal(t, "application/json", readyz.header.Get("Content-Type"))

			healthz := serve(t, fizzbuzz.NewService(), get(t, "/healthz"))
			assert.Equal(t, tt.wantHealthz, healthz.status,
				"liveness must not fail during shutdown, or the orchestrator severs the drain")
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n]
}

// ceilingSequence is built rather than written out: the literal would be 400
// entries long.
func ceilingSequence() []string {
	want := make([]string, 0, 400)
	for n := 1; n <= 398; n++ {
		want = append(want, strconv.Itoa(n))
	}

	return append(want, "y", "x")
}

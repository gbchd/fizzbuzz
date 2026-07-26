package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gbchd/fizzbuzz/internal/handler"
	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
	"github.com/gbchd/fizzbuzz/internal/service/fizzbuzz"
	"github.com/gbchd/fizzbuzz/internal/service/statistics"
)

const (
	// validBody is for cases that are about something other than the parameters.
	validBody = `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":15}`

	jsonContentType = "application/json"
)

// stubService lets a test observe what the handler passed down, and reach the
// failure path the real service cannot be talked into, since validation only
// ever passes it legal input.
type stubService struct {
	sequence []string
	err      error

	// got records the last call, so a test can pin the argument order.
	got stubCall
}

type stubCall struct {
	int1  int
	str1  string
	int2  int
	str2  string
	limit int
}

func (s *stubService) FizzBuzz(_ context.Context, int1 int, str1 string, int2 int, str2 string, limit int) ([]string, error) {
	s.got = stubCall{int1: int1, str1: str1, int2: int2, str2: str2, limit: limit}

	return s.sequence, s.err
}

// stubStats lets a test control what GetStatistics reads, including errors
// the real Repository never produces.
type stubStats struct {
	query statistics.Query
	hits  int
	err   error
}

func (s stubStats) Top(_ context.Context) (statistics.Query, int, error) {
	return s.query, s.hits, s.err
}

// response is everything a client can observe, which is all these tests assert
// on — never middleware order, generated type names, or log formatting.
type response struct {
	status int
	header http.Header
	body   []byte
}

// serve runs one request against the fully assembled handler, ready for traffic.
func serve(t *testing.T, svc handler.FizzBuzzService, req *http.Request) response {
	t.Helper()

	resp, _ := serveCapturingLogs(t, svc, req)

	return resp
}

// serveCapturingLogs also returns the lines the request wrote. ServeHTTP is
// synchronous, so by the time it returns the logger has seen everything —
// no server, and no waiting for it to drain.
func serveCapturingLogs(t *testing.T, svc handler.FizzBuzzService, req *http.Request) (response, []logLine) {
	t.Helper()

	return serveHandler(t, newHandler(t, svc, true), req)
}

// newHandler assembles the handler under test. ready is what GET /readyz reports.
func newHandler(t *testing.T, svc handler.FizzBuzzService, ready bool) http.Handler {
	t.Helper()

	return newHandlerWithStats(t, svc, stubStats{err: statistics.ErrNoHits}, ready)
}

func newHandlerWithStats(t *testing.T, svc handler.FizzBuzzService, stats handler.StatisticsReader, ready bool) http.Handler {
	t.Helper()

	// An allowance far above what any case here spends, so the limiter never
	// decides the outcome of a test aimed at something else.
	h, err := handler.New(svc, stats, func() bool { return ready }, handler.RateLimit{Requests: 10_000, Window: time.Hour})
	require.NoError(t, err)

	return h
}

func serveHandler(t *testing.T, h http.Handler, req *http.Request) (response, []logLine) {
	t.Helper()

	// The logger rides the request context, the way the entrypoint's BaseContext
	// puts it there in production.
	var logs bytes.Buffer
	req = req.WithContext(slogx.WithCtx(req.Context(), slog.New(slog.NewJSONHandler(&logs, nil))))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	result := rec.Result()
	body, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	require.NoError(t, result.Body.Close())

	return response{status: result.StatusCode, header: result.Header, body: body}, parseLogLines(t, logs.String())
}

type logLine map[string]any

func parseLogLines(t *testing.T, raw string) []logLine {
	t.Helper()

	var lines []logLine
	for entry := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if entry == "" {
			continue
		}

		var line logLine
		require.NoError(t, json.Unmarshal([]byte(entry), &line))
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

	return postFizzbuzzAs(t, jsonContentType, body)
}

// postFizzbuzzAs additionally takes the content type, so a test can get the
// header wrong on purpose. An empty contentType omits the header entirely.
func postFizzbuzzAs(t *testing.T, contentType, body string) *http.Request {
	t.Helper()

	req := newRequest(t, http.MethodPost, "/fizzbuzz", body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return req
}

func newRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
}

func TestPostFizzbuzz(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "substitutes both rules and concatenates a multi-match",
			body: validBody,
			want: []string{
				"1", "2", "fizz", "4", "buzz", "fizz", "7", "8", "fizz", "buzz",
				"11", "fizz", "13", "14", "fizzbuzz",
			},
		},
		{
			// The encoder escapes some of these by default; a client must get back
			// exactly what it sent.
			name: "non-ASCII and punctuation round-trip unchanged",
			body: `{"int1":2,"str1":"a & b = c","int2":3,"str2":"café 🍕","limit":6}`,
			want: []string{"1", "a & b = c", "café 🍕", "a & b = c", "5", "a & b = ccafé 🍕"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, fizzbuzz.NewService(), postFizzbuzz(t, tt.body))

			require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
			assert.Equal(t, jsonContentType, resp.header.Get("Content-Type"))

			var got []string
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.Equal(t, tt.want, got)
		})
	}
}

// What the sequence itself contains is the domain's business; that the body's
// fields arrive in the right positions is this layer's.
func TestPostFizzbuzzPassesRequestFieldsToTheService(t *testing.T) {
	tests := []struct {
		name string
		body string
		want stubCall
	}{
		{
			name: "each field reaches its own parameter",
			body: `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":15}`,
			want: stubCall{int1: 3, str1: "fizz", int2: 5, str2: "buzz", limit: 15},
		},
		{
			// Distinct values throughout, so a swapped pair cannot pass unnoticed.
			name: "no two parameters are interchangeable",
			body: `{"int1":2,"str1":"a","int2":7,"str2":"b","limit":9}`,
			want: stubCall{int1: 2, str1: "a", int2: 7, str2: "b", limit: 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubService{sequence: []string{"x"}}

			resp := serve(t, svc, postFizzbuzz(t, tt.body))

			require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
			assert.Equal(t, tt.want, svc.got)
		})
	}
}

// The ceiling is capacity policy the document states, so both sides of it are
// this layer's to enforce.
func TestPostFizzbuzzEnforcesTheLimitCeiling(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		wantStatus int
		wantLen    int
	}{
		{
			name:       "the ceiling itself is accepted",
			limit:      400,
			wantStatus: http.StatusOK,
			wantLen:    400,
		},
		{
			name:       "one above the ceiling is refused",
			limit:      401,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"int1":3,"str1":"fizz","int2":5,"str2":"buzz","limit":` + strconv.Itoa(tt.limit) + `}`

			resp := serve(t, fizzbuzz.NewService(), postFizzbuzz(t, body))

			require.Equal(t, tt.wantStatus, resp.status, "body: %s", resp.body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			var got []string
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.Len(t, got, tt.wantLen)
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
			// Satisfies the schema's minimum and still overflows the Go int it
			// decodes into, so this is the one body validation lets through to the
			// generated server's unmarshal.
			name:     "an integer too large for the decoded type",
			body:     `{"int1":99999999999999999999,"str1":"fizz","int2":5,"str2":"buzz","limit":15}`,
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
			assert.Equal(t, jsonContentType, resp.header.Get("Content-Type"))

			var got handler.Error
			require.NoError(t, json.Unmarshal(resp.body, &got), "body: %s", resp.body)
			assert.Contains(t, got.Message, tt.mentions)
			assert.NotContains(t, got.Message, "Schema:", "the schema must not be echoed back")
			assert.NotContains(t, got.Message, "Value:", "the submitted value must not be echoed back")
		})
	}
}

// The document declares one request media type, and the validator holds callers
// to it before the body is ever decoded.
func TestPostFizzbuzzRequiresAJSONContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		{
			name:        "the declared media type is accepted",
			contentType: jsonContentType,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "a charset parameter is still that media type",
			contentType: "application/json; charset=utf-8",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "another media type is refused",
			contentType: "text/plain",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "an absent content type is refused",
			contentType: "",
			wantStatus:  http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, fizzbuzz.NewService(), postFizzbuzzAs(t, tt.contentType, validBody))

			require.Equal(t, tt.wantStatus, resp.status, "body: %s", resp.body)
			if tt.wantStatus == http.StatusOK {
				return
			}

			var got handler.Error
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.Contains(t, got.Message, "Content-Type")
		})
	}
}

// A service error means the document and the domain have diverged, which is our
// fault, not the caller's.
func TestPostFizzbuzzReportsServiceErrorsAsServerFaults(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "domain error from the service",
			err:  fizzbuzz.ErrInvalidDivisor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, &stubService{err: tt.err}, postFizzbuzz(t, validBody))

			require.Equal(t, http.StatusInternalServerError, resp.status, "body: %s", resp.body)

			var got handler.Error
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.NotEmpty(t, got.Message)
			assert.NotContains(t, got.Message, tt.err.Error(),
				"internal detail must not leak to the client")
		})
	}
}

// The document declares an array, so an empty result has to encode as one. A
// nil slice would go out as null and break a client that trusts the schema.
func TestPostFizzbuzzAlwaysEncodesAnArray(t *testing.T) {
	tests := []struct {
		name     string
		sequence []string
	}{
		{name: "a nil sequence", sequence: nil},
		{name: "an empty sequence", sequence: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, &stubService{sequence: tt.sequence}, postFizzbuzz(t, validBody))

			require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
			assert.JSONEq(t, `[]`, string(resp.body))
		})
	}
}

func TestGetStatistics(t *testing.T) {
	tests := []struct {
		name       string
		stats      stubStats
		wantStatus int
		wantBody   *handler.Statistics
	}{
		{
			name: "reflects the Repository's most frequent Query",
			// Distinct values throughout, so a swapped field cannot pass unnoticed.
			stats: stubStats{
				query: statistics.Query{Int1: 2, Str1: "a", Int2: 7, Str2: "b", Limit: 9},
				hits:  4,
			},
			wantStatus: http.StatusOK,
			wantBody: &handler.Statistics{
				Request: handler.FizzbuzzRequest{Int1: 2, Str1: "a", Int2: 7, Str2: "b", Limit: 9},
				Hits:    4,
			},
		},
		{
			name:       "ErrNoHits is reported as 404",
			stats:      stubStats{err: statistics.ErrNoHits},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "any other error is a server fault, not leaked to the client",
			stats:      stubStats{err: assert.AnError},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandlerWithStats(t, fizzbuzz.NewService(), tt.stats, true)

			resp, _ := serveHandler(t, h, get(t, "/statistics"))

			require.Equal(t, tt.wantStatus, resp.status, "body: %s", resp.body)
			if tt.wantStatus != http.StatusOK {
				var got handler.Error
				require.NoError(t, json.Unmarshal(resp.body, &got))
				assert.NotEmpty(t, got.Message)
				if tt.stats.err != nil && !errors.Is(tt.stats.err, statistics.ErrNoHits) {
					assert.NotContains(t, got.Message, tt.stats.err.Error(),
						"internal detail must not leak to the client")
				}
				return
			}

			var got handler.Statistics
			require.NoError(t, json.Unmarshal(resp.body, &got))
			assert.Equal(t, *tt.wantBody, got)
		})
	}
}

// The middleware's own behaviour — minting, capping, adopting an upstream
// value — belongs to httpx. What matters here is that the chain includes it, on
// every response a client can get.
func TestRequestIDIsStampedOnEveryResponse(t *testing.T) {
	tests := []struct {
		name       string
		svc        handler.FizzBuzzService
		request    func(t *testing.T) *http.Request
		wantStatus int
	}{
		{
			name:       "on a served request",
			svc:        fizzbuzz.NewService(),
			request:    func(t *testing.T) *http.Request { t.Helper(); return postFizzbuzz(t, validBody) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "on a rejected request",
			svc:        fizzbuzz.NewService(),
			request:    func(t *testing.T) *http.Request { t.Helper(); return postFizzbuzz(t, `{}`) },
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "on a server fault",
			svc:        &stubService{err: fizzbuzz.ErrInvalidDivisor},
			request:    func(t *testing.T) *http.Request { t.Helper(); return postFizzbuzz(t, validBody) },
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "on a path the document does not declare",
			svc:        fizzbuzz.NewService(),
			request:    func(t *testing.T) *http.Request { t.Helper(); return get(t, "/docs") },
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := serve(t, tt.svc, tt.request(t))

			require.Equal(t, tt.wantStatus, resp.status, "body: %s", resp.body)
			assert.NotEmpty(t, resp.header.Get("X-Request-ID"),
				"a user reporting a failure has only this to quote")
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

func TestHealthz(t *testing.T) {
	resp := serve(t, fizzbuzz.NewService(), get(t, "/healthz"))

	require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
	assert.Equal(t, jsonContentType, resp.header.Get("Content-Type"))

	var got handler.Health
	require.NoError(t, json.Unmarshal(resp.body, &got))
	assert.NotEmpty(t, got.Status)
}

// Readiness must turn 503 the moment shutdown starts, so a load balancer stops
// routing before the drain, while liveness stays 200 so nothing restarts us.
func TestReadiness(t *testing.T) {
	tests := []struct {
		name        string
		ready       bool
		wantReadyz  int
		wantHealthz int
	}{
		{
			name:        "ready while serving",
			ready:       true,
			wantReadyz:  http.StatusOK,
			wantHealthz: http.StatusOK,
		},
		{
			name:        "not ready once shutdown has begun",
			ready:       false,
			wantReadyz:  http.StatusServiceUnavailable,
			wantHealthz: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(t, fizzbuzz.NewService(), tt.ready)

			readyz, _ := serveHandler(t, h, get(t, "/readyz"))
			require.Equal(t, tt.wantReadyz, readyz.status, "body: %s", readyz.body)
			assert.Equal(t, jsonContentType, readyz.header.Get("Content-Type"))

			healthz, _ := serveHandler(t, h, get(t, "/healthz"))
			assert.Equal(t, tt.wantHealthz, healthz.status,
				"liveness must not fail during shutdown, or the orchestrator severs the drain")
		})
	}
}

func TestDefinition(t *testing.T) {
	resp := serve(t, fizzbuzz.NewService(), get(t, "/definition"))

	require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
	assert.Equal(t, jsonContentType, resp.header.Get("Content-Type"))

	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(resp.body, &doc))

	assert.True(t, strings.HasPrefix(doc.OpenAPI, "3.0"), "got %q", doc.OpenAPI)

	paths := []string{"/fizzbuzz", "/healthz", "/readyz", "/definition", "/documentation", "/statistics"}
	for _, path := range paths {
		assert.Contains(t, doc.Paths, path)
	}
}

func TestDocumentation(t *testing.T) {
	resp := serve(t, fizzbuzz.NewService(), get(t, "/documentation"))

	require.Equal(t, http.StatusOK, resp.status, "body: %s", resp.body)
	assert.Equal(t, "text/html", resp.header.Get("Content-Type"))
	assert.Contains(t, string(resp.body), "<html", "the page must be HTML")
	assert.Contains(t, string(resp.body), "cdn.redocly.com", "the page must be rendered by Redoc")
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

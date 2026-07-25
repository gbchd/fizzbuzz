package httpx

import (
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/gbchd/fizzbuzz/internal/pkg/slogx"
)

// RequestIDHeader is the de-facto standard name, so an upstream proxy's
// identifier is the one adopted.
const RequestIDHeader = "X-Request-ID"

// caps what an untrusted caller can push into the request ID header.
const maxRequestIDLength = 64

// LimitRequestBody belongs outermost: an oversized payload then stops at the
// read that crosses the cap, rather than being buffered whole and rejected.
func LimitRequestBody(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID adopts the upstream proxy's identifier so a trace spans the whole
// system, echoes it so a user reporting a problem can quote it, and puts a
// logger carrying it on the context, so every line written below is
// attributable to one request. Stamping the identifier is inseparable from
// minting it: split apart, a chain missing one half logs an empty field that
// reads as data.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			// uuid.Must is safe: uuid.NewV7 fails only if crypto/rand does, which is already fatal.
			id = uuid.Must(uuid.NewV7()).String()
		}
		if len(id) > maxRequestIDLength {
			id = id[:maxRequestIDLength]
		}

		w.Header().Set(RequestIDHeader, id)

		ctx := r.Context()
		logger := slogx.FromCtx(ctx).With(slog.String("request_id", id))

		next.ServeHTTP(w, r.WithContext(slogx.WithCtx(ctx, logger)))
	})
}

// AccessLog only observes: the logger it writes through was injected upstream,
// so injecting and recording stay separate concerns. Paths given in skip are
// left unlogged, for probes whose per-second traffic would bury real requests.
func AccessLog(skip ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(skip, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			recorder := &rwrecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(recorder, r)

			slogx.FromCtx(r.Context()).Info("handled request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

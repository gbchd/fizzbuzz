package httpx

import (
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/httprate"
)

const RetryAfterHeader = "Retry-After"

func RateLimit(requests int, window time.Duration, reject http.Handler, skip ...string) Middleware {
	limit := httprate.LimitBy(requests, window, clientKey, httprate.WithLimitHandler(reject.ServeHTTP))

	return func(next http.Handler) http.Handler {
		limited := limit(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(skip, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			limited.ServeHTTP(w, r)
		})
	}
}

func clientKey(r *http.Request) (string, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	return httprate.CanonicalizeIP(host), nil
}

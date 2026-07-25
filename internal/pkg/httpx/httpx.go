// Package httpx provides HTTP middleware that is independent of any particular
// API contract. Whatever a middleware needs from the contract — a body cap, the
// paths to keep out of the log — arrives as an argument.
package httpx

import (
	"net/http"
	"slices"
)

// Middleware wraps a handler in one concern.
type Middleware func(http.Handler) http.Handler

// Chain makes the first middleware listed the outermost, so the call site reads
// in the order a request travels.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for _, m := range slices.Backward(middlewares) {
		h = m(h)
	}

	return h
}

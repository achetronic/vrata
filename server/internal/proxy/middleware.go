package proxy

import (
	"net/http"
)

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in order: the first middleware in the slice
// is the outermost (executes first on the request, last on the response).
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	// Apply in reverse so the first middleware is outermost.
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package middlewares

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// passthrough is a Middleware that passes the request to the next handler
// unchanged. Used as a no-op when middleware configuration is absent or invalid.
func passthrough(next http.Handler) http.Handler { return next }

// Chain applies middlewares in order: the first middleware in the slice
// is the outermost (executes first on the request, last on the response).
func Chain(handler http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		handler = mws[i](handler)
	}
	return handler
}

// errorBody is the JSON structure used for middleware error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSONError writes a JSON error response with the given status and
// message. All Vrata-native middleware error responses go through this
// function to ensure consistent format with the proxy core.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorBody{Error: msg}); err != nil {
		slog.Warn("writeJSONError: failed to encode response", slog.String("error", err.Error()))
	}
}

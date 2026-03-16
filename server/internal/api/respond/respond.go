// Package respond provides helpers for writing consistent JSON responses.
package respond

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorBody is the standard error response shape.
type errorBody struct {
	Error string `json:"error"`
}

// JSON encodes v as JSON and writes it to w with the given status code.
// If encoding fails, a plain 500 is written instead.
func JSON(w http.ResponseWriter, status int, v any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("respond: encoding JSON", slog.String("error", err.Error()))
	}
}

// Error writes a JSON error response with the given status and message.
func Error(w http.ResponseWriter, status int, msg string, logger *slog.Logger) {
	JSON(w, status, errorBody{Error: msg}, logger)
}

package respond

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
)

func TestJSON(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := httptest.NewRecorder()

	JSON(w, 200, map[string]string{"key": "value"}, logger)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["key"] != "value" {
		t.Errorf("expected value, got %q", body["key"])
	}
}

func TestError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	w := httptest.NewRecorder()

	Error(w, 404, "not found", logger)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}

	var body ErrorBody
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "not found" {
		t.Errorf("expected 'not found', got %q", body.Error)
	}
}

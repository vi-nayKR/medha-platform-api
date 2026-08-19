package middleware_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/medha/backend/internal/server/middleware"
)

func TestRecovery_Middleware(t *testing.T) {
	// Create a logger that discards output to avoid cluttering test runs
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create a handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Wrap handler in Recovery middleware
	recoveryMiddleware := middleware.Recovery(logger)(panicHandler)

	// Create test request and response recorder
	req := httptest.NewRequest("GET", "/test-panic", nil)
	rec := httptest.NewRecorder()

	// Execute request
	recoveryMiddleware.ServeHTTP(rec, req)

	// Verify status code is 500 Internal Server Error
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	// Verify header is application/json
	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	// Verify response body
	var body map[string]string
	err := json.NewDecoder(rec.Body).Decode(&body)
	if err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body["error"] != "Internal Server Error" {
		t.Errorf("expected error message %q, got %q", "Internal Server Error", body["error"])
	}
}

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/medha/backend/internal/config"
)

func TestNewBucketProxy(t *testing.T) {
	// Start a backend test server that mimics the S3-compatible storage backend and returns CORS headers
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "https://admin.medha.dev")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		w.Header().Set("X-Custom-Header", "should-remain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock-data"))
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("failed to parse backend URL: %v", err)
	}

	cfg := &config.Config{
		S3Endpoint: backendURL.Host,
	}

	handler := NewBucketProxy(cfg)
	if handler == nil {
		t.Fatal("expected handler to be non-nil")
	}

	t.Run("Unsupported Method returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ceremony-logo/logo.png", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 Method Not Allowed, got %d", w.Code)
		}
	})

	t.Run("Path Traversal returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ceremony-logo/../../secret.txt", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("Strips CORS headers returned by backend", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ceremony-logo/logo.png", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", w.Code)
		}

		// Verify CORS headers returned by the mocked backend are stripped
		corsHeaders := []string{
			"Access-Control-Allow-Origin",
			"Access-Control-Allow-Methods",
			"Access-Control-Allow-Headers",
			"Access-Control-Allow-Credentials",
			"Access-Control-Expose-Headers",
		}

		for _, header := range corsHeaders {
			if val := w.Header().Get(header); val != "" {
				t.Errorf("expected header %q to be stripped, but got %q", header, val)
			}
		}

		// Verify other custom headers remain untouched
		if val := w.Header().Get("X-Custom-Header"); val != "should-remain" {
			t.Errorf("expected X-Custom-Header to be 'should-remain', got %q", val)
		}
	})
}

func TestNewBucketProxyFailsClosedOnInvalidEndpoint(t *testing.T) {
	handler := NewBucketProxy(&config.Config{S3Endpoint: "%"})
	req := httptest.NewRequest(http.MethodGet, "/ceremony-logo/logo.png", nil)
	res := httptest.NewRecorder()
	handler(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusServiceUnavailable)
	}
}

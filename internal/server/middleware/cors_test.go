package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func preflightOrigin(t *testing.T, isDev bool, origin string) string {
	t.Helper()
	handler := CORS(isDev)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodOptions, "https://api.example.test/resource", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res.Header().Get("Access-Control-Allow-Origin")
}

func TestCORSSeparatesDevelopmentAndProductionOrigins(t *testing.T) {
	tests := []struct {
		name   string
		isDev  bool
		origin string
		allow  bool
	}{
		{"development admin in development", true, "https://admin-dev.medha.dev", true},
		{"production admin in production", false, "https://admin.medha.dev", true},
		{"localhost in development", true, "http://localhost:5173", true},
		{"development admin rejected in production", false, "https://admin-dev.medha.dev", false},
		{"production admin rejected in development", true, "https://admin.medha.dev", false},
		{"localhost rejected in production", false, "http://localhost:5173", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allowed := preflightOrigin(t, tc.isDev, tc.origin) == tc.origin
			if allowed != tc.allow {
				t.Fatalf("allowed = %v, want %v", allowed, tc.allow)
			}
		})
	}
}

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminLogin(t *testing.T) {
	logger := slog.Default()

	t.Run("unconfigured handler returns 503", func(t *testing.T) {
		h := NewHandler(nil, nil, "", "", "", logger)

		req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/login", bytes.NewReader([]byte(`{"username":"admin","password":"password"}`)))
		w := httptest.NewRecorder()

		h.Login(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got %d", w.Code)
		}
	})

	t.Run("configured handler with correct credentials returns 200 and token", func(t *testing.T) {
		// Use a non-nil dummy pool reference so Configured() is true
		dummyPool := &pgxpool.Pool{}
		h := NewHandler(dummyPool, nil, "admin", "admin123!", "test-token", logger)

		body := map[string]string{
			"username": "admin",
			"password": "admin123!",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/login", bytes.NewReader(bodyBytes))
		w := httptest.NewRecorder()

		h.Login(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d. Body: %s", w.Code, w.Body.String())
		}

		var res struct {
			Data struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
			} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if res.Data.TokenType != "Bearer" {
			t.Errorf("expected token type 'Bearer', got %q", res.Data.TokenType)
		}
		// access_token is a signed HS256 admin JWT — validate it parses and
		// carries the expected claims rather than checking for a raw token.
		claims, err := h.parseAdminJWT(res.Data.AccessToken)
		if err != nil {
			t.Fatalf("returned access_token is not a valid admin JWT: %v", err)
		}
		if claims.Username != "admin" {
			t.Errorf("expected JWT username 'admin', got %q", claims.Username)
		}
		if claims.Role != "owner" {
			t.Errorf("expected JWT role 'owner', got %q", claims.Role)
		}
	})

	t.Run("configured handler with wrong credentials returns 401", func(t *testing.T) {
		dummyPool := &pgxpool.Pool{}
		h := NewHandler(dummyPool, nil, "admin", "admin123!", "test-token", logger)

		body := map[string]string{
			"username": "admin",
			"password": "wrongpassword",
		}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest(http.MethodPost, "/api/v2/admin/login", bytes.NewReader(bodyBytes))
		w := httptest.NewRecorder()

		h.Login(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", w.Code)
		}
	})
}

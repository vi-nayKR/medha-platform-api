package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/server/middleware"
)

// mockJWTValidator implements middleware.JWTValidator for testing.
type mockJWTValidator struct {
	claims *authdomain.JWTClaims
	err    error
}

func (m *mockJWTValidator) ValidateJWT(tokenString string) (*authdomain.JWTClaims, error) {
	return m.claims, m.err
}

func TestAuth_ValidToken(t *testing.T) {
	userID := uuid.New()
	validator := &mockJWTValidator{
		claims: &authdomain.JWTClaims{
			UserID:   userID,
			Role:     "yajman",
			Provider: "google",
		},
	}

	var capturedUserID uuid.UUID
	var capturedRole string
	var capturedProvider string
	handler := middleware.Auth(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedUserID, err = middleware.UserIDFromContext(r.Context())
		if err != nil {
			t.Errorf("UserIDFromContext error: %v", err)
		}
		capturedRole, err = middleware.UserRoleFromContext(r.Context())
		if err != nil {
			t.Errorf("UserRoleFromContext error: %v", err)
		}
		capturedProvider, err = middleware.ProviderFromContext(r.Context())
		if err != nil {
			t.Errorf("ProviderFromContext error: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v2/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token-xyz")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedUserID != userID {
		t.Errorf("expected user_id %s, got %s", userID, capturedUserID)
	}
	if capturedRole != "yajman" {
		t.Errorf("expected role 'yajman', got '%s'", capturedRole)
	}
	if capturedProvider != "google" {
		t.Errorf("expected provider 'google', got '%s'", capturedProvider)
	}
}

func TestAuth_MissingHeader(t *testing.T) {
	validator := &mockJWTValidator{}
	handler := middleware.Auth(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v2/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_InvalidFormat(t *testing.T) {
	validator := &mockJWTValidator{}
	handler := middleware.Auth(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v2/test", nil)
	req.Header.Set("Authorization", "Basic abc123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_InvalidToken(t *testing.T) {
	validator := &mockJWTValidator{
		err: authdomain.ErrInvalidToken,
	}
	handler := middleware.Auth(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/v2/test", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestContextHelpers_NoContext(t *testing.T) {
	ctx := context.Background()

	_, err := middleware.UserIDFromContext(ctx)
	if err == nil {
		t.Error("expected error from UserIDFromContext with empty context")
	}

	_, err = middleware.UserRoleFromContext(ctx)
	if err == nil {
		t.Error("expected error from UserRoleFromContext with empty context")
	}

	_, err = middleware.ProviderFromContext(ctx)
	if err == nil {
		t.Error("expected error from ProviderFromContext with empty context")
	}
}

func TestRequirePandit(t *testing.T) {
	t.Run("Allowed for Pandit", func(t *testing.T) {
		handler := middleware.RequirePandit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserRole, "pandit")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req.WithContext(ctx))

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("Forbidden for Yajman", func(t *testing.T) {
		handler := middleware.RequirePandit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserRole, "yajman")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req.WithContext(ctx))

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rec.Code)
		}
	})
}

func TestRequireYajman(t *testing.T) {
	t.Run("Allowed for Yajman", func(t *testing.T) {
		handler := middleware.RequireYajman(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserRole, "yajman")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req.WithContext(ctx))

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("Forbidden for Pandit", func(t *testing.T) {
		handler := middleware.RequireYajman(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("handler should not be called")
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), middleware.ContextKeyUserRole, "pandit")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req.WithContext(ctx))

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rec.Code)
		}
	})
}

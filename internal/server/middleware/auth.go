package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	apierrors "github.com/medha/backend/pkg/errors"
)

// Auth context keys (contextKey type defined in request_id.go)
const (
	ContextKeyUserID   contextKey = "user_id"
	ContextKeyUserRole contextKey = "user_role"
	ContextKeyProvider contextKey = "provider"
)

// JWTValidator defines the interface for validating JWT tokens.
// This keeps the middleware decoupled from the auth service implementation.
type JWTValidator interface {
	ValidateJWT(tokenString string) (*authdomain.JWTClaims, error)
}

// Auth creates a JWT verification middleware using the provided validator.
// It extracts the token from the Authorization header, validates it,
// and sets user_id, role, and provider_uid in the request context.
func Auth(validator JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization: Bearer <token>
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
					http.StatusUnauthorized,
					"https://medha.app/errors/missing-token",
					"Unauthorized",
					"Authorization header is required.",
					r.URL.Path,
				))
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
					http.StatusUnauthorized,
					"https://medha.app/errors/invalid-token-format",
					"Unauthorized",
					"Authorization header must be in 'Bearer <token>' format.",
					r.URL.Path,
				))
				return
			}

			tokenString := parts[1]

			// Validate JWT
			claims, err := validator.ValidateJWT(tokenString)
			if err != nil {
				apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
					http.StatusUnauthorized,
					"https://medha.app/errors/invalid-token",
					"Unauthorized",
					"The provided token is invalid or expired.",
					r.URL.Path,
				))
				return
			}

			// Set user context
			ctx := r.Context()
			ctx = context.WithValue(ctx, ContextKeyUserID, claims.UserID)
			ctx = context.WithValue(ctx, ContextKeyUserRole, claims.Role)
			ctx = context.WithValue(ctx, ContextKeyProvider, claims.Provider)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePandit ensures that the authenticated user has the 'pandit' role.
func RequirePandit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, err := UserRoleFromContext(r.Context())
		if err != nil || role != string(authdomain.RolePandit) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"This endpoint is restricted to Pandits.",
				r.URL.Path,
			))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireYajman ensures that the authenticated user has the 'yajman' role.
func RequireYajman(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, err := UserRoleFromContext(r.Context())
		if err != nil || role != string(authdomain.RoleYajman) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"This endpoint is restricted to Yajmans.",
				r.URL.Path,
			))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole ensures that the authenticated user's role is one of the given
// allowed roles. It's the general-purpose primitive for gating routes to any
// combination of pandit/yajman/common (e.g. excluding "common" from a route
// that requires a completed profile selection).
func RequireRole(allowed ...authdomain.UserRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, err := UserRoleFromContext(r.Context())
			if err != nil {
				apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
					http.StatusForbidden,
					"https://medha.app/errors/forbidden",
					"Forbidden",
					"This endpoint requires an assigned role.",
					r.URL.Path,
				))
				return
			}
			for _, a := range allowed {
				if role == string(a) {
					next.ServeHTTP(w, r)
					return
				}
			}
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You do not have permission to access this resource.",
				r.URL.Path,
			))
		})
	}
}

// UserIDFromContext extracts the user ID from the request context.
func UserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	userID, ok := ctx.Value(ContextKeyUserID).(uuid.UUID)
	if !ok {
		return uuid.Nil, errors.New("user_id not found in context")
	}
	return userID, nil
}

// UserRoleFromContext extracts the user role from the request context.
func UserRoleFromContext(ctx context.Context) (string, error) {
	role, ok := ctx.Value(ContextKeyUserRole).(string)
	if !ok {
		return "", errors.New("user_role not found in context")
	}
	return role, nil
}

// ProviderFromContext extracts the OAuth provider name from the request context.
func ProviderFromContext(ctx context.Context) (string, error) {
	p, ok := ctx.Value(ContextKeyProvider).(string)
	if !ok {
		return "", errors.New("provider not found in context")
	}
	return p, nil
}

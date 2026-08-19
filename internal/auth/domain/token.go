package domain

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/platform/epoch"
)

// Sentinel errors for auth operations.
var (
	ErrInvalidToken        = errors.New("invalid or expired token")
	ErrInvalidRefreshToken = errors.New("invalid, expired, or revoked refresh token")
	ErrTokenExpired        = errors.New("token has expired")
	ErrUserDeactivated     = errors.New("user account has been deactivated")
	// ErrAccountCannotAuthenticate is returned when a token is requested for
	// an account that is explicitly barred from logging in (e.g. a platform
	// system account, not a real human user).
	ErrAccountCannotAuthenticate = errors.New("this account cannot authenticate")
)

// JWTClaims represents the custom claims in Medha app JWTs.
type JWTClaims struct {
	jwt.RegisteredClaims
	UserID   uuid.UUID `json:"user_id"`
	Role     string    `json:"role"`     // "yajman", "pandit", or "" (not set yet)
	Provider string    `json:"provider"` // auth provider that issued the session
}

// Token represents JWT authentication tokens issued by the backend.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

// IsExpired returns true if the access token has expired.
func (t *Token) IsExpired() bool {
	return epoch.Now() > t.ExpiresAt
}

// TokenPair holds a new access + refresh token pair.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds until access token expires
}

// RefreshToken represents a stored refresh token record.
type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt int64
	RevokedAt *int64
	CreatedAt int64
}

// IsRevoked returns true if the refresh token has been revoked.
func (rt *RefreshToken) IsRevoked() bool {
	return rt.RevokedAt != nil
}

// IsExpired returns true if the refresh token has expired.
func (rt *RefreshToken) IsExpired() bool {
	return epoch.Now() > rt.ExpiresAt
}

// TokenRepository defines the port for refresh token data access.
type TokenRepository interface {
	// StoreRefreshToken persists a new refresh token.
	StoreRefreshToken(ctx context.Context, rt *RefreshToken) error

	// GetRefreshTokenByHash returns a refresh token by its SHA-256 hash.
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)

	// RevokeRefreshToken marks a refresh token as revoked.
	RevokeRefreshToken(ctx context.Context, id uuid.UUID) error

	// RevokeAllUserTokens revokes all refresh tokens for a user.
	RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error
}

// LoginResponse holds the full login response data.
type LoginResponse struct {
	TokenPair
	User      *User
	IsNewUser bool
}

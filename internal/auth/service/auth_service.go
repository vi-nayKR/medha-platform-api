package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/platform/epoch"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// AuthService handles core authentication operations:
// JWT validation and token pair generation.
// V2 auth flows (OTP, Google Sign-In) are handled by AuthServiceV2.
type AuthService struct {
	userRepo          userdomain.UserRepository
	tokenRepo         authdomain.TokenRepository
	privateKey        *rsa.PrivateKey
	publicKey         *rsa.PublicKey
	jwtExpiryMinutes  int
	refreshExpiryDays int
	logger            *slog.Logger
}

// NewAuthService creates a new AuthService with the given dependencies.
func NewAuthService(
	userRepo userdomain.UserRepository,
	tokenRepo authdomain.TokenRepository,
	privateKeyPath string,
	publicKeyPath string,
	jwtExpiryMinutes int,
	logger *slog.Logger,
) (*AuthService, error) {
	// Load RSA private key
	privKeyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privKeyData)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	// Load RSA public key
	pubKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(pubKeyData)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	if jwtExpiryMinutes <= 0 {
		jwtExpiryMinutes = 15
	}

	logger.Info("auth service initialized",
		"jwt_expiry_minutes", jwtExpiryMinutes,
		"refresh_expiry_days", 90,
	)

	return &AuthService{
		userRepo:          userRepo,
		tokenRepo:         tokenRepo,
		privateKey:        privateKey,
		publicKey:         publicKey,
		jwtExpiryMinutes:  jwtExpiryMinutes,
		refreshExpiryDays: 90,
		logger:            logger,
	}, nil
}

// LoadRSAKeys loads and parses an RSA private + public key pair from PEM files.
// This is an exported helper so that V2 services can share the same key loading logic.
func LoadRSAKeys(privateKeyPath, publicKeyPath string) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privKeyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read private key: %w", err)
	}
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privKeyData)
	if err != nil {
		return nil, nil, fmt.Errorf("parse private key: %w", err)
	}

	pubKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read public key: %w", err)
	}
	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(pubKeyData)
	if err != nil {
		return nil, nil, fmt.Errorf("parse public key: %w", err)
	}

	return privateKey, publicKey, nil
}

// ValidateJWT parses and validates a JWT, returning the claims.
func (s *AuthService) ValidateJWT(tokenString string) (*authdomain.JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &authdomain.JWTClaims{}, func(_ *jwt.Token) (interface{}, error) {
		return s.publicKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithIssuer("medha-api"))
	if err != nil {
		return nil, authdomain.ErrInvalidToken
	}

	claims, ok := token.Claims.(*authdomain.JWTClaims)
	if !ok || !token.Valid {
		return nil, authdomain.ErrInvalidToken
	}

	// Verify user exists and is not deleted
	if s.userRepo != nil {
		user, err := s.userRepo.GetByID(context.Background(), claims.UserID)
		if err != nil || user == nil {
			return nil, authdomain.ErrInvalidToken
		}
	}

	return claims, nil
}

// generateTokenPair creates a new JWT + refresh token for a user.
func (s *AuthService) generateTokenPair(ctx context.Context, user *userdomain.User) (*authdomain.TokenPair, error) {
	// Generate JWT
	now := time.Now()
	expiresAt := now.Add(time.Duration(s.jwtExpiryMinutes) * time.Minute)

	claims := &authdomain.JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    "medha-api",
		},
		UserID:   user.ID,
		Role:     user.Role.String(),
		Provider: user.AuthProvider,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	accessToken, err := token.SignedString(s.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign jwt: %w", err)
	}

	// Generate refresh token (crypto-random 256-bit)
	refreshTokenBytes := make([]byte, 32)
	if _, err := rand.Read(refreshTokenBytes); err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshToken := base64.URLEncoding.EncodeToString(refreshTokenBytes)

	// Store refresh token hash
	refreshTokenHash := hashToken(refreshToken)
	storedRefresh := &authdomain.RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: refreshTokenHash,
		ExpiresAt: epoch.FromTime(now.Add(time.Duration(s.refreshExpiryDays) * 24 * time.Hour)),
	}
	if err := s.tokenRepo.StoreRefreshToken(ctx, storedRefresh); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &authdomain.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    s.jwtExpiryMinutes * 60,
	}, nil
}

// hashToken returns the SHA-256 hex digest of a token string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/platform/epoch"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/auth/domain"
)

// PostgresTokenRepository implements domain.TokenRepository using pgxpool.
type PostgresTokenRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresTokenRepository creates a new PostgresTokenRepository.
func NewPostgresTokenRepository(pool *pgxpool.Pool) *PostgresTokenRepository {
	return &PostgresTokenRepository{pool: pool}
}

// StoreRefreshToken persists a new refresh token.
func (r *PostgresTokenRepository) StoreRefreshToken(ctx context.Context, rt *domain.RefreshToken) error {
	query := `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		rt.ID,
		rt.UserID,
		rt.TokenHash,
		rt.ExpiresAt,
		epoch.Now(),
	)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}

	return nil
}

// GetRefreshTokenByHash returns a refresh token by its SHA-256 hash.
func (r *PostgresTokenRepository) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	query := `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE token_hash = $1
	`

	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, tokenHash)

	var rt domain.RefreshToken
	err := row.Scan(
		&rt.ID,
		&rt.UserID,
		&rt.TokenHash,
		&rt.ExpiresAt,
		&rt.RevokedAt,
		&rt.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("get refresh token by hash: %w", err)
	}

	return &rt, nil
}

// RevokeRefreshToken marks a refresh token as revoked.
func (r *PostgresTokenRepository) RevokeRefreshToken(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE refresh_tokens SET revoked_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND revoked_at IS NULL`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidRefreshToken
	}

	return nil
}

// RevokeAllUserTokens revokes all active refresh tokens for a user.
func (r *PostgresTokenRepository) RevokeAllUserTokens(ctx context.Context, userID uuid.UUID) error {
	query := `UPDATE refresh_tokens SET revoked_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE user_id = $1 AND revoked_at IS NULL`

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("revoke all user tokens: %w", err)
	}

	return nil
}

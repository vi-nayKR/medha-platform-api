package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/systempublisher/domain"
)

// PostgresSystemPublisherRepository implements domain.SystemPublisherRepository.
type PostgresSystemPublisherRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresSystemPublisherRepository creates a new PostgresSystemPublisherRepository.
func NewPostgresSystemPublisherRepository(pool *pgxpool.Pool) *PostgresSystemPublisherRepository {
	return &PostgresSystemPublisherRepository{pool: pool}
}

const selectPublisherColumns = `
	id, publisher_key, display_name, username, COALESCE(avatar_url, ''),
	description, is_verified, is_active, created_at, updated_at
`

func scanPublisher(row pgx.Row) (*domain.SystemPublisher, error) {
	var p domain.SystemPublisher
	err := row.Scan(
		&p.ID, &p.PublisherKey, &p.DisplayName, &p.Username, &p.AvatarURL,
		&p.Description, &p.IsVerified, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPublisherNotFound
		}
		return nil, fmt.Errorf("scan system publisher: %w", err)
	}
	return &p, nil
}

// GetByKey returns a publisher by its stable publisher_key.
func (r *PostgresSystemPublisherRepository) GetByKey(ctx context.Context, publisherKey string) (*domain.SystemPublisher, error) {
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT `+selectPublisherColumns+` FROM system_publishers WHERE publisher_key = $1`,
		publisherKey,
	)
	return scanPublisher(row)
}

// GetByID returns a publisher by its UUID.
func (r *PostgresSystemPublisherRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.SystemPublisher, error) {
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT `+selectPublisherColumns+` FROM system_publishers WHERE id = $1`,
		id,
	)
	return scanPublisher(row)
}

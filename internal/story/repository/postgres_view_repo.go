package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
)

// PostgresStoryViewRepository implements domain.StoryViewRepository.
type PostgresStoryViewRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresStoryViewRepository creates a new PostgresStoryViewRepository.
func NewPostgresStoryViewRepository(pool *pgxpool.Pool) *PostgresStoryViewRepository {
	return &PostgresStoryViewRepository{pool: pool}
}

// RecordView idempotently upserts a (story_id, viewer_user_id) view row.
func (r *PostgresStoryViewRepository) RecordView(ctx context.Context, storyID, viewerUserID uuid.UUID) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, `
		INSERT INTO official_story_views (story_id, viewer_user_id, first_viewed_at, last_viewed_at, view_count)
		VALUES ($1, $2, (EXTRACT(EPOCH FROM NOW()))::BIGINT, (EXTRACT(EPOCH FROM NOW()))::BIGINT, 1)
		ON CONFLICT (story_id, viewer_user_id) DO UPDATE SET
			last_viewed_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT,
			view_count = official_story_views.view_count + 1
	`, storyID, viewerUserID)
	if err != nil {
		return fmt.Errorf("record story view: %w", err)
	}
	return nil
}

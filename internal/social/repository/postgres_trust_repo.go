package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/social/domain"
)

// PostgresTrustRepository implements domain.TrustRepository using pgx.
type PostgresTrustRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresTrustRepository creates a new PostgresTrustRepository.
func NewPostgresTrustRepository(pool *pgxpool.Pool) *PostgresTrustRepository {
	return &PostgresTrustRepository{pool: pool}
}

// CreateFeedback inserts event feedback.
func (r *PostgresTrustRepository) CreateFeedback(ctx context.Context, feedback *domain.EventFeedback) error {
	query := `
		INSERT INTO event_feedback (id, match_id, yajman_id, pandit_id, rating, comment, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		feedback.ID, feedback.MatchID, feedback.YajmanID, feedback.PanditID,
		feedback.Rating, feedback.Comment, feedback.CreatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return domain.ErrFeedbackAlreadyExists
		}
		return fmt.Errorf("insert feedback: %w", err)
	}
	return nil
}

// GetFeedbackByMatchID returns feedback for a specific match.
func (r *PostgresTrustRepository) GetFeedbackByMatchID(ctx context.Context, matchID uuid.UUID) (*domain.EventFeedback, error) {
	query := `
		SELECT id, match_id, yajman_id, pandit_id, rating, comment, created_at
		FROM event_feedback WHERE match_id = $1
	`
	var f domain.EventFeedback
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, matchID).Scan(
		&f.ID, &f.MatchID, &f.YajmanID, &f.PanditID,
		&f.Rating, &f.Comment, &f.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrFeedbackNotFound
		}
		return nil, fmt.Errorf("get feedback: %w", err)
	}
	return &f, nil
}

// GetBadge returns a pandit's badge record.
func (r *PostgresTrustRepository) GetBadge(ctx context.Context, panditID uuid.UUID) (*domain.PanditBadge, error) {
	query := `
		SELECT id, pandit_id, badge_tier, ceremonies_completed, average_rating, total_feedback_count, updated_at
		FROM pandit_badges WHERE pandit_id = $1
	`
	var b domain.PanditBadge
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, panditID).Scan(
		&b.ID, &b.PanditID, &b.BadgeTier, &b.CeremoniesCompleted,
		&b.AverageRating, &b.TotalFeedbackCount, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBadgeNotFound
		}
		return nil, fmt.Errorf("get badge: %w", err)
	}
	return &b, nil
}

// UpdateBadge updates a pandit's badge stats and tier.
func (r *PostgresTrustRepository) UpdateBadge(ctx context.Context, badge *domain.PanditBadge) error {
	query := `
		UPDATE pandit_badges
		SET badge_tier = $1, ceremonies_completed = $2, average_rating = $3, total_feedback_count = $4
		WHERE pandit_id = $5
	`
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		badge.BadgeTier, badge.CeremoniesCompleted, badge.AverageRating,
		badge.TotalFeedbackCount, badge.PanditID,
	)
	if err != nil {
		return fmt.Errorf("update badge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBadgeNotFound
	}
	return nil
}

// RecalculateBadgeStats recalculates from aggregate feedback and updates badge tier.
func (r *PostgresTrustRepository) RecalculateBadgeStats(ctx context.Context, panditID uuid.UUID) (*domain.PanditBadge, error) {
	// Calculate aggregate stats from event_feedback
	var count int
	var avgRating float64
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(AVG(rating::decimal), 0) FROM event_feedback WHERE pandit_id = $1`,
		panditID,
	).Scan(&count, &avgRating)
	if err != nil {
		return nil, fmt.Errorf("calculate stats: %w", err)
	}

	// Compute new tier
	newTier := domain.ComputeTier(count, avgRating)

	// Update badge record
	query := `
		UPDATE pandit_badges
		SET badge_tier = $1, ceremonies_completed = $2, average_rating = $3, total_feedback_count = $4
		WHERE pandit_id = $5
		RETURNING id, pandit_id, badge_tier, ceremonies_completed, average_rating, total_feedback_count, updated_at
	`
	var b domain.PanditBadge
	err = database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, newTier, count, avgRating, count, panditID).Scan(
		&b.ID, &b.PanditID, &b.BadgeTier, &b.CeremoniesCompleted,
		&b.AverageRating, &b.TotalFeedbackCount, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrBadgeNotFound
		}
		return nil, fmt.Errorf("recalculate badge: %w", err)
	}

	return &b, nil
}

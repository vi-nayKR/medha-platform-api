package domain

import (
	"context"

	"github.com/google/uuid"
)

// TrustRepository defines the port for feedback and badge data access.
type TrustRepository interface {
	// CreateFeedback inserts event feedback.
	// Returns ErrFeedbackAlreadyExists if feedback exists for this match.
	CreateFeedback(ctx context.Context, feedback *EventFeedback) error

	// GetFeedbackByMatchID returns feedback for a specific match.
	GetFeedbackByMatchID(ctx context.Context, matchID uuid.UUID) (*EventFeedback, error)

	// GetBadge returns a pandit's badge record.
	GetBadge(ctx context.Context, panditID uuid.UUID) (*PanditBadge, error)

	// UpdateBadge updates a pandit's badge stats and tier.
	UpdateBadge(ctx context.Context, badge *PanditBadge) error

	// RecalculateBadgeStats recalculates ceremonies_completed, average_rating,
	// and total_feedback_count from event_feedback for a pandit, then updates badge tier.
	RecalculateBadgeStats(ctx context.Context, panditID uuid.UUID) (*PanditBadge, error)
}

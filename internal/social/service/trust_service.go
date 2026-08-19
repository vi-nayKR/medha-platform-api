package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	matchingdomain "github.com/medha/backend/internal/matching/domain"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/social/domain"
)

// TrustService handles feedback and badge business logic.
type TrustService struct {
	trustRepo domain.TrustRepository
	matchRepo matchingdomain.MatchRepository
	logger    *slog.Logger
}

// NewTrustService creates a new TrustService.
func NewTrustService(trustRepo domain.TrustRepository, matchRepo matchingdomain.MatchRepository, logger *slog.Logger) *TrustService {
	return &TrustService{
		trustRepo: trustRepo,
		matchRepo: matchRepo,
		logger:    logger,
	}
}

// SubmitFeedbackParams contains the parameters for submitting feedback.
type SubmitFeedbackParams struct {
	MatchID  uuid.UUID
	YajmanID uuid.UUID
	Rating   int
	Comment  string
}

// SubmitFeedback creates event feedback and recalculates the pandit's badge.
func (s *TrustService) SubmitFeedback(ctx context.Context, params SubmitFeedbackParams) (*domain.EventFeedback, *domain.PanditBadge, error) {
	// Verify the match exists and belongs to this yajman
	match, err := s.matchRepo.GetByID(ctx, params.MatchID)
	if err != nil {
		return nil, nil, fmt.Errorf("get match: %w", err)
	}

	if match.YajmanID != params.YajmanID {
		return nil, nil, domain.ErrNotYajmanInMatch
	}

	if match.Status != matchingdomain.MatchStatusCompleted {
		return nil, nil, domain.ErrMatchNotCompleted
	}

	// Validate rating
	if params.Rating < 1 || params.Rating > 5 {
		return nil, nil, domain.ErrInvalidRating
	}

	feedback := &domain.EventFeedback{
		ID:        uuid.New(),
		MatchID:   params.MatchID,
		YajmanID:  params.YajmanID,
		PanditID:  match.PanditID,
		Rating:    params.Rating,
		Comment:   params.Comment,
		CreatedAt: epoch.Now(),
	}

	if err := s.trustRepo.CreateFeedback(ctx, feedback); err != nil {
		return nil, nil, fmt.Errorf("create feedback: %w", err)
	}

	// Recalculate badge
	badge, err := s.trustRepo.RecalculateBadgeStats(ctx, match.PanditID)
	if err != nil {
		s.logger.Error("failed to recalculate badge after feedback",
			"error", err,
			"pandit_id", match.PanditID,
		)
		// Don't fail the feedback — badge update is best-effort
	}

	s.logger.Info("event feedback submitted",
		"feedback_id", feedback.ID,
		"match_id", params.MatchID,
		"pandit_id", match.PanditID,
		"rating", params.Rating,
	)

	return feedback, badge, nil
}

// GetBadge returns a pandit's badge info.
func (s *TrustService) GetBadge(ctx context.Context, panditID uuid.UUID) (*domain.PanditBadge, error) {
	badge, err := s.trustRepo.GetBadge(ctx, panditID)
	if err != nil {
		return nil, fmt.Errorf("get badge: %w", err)
	}
	return badge, nil
}

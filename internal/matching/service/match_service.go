package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	eventdomain "github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/matching/domain"
	messagingdomain "github.com/medha/backend/internal/messaging/domain"
	messagingservice "github.com/medha/backend/internal/messaging/service"
	notifdomain "github.com/medha/backend/internal/notification/domain"
	notifservice "github.com/medha/backend/internal/notification/service"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/platform/worker"
)

// MatchService handles match-related business logic.
type MatchService struct {
	pool       *pgxpool.Pool
	matchRepo  domain.MatchRepository
	eventRepo  eventdomain.EventRepository
	workerPool *worker.Pool
	notifSvc   *notifservice.NotificationService  // optional — nil-safe
	msgSvc     *messagingservice.MessagingService // optional — nil-safe, auto-creates conversations on match accept
	logger     *slog.Logger
}

// NewMatchService creates a new MatchService.
func NewMatchService(pool *pgxpool.Pool, matchRepo domain.MatchRepository, eventRepo eventdomain.EventRepository, workerPool *worker.Pool, logger *slog.Logger) *MatchService {
	return &MatchService{
		pool:       pool,
		matchRepo:  matchRepo,
		eventRepo:  eventRepo,
		workerPool: workerPool,
		logger:     logger,
	}
}

// SetNotificationService wires the optional notification service for push notification triggers.
func (s *MatchService) SetNotificationService(notifSvc *notifservice.NotificationService) {
	s.notifSvc = notifSvc
}

// SetMessagingService wires the optional messaging service for auto-creating conversations on match accept.
func (s *MatchService) SetMessagingService(msgSvc *messagingservice.MessagingService) {
	s.msgSvc = msgSvc
}

// CreateMatch creates a match record when a pandit expresses interest in an event.
// Initial status is 'created'. Enforces the max matches per event cap (~3).
func (s *MatchService) CreateMatch(ctx context.Context, yajmanID, panditID, eventID uuid.UUID) (*domain.Match, error) {
	// Verify event exists
	_, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	// Check match cap
	count, err := s.matchRepo.CountByEventID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("count matches: %w", err)
	}
	if count >= domain.MaxMatchesPerEvent {
		return nil, domain.ErrMaxMatchesReached
	}

	now := epoch.Now()
	match := &domain.Match{
		ID:        uuid.New(),
		YajmanID:  yajmanID,
		PanditID:  panditID,
		EventID:   eventID,
		Status:    domain.MatchStatusCreated,
		MatchedAt: now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.matchRepo.Create(ctx, match); err != nil {
		return nil, fmt.Errorf("create match: %w", err)
	}

	s.logger.Info("match created (pandit interested)",
		"match_id", match.ID,
		"yajman_id", yajmanID,
		"pandit_id", panditID,
		"event_id", eventID,
	)

	return match, nil
}

// GetMatch returns a match with full details if the user is a party.
func (s *MatchService) GetMatch(ctx context.Context, matchID, userID uuid.UUID) (*domain.MatchWithDetails, error) {
	match, err := s.matchRepo.GetByIDWithDetails(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get match: %w", err)
	}

	// Only match parties can view match details (contains phone numbers)
	if match.YajmanID != userID && match.PanditID != userID {
		return nil, domain.ErrNotMatchParty
	}

	return match, nil
}

func (s *MatchService) AcceptMatch(ctx context.Context, matchID, yajmanID uuid.UUID) (*domain.Match, error) {
	match, err := s.matchRepo.GetByID(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get match: %w", err)
	}
	if match.YajmanID != yajmanID {
		return nil, domain.ErrNotMatchParty
	}
	if !CanTransition(match.Status, domain.MatchStatusMatched) {
		return nil, domain.ErrInvalidStatusTransition
	}

	err = database.WithTx(ctx, s.pool, func(txCtx context.Context) error {
		if err := s.matchRepo.UpdateStatus(txCtx, matchID, domain.MatchStatusMatched); err != nil {
			return fmt.Errorf("accept match: %w", err)
		}
		if err := s.eventRepo.UpdateStatus(txCtx, match.EventID, eventdomain.EventStatusBooked); err != nil {
			return fmt.Errorf("mark event booked: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	match.Status = domain.MatchStatusMatched

	s.logger.Info("match accepted (yajman accepted interest)",
		"match_id", matchID,
		"yajman_id", yajmanID,
		"pandit_id", match.PanditID,
	)

	// Dispatch async notification
	if s.workerPool != nil && s.notifSvc != nil {
		s.workerPool.Enqueue(func(jobCtx context.Context) error {
			s.logger.Info("sending accept match async event", "match_id", matchID)

			// Notify Yajman
			_, errY := s.notifSvc.CreateNotification(jobCtx, notifservice.CreateNotificationParams{
				UserID: yajmanID,
				Type:   notifdomain.NotifBookingConfirmed,
				Title:  "Event is booked",
				Body:   "Your ceremony booking has been confirmed.",
				Data: map[string]string{
					"match_id": matchID.String(),
					"event_id": match.EventID.String(),
					"type":     "booking_confirmed",
				},
			})
			if errY != nil {
				s.logger.Error("failed to notify yajman of booking", "error", errY, "yajman_id", yajmanID)
			}

			// Notify Pandit
			_, errP := s.notifSvc.CreateNotification(jobCtx, notifservice.CreateNotificationParams{
				UserID: match.PanditID,
				Type:   notifdomain.NotifBookingConfirmed,
				Title:  "Event is booked",
				Body:   "Your booking for this event has been finalized and confirmed.",
				Data: map[string]string{
					"match_id": matchID.String(),
					"event_id": match.EventID.String(),
					"type":     "booking_confirmed",
				},
			})
			if errP != nil {
				s.logger.Error("failed to notify pandit of booking", "error", errP, "pandit_id", match.PanditID)
			}

			return nil
		})
	}

	// Auto-create pandit↔yajman conversation on match acceptance
	if s.msgSvc != nil && s.workerPool != nil {
		s.workerPool.Enqueue(func(jobCtx context.Context) error {
			eventIDCopy := match.EventID
			matchIDCopy := matchID
			_, err := s.msgSvc.CreateConversation(jobCtx, yajmanID, messagingservice.CreateConversationParams{
				Type:           messagingdomain.ConvPanditYajman,
				ParticipantIDs: []uuid.UUID{match.PanditID},
				EventID:        &eventIDCopy,
				MatchID:        &matchIDCopy,
				Title:          "Booking Conversation",
			})
			if err != nil {
				s.logger.Error("failed to auto-create conversation on match accept",
					"error", err, "match_id", matchID, "yajman_id", yajmanID, "pandit_id", match.PanditID,
				)
			} else {
				s.logger.Info("auto-created pandit_yajman conversation",
					"match_id", matchID, "yajman_id", yajmanID, "pandit_id", match.PanditID,
				)
			}
			return nil
		})
	}

	return match, nil
}

// CancelMatch transitions a match to cancelled status.
func (s *MatchService) CancelMatch(ctx context.Context, matchID, userID uuid.UUID) error {
	match, err := s.matchRepo.GetByID(ctx, matchID)
	if err != nil {
		return fmt.Errorf("get match: %w", err)
	}
	if match.YajmanID != userID && match.PanditID != userID {
		return domain.ErrNotMatchParty
	}
	if !CanTransition(match.Status, domain.MatchStatusCancelled) {
		return domain.ErrInvalidStatusTransition
	}

	if err := s.matchRepo.UpdateStatus(ctx, matchID, domain.MatchStatusCancelled); err != nil {
		return fmt.Errorf("cancel match: %w", err)
	}

	s.logger.Info("match cancelled", "match_id", matchID, "user_id", userID)
	return nil
}

func (s *MatchService) ActivateMatch(ctx context.Context, matchID, userID uuid.UUID) error {
	match, err := s.matchRepo.GetByID(ctx, matchID)
	if err != nil {
		return fmt.Errorf("get match: %w", err)
	}
	if match.YajmanID != userID && match.PanditID != userID {
		return domain.ErrNotMatchParty
	}
	if !CanTransition(match.Status, domain.MatchStatusActive) {
		return domain.ErrInvalidStatusTransition
	}

	err = database.WithTx(ctx, s.pool, func(txCtx context.Context) error {
		if err := s.matchRepo.UpdateStatus(txCtx, matchID, domain.MatchStatusActive); err != nil {
			return fmt.Errorf("activate match: %w", err)
		}
		if err := s.eventRepo.UpdateStatus(txCtx, match.EventID, eventdomain.EventStatusActive); err != nil {
			return fmt.Errorf("mark event active: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.logger.Info("match activated (ceremony in progress)", "match_id", matchID)

	if s.workerPool != nil {
		s.workerPool.Enqueue(func(jobCtx context.Context) error {
			s.logger.Info("dispatching async event for match activation", "match_id", matchID)
			return nil
		})
	}
	return nil
}

func (s *MatchService) CompleteMatch(ctx context.Context, matchID, userID uuid.UUID) error {
	match, err := s.matchRepo.GetByID(ctx, matchID)
	if err != nil {
		return fmt.Errorf("get match: %w", err)
	}
	if match.YajmanID != userID {
		return domain.ErrNotMatchParty
	}
	if !CanTransition(match.Status, domain.MatchStatusCompleted) {
		return domain.ErrInvalidStatusTransition
	}

	err = database.WithTx(ctx, s.pool, func(txCtx context.Context) error {
		if err := s.matchRepo.UpdateStatus(txCtx, matchID, domain.MatchStatusCompleted); err != nil {
			return fmt.Errorf("complete match: %w", err)
		}
		if err := s.eventRepo.UpdateStatus(txCtx, match.EventID, eventdomain.EventStatusCompleted); err != nil {
			return fmt.Errorf("mark event completed: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.logger.Info("match completed (ceremony done)", "match_id", matchID)

	if s.workerPool != nil {
		s.workerPool.Enqueue(func(jobCtx context.Context) error {
			s.logger.Info("dispatching async event for match completion", "match_id", matchID)
			return nil
		})
	}
	return nil
}

// ListMyMatches returns all matches for the authenticated user.
func (s *MatchService) ListMyMatches(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*domain.MatchWithDetails, string, error) {
	matches, nextCursor, err := s.matchRepo.ListByUserID(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list my matches: %w", err)
	}
	return matches, nextCursor, nil
}

// ListEventMatches returns all matches for an event (for event owner).
func (s *MatchService) ListEventMatches(ctx context.Context, eventID, userID uuid.UUID) ([]*domain.MatchWithDetails, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}
	if event.YajmanID != userID {
		return nil, domain.ErrNotMatchParty
	}
	matches, err := s.matchRepo.ListByEventID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list event matches: %w", err)
	}
	return matches, nil
}

// FindByPanditAndEvent returns the match for a specific pandit+event pair.
func (s *MatchService) FindByPanditAndEvent(ctx context.Context, panditID, eventID uuid.UUID) (*domain.Match, error) {
	match, err := s.matchRepo.GetByPanditAndEvent(ctx, panditID, eventID)
	if err != nil {
		return nil, fmt.Errorf("find match by pandit and event: %w", err)
	}
	return match, nil
}

// ConnectMatch transitions a match to matched status (without updating event status).
func (s *MatchService) ConnectMatch(ctx context.Context, matchID, yajmanID uuid.UUID) (*domain.Match, error) {
	match, err := s.matchRepo.GetByID(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get match: %w", err)
	}
	if match.YajmanID != yajmanID {
		return nil, domain.ErrNotMatchParty
	}
	if !CanTransition(match.Status, domain.MatchStatusMatched) {
		return nil, domain.ErrInvalidStatusTransition
	}

	if err := s.matchRepo.UpdateStatus(ctx, matchID, domain.MatchStatusMatched); err != nil {
		return nil, fmt.Errorf("connect match: %w", err)
	}
	match.Status = domain.MatchStatusMatched

	s.logger.Info("match connected (yajman accepted interest)",
		"match_id", matchID,
		"yajman_id", yajmanID,
		"pandit_id", match.PanditID,
	)

	return match, nil
}

// BookMatch transitions a match to active (booked) status and triggers notifications.
func (s *MatchService) BookMatch(ctx context.Context, matchID, yajmanID uuid.UUID) (*domain.Match, error) {
	match, err := s.matchRepo.GetByID(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get match: %w", err)
	}
	if match.YajmanID != yajmanID {
		return nil, domain.ErrNotMatchParty
	}
	if !CanTransition(match.Status, domain.MatchStatusActive) {
		return nil, domain.ErrInvalidStatusTransition
	}

	if err := s.matchRepo.UpdateStatus(ctx, matchID, domain.MatchStatusActive); err != nil {
		return nil, fmt.Errorf("book match: %w", err)
	}
	match.Status = domain.MatchStatusActive

	s.logger.Info("match booked (yajman confirmed booking)",
		"match_id", matchID,
		"yajman_id", yajmanID,
		"pandit_id", match.PanditID,
	)

	// Dispatch async push notifications
	if s.workerPool != nil && s.notifSvc != nil {
		s.workerPool.Enqueue(func(jobCtx context.Context) error {
			s.logger.Info("sending book match async event", "match_id", matchID)

			// Notify Yajman
			_, errY := s.notifSvc.CreateNotification(jobCtx, notifservice.CreateNotificationParams{
				UserID: yajmanID,
				Type:   notifdomain.NotifBookingConfirmed,
				Title:  "Event is booked",
				Body:   "Your ceremony booking has been confirmed.",
				Data: map[string]string{
					"match_id": matchID.String(),
					"event_id": match.EventID.String(),
					"type":     "booking_confirmed",
				},
			})
			if errY != nil {
				s.logger.Error("failed to notify yajman of booking", "error", errY, "yajman_id", yajmanID)
			}

			// Notify Pandit
			_, errP := s.notifSvc.CreateNotification(jobCtx, notifservice.CreateNotificationParams{
				UserID: match.PanditID,
				Type:   notifdomain.NotifBookingConfirmed,
				Title:  "Event is booked",
				Body:   "Your booking for this event has been finalized and confirmed.",
				Data: map[string]string{
					"match_id": matchID.String(),
					"event_id": match.EventID.String(),
					"type":     "booking_confirmed",
				},
			})
			if errP != nil {
				s.logger.Error("failed to notify pandit of booking", "error", errP, "pandit_id", match.PanditID)
			}

			return nil
		})
	}

	return match, nil
}

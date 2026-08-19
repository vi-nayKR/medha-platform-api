package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	eventdomain "github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/interest/domain"
	matchservice "github.com/medha/backend/internal/matching/service"
	messagingdomain "github.com/medha/backend/internal/messaging/domain"
	messagingservice "github.com/medha/backend/internal/messaging/service"
	notifdomain "github.com/medha/backend/internal/notification/domain"
	notifservice "github.com/medha/backend/internal/notification/service"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// InterestService handles interest-related business logic.
type InterestService struct {
	pool         *pgxpool.Pool
	interestRepo domain.InterestRepository
	eventRepo    eventdomain.EventRepository
	matchSvc     *matchservice.MatchService
	userRepo     userdomain.UserRepository
	notifSvc     *notifservice.NotificationService // optional — nil-safe
	msgSvc       *messagingservice.MessagingService // optional — nil-safe
	logger       *slog.Logger
}

// NewInterestService creates a new InterestService.
func NewInterestService(pool *pgxpool.Pool, interestRepo domain.InterestRepository, eventRepo eventdomain.EventRepository, matchSvc *matchservice.MatchService, userRepo userdomain.UserRepository, logger *slog.Logger) *InterestService {
	return &InterestService{
		pool:         pool,
		interestRepo: interestRepo,
		eventRepo:    eventRepo,
		matchSvc:     matchSvc,
		userRepo:     userRepo,
		logger:       logger,
	}
}


// SetNotificationService wires the optional notification service for push notification triggers.
func (s *InterestService) SetNotificationService(notifSvc *notifservice.NotificationService) {
	s.notifSvc = notifSvc
}

// SetMessagingService wires the optional messaging service for auto-conversation creation.
func (s *InterestService) SetMessagingService(msgSvc *messagingservice.MessagingService) {
	s.msgSvc = msgSvc
}

// ExpressInterestParams contains the parameters for expressing interest.
type ExpressInterestParams struct {
	PanditID uuid.UUID
	EventID  uuid.UUID
	Message  string
}

// ExpressInterest creates an interest from a pandit for an event.
// Under the manual-approval approach, this interest is created as pending and requires Yajman acceptance.
func (s *InterestService) ExpressInterest(ctx context.Context, params ExpressInterestParams) (*domain.Interest, error) {
	// Verify event exists and is active
	event, err := s.eventRepo.GetByID(ctx, params.EventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	if !event.IsActive() {
		return nil, domain.ErrEventNotAcceptingInterests
	}

	// Prevent pandit from expressing interest in their own event
	if event.YajmanID == params.PanditID {
		return nil, domain.ErrCannotInterestOwnEvent
	}

	interest := &domain.Interest{
		ID:       uuid.New(),
		PanditID: params.PanditID,
		EventID:  params.EventID,
		Message:  params.Message,
		Status:   domain.InterestStatusPending, // pending
	}

	if err := s.interestRepo.Create(ctx, interest); err != nil {
		return nil, fmt.Errorf("create interest: %w", err)
	}

	// Auto-transition event status based on active applicant count
	if err := s.evaluateAndTransitionEventStatus(ctx, params.EventID); err != nil {
		s.logger.Error("failed to auto-transition event status after interest expression", "error", err, "event_id", params.EventID)
	}

	// Create match record in 'created' status
	if s.matchSvc != nil {
		_, matchErr := s.matchSvc.CreateMatch(ctx, event.YajmanID, params.PanditID, params.EventID)
		if matchErr != nil {
			s.logger.Error("failed to create match record for express interest",
				"error", matchErr,
				"pandit_id", params.PanditID,
				"event_id", params.EventID,
			)
		}
	}


	// Re-fetch to get timestamps
	created, err := s.interestRepo.GetByID(ctx, interest.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch created interest: %w", err)
	}

	s.logger.Info("interest expressed",
		"interest_id", interest.ID,
		"pandit_id", params.PanditID,
		"event_id", params.EventID,
	)

	// Send notification to Yajman about interest received
	if s.notifSvc != nil {
		go func() {
			ceremonyName := strings.ReplaceAll(string(event.CeremonyType), "_", " ")
			var panditName = "A Pandit"
			if s.userRepo != nil {
				pandit, userErr := s.userRepo.GetByID(context.Background(), params.PanditID)
				if userErr == nil && pandit != nil {
					panditName = pandit.DisplayName()
				}
			}
			_, notifErr := s.notifSvc.CreateNotification(context.Background(), notifservice.CreateNotificationParams{
				UserID: event.YajmanID,
				Type:   notifdomain.NotifInterestReceived,
				Title:  "🙏 Pandit showed interest!",
				Body:   fmt.Sprintf("%s showed interest in your %s ceremony.", panditName, ceremonyName),
				Data: map[string]string{
					"event_id":    interest.EventID.String(),
					"interest_id": interest.ID.String(),
					"type":        "interest_received",
				},
			})
			if notifErr != nil {
				s.logger.Error("failed to send interest notification", "error", notifErr)
			}
		}()
	}

	return created, nil
}

// RespondToInterest allows a yajman to accept or reject an interest.
func (s *InterestService) RespondToInterest(ctx context.Context, interestID, yajmanID uuid.UUID, accept bool) (*domain.Interest, error) {
	if accept {
		return s.AcceptInterest(ctx, interestID, yajmanID)
	}

	interest, err := s.interestRepo.GetByID(ctx, interestID)
	if err != nil {
		return nil, fmt.Errorf("get interest: %w", err)
	}

	// Verify the event belongs to this yajman
	event, err := s.eventRepo.GetByID(ctx, interest.EventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	if event.YajmanID != yajmanID {
		return nil, domain.ErrNotEventOwner
	}

	// Prevent re-responding to an already-resolved interest (connected interests can still be rejected)
	if interest.Status != domain.InterestStatusPending && interest.Status != domain.InterestStatusConnected {
		return nil, domain.ErrInterestAlreadyResponded
	}

	if err := s.interestRepo.UpdateStatus(ctx, interestID, domain.InterestStatusRejected); err != nil {
		return nil, fmt.Errorf("update interest status: %w", err)
	}

	if s.matchSvc != nil {
		// Find the match by pandit+event and cancel it
		existingMatch, matchErr := s.matchSvc.FindByPanditAndEvent(ctx, interest.PanditID, interest.EventID)
		if matchErr != nil {
			s.logger.Warn("could not find match to cancel",
				"error", matchErr,
				"pandit_id", interest.PanditID,
				"event_id", interest.EventID,
			)
		} else {
			cancelErr := s.matchSvc.CancelMatch(ctx, existingMatch.ID, yajmanID)
			if cancelErr != nil {
				s.logger.Error("failed to cancel match on interest rejection",
					"error", cancelErr,
					"match_id", existingMatch.ID,
				)
			}
		}
	}

	s.logger.Info("interest rejected",
		"interest_id", interestID,
		"yajman_id", yajmanID,
	)

	// Re-fetch
	return s.interestRepo.GetByID(ctx, interestID)
}

// AcceptInterest transitions interest status from pending to connected.
func (s *InterestService) AcceptInterest(ctx context.Context, interestID, yajmanID uuid.UUID) (*domain.Interest, error) {
	interest, err := s.interestRepo.GetByID(ctx, interestID)
	if err != nil {
		return nil, fmt.Errorf("get interest: %w", err)
	}

	// Verify the event belongs to this yajman
	event, err := s.eventRepo.GetByID(ctx, interest.EventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	if event.YajmanID != yajmanID {
		return nil, domain.ErrNotEventOwner
	}

	if interest.Status != domain.InterestStatusPending {
		return nil, domain.ErrInterestAlreadyResponded
	}

	if err := s.interestRepo.UpdateStatus(ctx, interestID, domain.InterestStatusConnected); err != nil {
		return nil, fmt.Errorf("update interest status to connected: %w", err)
	}

	// Transition match from 'created' to 'matched' when yajman accepts interest
	if s.matchSvc != nil {
		existingMatch, matchErr := s.matchSvc.FindByPanditAndEvent(ctx, interest.PanditID, interest.EventID)
		if matchErr != nil {
			s.logger.Warn("could not find match to accept",
				"error", matchErr,
				"pandit_id", interest.PanditID,
				"event_id", interest.EventID,
			)
		} else {
			_, acceptErr := s.matchSvc.ConnectMatch(ctx, existingMatch.ID, yajmanID)
			if acceptErr != nil {
				s.logger.Error("failed to connect match on interest acceptance",
					"error", acceptErr,
					"match_id", existingMatch.ID,
				)
			}
		}
	}

	s.logger.Info("interest accepted (connected)",
		"interest_id", interestID,
		"yajman_id", yajmanID,
	)

	// Auto-create conversation workspace when interest is accepted (connected)
	if s.msgSvc != nil {
		go func() {
			bgCtx := context.Background()
			ceremonyName := strings.ReplaceAll(string(event.CeremonyType), "_", " ")
			var panditName = "A Pandit"
			if s.userRepo != nil {
				pandit, userErr := s.userRepo.GetByID(bgCtx, interest.PanditID)
				if userErr == nil && pandit != nil {
					panditName = pandit.DisplayName()
				}
			}

			conv, convErr := s.msgSvc.CreateConversation(bgCtx, interest.PanditID, messagingservice.CreateConversationParams{
				Type:           messagingdomain.ConvPanditYajman,
				ParticipantIDs: []uuid.UUID{event.YajmanID},
				Title:          fmt.Sprintf("%s – %s", panditName, ceremonyName),
			})
			var convIDStr string
			if convErr != nil {
				s.logger.Error("failed to auto-create conversation on interest acceptance",
					"error", convErr,
					"pandit_id", interest.PanditID,
					"yajman_id", event.YajmanID,
				)
			} else {
				convIDStr = conv.ID.String()
				// Post system intro message
				_, sysMsgErr := s.msgSvc.SendMessage(bgCtx, messagingservice.SendMessageParams{
					ConversationID: conv.ID,
					SenderID:       interest.PanditID,
					ContentType:    messagingdomain.ContentSystem,
					Content:        fmt.Sprintf("Your interest in the %s ceremony was accepted. You can now chat or call directly!", ceremonyName),
				})
				if sysMsgErr != nil {
					s.logger.Error("failed to post system message to interest conversation",
						"error", sysMsgErr,
						"conversation_id", conv.ID,
					)
				}
			}

			// Send notification to Pandit that they are connected
			if s.notifSvc != nil {
				yajmanName := "Yajman"
				if s.userRepo != nil {
					yajman, userErr := s.userRepo.GetByID(bgCtx, yajmanID)
					if userErr == nil && yajman != nil {
						yajmanName = yajman.DisplayName()
					}
				}

				notifData := map[string]string{
					"event_id":    interest.EventID.String(),
					"interest_id": interest.ID.String(),
					"type":        "interest_accepted",
				}
				if convIDStr != "" {
					notifData["conversation_id"] = convIDStr
				}

				_, notifErr := s.notifSvc.CreateNotification(bgCtx, notifservice.CreateNotificationParams{
					UserID: interest.PanditID,
					Type:   notifdomain.NotifInterestAccepted,
					Title:  "✅ You are connected!",
					Body:   fmt.Sprintf("You are now connected with %s for the %s ceremony. You can chat or call directly!", yajmanName, ceremonyName),
					Data:   notifData,
				})
				if notifErr != nil {
					s.logger.Error("failed to send interest connected notification to pandit",
						"error", notifErr,
						"pandit_id", interest.PanditID,
						"event_id", interest.EventID,
					)
				}
			}
		}()
	} else {
		// Send notification without conversation id
		if s.notifSvc != nil {
			go func() {
				bgCtx := context.Background()
				ceremonyName := strings.ReplaceAll(string(event.CeremonyType), "_", " ")
				yajmanName := "Yajman"
				if s.userRepo != nil {
					yajman, userErr := s.userRepo.GetByID(bgCtx, yajmanID)
					if userErr == nil && yajman != nil {
						yajmanName = yajman.DisplayName()
					}
				}

				_, notifErr := s.notifSvc.CreateNotification(bgCtx, notifservice.CreateNotificationParams{
					UserID: interest.PanditID,
					Type:   notifdomain.NotifInterestAccepted,
					Title:  "✅ You are connected!",
					Body:   fmt.Sprintf("You are now connected with %s for the %s ceremony. You can chat or call directly!", yajmanName, ceremonyName),
					Data: map[string]string{
						"event_id":    interest.EventID.String(),
						"interest_id": interest.ID.String(),
						"type":        "interest_accepted",
					},
				})
				if notifErr != nil {
					s.logger.Error("failed to send interest connected notification to pandit",
						"error", notifErr,
						"pandit_id", interest.PanditID,
						"event_id", interest.EventID,
					)
				}
			}()
		}
	}

	return s.interestRepo.GetByID(ctx, interestID)
}

// ConfirmBooking books a specific connected pandit, sets others to rejected, and sets the event status to Booked.
func (s *InterestService) ConfirmBooking(ctx context.Context, interestID, yajmanID uuid.UUID) (*domain.Interest, error) {
	interest, err := s.interestRepo.GetByID(ctx, interestID)
	if err != nil {
		return nil, fmt.Errorf("get interest: %w", err)
	}

	// Verify the event belongs to this yajman
	event, err := s.eventRepo.GetByID(ctx, interest.EventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	if event.YajmanID != yajmanID {
		return nil, domain.ErrNotEventOwner
	}

	// Interest must be connected to be booked
	if interest.Status != domain.InterestStatusConnected {
		return nil, domain.ErrInterestNotConnected
	}

	// Find matching match record to accept
	var matchID uuid.UUID
	if s.matchSvc != nil {
		existingMatch, matchErr := s.matchSvc.FindByPanditAndEvent(ctx, interest.PanditID, interest.EventID)
		if matchErr != nil {
			s.logger.Warn("could not find match to accept",
				"error", matchErr,
				"pandit_id", interest.PanditID,
				"event_id", interest.EventID,
			)
		} else {
			matchID = existingMatch.ID
		}
	}

	// Perform database status transitions atomically
	err = database.WithTx(ctx, s.pool, func(txCtx context.Context) error {
		// Update this interest status to accepted
		if err := s.interestRepo.UpdateStatus(txCtx, interestID, domain.InterestStatusAccepted); err != nil {
			return fmt.Errorf("update interest status to accepted: %w", err)
		}

		// Bulk reject all other active interests
		if err := s.interestRepo.BulkRejectByEventID(txCtx, interest.EventID, interest.PanditID); err != nil {
			return fmt.Errorf("bulk reject other interests: %w", err)
		}

		// Update event status to booked
		if err := s.eventRepo.UpdateStatus(txCtx, interest.EventID, eventdomain.EventStatusBooked); err != nil {
			return fmt.Errorf("update event status to booked: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Transition match and trigger notifications asynchronously using s.matchSvc.BookMatch
	if s.matchSvc != nil && matchID != uuid.Nil {
		_, acceptErr := s.matchSvc.BookMatch(ctx, matchID, yajmanID)
		if acceptErr != nil {
			s.logger.Error("failed to book match on booking confirmation",
				"error", acceptErr,
				"match_id", matchID,
			)
		}
	}

	s.logger.Info("booking confirmed",
		"interest_id", interestID,
		"yajman_id", yajmanID,
		"pandit_id", interest.PanditID,
		"event_id", interest.EventID,
	)

	return s.interestRepo.GetByID(ctx, interestID)
}

// WithdrawInterest allows a pandit to withdraw interest, marking themselves as unavailable.
func (s *InterestService) WithdrawInterest(ctx context.Context, interestID, panditID uuid.UUID) (*domain.Interest, error) {
	interest, err := s.interestRepo.GetByID(ctx, interestID)
	if err != nil {
		return nil, fmt.Errorf("get interest: %w", err)
	}

	// Only interest owner can withdraw
	if interest.PanditID != panditID {
		return nil, domain.ErrNotInterestOwner
	}

	// Must be pending or connected to withdraw
	if interest.Status != domain.InterestStatusPending && interest.Status != domain.InterestStatusConnected {
		return nil, domain.ErrInterestAlreadyResponded
	}

	// Find match to cancel
	var matchID uuid.UUID
	if s.matchSvc != nil {
		existingMatch, matchErr := s.matchSvc.FindByPanditAndEvent(ctx, interest.PanditID, interest.EventID)
		if matchErr != nil {
			s.logger.Warn("could not find match to cancel on withdraw",
				"error", matchErr,
				"pandit_id", interest.PanditID,
				"event_id", interest.EventID,
			)
		} else {
			matchID = existingMatch.ID
		}
	}

	err = database.WithTx(ctx, s.pool, func(txCtx context.Context) error {
		// Update interest status to withdrawn
		if err := s.interestRepo.UpdateStatus(txCtx, interestID, domain.InterestStatusWithdrawn); err != nil {
			return fmt.Errorf("update interest status to withdrawn: %w", err)
		}

		// Re-evaluate event status based on remaining active interests count
		if err := s.evaluateAndTransitionEventStatus(txCtx, interest.EventID); err != nil {
			return fmt.Errorf("evaluate and transition event status: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Cancel the match asynchronously if match service is available
	if s.matchSvc != nil && matchID != uuid.Nil {
		cancelErr := s.matchSvc.CancelMatch(ctx, matchID, panditID)
		if cancelErr != nil {
			s.logger.Error("failed to cancel match on interest withdrawal",
				"error", cancelErr,
				"match_id", matchID,
			)
		}
	}

	s.logger.Info("interest withdrawn",
		"interest_id", interestID,
		"pandit_id", panditID,
		"event_id", interest.EventID,
	)

	return s.interestRepo.GetByID(ctx, interestID)
}

// evaluateAndTransitionEventStatus counts active interests (pending/connected) and transitions the event status.
func (s *InterestService) evaluateAndTransitionEventStatus(ctx context.Context, eventID uuid.UUID) error {
	count, err := s.interestRepo.CountActiveByEventID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("count active interests: %w", err)
	}

	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("get event: %w", err)
	}

	// Only transition status if event is currently active (Pushed or Pending)
	if event.Status != eventdomain.EventStatusPushed && event.Status != eventdomain.EventStatusPending {
		return nil
	}

	var newStatus eventdomain.EventStatus
	if count >= 3 {
		newStatus = eventdomain.EventStatusPending
	} else {
		newStatus = eventdomain.EventStatusPushed
	}

	if event.Status != newStatus {
		if err := s.eventRepo.UpdateStatus(ctx, eventID, newStatus); err != nil {
			return fmt.Errorf("update event status: %w", err)
		}
		s.logger.Info("event status auto-transitioned",
			"event_id", eventID,
			"old_status", event.Status,
			"new_status", newStatus,
			"active_applicant_count", count,
		)
	}

	return nil
}

// ListEventInterests returns all interests for an event (for yajman to review).
func (s *InterestService) ListEventInterests(ctx context.Context, eventID, yajmanID uuid.UUID, cursor string, limit int) ([]*domain.InterestWithDetails, string, error) {
	// Verify event ownership
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, "", fmt.Errorf("get event: %w", err)
	}

	if event.YajmanID != yajmanID {
		return nil, "", domain.ErrNotEventOwner
	}

	interests, nextCursor, err := s.interestRepo.ListByEventID(ctx, eventID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list event interests: %w", err)
	}
	return interests, nextCursor, nil
}

// ListMyInterests returns all interests for the authenticated pandit.
func (s *InterestService) ListMyInterests(ctx context.Context, panditID uuid.UUID, cursor string, limit int) ([]*domain.InterestWithDetails, string, error) {
	interests, nextCursor, err := s.interestRepo.ListByPanditID(ctx, panditID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list my interests: %w", err)
	}
	return interests, nextCursor, nil
}

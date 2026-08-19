package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/medha/backend/internal/admin/assignment"
	"github.com/medha/backend/internal/platform/epoch"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	domain "github.com/medha/backend/internal/event/domain"
	interestdomain "github.com/medha/backend/internal/interest/domain"
	notifdomain "github.com/medha/backend/internal/notification/domain"
	notifservice "github.com/medha/backend/internal/notification/service"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// EventService handles event-related business logic.
type EventService struct {
	Pool             *pgxpool.Pool
	eventRepo        domain.EventRepository
	interestRepo     interestdomain.InterestRepository // optional — nil-safe
	userRepo         userdomain.UserRepository         // optional — for notification enrichment
	notifSvc         *notifservice.NotificationService // optional — nil-safe
	assignmentEngine *assignment.Engine                // optional — nil-safe
	nearbyRadiusKM   int
	logger           *slog.Logger
}

// NewEventService creates a new EventService.
func NewEventService(pool *pgxpool.Pool, eventRepo domain.EventRepository, logger *slog.Logger) *EventService {
	return &EventService{
		Pool:      pool,
		eventRepo: eventRepo,
		logger:    logger,
	}
}

// SetNotificationDependencies wires the optional notification service, user repository, and nearby radius for push notification triggers.
func (s *EventService) SetNotificationDependencies(notifSvc *notifservice.NotificationService, userRepo userdomain.UserRepository, radiusKM int) {
	s.notifSvc = notifSvc
	s.userRepo = userRepo
	s.nearbyRadiusKM = radiusKM
}

// SetInterestRepository wires the optional interest repository for cascading cancellation updates.
func (s *EventService) SetInterestRepository(interestRepo interestdomain.InterestRepository) {
	s.interestRepo = interestRepo
}

// SetAssignmentEngine wires the optional assignment engine for auto-assigning Pandits on event creation.
func (s *EventService) SetAssignmentEngine(engine *assignment.Engine) {
	s.assignmentEngine = engine
}

// CreateEventParams contains the parameters for creating an event.
type CreateEventParams struct {
	YajmanID                  uuid.UUID
	CeremonyType              string
	CustomCeremonyName        string
	CustomCeremonyDescription string
	EventDate                 time.Time
	Latitude                  float64
	Longitude                 float64
	Address                   string
	Description               string
}

// CreateEvent creates a new ceremony event.
func (s *EventService) CreateEvent(ctx context.Context, params CreateEventParams) (*domain.Event, error) {
	params.CeremonyType = strings.TrimSpace(params.CeremonyType)
	params.CustomCeremonyName = strings.TrimSpace(params.CustomCeremonyName)
	params.CustomCeremonyDescription = strings.TrimSpace(params.CustomCeremonyDescription)

	// Validate ceremony type
	if !domain.IsValidCeremonyType(params.CeremonyType) {
		return nil, domain.ErrInvalidCeremonyType
	}
	if err := validateCustomCeremony(params.CeremonyType, params.CustomCeremonyName, params.CustomCeremonyDescription); err != nil {
		return nil, err
	}

	// Validate event date is not in the past
	if !params.EventDate.IsZero() && params.EventDate.UTC().Before(time.Now().UTC().Truncate(24*time.Hour)) {
		return nil, domain.ErrEventDateInPast
	}

	event := &domain.Event{
		ID:                        uuid.New(),
		YajmanID:                  params.YajmanID,
		CeremonyType:              domain.CeremonyType(params.CeremonyType),
		CustomCeremonyName:        params.CustomCeremonyName,
		CustomCeremonyDescription: params.CustomCeremonyDescription,
		EventDate:                 epoch.FromTime(params.EventDate),
		Address:                   params.Address,
		Description:               params.Description,
		Status:                    domain.EventStatusCreated,
	}

	if params.Latitude != 0 || params.Longitude != 0 {
		event.Location = &authdomain.GeoPoint{
			Latitude:  params.Latitude,
			Longitude: params.Longitude,
		}
	}

	if err := s.eventRepo.Create(ctx, event); err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}

	// Re-fetch to get server-generated fields (timestamps)
	created, err := s.eventRepo.GetByID(ctx, event.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch created event: %w", err)
	}

	s.logger.Info("event created",
		"event_id", event.ID,
		"yajman_id", params.YajmanID,
		"ceremony_type", params.CeremonyType,
	)

	// Send notification to the Yajman
	if s.notifSvc != nil {
		_, _ = s.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
			UserID: params.YajmanID,
			Type:   notifdomain.NotifNewEventCreated,
			Title:  "New Event Created",
			Body:   "Your event has been successfully created and is awaiting review.",
			Data: map[string]string{
				"event_id": event.ID.String(),
				"type":     "new_event_created",
			},
		})
	}

	// Send notification to all Admin/Owners
	if s.notifSvc != nil && s.Pool != nil {
		go func() {
			rows, err := s.Pool.Query(context.Background(), "SELECT id FROM admin_users")
			if err != nil {
				s.logger.Error("failed to query admin users for event creation notification", "error", err)
				return
			}
			defer rows.Close()

			for rows.Next() {
				var adminID uuid.UUID
				if err := rows.Scan(&adminID); err == nil {
					_, _ = s.notifSvc.CreateNotification(context.Background(), notifservice.CreateNotificationParams{
						UserID: adminID,
						Type:   notifdomain.NotifNewEventCreated,
						Title:  "New Event Created",
						Body:   "A Yajman has created a new event. Tap to review.",
						Data: map[string]string{
							"event_id": event.ID.String(),
							"type":     "new_event_created",
						},
					})
				}
			}
		}()
	}

	// ── FCM Flow 2a: Nearby Event Alert (DISABLED) ──
	// Disabled: Yajman event creation should not directly notify Pandits since the admin/owner must assign the event first.
	/*
	if s.notifSvc != nil && s.userRepo != nil && event.Location != nil && s.nearbyRadiusKM > 0 {
		go func() {
			yajmanName := "Someone"
			if yajman, err := s.userRepo.GetByID(context.Background(), event.YajmanID); err == nil && yajman != nil {
				yajmanName = strings.TrimSpace(yajman.FirstName + " " + yajman.LastName)
				if yajmanName == "" {
					yajmanName = "Someone"
				}
			}

			ceremonyName := strings.ReplaceAll(string(event.CeremonyType), "_", " ")
			if event.CeremonyType == domain.CeremonyCustom && event.CustomCeremonyName != "" {
				ceremonyName = event.CustomCeremonyName
			}
			pandits, err := s.eventRepo.FindNearbyPandits(context.Background(), event.Location.Latitude, event.Location.Longitude, s.nearbyRadiusKM)
			if err != nil {
				s.logger.Error("failed to find nearby pandits for notification", "error", err, "event_id", event.ID)
				return
			}

			if len(pandits) == 0 {
				s.logger.Debug("no nearby pandits found for notification", "event_id", event.ID, "radius_km", s.nearbyRadiusKM)
				return
			}

			for _, p := range pandits {
				_, notifErr := s.notifSvc.CreateNotification(context.Background(), notifservice.CreateNotificationParams{
					UserID: p.UserID,
					Type:   notifdomain.NotifEventNearby,
					Title:  "🔔 New Event Nearby!",
					Body:   fmt.Sprintf("%s needs a Pandit for %s (%.1f km away)", yajmanName, ceremonyName, p.DistanceKM),
					Data: map[string]string{
						"event_id": event.ID.String(),
						"type":     "event_nearby",
					},
				})
				if notifErr != nil {
					s.logger.Error("failed to send nearby alert to pandit", "error", notifErr, "pandit_id", p.UserID)
				}
			}
		}()
	}
	*/

	// ── Assignment Engine: Auto-assign Pandits (DISABLED) ──
	// Disabled: Auto-assignment is disabled on creation. Admin assigns/pushes events manually.
	/*
	if s.assignmentEngine != nil && event.Location != nil {
		go s.assignmentEngine.ProcessEvent(context.Background(), created)
	}
	*/

	return created, nil
}

// GetByID retrieves an event by ID.
func (s *EventService) GetByID(ctx context.Context, eventID uuid.UUID) (*domain.Event, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}
	return event, nil
}

// UpdateEventParams contains the parameters for updating an event.
type UpdateEventParams struct {
	EventID                   uuid.UUID
	YajmanID                  uuid.UUID // for ownership check
	CeremonyType              *string
	CustomCeremonyName        *string
	CustomCeremonyDescription *string
	EventDate                 *time.Time
	Latitude                  *float64
	Longitude                 *float64
	Address                   *string
	Description               *string
}

// UpdateEvent updates an existing event (owner-only).
func (s *EventService) UpdateEvent(ctx context.Context, params UpdateEventParams) (*domain.Event, error) {
	event, err := s.eventRepo.GetByID(ctx, params.EventID)
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	// Ownership check
	if event.YajmanID != params.YajmanID {
		return nil, domain.ErrEventNotOwned
	}

	// Validate ceremony type if provided
	if params.CeremonyType != nil && *params.CeremonyType != "" {
		trimmed := strings.TrimSpace(*params.CeremonyType)
		if !domain.IsValidCeremonyType(trimmed) {
			return nil, domain.ErrInvalidCeremonyType
		}
		event.CeremonyType = domain.CeremonyType(trimmed)
	}
	if params.CustomCeremonyName != nil {
		event.CustomCeremonyName = strings.TrimSpace(*params.CustomCeremonyName)
	}
	if params.CustomCeremonyDescription != nil {
		event.CustomCeremonyDescription = strings.TrimSpace(*params.CustomCeremonyDescription)
	}
	if event.CeremonyType == domain.CeremonyCustom {
		if err := validateCustomCeremony(event.CeremonyType.String(), event.CustomCeremonyName, event.CustomCeremonyDescription); err != nil {
			return nil, err
		}
	} else {
		event.CustomCeremonyName = ""
		event.CustomCeremonyDescription = ""
	}

	if params.EventDate != nil {
		// Validate updated date is not in the past
		if params.EventDate.UTC().Before(time.Now().UTC().Truncate(24 * time.Hour)) {
			return nil, domain.ErrEventDateInPast
		}
		event.EventDate = epoch.FromTime(*params.EventDate)
	}

	if params.Latitude != nil || params.Longitude != nil {
		var lat, lng float64
		if event.Location != nil {
			lat = event.Location.Latitude
			lng = event.Location.Longitude
		}
		if params.Latitude != nil {
			lat = *params.Latitude
		}
		if params.Longitude != nil {
			lng = *params.Longitude
		}
		event.Location = &authdomain.GeoPoint{
			Latitude:  lat,
			Longitude: lng,
		}
	}

	if params.Address != nil && *params.Address != "" {
		event.Address = *params.Address
	}

	if params.Description != nil {
		event.Description = *params.Description
	}

	if err := s.eventRepo.Update(ctx, event); err != nil {
		return nil, fmt.Errorf("update event: %w", err)
	}

	s.logger.Info("event updated",
		"event_id", event.ID,
		"yajman_id", params.YajmanID,
	)

	return event, nil
}

// CancelEvent soft-deletes an event (owner-only).
func (s *EventService) CancelEvent(ctx context.Context, eventID, yajmanID uuid.UUID) error {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("get event: %w", err)
	}

	// Ownership check
	if event.YajmanID != yajmanID {
		return domain.ErrEventNotOwned
	}

	if err := s.eventRepo.SoftDelete(ctx, eventID); err != nil {
		return fmt.Errorf("cancel event: %w", err)
	}

	if s.interestRepo != nil {
		if err := s.interestRepo.BulkCancelByEventID(ctx, eventID); err != nil {
			s.logger.Error("failed to bulk cancel interests on event cancellation", "error", err, "event_id", eventID)
		}
	}

	s.logger.Info("event cancelled",
		"event_id", eventID,
		"yajman_id", yajmanID,
	)

	return nil
}

// ListMyEvents returns events for the authenticated yajman.
func (s *EventService) ListMyEvents(ctx context.Context, yajmanID uuid.UUID, cursor string, limit int) ([]*domain.Event, string, error) {
	events, nextCursor, err := s.eventRepo.ListByYajmanID(ctx, yajmanID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list my events: %w", err)
	}
	return events, nextCursor, nil
}

// ListNearbyEvents returns active events near a point.
// If userID is provided, distance is calculated from the user's stored home location.
func (s *EventService) ListNearbyEvents(ctx context.Context, lat, lng float64, radiusKM int, userID *uuid.UUID, cursor string, limit int) ([]*domain.EventWithDistance, string, error) {
	if radiusKM <= 0 {
		radiusKM = 25
	}
	if limit <= 0 {
		limit = 20
	}

	var refLat, refLng *float64
	if userID != nil && s.userRepo != nil {
		if user, err := s.userRepo.GetByID(ctx, *userID); err == nil && user != nil && user.Location != nil {
			refLat = &user.Location.Latitude
			refLng = &user.Location.Longitude
		}
	}

	events, nextCursor, err := s.eventRepo.ListNearby(ctx, lat, lng, radiusKM, refLat, refLng, userID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list nearby events: %w", err)
	}
	return events, nextCursor, nil
}

// ListInBoundingBox returns active events within a map area.
func (s *EventService) ListInBoundingBox(ctx context.Context, minLat, maxLat, minLng, maxLng float64, userID *uuid.UUID, cursor string, limit int) ([]*domain.EventWithDistance, string, error) {
	if limit <= 0 {
		limit = 100
	}

	var refLat, refLng *float64
	if userID != nil && s.userRepo != nil {
		if user, err := s.userRepo.GetByID(ctx, *userID); err == nil && user != nil && user.Location != nil {
			refLat = &user.Location.Latitude
			refLng = &user.Location.Longitude
		}
	}

	events, nextCursor, err := s.eventRepo.ListInBoundingBox(ctx, minLat, maxLat, minLng, maxLng, refLat, refLng, userID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list events in box: %w", err)
	}
	return events, nextCursor, nil
}

// ListCeremonies returns all entries in the ceremony catalog.
func (s *EventService) ListCeremonies(ctx context.Context) ([]*domain.Ceremony, error) {
	ceremonies, err := s.eventRepo.ListCeremonies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ceremonies: %w", err)
	}
	return withCustomCeremony(ceremonies), nil
}

// SearchCeremonies returns ceremonies matching the search query (fuzzy ILIKE).
func (s *EventService) SearchCeremonies(ctx context.Context, query string) ([]*domain.Ceremony, error) {
	ceremonies, err := s.eventRepo.SearchCeremonies(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search ceremonies: %w", err)
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if len(ceremonies) == 0 || query == "" || strings.Contains("custom ceremony ritual pooja puja other", query) || strings.Contains(query, "custom") || strings.Contains(query, "other") {
		return withCustomCeremony(ceremonies), nil
	}
	return ceremonies, nil
}

func validateCustomCeremony(ceremonyType, name, description string) error {
	if ceremonyType != domain.CeremonyCustom.String() {
		return nil
	}
	if len(name) < 2 || len(name) > 120 {
		return domain.ErrInvalidCustomCeremony
	}
	if description == "" || len(description) > 1000 {
		return domain.ErrInvalidCustomCeremony
	}
	return nil
}

func withCustomCeremony(ceremonies []*domain.Ceremony) []*domain.Ceremony {
	for _, c := range ceremonies {
		if c.Slug == domain.CeremonyCustom.String() {
			return ceremonies
		}
	}
	custom := &domain.Ceremony{
		ID:           uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Slug:         domain.CeremonyCustom.String(),
		DisplayName:  "Custom Ceremony",
		Category:     "custom",
		Description:  "Create a user-defined ceremony or ritual, such as Gudli Pooja, with custom vidhi and samagri details.",
		DisplayOrder: 9999,
		IsActive:     true,
		CreatedAt:    epoch.Now(),
	}
	return append(ceremonies, custom)
}

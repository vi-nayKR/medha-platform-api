package domain

import (
	"context"

	"github.com/google/uuid"
)

// DeviceToken represents a push notification token (FCM or APNs).
type DeviceToken struct {
	Token    string
	Platform string // "android" | "ios"
	IsActive bool
}

// NotificationRepository defines the port for notification data access.
type NotificationRepository interface {
	// ... (Create, GetByID, ListByUserID, MarkRead, MarkAllRead, CountUnread remain the same)
	Create(ctx context.Context, notification *Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*Notification, error)
	ListByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*Notification, string, error)
	MarkRead(ctx context.Context, id uuid.UUID) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
	CountUnread(ctx context.Context, userID uuid.UUID) (int, error)

	// SaveDeviceToken stores or updates a device token (FCM/APNs) for a user.
	SaveDeviceToken(ctx context.Context, userID uuid.UUID, token, deviceID, platform string) error

	// GetActiveDeviceTokens returns all active tokens for a user.
	GetActiveDeviceTokens(ctx context.Context, userID uuid.UUID) ([]DeviceToken, error)

	// MarkDeviceTokenInactive marks a token as inactive (e.g. on logout or failure).
	MarkDeviceTokenInactive(ctx context.Context, token string) error

	// --- Risk notification deduplication ---

	// SaveNotificationLog records that a risk-tier notification was sent for an event.
	SaveNotificationLog(ctx context.Context, eventID uuid.UUID, tier string) error

	// HasNotificationLog checks if a risk-tier notification was already sent for an event.
	HasNotificationLog(ctx context.Context, eventID uuid.UUID, tier string) (bool, error)

	// GetEventsAtRisk returns events with zero interests whose event_date is within daysThreshold days.
	GetEventsAtRisk(ctx context.Context, daysThreshold int) ([]RiskEvent, error)
}

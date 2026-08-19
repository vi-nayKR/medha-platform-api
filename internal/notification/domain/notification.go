package domain

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotifEventNearby         NotificationType = "event_nearby"
	NotifInterestReceived    NotificationType = "interest_received"
	NotifMatchConfirmed      NotificationType = "match_confirmed"
	NotifSocialLike          NotificationType = "social_like"
	NotifEventExpiring       NotificationType = "event_expiring"
	NotifBadgeEarned         NotificationType = "badge_earned"
	NotifMatchCompleted      NotificationType = "match_completed"
	NotifInterestAccepted    NotificationType = "interest_accepted"
	NotifConnectionRequest   NotificationType = "connection_request"
	NotifConnectionConfirmed NotificationType = "connection_confirmed"
	NotifChatMessage         NotificationType = "chat_message"
	NotifBookingConfirmed    NotificationType = "booking_confirmed"
	NotifTimelineRisk        NotificationType = "timeline_risk"
	NotifNewEventCreated     NotificationType = "new_event_created"
	NotifEventPushed         NotificationType = "event_pushed"
	NotifNewEventAvailable   NotificationType = "new_event_available"
	NotifPanditInterest      NotificationType = "pandit_interest"
)

func (n NotificationType) String() string { return string(n) }

// NotificationPriority returns the priority level for a notification type.
func (n NotificationType) Priority() string {
	switch n {
	case NotifMatchConfirmed, NotifBookingConfirmed:
		return "critical"
	case NotifInterestReceived, NotifEventExpiring, NotifTimelineRisk, NotifInterestAccepted, NotifNewEventCreated, NotifEventPushed, NotifNewEventAvailable, NotifPanditInterest:
		return "high"
	case NotifEventNearby, NotifBadgeEarned, NotifMatchCompleted, NotifConnectionRequest, NotifConnectionConfirmed:
		return "medium"
	case NotifSocialLike, NotifChatMessage:
		return "silent"
	default:
		return "medium"
	}
}

// Notification represents a notification to a user.
type Notification struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Type        NotificationType
	Title       string
	Body        string
	Data        json.RawMessage // JSONB for deep-link payload
	Read        bool
	DeliveredAt *int64
	CreatedAt   int64
}

// RiskEvent represents an event at risk of having no pandit interest.
type RiskEvent struct {
	EventID       uuid.UUID
	YajmanID      uuid.UUID
	CeremonyType  string
	EventDate     int64
	DaysUntil     int
	InterestCount int
}

// Sentinel errors for notification operations.
var (
	ErrNotificationNotFound  = errors.New("notification not found")
	ErrInvalidPlatform       = errors.New("device platform must be android or ios")
	ErrDeviceTokenRequired   = errors.New("device token is required")
)

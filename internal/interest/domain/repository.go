package domain

import (
	"context"

	"github.com/google/uuid"
)

// InterestRepository defines the port for interest data access.
type InterestRepository interface {
	// Create inserts a new interest.
	// Returns ErrInterestAlreadyExists if pandit already expressed interest in this event.
	Create(ctx context.Context, interest *Interest) error

	// GetByID returns an interest by its ID.
	GetByID(ctx context.Context, id uuid.UUID) (*Interest, error)

	// UpdateStatus updates the status of an interest (accept/reject).
	UpdateStatus(ctx context.Context, id uuid.UUID, status InterestStatus) error

	// ListByEventID returns all interests for a given event, with pandit details.
	ListByEventID(ctx context.Context, eventID uuid.UUID, cursor string, limit int) ([]*InterestWithDetails, string, error)

	// ListByPanditID returns all interests for a given pandit, with event details.
	ListByPanditID(ctx context.Context, panditID uuid.UUID, cursor string, limit int) ([]*InterestWithDetails, string, error)

	// GetByPanditAndEvent returns the interest for a specific pandit+event pair.
	GetByPanditAndEvent(ctx context.Context, panditID, eventID uuid.UUID) (*Interest, error)

	// CountByEventID returns the number of pandits who have expressed interest in an event.
	CountByEventID(ctx context.Context, eventID uuid.UUID) (int, error)

	// BulkRejectByEventID updates all pending/connected interests for an event to rejected, excluding a specific pandit.
	BulkRejectByEventID(ctx context.Context, eventID uuid.UUID, excludePanditID uuid.UUID) error

	// BulkCancelByEventID updates all interests for an event to event_cancelled.
	BulkCancelByEventID(ctx context.Context, eventID uuid.UUID) error

	// BulkCompleteByEventID updates the accepted interest for an event to completed.
	BulkCompleteByEventID(ctx context.Context, eventID uuid.UUID) error

	// CountActiveByEventID returns the count of interests in pending or connected status for an event.
	CountActiveByEventID(ctx context.Context, eventID uuid.UUID) (int, error)
}

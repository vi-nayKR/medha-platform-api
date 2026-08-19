package domain

import (
	"context"

	"github.com/google/uuid"
)

// MatchRepository defines the port for match data access.
type MatchRepository interface {
	// Create inserts a new match.
	// Returns ErrMatchAlreadyExists if a match already exists for this pandit+event.
	Create(ctx context.Context, match *Match) error

	// GetByID returns a match by ID.
	GetByID(ctx context.Context, id uuid.UUID) (*Match, error)

	// GetByIDWithDetails returns a match with joined user/event details.
	GetByIDWithDetails(ctx context.Context, id uuid.UUID) (*MatchWithDetails, error)

	// UpdateStatus updates the status of a match.
	UpdateStatus(ctx context.Context, id uuid.UUID, status MatchStatus) error

	// ListByUserID returns all matches for a user (yajman or pandit), with details.
	ListByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*MatchWithDetails, string, error)

	// ListByEventID returns all matches for an event.
	ListByEventID(ctx context.Context, eventID uuid.UUID) ([]*MatchWithDetails, error)

	// CountByEventID returns the number of active matches for an event.
	CountByEventID(ctx context.Context, eventID uuid.UUID) (int, error)

	// GetByPanditAndEvent returns the match for a specific pandit+event pair, if it exists.
	GetByPanditAndEvent(ctx context.Context, panditID, eventID uuid.UUID) (*Match, error)
}

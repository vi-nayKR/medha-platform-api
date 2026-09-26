package domain

import (
	"context"

	"github.com/google/uuid"
)

// NearbyPandit represents a pandit found near an event location.
type NearbyPandit struct {
	UserID     uuid.UUID
	FirstName  string
	LastName   string
	DistanceKM float64
}

// EventRepository defines the port for event data access.
// Implementations must always filter by deleted_at IS NULL.
type EventRepository interface {
	// Create inserts a new event.
	Create(ctx context.Context, event *Event) error

	// GetByID returns a non-deleted event by primary key.
	// Returns ErrEventNotFound if no event exists.
	GetByID(ctx context.Context, id uuid.UUID) (*Event, error)

	// LockForBooking returns a non-deleted event while holding its row lock for the transaction.
	LockForBooking(ctx context.Context, id uuid.UUID) (*Event, error)

	// Update modifies an existing non-deleted event.
	// Returns ErrEventNotFound if event doesn't exist or is deleted.
	Update(ctx context.Context, event *Event) error

	// UpdateStatus updates the status of an event.
	UpdateStatus(ctx context.Context, id uuid.UUID, status EventStatus) error

	// SoftDelete marks an event as deleted by setting deleted_at.
	// Returns ErrEventNotFound if event doesn't exist or is already deleted.
	SoftDelete(ctx context.Context, id uuid.UUID) error

	// ListByYajmanID returns all non-deleted events for a given yajman,
	// ordered by event_date ascending.
	ListByYajmanID(ctx context.Context, yajmanID uuid.UUID, cursor string, limit int) ([]*Event, string, error)

	// ListNearby returns active events within a given radius of a point,
	// sorted by ascending distance. Uses ST_DWithin on events.location.
	// If refLat/refLng are provided, distance is calculated from that point instead of the search center.
	ListNearby(ctx context.Context, lat, lng float64, radiusKM int, refLat, refLng *float64, excludePanditID *uuid.UUID, cursor string, limit int) ([]*EventWithDistance, string, error)

	// ListInBoundingBox returns active events within a rectangular geographic area.
	// If refLat/refLng are provided, distance is calculated from that point.
	ListInBoundingBox(ctx context.Context, minLat, maxLat, minLng, maxLng float64, refLat, refLng *float64, excludePanditID *uuid.UUID, cursor string, limit int) ([]*EventWithDistance, string, error)

	// ListCeremonies returns all entries in the ceremony catalog.
	ListCeremonies(ctx context.Context) ([]*Ceremony, error)

	// SearchCeremonies returns ceremonies matching the query (case-insensitive ILIKE on slug, display_name, category, description).
	SearchCeremonies(ctx context.Context, query string) ([]*Ceremony, error)

	// FindNearbyPandits returns pandits (role='pandit') within radiusKM of the given coordinates.
	// Uses ST_DWithin on users.location with PostGIS.
	FindNearbyPandits(ctx context.Context, lat, lng float64, radiusKM int) ([]NearbyPandit, error)
}

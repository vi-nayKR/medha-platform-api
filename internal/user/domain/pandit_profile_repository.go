package domain

import (
	"context"

	"github.com/google/uuid"
)

// PanditProfileRepository defines the port for pandit profile data access.
// Implementations must always filter by deleted_at IS NULL on both
// pandit_profiles and users tables when joining.
type PanditProfileRepository interface {
	// Create inserts a new pandit profile.
	// Returns ErrPanditProfileAlreadyExists if user_id already has a profile.
	Create(ctx context.Context, profile *PanditProfile) error

	// GetByUserID returns a non-deleted pandit profile by user ID.
	// Returns ErrPanditProfileNotFound if no profile exists.
	GetByUserID(ctx context.Context, userID uuid.UUID) (*PanditProfile, error)

	// Update modifies an existing non-deleted pandit profile.
	// Returns ErrPanditProfileNotFound if profile doesn't exist or is deleted.
	Update(ctx context.Context, profile *PanditProfile) error

	// SoftDelete marks a pandit profile as deleted by setting deleted_at.
	// Returns ErrPanditProfileNotFound if profile doesn't exist or is already deleted.
	SoftDelete(ctx context.Context, userID uuid.UUID) error

	// ListNearby returns pandit profiles within a given radius of a point,
	// sorted by ascending distance. Uses ST_DWithin on users.location.
	//
	// Parameters:
	//   - lat, lng: center point coordinates (latitude, longitude)
	//   - radiusKM: search radius in kilometers
	//   - filters: optional ceremony type, parampara, and availability filters
	//   - cursor: opaque pagination cursor (empty for first page)
	//   - limit: maximum number of results to return
	//   - viewerID: optional UUID of the requesting Yajman; when set, each result
	//               includes connection_status and conversation_id relative to the viewer.
	//
	// Returns:
	//   - profiles: list of pandit profiles with user data, distance, and connection context
	//   - nextCursor: cursor for the next page (empty if no more results)
	//   - error: any database error
	ListNearby(ctx context.Context, lat, lng float64, radiusKM int, filters PanditFilters, cursor string, limit int, viewerID *uuid.UUID) ([]*PanditProfileWithDistance, string, error)

	// ReplaceServiceCities transactionally replaces a pandit's home and additional service cities.
	ReplaceServiceCities(ctx context.Context, userID uuid.UUID, homeCityID uuid.UUID, additionalCityIDs []uuid.UUID) error

	// GetServiceCities retrieves the list of service cities with name and distance details.
	GetServiceCities(ctx context.Context, userID uuid.UUID) ([]ServiceCityDetail, error)

	// VerifyCeremonySlugExists checks if the ceremony slug exists in the festival logos database.
	VerifyCeremonySlugExists(ctx context.Context, slug string) (bool, error)
}

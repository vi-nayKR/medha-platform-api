package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
)

// User is a type alias re-exporting the auth domain User entity
// for use within the user bounded context.
type User = authdomain.User

// UserRole and GeoPoint re-exported for convenience.
type UserRole = authdomain.UserRole
type GeoPoint = authdomain.GeoPoint

// Sentinel errors for user operations.
var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrUsernameTaken     = errors.New("username already taken")
	ErrInvalidUserRole   = errors.New("invalid user role")
	ErrInvalidLocation   = errors.New("invalid location coordinates")
)

// UserRepository defines the port for user data access.
// Implementations must always filter by deleted_at IS NULL.
type UserRepository interface {
	// Create inserts a new user. Returns ErrUserAlreadyExists if provider uid conflicts.
	Create(ctx context.Context, user *User) error

	// GetByID returns a non-deleted user by primary key.
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)

	// FindByPhone returns a non-deleted user by their phone number (E.164 format).
	FindByPhone(ctx context.Context, phone string) (*User, error)

	// Update modifies an existing non-deleted user.
	Update(ctx context.Context, user *User) error

	// SoftDelete marks a user as deleted by setting deleted_at.
	SoftDelete(ctx context.Context, id uuid.UUID) error

	// Delete permanently deletes a user.
	Delete(ctx context.Context, id uuid.UUID) error

	// --- V2 additions ---

	// GetByPhone returns a non-deleted user by their E.164 phone (alias for FindByPhone).
	GetByPhone(ctx context.Context, phone string) (*User, error)

	// UpdatePhoneVerified sets phone_verified for a user.
	UpdatePhoneVerified(ctx context.Context, userID uuid.UUID, verified bool) error

	// UpdateProfileComplete sets profile_complete for a user.
	UpdateProfileComplete(ctx context.Context, userID uuid.UUID, complete bool) error

	// SetupProfile updates first_name, last_name, role, username, email, and marks profile_complete=true.
	SetupProfile(ctx context.Context, userID uuid.UUID, firstName, lastName, role, username, email string) error

	// CheckUsernameExists natively queries the database to see if a username is taken.
	CheckUsernameExists(ctx context.Context, username string) (bool, error)

	// UpdateLocation stores the user's geographic coordinates and geocoded address components.
	UpdateLocation(ctx context.Context, userID uuid.UUID, latitude, longitude float64, city, state, pincode string) error

	// ListAll returns all non-deleted users ordered by role ASC, first_name ASC.
	// Used exclusively by the dev-users endpoint (non-production only).
	ListAll(ctx context.Context) ([]*User, error)

	// CanViewContactInfo checks if a viewer is authorized to view a pandit's contact number.
	CanViewContactInfo(ctx context.Context, viewerID, panditID uuid.UUID) (bool, error)

	// UpdatePremium updates is_premium, premium_until, and premium_badge for a user.
	UpdatePremium(ctx context.Context, userID uuid.UUID, isPremium bool, premiumUntil *int64, premiumBadge string) error

	// GetByUsername returns a non-deleted user by username (case-insensitive).
	GetByUsername(ctx context.Context, username string) (*User, error)

	// GetBadgeTier returns a pandit's current badge tier from pandit_badges table, default 'pandit_ji'.
	GetBadgeTier(ctx context.Context, userID uuid.UUID) (string, error)
}

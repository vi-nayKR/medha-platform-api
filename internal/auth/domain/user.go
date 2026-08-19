package domain

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// User represents a registered user in the Medha platform.
type User struct {
	ID              uuid.UUID
	AuthProvider    string // "google" | "apple"
	ProviderUID     string // Stable user ID from the OAuth provider
	Role            UserRole
	FirstName       string
	LastName        string
	Username        string
	Email           string
	Phone           string // E.164 phone number
	PhoneVerified   bool
	ProfilePhotoURL string
	Location        *GeoPoint
	CreatedAt       int64
	UpdatedAt       int64
	DeletedAt       *int64 // nil = active, non-nil = soft-deleted

	// V2 fields
	GoogleID        string // google_id column — set on Google sign-in
	AppleID         string // apple_id column — set on Apple sign-in
	ProfileComplete bool   // profile_complete column

	// Direct connection & location caching fields
	City    string
	State   string
	Pincode string

	// Premium features & monetization fields
	IsPremium       bool
	PremiumUntil    *int64
	PremiumBadge    string
	PremiumFeatures json.RawMessage

	// CanAuthenticate is false for platform system accounts (auth_provider =
	// "system"), which must never receive a login/refresh token regardless
	// of which provider or lookup path resolves to them.
	CanAuthenticate bool
}

// DisplayName returns "FirstName LastName" as a computed field.
func (u *User) DisplayName() string {
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// IsDeleted returns true if the user has been soft-deleted.
func (u *User) IsDeleted() bool {
	return u.DeletedAt != nil
}

// IsPandit returns true if the user has the Pandit role.
func (u *User) IsPandit() bool {
	return u.Role == RolePandit
}

// IsYajman returns true if the user has the Yajman role.
func (u *User) IsYajman() bool {
	return u.Role == RoleYajman
}

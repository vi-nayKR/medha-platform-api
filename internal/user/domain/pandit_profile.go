package domain

import (
	"errors"

	"github.com/google/uuid"
)

// Sentinel errors for pandit profile operations.
var (
	ErrPanditProfileNotFound      = errors.New("pandit profile not found")
	ErrPanditProfileAlreadyExists = errors.New("pandit profile already exists for this user")
	ErrTooManyServiceCities       = errors.New("too many service cities allowed")
	ErrDuplicateServiceCity       = errors.New("duplicate service city")
	ErrCityTooFar                 = errors.New("city is too far from home city")
)

// AvailabilityStatus represents whether a Pandit is currently available for ceremonies.
type AvailabilityStatus string

const (
	AvailabilityAvailable   AvailabilityStatus = "available"
	AvailabilityUnavailable AvailabilityStatus = "unavailable"
)

// IsValid returns true if the availability status is a recognized value.
func (a AvailabilityStatus) IsValid() bool {
	return a == AvailabilityAvailable || a == AvailabilityUnavailable
}

// String returns the string representation of the availability status.
func (a AvailabilityStatus) String() string {
	return string(a)
}

// Well-known ceremony types — not enforced at DB level (stored as text[]),
// validated at handler/service level for user input.
const (
	CeremonyShraadh      = "shraadh"
	CeremonyGrihapravesh = "grihapravesh"
	CeremonyVivah        = "vivah"
	CeremonySatyanarayan = "satyanarayan"
	CeremonyMundan       = "mundan"
	CeremonyAntimSanskar = "antim_sanskar"
	CeremonyVastuShanti  = "vastu_shanti"
	CeremonyNaamkaran    = "naamkaran"
	CeremonyUpanayana    = "upanayana"
)

// validCeremonyTypes is the canonical list of recognized ceremony types.
// Unexported to prevent mutation; access via ValidCeremonyTypes().
var validCeremonyTypes = [...]string{
	CeremonyShraadh,
	CeremonyGrihapravesh,
	CeremonyVivah,
	CeremonySatyanarayan,
	CeremonyMundan,
	CeremonyAntimSanskar,
	CeremonyVastuShanti,
	CeremonyNaamkaran,
	CeremonyUpanayana,
}

// ValidCeremonyTypes returns a copy of the canonical ceremony type list.
func ValidCeremonyTypes() []string {
	result := make([]string, len(validCeremonyTypes))
	copy(result, validCeremonyTypes[:])
	return result
}

// IsValidCeremonyType checks if a ceremony type string is in the known list.
func IsValidCeremonyType(ct string) bool {
	for _, valid := range validCeremonyTypes {
		if ct == valid {
			return true
		}
	}
	return false
}

// PanditProfile represents a Pandit's professional profile linked to a User.
// One-to-one relationship: users.id ← pandit_profiles.user_id (UNIQUE).
// Location is NOT stored here — it lives in users.location (shared by both roles).
type PanditProfile struct {
	ID                      uuid.UUID
	UserID                  uuid.UUID
	Parampara               string
	VedaAffiliation         string
	CeremonySpecializations []string
	Languages               []string
	ServiceRadiusKM         int
	AvailabilityStatus      AvailabilityStatus
	About                   string
	CreatedAt               int64
	UpdatedAt               int64
	DeletedAt               *int64
}

// IsDeleted returns true if the pandit profile has been soft-deleted.
func (p *PanditProfile) IsDeleted() bool {
	return p.DeletedAt != nil
}

// PanditProfileWithDistance extends PanditProfile with joined user data,
// a computed distance field, and optional connection context for the viewer.
type PanditProfileWithDistance struct {
	PanditProfile
	User             *User      // joined user data (display_name, photo, location)
	DistanceKM       float64    // ST_Distance result converted to kilometers
	ConnectionStatus string     // "not_connected" | "pending" | "connected" (viewer-specific)
	ConversationID   *uuid.UUID // set when status is "connected" (deep-link to chat)
}

// PanditFilters holds optional filter criteria for ListNearby queries.
type PanditFilters struct {
	CeremonyTypes []string // filter: ceremony_specializations @> ARRAY[...]
	Parampara     string   // filter: parampara = ?
	AvailableOnly bool     // filter: availability_status = 'available'
}

// ServiceCity represents a pandit's service city entry
type ServiceCity struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	CityID    uuid.UUID
	IsHome    bool
	CreatedAt int64
}

// ServiceCityDetail is the enriched view returned to clients
type ServiceCityDetail struct {
	CityID     uuid.UUID `json:"cityId"`
	CityName   string    `json:"cityName"`
	StateName  string    `json:"stateName"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	IsHome     bool      `json:"isHome"`
	DistanceKm float64   `json:"distanceKm"` // distance from home city, 0 for home itself
}


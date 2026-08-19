package domain

import (
	"errors"
	"fmt"
)

// UserRole represents the role of a user in the platform.
type UserRole string

const (
	RoleYajman UserRole = "yajman"
	RolePandit UserRole = "pandit"
	RoleCommon UserRole = "common"
	// RoleSystem marks non-login platform accounts (e.g. the Medha App
	// system user). Never a valid role for a real, authenticating user.
	RoleSystem UserRole = "system"
)

// IsValid returns true if the role is a recognized value.
func (r UserRole) IsValid() bool {
	return r == RoleYajman || r == RolePandit || r == RoleCommon || r == RoleSystem
}

// IsProfileRole returns true if the role represents a completed profile
// selection (pandit or yajman), as opposed to the default "common" state.
func (r UserRole) IsProfileRole() bool {
	return r == RoleYajman || r == RolePandit
}

// String returns the string representation of the role.
func (r UserRole) String() string {
	return string(r)
}

// GeoPoint represents a geographic coordinate (latitude, longitude).
// In Go code, use (Latitude, Longitude) order.
// In PostGIS SQL, use POINT(longitude, latitude) — lng FIRST.
type GeoPoint struct {
	Latitude  float64 // -90 to 90
	Longitude float64 // -180 to 180
}

// Validate checks if the coordinates are within valid ranges.
func (g GeoPoint) Validate() error {
	var errs []error
	if g.Latitude < -90 || g.Latitude > 90 {
		errs = append(errs, fmt.Errorf("latitude %f out of range [-90, 90]", g.Latitude))
	}
	if g.Longitude < -180 || g.Longitude > 180 {
		errs = append(errs, fmt.Errorf("longitude %f out of range [-180, 180]", g.Longitude))
	}
	return errors.Join(errs...)
}

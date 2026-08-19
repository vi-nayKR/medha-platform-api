package domain

import (
	"errors"

	"github.com/google/uuid"
)

// MatchStatus represents the status of a match between yajman and pandit.
type MatchStatus string

const (
	MatchStatusCreated   MatchStatus = "created"   // pandit expressed interest
	MatchStatusMatched   MatchStatus = "matched"   // yajman accepted pandit's interest
	MatchStatusActive    MatchStatus = "active"    // ceremony confirmed / in progress
	MatchStatusCompleted MatchStatus = "completed" // ceremony done
	MatchStatusCancelled MatchStatus = "cancelled" // cancelled by either party
)

func (s MatchStatus) IsValid() bool {
	return s == MatchStatusCreated || s == MatchStatusMatched || s == MatchStatusActive || s == MatchStatusCompleted || s == MatchStatusCancelled
}

func (s MatchStatus) String() string {
	return string(s)
}

// Match represents a confirmed connection between a yajman and pandit for an event.
type Match struct {
	ID        uuid.UUID
	YajmanID  uuid.UUID
	PanditID  uuid.UUID
	EventID   uuid.UUID
	Status    MatchStatus
	MatchedAt int64
	CreatedAt int64
	UpdatedAt int64
}

// MatchWithDetails extends Match with joined user and event information.
type MatchWithDetails struct {
	Match
	// Yajman info
	YajmanFirstName string
	YajmanLastName  string
	YajmanPhotoURL  *string
	YajmanPhone     string
	// Pandit info
	PanditFirstName string
	PanditLastName  string
	PanditPhotoURL  *string
	PanditPhone     string
	// Event info
	CeremonyType string
	EventDate    string
	EventAddress string
}

// Sentinel errors for match operations.
var (
	ErrMatchNotFound         = errors.New("match not found")
	ErrMatchAlreadyExists    = errors.New("match already exists for this pandit and event")
	ErrNotMatchParty         = errors.New("user is not a party in this match")
	ErrMaxMatchesReached     = errors.New("maximum matches reached for this event")
	ErrInvalidStatusTransition = errors.New("this match cannot be transitioned in its current state")
)

// MaxMatchesPerEvent is the cap on acceptances per event (~3 as per spec).
const MaxMatchesPerEvent = 3

package domain

import (
	"errors"

	"github.com/google/uuid"
)

// InterestStatus represents the status of a pandit's interest in an event.
type InterestStatus string

const (
	InterestStatusPending        InterestStatus = "pending"
	InterestStatusAccepted       InterestStatus = "accepted"
	InterestStatusRejected       InterestStatus = "rejected"
	InterestStatusConnected      InterestStatus = "connected"
	InterestStatusWithdrawn      InterestStatus = "withdrawn"
	InterestStatusEventCancelled InterestStatus = "event_cancelled"
	InterestStatusCompleted      InterestStatus = "completed"
)

func (s InterestStatus) IsValid() bool {
	return s == InterestStatusPending ||
		s == InterestStatusAccepted ||
		s == InterestStatusRejected ||
		s == InterestStatusConnected ||
		s == InterestStatusWithdrawn ||
		s == InterestStatusEventCancelled ||
		s == InterestStatusCompleted
}

func (s InterestStatus) String() string {
	return string(s)
}

// Interest represents a pandit's interest in a ceremony event.
type Interest struct {
	ID             uuid.UUID
	PanditID       uuid.UUID
	EventID        uuid.UUID
	Message        string
	Status         InterestStatus
	ConversationID *uuid.UUID // auto-created on interest expression (First Flow)
	CreatedAt      int64
	UpdatedAt      int64
}

// InterestWithDetails extends Interest with joined pandit and event information.
type InterestWithDetails struct {
	Interest
	// Pandit info
	PanditFirstName string
	PanditLastName  string
	PanditPhotoURL  *string
	// Event info
	CeremonyType      string
	CeremonyLogoURL   *string
	EventDate         string
	EventAddress      string
	YajmanPhoneNumber *string
	// Connection context
	ConversationID *uuid.UUID // populated when a conversation workspace exists
}

// Sentinel errors for interest operations.
var (
	ErrInterestNotFound           = errors.New("interest not found")
	ErrInterestAlreadyExists      = errors.New("interest already exists for this event")
	ErrNotEventOwner              = errors.New("only the event owner can perform this action")
	ErrNotPandit                  = errors.New("only pandits can express interest")
	ErrCannotInterestOwnEvent     = errors.New("cannot express interest in your own event")
	ErrInterestAlreadyResponded   = errors.New("this interest has already been accepted or rejected")
	ErrEventNotAcceptingInterests = errors.New("this event is not accepting new interests")
	ErrInterestNotConnected       = errors.New("interest must be in connected status to book")
	ErrNotInterestOwner           = errors.New("only the interest owner can perform this action")
)

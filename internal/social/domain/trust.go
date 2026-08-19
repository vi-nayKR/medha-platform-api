package domain

import (
	"errors"

	"github.com/google/uuid"
)

// BadgeTier represents a Pandit's dharmic badge tier.
type BadgeTier string

const (
	BadgePanditJi      BadgeTier = "pandit_ji"
	BadgePujaPraveen   BadgeTier = "puja_praveen"
	BadgeKarmaKandi    BadgeTier = "karma_kandi"
	BadgeYajnaMaharshi BadgeTier = "yajna_maharshi"
	BadgeDharmaRatna   BadgeTier = "dharma_ratna"
)

func (b BadgeTier) String() string { return string(b) }

// BadgeDisplay returns the display icon and label for a badge tier.
func (b BadgeTier) Display() (icon string, label string) {
	switch b {
	case BadgePanditJi:
		return "🔶", "Pandit ji"
	case BadgePujaPraveen:
		return "🟠", "Puja Praveen"
	case BadgeKarmaKandi:
		return "🟤", "Karma Kandi"
	case BadgeYajnaMaharshi:
		return "⭐", "Yajna Maharshi"
	case BadgeDharmaRatna:
		return "💎", "Dharma Ratna"
	default:
		return "🔶", "Pandit ji"
	}
}

// BadgeThreshold defines the requirements for a badge tier.
type BadgeThreshold struct {
	Tier             BadgeTier
	MinCeremonies    int
	MinAverageRating float64
}

// BadgeThresholds defines the progression thresholds.
var BadgeThresholds = []BadgeThreshold{
	{BadgeDharmaRatna, 50, 4.7},
	{BadgeYajnaMaharshi, 30, 4.5},
	{BadgeKarmaKandi, 15, 4.2},
	{BadgePujaPraveen, 5, 4.0},
	{BadgePanditJi, 0, 0},
}

// EventFeedback represents feedback from a yajman after an event.
type EventFeedback struct {
	ID        uuid.UUID
	MatchID   uuid.UUID
	YajmanID  uuid.UUID
	PanditID  uuid.UUID
	Rating    int
	Comment   string
	CreatedAt int64
}

// PanditBadge represents a pandit's badge and reputation stats.
type PanditBadge struct {
	ID                  uuid.UUID
	PanditID            uuid.UUID
	BadgeTier           BadgeTier
	CeremoniesCompleted int
	AverageRating       float64
	TotalFeedbackCount  int
	UpdatedAt           int64
}

// ComputeTier determines the appropriate badge tier based on stats.
func ComputeTier(ceremoniesCompleted int, averageRating float64) BadgeTier {
	for _, threshold := range BadgeThresholds {
		if ceremoniesCompleted >= threshold.MinCeremonies &&
			averageRating >= threshold.MinAverageRating {
			return threshold.Tier
		}
	}
	return BadgePanditJi
}

// Sentinel errors.
var (
	ErrFeedbackNotFound      = errors.New("feedback not found")
	ErrFeedbackAlreadyExists = errors.New("feedback already submitted for this match")
	ErrBadgeNotFound         = errors.New("badge not found")
	ErrNotYajmanInMatch      = errors.New("only the yajman in this match can submit feedback")
	ErrMatchNotCompleted     = errors.New("match must be completed before submitting feedback")
	ErrInvalidRating         = errors.New("rating must be between 1 and 5")
)

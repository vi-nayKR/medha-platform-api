package service

import (
	"github.com/medha/backend/internal/matching/domain"
)

// validTransitions defines the allowed state transitions for a match.
// Map key is "from state", value map key is "to state".
var validTransitions = map[domain.MatchStatus]map[domain.MatchStatus]bool{
	domain.MatchStatusCreated: {
		domain.MatchStatusMatched:   true,
		domain.MatchStatusCancelled: true, // Optional: if yajman rejects
	},
	domain.MatchStatusMatched: {
		domain.MatchStatusActive:    true,
		domain.MatchStatusCancelled: true,
	},
	domain.MatchStatusActive: {
		domain.MatchStatusCompleted: true,
		domain.MatchStatusCancelled: true,
	},
	domain.MatchStatusCompleted: {}, // Terminal state
	domain.MatchStatusCancelled: {}, // Terminal state
}

// CanTransition returns true if the match can transition from the current state to the next state.
func CanTransition(from, to domain.MatchStatus) bool {
	if allowedDestinations, ok := validTransitions[from]; ok {
		return allowedDestinations[to]
	}
	return false
}

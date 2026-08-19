package domain

import (
	"context"
)

// PanchangaRepository defines the data access interface for panchanga and festivals.
type PanchangaRepository interface {
	// GetByDate returns the Panchanga record for a specific calendar date (midnight IST epoch).
	GetByDate(ctx context.Context, dateEpoch int64) (*Panchanga, error)

	// GetByDateRange returns all Panchanga records between fromEpoch and toEpoch (inclusive).
	GetByDateRange(ctx context.Context, fromEpoch, toEpoch int64) ([]*Panchanga, error)

	// GetUpcomingFestivals returns festivals occurring on or after `fromEpoch`, ordered by date.
	GetUpcomingFestivals(ctx context.Context, fromEpoch int64, limit int) ([]*Festival, error)
}

package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// EventCompletionEventRepo defines the minimal event repository surface needed
// by the worker. This allows the worker to depend on an interface rather than
// a concrete type, keeping the package testable.
type EventCompletionEventRepo interface {
	// MarkPastEventsCompleted marks all non-deleted, past-date events as Completed
	// and returns the IDs of the events that were updated.
	MarkPastEventsCompleted(ctx context.Context) ([]uuid.UUID, error)
}

// EventCompletionMatchRepo defines the minimal match repository surface needed
// by the worker.
type EventCompletionMatchRepo interface {
	// MarkMatchesCompletedForEvents marks all active matches for the given event IDs
	// as completed and returns the count of updated rows.
	MarkMatchesCompletedForEvents(ctx context.Context, eventIDs []uuid.UUID) (int64, error)
}

// EventCompletionInterestRepo defines the minimal interest repository surface needed
// by the worker.
type EventCompletionInterestRepo interface {
	// BulkCompleteByEventID updates the accepted interest for an event to completed.
	BulkCompleteByEventID(ctx context.Context, eventID uuid.UUID) error
}

// EventCompletionWorker is a background goroutine that periodically checks for
// events whose event_date has passed and transitions them (and their matches)
// to a terminal "Completed" state.
//
// This removes the need for Yajman/Pandit to manually close out events after
// the ceremony, while ensuring the data model stays consistent.
type EventCompletionWorker struct {
	eventRepo    EventCompletionEventRepo
	matchRepo    EventCompletionMatchRepo
	interestRepo EventCompletionInterestRepo // optional — nil-safe
	interval     time.Duration
	logger       *slog.Logger
}

// NewEventCompletionWorker creates a new EventCompletionWorker.
// interval controls how often the sweep runs (e.g. time.Hour).
func NewEventCompletionWorker(
	eventRepo EventCompletionEventRepo,
	matchRepo EventCompletionMatchRepo,
	interval time.Duration,
	logger *slog.Logger,
) *EventCompletionWorker {
	return &EventCompletionWorker{
		eventRepo: eventRepo,
		matchRepo: matchRepo,
		interval:  interval,
		logger:    logger,
	}
}

// SetInterestRepository sets the optional interest repository for cascading completions.
func (w *EventCompletionWorker) SetInterestRepository(interestRepo EventCompletionInterestRepo) {
	w.interestRepo = interestRepo
}

// Start launches the periodic completion sweep.
// It runs immediately on startup, then repeats on the configured interval.
// The goroutine exits cleanly when ctx is cancelled (e.g. on server shutdown).
func (w *EventCompletionWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.Info("event completion worker started", "interval", w.interval)

	// Run immediately on startup to handle any backlog
	w.run(ctx)

	for {
		select {
		case <-ticker.C:
			w.run(ctx)
		case <-ctx.Done():
			w.logger.Info("event completion worker stopped")
			return
		}
	}
}

// run performs a single completion sweep.
func (w *EventCompletionWorker) run(ctx context.Context) {
	eventIDs, err := w.eventRepo.MarkPastEventsCompleted(ctx)
	if err != nil {
		w.logger.Error("event completion worker: failed to mark events completed", "error", err)
		return
	}

	if len(eventIDs) == 0 {
		w.logger.Debug("event completion worker: no past events to complete")
		return
	}

	w.logger.Info("event completion worker: events marked completed",
		"count", len(eventIDs),
		"event_ids", eventIDs,
	)

	// Cascade: also complete associated matches
	matchCount, err := w.matchRepo.MarkMatchesCompletedForEvents(ctx, eventIDs)
	if err != nil {
		w.logger.Error("event completion worker: failed to mark matches completed",
			"error", err,
			"event_count", len(eventIDs),
		)
		return
	}

	w.logger.Info("event completion worker: matches marked completed",
		"match_count", matchCount,
	)

	// Cascade: also complete associated interests
	if w.interestRepo != nil {
		for _, eventID := range eventIDs {
			if err := w.interestRepo.BulkCompleteByEventID(ctx, eventID); err != nil {
				w.logger.Error("event completion worker: failed to mark interest completed for event",
					"error", err,
					"event_id", eventID,
				)
			}
		}
	}
}

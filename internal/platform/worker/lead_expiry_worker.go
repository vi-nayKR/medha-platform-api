package worker

import (
	"context"
	"log/slog"
	"time"
)

// LeadExpirer defines the port for lead expiry operation.
type LeadExpirer interface {
	ExpirePendingLeads(ctx context.Context, expiryDuration time.Duration) (int64, error)
}

// LeadExpiryWorker is a background goroutine that periodically checks for
// job leads whose created_at is older than a threshold (e.g., 24 hours)
// and transitions them to "expired" state.
type LeadExpiryWorker struct {
	expirer  LeadExpirer
	interval time.Duration
	expiry   time.Duration
	logger   *slog.Logger
}

// NewLeadExpiryWorker creates a new LeadExpiryWorker.
func NewLeadExpiryWorker(
	expirer LeadExpirer,
	interval time.Duration,
	expiry time.Duration,
	logger *slog.Logger,
) *LeadExpiryWorker {
	return &LeadExpiryWorker{
		expirer:  expirer,
		interval: interval,
		expiry:   expiry,
		logger:   logger,
	}
}

// Start launches the periodic sweep.
func (w *LeadExpiryWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.Info("lead expiry worker started", "interval", w.interval, "expiry_threshold", w.expiry)

	// Run immediately on startup
	w.run(ctx)

	for {
		select {
		case <-ticker.C:
			w.run(ctx)
		case <-ctx.Done():
			w.logger.Info("lead expiry worker stopped")
			return
		}
	}
}

func (w *LeadExpiryWorker) run(ctx context.Context) {
	count, err := w.expirer.ExpirePendingLeads(ctx, w.expiry)
	if err != nil {
		w.logger.Error("lead expiry worker failed", "error", err)
		return
	}

	if count > 0 {
		w.logger.Info("lead expiry worker sweep completed", "expired_count", count)
	} else {
		w.logger.Debug("lead expiry worker sweep: no leads expired")
	}
}

package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/medha/backend/internal/notification/domain"
)

// RiskChecker identifies events that are approaching their date without
// any pandit interests, and sends reminder notifications to the yajman.
type RiskChecker struct {
	notifRepo domain.NotificationRepository
	notifSvc  *NotificationService
	logger    *slog.Logger
}

// NewRiskChecker creates a new RiskChecker.
func NewRiskChecker(notifRepo domain.NotificationRepository, notifSvc *NotificationService, logger *slog.Logger) *RiskChecker {
	return &RiskChecker{
		notifRepo: notifRepo,
		notifSvc:  notifSvc,
		logger:    logger,
	}
}

// Run executes a single pass of the risk check logic.
func (c *RiskChecker) Run(ctx context.Context) error {
	c.logger.Info("starting event risk check")

	// We check events that are within 14 days and have 0 interests
	events, err := c.notifRepo.GetEventsAtRisk(ctx, 14)
	if err != nil {
		return fmt.Errorf("failed to get risk events: %w", err)
	}

	for _, ev := range events {
		var tier string
		var title string
		var body string

		if ev.DaysUntil <= 3 {
			tier = "high"
			title = "⚠️ Urgent: No Pandits yet!"
			body = fmt.Sprintf("Your %s is in %d days and has no interests yet. Consider updating the description.", formatCeremony(ev.CeremonyType), ev.DaysUntil)
		} else if ev.DaysUntil <= 7 {
			tier = "medium"
			title = "⏳ Upcoming Event"
			body = fmt.Sprintf("Your %s is a week away. We are still looking for Pandits.", formatCeremony(ev.CeremonyType))
		} else {
			tier = "low"
			title = "📅 Event Reminder"
			body = fmt.Sprintf("Your %s is in 2 weeks. Make sure your profile is complete to attract Pandits.", formatCeremony(ev.CeremonyType))
		}

		// Check deduplication
		sent, checkErr := c.notifRepo.HasNotificationLog(ctx, ev.EventID, tier)
		if checkErr != nil {
			c.logger.Error("failed to check notification log", "error", checkErr, "event_id", ev.EventID)
			continue
		}

		if sent {
			continue // Already notified for this tier
		}

		// Send Notification
		_, notifErr := c.notifSvc.CreateNotification(ctx, CreateNotificationParams{
			UserID: ev.YajmanID,
			Type:   domain.NotifTimelineRisk,
			Title:  title,
			Body:   body,
			Data: map[string]string{
				"event_id": ev.EventID.String(),
				"tier":     tier,
				"type":     "timeline_risk",
			},
		})

		if notifErr != nil {
			c.logger.Error("failed to send risk notification", "error", notifErr, "event_id", ev.EventID)
			continue
		}

		// Mark as sent
		if saveErr := c.notifRepo.SaveNotificationLog(ctx, ev.EventID, tier); saveErr != nil {
			c.logger.Error("failed to save notification log", "error", saveErr, "event_id", ev.EventID)
		}

		c.logger.Info("sent risk notification", "event_id", ev.EventID, "tier", tier)
	}

	c.logger.Info("finished event risk check", "events_processed", len(events))
	return nil
}

func formatCeremony(c string) string {
	return strings.ReplaceAll(c, "_", " ")
}

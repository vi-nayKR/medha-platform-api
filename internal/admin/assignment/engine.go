package assignment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	eventdomain "github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/infra/database"
	notifdomain "github.com/medha/backend/internal/notification/domain"
	notifservice "github.com/medha/backend/internal/notification/service"
	"github.com/medha/backend/internal/platform/epoch"
)

// Weights for scoring factors (must sum to 1.0).
const (
	weightDistance   = 0.30
	weightExpertise  = 0.25
	weightAvailable  = 0.15
	weightLanguage   = 0.10
	weightRadius     = 0.10
	weightBadge      = 0.10
)

// DefaultSearchRadiusKM is the initial radius to search for Pandits.
const DefaultSearchRadiusKM = 50

// FallbackSearchRadiusKM is used when < minCandidates are found in DefaultSearchRadiusKM.
const FallbackSearchRadiusKM = 100

// DefaultTopN is the maximum number of leads generated per event.
const DefaultTopN = 5

const minCandidates = 3

// ScoreResult holds the computed score for a single Pandit.
type ScoreResult struct {
	PanditID    uuid.UUID          `json:"pandit_id"`
	TotalScore  float64            `json:"total_score"`
	Breakdown   map[string]float64 `json:"breakdown"`
	DistanceKM  float64            `json:"distance_km"`
	IsAvailable bool               `json:"is_available"`
	HasExpertise bool              `json:"has_expertise"`
	FirstName   string             `json:"first_name"`
	LastName    string             `json:"last_name"`
}

// candidateRow maps the raw SQL result for a Pandit candidate.
type candidateRow struct {
	UserID                  uuid.UUID
	FirstName               string
	LastName                string
	DistanceKM              float64
	CeremonySpecializations []string
	Languages               []string
	ServiceRadiusKM         int
	AvailabilityStatus      string
	BadgeTier               string
	State                   string
}

// Engine scores and assigns Pandits to events.
type Engine struct {
	pool     *pgxpool.Pool
	notifSvc *notifservice.NotificationService // optional
	logger   *slog.Logger
}

// NewEngine creates a new assignment engine.
func NewEngine(pool *pgxpool.Pool, logger *slog.Logger) *Engine {
	return &Engine{pool: pool, logger: logger}
}

// SetNotificationService wires the optional notification service.
func (e *Engine) SetNotificationService(notifSvc *notifservice.NotificationService) {
	e.notifSvc = notifSvc
}

// ProcessEvent scores nearby Pandits and creates job_leads for an event.
// Called asynchronously after event creation.
func (e *Engine) ProcessEvent(ctx context.Context, event *eventdomain.Event) {
	if event == nil || event.Location == nil {
		e.logger.Debug("assignment engine skipped: no event or location")
		return
	}

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("assignment engine panic recovered", "panic", r, "event_id", event.ID)
		}
	}()

	lat := event.Location.Latitude
	lng := event.Location.Longitude
	ceremonyType := event.CeremonyType.String()

	// Fetch event-specific platform fee or fallback to ceremony catalog fee
	var fee float64
	if event.PlatformFee != nil {
		fee = *event.PlatformFee
	} else {
		fee = e.getFeeAmount(ctx, ceremonyType)
	}

	// Find candidates
	candidates := e.findCandidates(ctx, lat, lng, DefaultSearchRadiusKM)
	if len(candidates) < minCandidates {
		// Expand radius
		candidates = e.findCandidates(ctx, lat, lng, FallbackSearchRadiusKM)
	}

	if len(candidates) == 0 {
		e.logger.Info("assignment engine: no candidates found", "event_id", event.ID)
		return
	}

	// Get Yajman's state for language matching heuristic
	yajmanState := e.getYajmanState(ctx, event.YajmanID)

	// Score all candidates
	scored := make([]ScoreResult, 0, len(candidates))
	for _, c := range candidates {
		result := score(c, ceremonyType, yajmanState)
		scored = append(scored, result)
	}

	// Sort by score descending (simple selection — N is small)
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].TotalScore > scored[i].TotalScore {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Take top N
	topN := DefaultTopN
	if len(scored) < topN {
		topN = len(scored)
	}
	scored = scored[:topN]

	// Create job_leads
	for _, s := range scored {
		breakdownJSON, _ := json.Marshal(s.Breakdown)
		_, err := e.pool.Exec(ctx, `
			INSERT INTO job_leads (pandit_id, yajman_id, event_id, source, score, score_breakdown, platform_fee, status)
			VALUES ($1, $2, $3, 'event', $4, $5, $6, 'sent')
			ON CONFLICT (pandit_id, event_id) WHERE deleted_at IS NULL DO NOTHING
		`, s.PanditID, event.YajmanID, event.ID, s.TotalScore, breakdownJSON, fee)
		if err != nil {
			e.logger.Error("failed to create job lead", "error", err, "pandit_id", s.PanditID, "event_id", event.ID)
			continue
		}

		// Send notification to Pandit
		if e.notifSvc != nil {
			ceremonyName := strings.ReplaceAll(ceremonyType, "_", " ")
			if event.CeremonyType == eventdomain.CeremonyCustom && event.CustomCeremonyName != "" {
				ceremonyName = event.CustomCeremonyName
			}
			_, notifErr := e.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
				UserID: s.PanditID,
				Type:   notifdomain.NotifEventNearby,
				Title:  "📋 New Job Lead!",
				Body:   fmt.Sprintf("A Yajman needs a Pandit for %s (%.1f km away). Tap to view.", ceremonyName, s.DistanceKM),
				Data: map[string]string{
					"event_id": event.ID.String(),
					"type":     "job_lead",
				},
			})
			if notifErr != nil {
				e.logger.Error("failed to send lead notification", "error", notifErr, "pandit_id", s.PanditID)
			}
		}
	}

	e.logger.Info("assignment engine completed",
		"event_id", event.ID,
		"candidates_found", len(candidates),
		"leads_created", topN,
	)
}

// ProcessDirectChat scores Pandits for a direct chat request.
// Returns the top candidates based on yajman location and optional ceremony type hint.
func (e *Engine) ProcessDirectChat(ctx context.Context, yajmanID uuid.UUID, ceremonyHint string, lat, lng float64) []ScoreResult {
	if lat == 0 && lng == 0 {
		// Try to get yajman's stored location
		var storedLat, storedLng float64
		err := e.pool.QueryRow(ctx, `
			SELECT ST_Y(location::geometry), ST_X(location::geometry)
			FROM users WHERE id = $1 AND location IS NOT NULL AND deleted_at IS NULL
		`, yajmanID).Scan(&storedLat, &storedLng)
		if err != nil || (storedLat == 0 && storedLng == 0) {
			e.logger.Debug("direct chat routing: no location available", "yajman_id", yajmanID)
			return nil
		}
		lat, lng = storedLat, storedLng
	}

	candidates := e.findCandidates(ctx, lat, lng, DefaultSearchRadiusKM)
	if len(candidates) < minCandidates {
		candidates = e.findCandidates(ctx, lat, lng, FallbackSearchRadiusKM)
	}

	if len(candidates) == 0 {
		return nil
	}

	yajmanState := e.getYajmanState(ctx, yajmanID)

	scored := make([]ScoreResult, 0, len(candidates))
	for _, c := range candidates {
		result := score(c, ceremonyHint, yajmanState)
		scored = append(scored, result)
	}

	// Sort descending
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].TotalScore > scored[i].TotalScore {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	topN := DefaultTopN
	if len(scored) < topN {
		topN = len(scored)
	}
	return scored[:topN]
}

// ManualAssign creates a job lead for a specific Pandit-Event pair (admin action).
func (e *Engine) ManualAssign(ctx context.Context, panditID, yajmanID, eventID uuid.UUID, adminNotes string) error {
	var ceremonyType string
	var customCeremonyName string
	var eventFee *float64
	err := e.pool.QueryRow(ctx, `SELECT ceremony_type::text, COALESCE(custom_ceremony_name, ''), platform_fee FROM events WHERE id = $1 AND deleted_at IS NULL`, eventID).Scan(&ceremonyType, &customCeremonyName, &eventFee)
	if err != nil {
		return fmt.Errorf("fetching event: %w", err)
	}

	var fee float64
	if eventFee != nil {
		fee = *eventFee
	} else {
		fee = e.getFeeAmount(ctx, ceremonyType)
	}

	// Check if this is the first time the event is being assigned/pushed to any Pandit
	var existingLeads int
	_ = e.pool.QueryRow(ctx, `SELECT COUNT(*) FROM job_leads WHERE event_id = $1 AND deleted_at IS NULL`, eventID).Scan(&existingLeads)

	_, err = e.pool.Exec(ctx, `
		INSERT INTO job_leads (pandit_id, yajman_id, event_id, source, score, platform_fee, status, assigned_by, admin_notes)
		VALUES ($1, $2, $3, 'event', 100.00, $4, 'sent', 'admin', $5)
		ON CONFLICT (pandit_id, event_id) WHERE deleted_at IS NULL DO NOTHING
	`, panditID, yajmanID, eventID, fee, adminNotes)
	if err != nil {
		return fmt.Errorf("manual assign: %w", err)
	}

	// Update event status to 'Pushed'
	now := epoch.Now()
	_, err = e.pool.Exec(ctx, `
		UPDATE events SET status = 'Pushed', updated_at = $2
		WHERE id = $1 AND deleted_at IS NULL AND status = 'Created'
	`, eventID, now)
	if err != nil {
		return fmt.Errorf("update event status to Pushed: %w", err)
	}

	// Notify pandit
	if e.notifSvc != nil {
		_, _ = e.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
			UserID: panditID,
			Type:   notifdomain.NotifEventNearby,
			Title:  "New Event is available",
			Body:   "You have a new recommended job assignment available. Tap to view.",
			Data: map[string]string{
				"event_id": eventID.String(),
				"type":     "job_lead",
			},
		})
	}

	// Notify yajman (only on the first push/assignment to avoid duplicate alerts during bulk pushes)
	if e.notifSvc != nil && existingLeads == 0 {
		_, _ = e.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
			UserID: yajmanID,
			Type:   notifdomain.NotifEventPushed,
			Title:  "Event is pushed to selective pandits",
			Body:   "Your ceremony event has been shared with selected Pandit candidates.",
			Data: map[string]string{
				"event_id": eventID.String(),
				"type":     "event_pushed",
			},
		})
	}

	return nil
}

// AcceptLead transitions lead status to 'interested' when a Pandit expresses interest.
func (e *Engine) AcceptLead(ctx context.Context, leadID, panditID uuid.UUID) error {
	// Security check: Verify the lead belongs to the calling pandit
	var leadPanditID uuid.UUID
	err := e.pool.QueryRow(ctx, `SELECT pandit_id FROM job_leads WHERE id = $1 AND deleted_at IS NULL`, leadID).Scan(&leadPanditID)
	if err != nil {
		return fmt.Errorf("lead not found")
	}
	if leadPanditID != panditID {
		return fmt.Errorf("unauthorized: lead does not belong to this pandit")
	}

	var eventID uuid.UUID
	var status string
	now := epoch.Now()

	err = database.WithTx(ctx, e.pool, func(txCtx context.Context) error {
		exec := database.GetExecutor(txCtx, e.pool)

		// 1. Get lead details
		err := exec.QueryRow(txCtx, `
			SELECT event_id, status
			FROM job_leads
			WHERE id = $1 AND deleted_at IS NULL
		`, leadID).Scan(&eventID, &status)
		if err != nil {
			return fmt.Errorf("lead not found: %w", err)
		}

		if status != "sent" && status != "pending" {
			return fmt.Errorf("lead cannot be updated in status: %s", status)
		}

		// 2. Enforce check that event is still active & valid
		var eventStatus string
		err = exec.QueryRow(txCtx, `
			SELECT status FROM events WHERE id = $1 AND deleted_at IS NULL
		`, eventID).Scan(&eventStatus)
		if err != nil {
			return fmt.Errorf("associated event not found or deleted")
		}
		if eventStatus == "Completed" || eventStatus == "Cancelled" || eventStatus == "Booked" {
			return fmt.Errorf("associated event is already completed, cancelled, or booked")
		}

		// 3. Update lead status to 'interested'
		_, err = exec.Exec(txCtx, `
			UPDATE job_leads SET status = 'interested', updated_at = $2
			WHERE id = $1 AND deleted_at IS NULL
		`, leadID, now)
		if err != nil {
			return fmt.Errorf("update lead status to interested: %w", err)
		}

		// 4. Update the event status based on interest count
		return e.updateEventPushedPendingStatus(txCtx, exec, eventID, now)
	})
	if err != nil {
		return err
	}

	// 5. Notify all Admin/Owners: "Pandits showed interest"
	if e.notifSvc != nil {
		go func() {
			rows, err := e.pool.Query(context.Background(), "SELECT id FROM admin_users")
			if err != nil {
				e.logger.Error("failed to query admin users for pandit interest notification", "error", err)
				return
			}
			defer rows.Close()

			for rows.Next() {
				var adminID uuid.UUID
				if err := rows.Scan(&adminID); err == nil {
					_, _ = e.notifSvc.CreateNotification(context.Background(), notifservice.CreateNotificationParams{
						UserID: adminID,
						Type:   notifdomain.NotifPanditInterest,
						Title:  "Pandits shows interest",
						Body:   "A Pandit has expressed interest in an assigned event.",
						Data: map[string]string{
							"event_id": eventID.String(),
							"type":     "pandit_interest",
						},
					})
				}
			}
		}()
	}

	return nil
}

// DeclineLead marks a lead as declined (no fee charged).
func (e *Engine) DeclineLead(ctx context.Context, leadID, panditID uuid.UUID) error {
	var eventID uuid.UUID
	now := epoch.Now()

	err := database.WithTx(ctx, e.pool, func(txCtx context.Context) error {
		exec := database.GetExecutor(txCtx, e.pool)

		err := exec.QueryRow(txCtx, `
			SELECT event_id FROM job_leads WHERE id = $1 AND pandit_id = $2 AND deleted_at IS NULL
		`, leadID, panditID).Scan(&eventID)
		if err != nil {
			return err
		}

		tag, err := exec.Exec(txCtx, `
			UPDATE job_leads SET status = 'declined', updated_at = $3
			WHERE id = $1 AND pandit_id = $2 AND status IN ('sent', 'pending', 'interested') AND deleted_at IS NULL
		`, leadID, panditID, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("lead not found or already processed")
		}

		return e.updateEventPushedPendingStatus(txCtx, exec, eventID, now)
	})
	if err != nil {
		return fmt.Errorf("decline lead: %w", err)
	}
	return nil
}

// updateEventPushedPendingStatus helper updates the event status to Pending if interested pandits >= 3, else Pushed.
func (e *Engine) updateEventPushedPendingStatus(ctx context.Context, exec database.DBTX, eventID uuid.UUID, now int64) error {
	var currentStatus string
	err := exec.QueryRow(ctx, `SELECT status::text FROM events WHERE id = $1 AND deleted_at IS NULL`, eventID).Scan(&currentStatus)
	if err != nil {
		return err
	}
	if currentStatus != "Pushed" && currentStatus != "Pending" {
		return nil
	}

	var interestedCount int
	err = exec.QueryRow(ctx, `
		SELECT COUNT(*) FROM job_leads 
		WHERE event_id = $1 AND status = 'interested' AND deleted_at IS NULL
	`, eventID).Scan(&interestedCount)
	if err != nil {
		return err
	}

	var newStatus string
	if interestedCount >= 3 {
		newStatus = "Pending"
	} else {
		newStatus = "Pushed"
	}

	if currentStatus != newStatus {
		_, err = exec.Exec(ctx, `
			UPDATE events SET status = $2::event_status, updated_at = $3
			WHERE id = $1 AND deleted_at IS NULL
		`, eventID, newStatus, now)
		return err
	}
	return nil
}

// score computes the assignment score for a single candidate.
func score(c candidateRow, ceremonyType, yajmanState string) ScoreResult {
	breakdown := make(map[string]float64)

	// 1. Distance score: closer = higher. Max 100 at 0km, decays linearly to 0 at 100km.
	distScore := math.Max(0, 100.0-c.DistanceKM)
	breakdown["distance"] = distScore * weightDistance

	// 2. Expertise: binary — does Pandit specialize in this ceremony?
	hasExpertise := false
	if ceremonyType != "" {
		for _, spec := range c.CeremonySpecializations {
			if strings.EqualFold(spec, ceremonyType) {
				hasExpertise = true
				break
			}
		}
	}
	expertiseScore := 0.0
	if hasExpertise {
		expertiseScore = 100.0
	}
	breakdown["expertise"] = expertiseScore * weightExpertise

	// 3. Availability: binary
	isAvailable := c.AvailabilityStatus == "available"
	availScore := 0.0
	if isAvailable {
		availScore = 100.0
	}
	breakdown["availability"] = availScore * weightAvailable

	// 4. Language match: heuristic — check if Pandit's state matches Yajman's state
	// ponytail: crude heuristic, upgrade path = actual language preference field on users table
	langScore := 0.0
	if yajmanState != "" && strings.EqualFold(c.State, yajmanState) {
		langScore = 100.0
	}
	breakdown["language"] = langScore * weightLanguage

	// 5. Service radius fit: does the Pandit's configured radius cover the actual distance?
	radiusScore := 0.0
	if c.ServiceRadiusKM > 0 && c.DistanceKM <= float64(c.ServiceRadiusKM) {
		radiusScore = 100.0
	}
	breakdown["service_radius"] = radiusScore * weightRadius

	// 6. Badge tier score
	badgeScore := badgeTierScore(c.BadgeTier)
	breakdown["badge"] = badgeScore * weightBadge

	total := 0.0
	for _, v := range breakdown {
		total += v
	}

	return ScoreResult{
		PanditID:     c.UserID,
		TotalScore:   math.Round(total*100) / 100,
		Breakdown:    breakdown,
		DistanceKM:   c.DistanceKM,
		IsAvailable:  isAvailable,
		HasExpertise: hasExpertise,
		FirstName:    c.FirstName,
		LastName:     c.LastName,
	}
}

// badgeTierScore returns a score (0-100) for a Pandit's badge tier.
func badgeTierScore(tier string) float64 {
	switch tier {
	case "dharma_ratna":
		return 100
	case "yajna_maharshi":
		return 80
	case "karma_kandi":
		return 60
	case "puja_praveen":
		return 40
	case "pandit_ji":
		return 20
	default:
		return 10
	}
}

// findCandidates queries Pandits within radiusKM of (lat, lng) with profile + badge data,
// and also matches pandits who explicitly registered to serve the nearest city of the event.
func (e *Engine) findCandidates(ctx context.Context, lat, lng float64, radiusKM int) []candidateRow {
	rows, err := e.pool.Query(ctx, `
		WITH nearest_city AS (
			SELECT id FROM cities
			WHERE is_active = true
			ORDER BY location <-> ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography
			LIMIT 1
		)
		SELECT
			u.id,
			COALESCE(u.first_name, '') AS first_name,
			COALESCE(u.last_name, '') AS last_name,
			ST_Distance(u.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography) / 1000.0 AS distance_km,
			COALESCE(pp.ceremony_specializations, '{}') AS ceremony_specializations,
			COALESCE(pp.languages, '{}') AS languages,
			COALESCE(pp.service_radius_km, 25) AS service_radius_km,
			COALESCE(pp.availability_status::text, 'available') AS availability_status,
			COALESCE(pb.tier, 'pandit_ji') AS badge_tier,
			COALESCE(u.state, '') AS state
		FROM users u
		JOIN pandit_profiles pp ON pp.user_id = u.id AND pp.deleted_at IS NULL
		LEFT JOIN pandit_badges pb ON pb.pandit_id = u.id
		WHERE u.role = 'pandit'
			AND u.deleted_at IS NULL
			AND u.location IS NOT NULL
			AND (
				ST_DWithin(u.location, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, $3)
				OR EXISTS (
					SELECT 1 FROM pandit_service_cities psc
					WHERE psc.user_id = u.id
					  AND psc.city_id = (SELECT id FROM nearest_city)
				)
			)
		ORDER BY distance_km ASC
	`, lat, lng, radiusKM*1000)
	if err != nil {
		e.logger.Error("assignment engine: failed to find candidates", "error", err)
		return nil
	}
	defer rows.Close()

	var candidates []candidateRow
	for rows.Next() {
		var c candidateRow
		if err := rows.Scan(
			&c.UserID, &c.FirstName, &c.LastName, &c.DistanceKM,
			&c.CeremonySpecializations, &c.Languages,
			&c.ServiceRadiusKM, &c.AvailabilityStatus,
			&c.BadgeTier, &c.State,
		); err != nil {
			e.logger.Error("assignment engine: scan candidate failed", "error", err)
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates
}

// getFeeAmount returns the platform fee for a ceremony type (falls back to default '*').
func (e *Engine) getFeeAmount(ctx context.Context, ceremonyType string) float64 {
	var fee float64
	// Try specific ceremony type first
	err := e.pool.QueryRow(ctx, `
		SELECT fee_amount FROM platform_fee_config
		WHERE ceremony_type = $1 AND is_active = true
		ORDER BY created_at DESC LIMIT 1
	`, ceremonyType).Scan(&fee)
	if err == nil {
		return fee
	}
	// Fallback to default
	err = e.pool.QueryRow(ctx, `
		SELECT fee_amount FROM platform_fee_config
		WHERE ceremony_type = '*' AND is_active = true
		ORDER BY created_at DESC LIMIT 1
	`, ).Scan(&fee)
	if err != nil {
		return 49.00 // ponytail: hardcoded fallback, ceiling = must always have a row in platform_fee_config
	}
	return fee
}

// getYajmanState returns the Yajman's state from their profile (for language matching heuristic).
func (e *Engine) getYajmanState(ctx context.Context, yajmanID uuid.UUID) string {
	var state string
	_ = e.pool.QueryRow(ctx, `SELECT COALESCE(state, '') FROM users WHERE id = $1 AND deleted_at IS NULL`, yajmanID).Scan(&state)
	return state
}

// ExpirePendingLeads transitions pending/sent leads that are older than expiryDuration to 'expired'.
// Also expires any active leads associated with deleted, completed, or cancelled events.
// Returns the count of affected leads.
func (e *Engine) ExpirePendingLeads(ctx context.Context, expiryDuration time.Duration) (int64, error) {
	cutoff := epoch.Now() - int64(expiryDuration.Seconds())
	tag, err := e.pool.Exec(ctx, `
		UPDATE job_leads
		SET status = 'expired',
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE status IN ('pending', 'sent')
		  AND created_at < $1
		  AND deleted_at IS NULL
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("expire pending leads: %w", err)
	}

	// Clean up any leads whose associated event was deleted, completed, or cancelled
	_, _ = e.pool.Exec(ctx, `
		UPDATE job_leads jl
		SET status = 'expired',
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		FROM events e
		WHERE jl.event_id = e.id
		  AND jl.status IN ('pending', 'sent', 'interested')
		  AND jl.deleted_at IS NULL
		  AND (e.deleted_at IS NOT NULL OR e.status IN ('Completed', 'Cancelled'))
	`)

	return tag.RowsAffected(), nil
}

// FinalizeAssignment transitions a lead to 'accepted' (admin action), creates match & conversation, and logs fee.
func (e *Engine) FinalizeAssignment(ctx context.Context, leadID uuid.UUID) error {
	var fee float64
	var eventID uuid.UUID
	var yajmanID uuid.UUID
	var panditID uuid.UUID
	var status string

	now := epoch.Now()

	err := database.WithTx(ctx, e.pool, func(txCtx context.Context) error {
		exec := database.GetExecutor(txCtx, e.pool)

		// 1. Get lead details
		err := exec.QueryRow(txCtx, `
			SELECT platform_fee, event_id, yajman_id, pandit_id, status
			FROM job_leads
			WHERE id = $1 AND deleted_at IS NULL
		`, leadID).Scan(&fee, &eventID, &yajmanID, &panditID, &status)
		if err != nil {
			return fmt.Errorf("lead not found: %w", err)
		}

		// Enforce that only sent/pending/interested/fee_agreed leads can be finalized
		if status != "interested" && status != "sent" && status != "pending" && status != "fee_agreed" {
			return fmt.Errorf("lead cannot be finalized in status: %s", status)
		}

		// 2. Enforce check that event is still active & valid
		var eventStatus, ceremonyType string
		err = exec.QueryRow(txCtx, `
			SELECT status, ceremony_type::text FROM events WHERE id = $1 AND deleted_at IS NULL
		`, eventID).Scan(&eventStatus, &ceremonyType)
		if err != nil {
			return fmt.Errorf("associated event not found or deleted")
		}
		if eventStatus == "Completed" || eventStatus == "Cancelled" || eventStatus == "Booked" {
			return fmt.Errorf("associated event is already completed, cancelled, or booked")
		}

		// 3. Update lead status to accepted
		_, err = exec.Exec(txCtx, `
			UPDATE job_leads SET status = 'accepted', fee_status = 'paid', updated_at = $2
			WHERE id = $1 AND deleted_at IS NULL
		`, leadID, now)
		if err != nil {
			return fmt.Errorf("update lead status: %w", err)
		}

		// Also update corresponding interest status to accepted (if exists)
		_, _ = exec.Exec(txCtx, `
			UPDATE interests SET status = 'accepted', updated_at = $3
			WHERE event_id = $1 AND pandit_id = $2
		`, eventID, panditID, now)

		// 4. Create Match
		matchID := uuid.New()
		_, err = exec.Exec(txCtx, `
			INSERT INTO matches (id, yajman_id, pandit_id, event_id, status, matched_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'matched', $5, $5, $5)
			ON CONFLICT (pandit_id, event_id) DO NOTHING
		`, matchID, yajmanID, panditID, eventID, now)
		if err != nil {
			return fmt.Errorf("create match: %w", err)
		}

		// 5. Update event status to 'Booked'
		_, err = exec.Exec(txCtx, `
			UPDATE events SET status = 'Booked', updated_at = $2
			WHERE id = $1 AND deleted_at IS NULL
		`, eventID, now)
		if err != nil {
			return fmt.Errorf("mark event booked: %w", err)
		}

		// 6. Create Chat Conversation (if not exists)
		var convID uuid.UUID
		err = exec.QueryRow(txCtx, `
			SELECT c.id FROM conversations c
			WHERE c.type = 'pandit_yajman' AND c.event_id = $3 AND c.is_active = true
			  AND ((c.user_one_id = $1 AND c.user_two_id = $2) OR (c.user_one_id = $2 AND c.user_two_id = $1))
			LIMIT 1
		`, yajmanID, panditID, eventID).Scan(&convID)

		if err != nil { // not exists, create it
			convTitle := fmt.Sprintf("%s Conversation", strings.ReplaceAll(ceremonyType, "_", " "))
			convID = uuid.New()
			_, err = exec.Exec(txCtx, `
				INSERT INTO conversations (id, type, event_id, match_id, title, is_active, user_one_id, user_two_id, created_at, updated_at)
				VALUES ($1, 'pandit_yajman', $2, $3, $4, true, $5, $6, $7, $7)
			`, convID, eventID, matchID, convTitle, yajmanID, panditID, now)
			if err != nil {
				return fmt.Errorf("create conversation: %w", err)
			}
		}

		// 7. Update lead's conversation_id so it links
		_, err = exec.Exec(txCtx, `
			UPDATE job_leads SET conversation_id = $2 WHERE id = $1
		`, leadID, convID)
		if err != nil {
			return fmt.Errorf("update lead conversation id: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Notify yajman
	if e.notifSvc != nil {
		_, _ = e.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
			UserID: yajmanID,
			Type:   notifdomain.NotifBookingConfirmed,
			Title:  "Event is booked",
			Body:   "Your ceremony booking has been confirmed.",
			Data: map[string]string{
				"event_id": eventID.String(),
				"type":     "booking_confirmed",
			},
		})
	}

	// Notify pandit
	if e.notifSvc != nil {
		_, _ = e.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
			UserID: panditID,
			Type:   notifdomain.NotifBookingConfirmed,
			Title:  "Event is booked",
			Body:   "Your booking for this event has been finalized and confirmed.",
			Data: map[string]string{
				"event_id": eventID.String(),
				"type":     "booking_confirmed",
			},
		})
	}

	return nil
}


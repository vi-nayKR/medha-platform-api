package admin

import (
	"math"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// ManualAssignRequest is the admin payload to manually assign a Pandit to an event.
type ManualAssignRequest struct {
	PanditID   string `json:"pandit_id"`
	YajmanID   string `json:"yajman_id"`
	EventID    string `json:"event_id"`
	AdminNotes string `json:"admin_notes"`
}

// UpdateLeadStatusRequest is the admin payload to update a lead's status.
type UpdateLeadStatusRequest struct {
	Status string `json:"status"`
}

// FinalizeAssignmentRequest is the admin payload to finalize a Pandit assignment.
type FinalizeAssignmentRequest struct {
	LeadID string `json:"lead_id"`
}

// ListJobLeads returns paginated job leads for the admin dashboard.
// @Summary List job leads
// @Description Fetch all job leads with optional filters.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param limit query int false "Page size (default 50)"
// @Param offset query int false "Page offset (default 0)"
// @Param status query string false "Filter by status"
// @Param source query string false "Filter by source"
// @Success 200 {object} response.DataResponse "Job leads list"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/leads [get]
func (h *Handler) ListJobLeads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	statusFilter := r.URL.Query().Get("status")
	sourceFilter := r.URL.Query().Get("source")

	limit := 50
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	query := `
		SELECT jl.id, jl.event_id, jl.pandit_id, jl.yajman_id, jl.source, jl.score,
		       jl.score_breakdown, jl.status, jl.platform_fee, jl.fee_status,
		       jl.admin_notes, jl.assigned_by, jl.created_at, jl.updated_at,
		       COALESCE(p.first_name, '') AS pandit_first, COALESCE(p.last_name, '') AS pandit_last,
		       COALESCE(y.first_name, '') AS yajman_first, COALESCE(y.last_name, '') AS yajman_last,
		       COALESCE(e.ceremony_type::text, '') AS ceremony_type,
		       COALESCE(e.address, '') AS event_address
		FROM job_leads jl
		LEFT JOIN users p ON p.id = jl.pandit_id
		LEFT JOIN users y ON y.id = jl.yajman_id
		LEFT JOIN events e ON e.id = jl.event_id
		WHERE jl.deleted_at IS NULL
	`
	var args []any
	argN := 1

	if statusFilter != "" {
		query += " AND jl.status = $" + strconv.Itoa(argN)
		args = append(args, statusFilter)
		argN++
	}
	if sourceFilter != "" {
		query += " AND jl.source = $" + strconv.Itoa(argN)
		args = append(args, sourceFilter)
		argN++
	}

	query += " ORDER BY jl.created_at DESC LIMIT $" + strconv.Itoa(argN) + " OFFSET $" + strconv.Itoa(argN+1)
	args = append(args, limit, offset)

	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		h.internal(w, r, "list job leads", err)
		return
	}
	defer rows.Close()

	leads := make([]map[string]any, 0)
	for rows.Next() {
		var id, eventID, panditID, yajmanID uuid.UUID
		var source, status, feeStatus, adminNotes, assignedBy string
		var score, platformFee float64
		var scoreBreakdown []byte
		var createdAt, updatedAt int64
		var panditFirst, panditLast, yajmanFirst, yajmanLast string
		var ceremonyType, eventAddress string

		if err := rows.Scan(
			&id, &eventID, &panditID, &yajmanID, &source, &score,
			&scoreBreakdown, &status, &platformFee, &feeStatus,
			&adminNotes, &assignedBy, &createdAt, &updatedAt,
			&panditFirst, &panditLast, &yajmanFirst, &yajmanLast,
			&ceremonyType, &eventAddress,
		); err != nil {
			h.internal(w, r, "scan job lead", err)
			return
		}

		leads = append(leads, map[string]any{
			"id":              id,
			"event_id":        eventID,
			"pandit_id":       panditID,
			"yajman_id":       yajmanID,
			"source":          source,
			"score":           score,
			"score_breakdown": string(scoreBreakdown),
			"status":          status,
			"platform_fee":    platformFee,
			"fee_status":      feeStatus,
			"admin_notes":     adminNotes,
			"assigned_by":     assignedBy,
			"created_at":      createdAt,
			"updated_at":      updatedAt,
			"pandit_name":     panditFirst + " " + panditLast,
			"yajman_name":     yajmanFirst + " " + yajmanLast,
			"ceremony_type":   ceremonyType,
			"event_address":   eventAddress,
		})
	}

	response.WriteData(w, http.StatusOK, leads)
}

// GetJobLead returns a single job lead by ID.
// @Summary Get job lead
// @Description Fetch a single job lead by ID.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param id path string true "Job Lead ID"
// @Success 200 {object} response.DataResponse "Job lead details"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Not found"
// @Router /api/v2/admin/leads/{id} [get]
func (h *Handler) GetJobLead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	var eventID, panditID, yajmanID uuid.UUID
	var source, status, feeStatus, adminNotes, assignedBy string
	var scoreVal, platformFee float64
	var scoreBreakdown []byte
	var createdAt, updatedAt int64

	err := h.pool.QueryRow(ctx, `
		SELECT event_id, pandit_id, yajman_id, source, score, score_breakdown,
		       status, platform_fee, fee_status, admin_notes, assigned_by, created_at, updated_at
		FROM job_leads WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(
		&eventID, &panditID, &yajmanID, &source, &scoreVal, &scoreBreakdown,
		&status, &platformFee, &feeStatus, &adminNotes, &assignedBy, &createdAt, &updatedAt,
	)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("lead not found", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{
		"id":              id,
		"event_id":        eventID,
		"pandit_id":       panditID,
		"yajman_id":       yajmanID,
		"source":          source,
		"score":           scoreVal,
		"score_breakdown": string(scoreBreakdown),
		"status":          status,
		"platform_fee":    platformFee,
		"fee_status":      feeStatus,
		"admin_notes":     adminNotes,
		"assigned_by":     assignedBy,
		"created_at":      createdAt,
		"updated_at":      updatedAt,
	})
}

// ManualAssign creates a job lead for a Pandit-Event pair via admin action.
// @Summary Manually assign Pandit
// @Description Admin manually assigns a Pandit to an event.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body ManualAssignRequest true "Assignment data"
// @Success 201 {object} response.DataResponse "Assignment created"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/leads/assign [post]
func (h *Handler) ManualAssign(w http.ResponseWriter, r *http.Request) {
	var req ManualAssignRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	panditID, err := uuid.Parse(req.PanditID)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid pandit_id", r.URL.Path))
		return
	}
	yajmanID, err := uuid.Parse(req.YajmanID)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid yajman_id", r.URL.Path))
		return
	}
	eventID, err := uuid.Parse(req.EventID)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid event_id", r.URL.Path))
		return
	}

	if h.assignmentEngine == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("assignment engine not configured", r.URL.Path))
		return
	}

	if err := h.assignmentEngine.ManualAssign(r.Context(), panditID, yajmanID, eventID, req.AdminNotes); err != nil {
		h.internal(w, r, "manual assign", err)
		return
	}

	// Fetch lead ID if it was successfully created/exists
	var leadID uuid.UUID
	_ = h.pool.QueryRow(r.Context(), "SELECT id FROM job_leads WHERE event_id = $1 AND pandit_id = $2 AND deleted_at IS NULL", eventID, panditID).Scan(&leadID)

	changedBy, _ := r.Context().Value(adminUsernameKey).(string)
	if changedBy == "" {
		changedBy = "system"
	}
	
	var leadIDVal *uuid.UUID
	if leadID != (uuid.UUID{}) {
		leadIDVal = &leadID
	}

	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO event_assignment_logs (lead_id, event_id, changed_by, from_status, to_status, notes)
		VALUES ($1, $2, $3, 'none', 'pushed', $4)
	`, leadIDVal, eventID, changedBy, req.AdminNotes)

	response.WriteData(w, http.StatusCreated, map[string]any{"status": "pushed"})
}

// UpdateLeadStatus updates the status of a job lead.
// @Summary Update lead status
// @Description Admin updates the status of a job lead.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Job Lead ID"
// @Param body body UpdateLeadStatusRequest true "Status update"
// @Success 200 {object} response.DataResponse "Updated"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/leads/{id}/status [put]
func (h *Handler) UpdateLeadStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	var req UpdateLeadStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	validStatuses := map[string]bool{"pending": true, "sent": true, "accepted": true, "declined": true, "expired": true, "fee_agreed": true}
	if !validStatuses[req.Status] {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid status, must be one of: pending, sent, accepted, declined, expired, fee_agreed", r.URL.Path))
		return
	}

	// Fetch current status and event ID before updating
	var fromStatus string
	var eventID uuid.UUID
	err := h.pool.QueryRow(ctx, "SELECT status, event_id FROM job_leads WHERE id = $1 AND deleted_at IS NULL", id).Scan(&fromStatus, &eventID)
	if err != nil {
		if err == pgx.ErrNoRows {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("lead not found", r.URL.Path))
		} else {
			h.internal(w, r, "fetch current lead status", err)
		}
		return
	}

	tag, err := h.pool.Exec(ctx, `
		UPDATE job_leads SET status = $2, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE id = $1 AND deleted_at IS NULL
	`, id, req.Status)
	if err != nil {
		h.internal(w, r, "update lead status", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("lead not found", r.URL.Path))
		return
	}

	changedBy, _ := ctx.Value(adminUsernameKey).(string)
	if changedBy == "" {
		changedBy = "system"
	}

	fromStatusLogged := fromStatus
	if fromStatusLogged == "sent" {
		fromStatusLogged = "pushed"
	}
	toStatusLogged := req.Status
	if toStatusLogged == "sent" {
		toStatusLogged = "pushed"
	}

	_, _ = h.pool.Exec(ctx, `
		INSERT INTO event_assignment_logs (lead_id, event_id, changed_by, from_status, to_status)
		VALUES ($1, $2, $3, $4, $5)
	`, id, eventID, changedBy, fromStatusLogged, toStatusLogged)

	response.WriteData(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *Handler) GetEventAssignmentLogs(w http.ResponseWriter, r *http.Request) {
	eventIDStr := chi.URLParam(r, "id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid event_id", r.URL.Path))
		return
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT l.id, COALESCE(l.lead_id::text, '') AS lead_id, l.changed_by,
		       COALESCE(au.role, 'system') AS role,
		       l.from_status, l.to_status, COALESCE(l.notes, '') AS notes, l.created_at,
		       COALESCE(u.first_name || ' ' || u.last_name, '') AS pandit_name
		FROM event_assignment_logs l
		LEFT JOIN admin_users au ON au.username = l.changed_by
		LEFT JOIN job_leads jl ON jl.id = l.lead_id
		LEFT JOIN users u ON u.id = jl.pandit_id
		WHERE l.event_id = $1
		ORDER BY l.created_at DESC
	`, eventID)
	if err != nil {
		h.internal(w, r, "get event assignment logs", err)
		return
	}
	defer rows.Close()

	logs := make([]map[string]any, 0)
	for rows.Next() {
		var id, leadID, changedBy, role, fromStatus, toStatus, notes, panditName string
		var createdAt int64
		if err := rows.Scan(&id, &leadID, &changedBy, &role, &fromStatus, &toStatus, &notes, &createdAt, &panditName); err != nil {
			h.internal(w, r, "scan event assignment log", err)
			return
		}
		logs = append(logs, map[string]any{
			"id":          id,
			"lead_id":     leadID,
			"changed_by":  changedBy,
			"role":        role,
			"from_status": fromStatus,
			"to_status":   toStatus,
			"notes":       notes,
			"created_at":  createdAt,
			"pandit_name": panditName,
		})
	}
	response.WriteData(w, http.StatusOK, logs)
}

// ListEventLeads returns all job leads for a specific event.
// @Summary List leads for event
// @Description Fetch all job leads for a specific event.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Param eventId path string true "Event ID"
// @Success 200 {object} response.DataResponse "Event leads"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/leads/event/{eventId} [get]
func (h *Handler) ListEventLeads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	eventIDStr := chi.URLParam(r, "eventId")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid event_id", r.URL.Path))
		return
	}

	rows, err := h.pool.Query(ctx, `
		WITH event_details AS (
			SELECT location, ceremony_type::text AS ceremony, address
			FROM events WHERE id = $1
		),
		nearest_city AS (
			SELECT c.id FROM cities c
			CROSS JOIN event_details ed
			WHERE c.is_active = true
			ORDER BY 
				CASE WHEN c.name ILIKE ed.address THEN 0 ELSE 1 END ASC,
				ST_Distance(c.location, ed.location) ASC
			LIMIT 1
		)
		SELECT jl.id, jl.pandit_id, jl.score, jl.status, jl.platform_fee, jl.fee_status,
		       jl.assigned_by, jl.created_at,
		       COALESCE(p.first_name, '') AS pandit_first, COALESCE(p.last_name, '') AS pandit_last,
		       COALESCE(p.phone, '') AS pandit_phone,
		       COALESCE(p.profile_photo_url, '') AS profile_photo_url,
		       COALESCE(p.email, '') AS pandit_email,
		       ST_Distance(p.location, ed.location) / 1000.0 AS distance_km,
		       COALESCE(pp.ceremony_specializations, '{}') AS ceremony_specializations,
		       COALESCE(pp.availability_status::text, 'available') AS availability_status,
		       COALESCE(pb.badge_tier::text, 'pandit_ji') AS badge_tier,
		       COALESCE(
		           (SELECT string_agg(c.name || ' (' || c.state || ')', ', ' ORDER BY c.name)
		            FROM pandit_service_cities psc
		            JOIN cities c ON c.id = psc.city_id
		            WHERE psc.user_id = p.id),
		           ''
		       ) AS service_cities,
		       CASE WHEN EXISTS (
		           SELECT 1 FROM unnest(pp.ceremony_specializations) s 
		           WHERE s ILIKE ed.ceremony OR replace(s, '_', '-') ILIKE replace(ed.ceremony, '_', '-')
		       ) THEN true ELSE false END AS is_specialization_match,
		       CASE WHEN EXISTS (
		           SELECT 1 FROM pandit_service_cities psc
		           WHERE psc.user_id = p.id AND psc.city_id = (SELECT id FROM nearest_city)
		       ) THEN true ELSE false END AS is_city_match
		FROM job_leads jl
		LEFT JOIN users p ON p.id = jl.pandit_id
		LEFT JOIN pandit_profiles pp ON pp.user_id = p.id AND pp.deleted_at IS NULL
		LEFT JOIN pandit_badges pb ON pb.pandit_id = p.id
		CROSS JOIN event_details ed
		WHERE jl.event_id = $1 AND jl.deleted_at IS NULL
		ORDER BY jl.score DESC
	`, eventID)
	if err != nil {
		h.internal(w, r, "list event leads", err)
		return
	}
	defer rows.Close()

	leads := make([]map[string]any, 0)
	for rows.Next() {
		var id, panditID uuid.UUID
		var scoreVal, platformFee float64
		var status, feeStatus, assignedBy, panditFirst, panditLast, panditPhone string
		var profilePhotoURL, panditEmail, serviceCities, availabilityStatus, badgeTier string
		var distanceKM *float64
		var ceremonySpecializations []string
		var isSpecializationMatch, isCityMatch bool
		var createdAt int64

		if err := rows.Scan(
			&id, &panditID, &scoreVal, &status, &platformFee, &feeStatus,
			&assignedBy, &createdAt, &panditFirst, &panditLast, &panditPhone,
			&profilePhotoURL, &panditEmail, &distanceKM, &ceremonySpecializations,
			&availabilityStatus, &badgeTier, &serviceCities,
			&isSpecializationMatch, &isCityMatch,
		); err != nil {
			h.internal(w, r, "scan event lead", err)
			return
		}

		var dist float64 = 0.0
		if distanceKM != nil {
			dist = math.Round(*distanceKM*10) / 10
		}

		leads = append(leads, map[string]any{
			"id":                       id,
			"pandit_id":                panditID,
			"pandit_name":              panditFirst + " " + panditLast,
			"pandit_phone":             panditPhone,
			"pandit_email":             panditEmail,
			"profile_photo_url":        profilePhotoURL,
			"score":                    scoreVal,
			"status":                   status,
			"platform_fee":             platformFee,
			"fee_status":               feeStatus,
			"assigned_by":              assignedBy,
			"created_at":               createdAt,
			"distance_km":              dist,
			"ceremony_specializations": ceremonySpecializations,
			"availability_status":      availabilityStatus,
			"badge_tier":               badgeTier,
			"service_cities":           serviceCities,
			"is_specialization_match":  isSpecializationMatch,
			"is_city_match":            isCityMatch,
		})
	}

	response.WriteData(w, http.StatusOK, leads)
}

// AssignmentDashboard returns overview stats for the assignment engine.
// @Summary Assignment dashboard
// @Description Overview metrics for assignment engine and leads.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse "Dashboard metrics"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/assignment/dashboard [get]
func (h *Handler) AssignmentDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	count := func(query string) int {
		var v int
		if err := h.pool.QueryRow(ctx, query).Scan(&v); err != nil {
			h.logger.Warn("dashboard count failed", "query", query, "error", err)
		}
		return v
	}

	var totalFeeCollected float64
	_ = h.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(platform_fee), 0.00) FROM job_leads
		WHERE fee_status = 'paid' AND deleted_at IS NULL
	`).Scan(&totalFeeCollected)

	response.WriteData(w, http.StatusOK, map[string]any{
		"total_leads":         count("SELECT COUNT(*) FROM job_leads WHERE deleted_at IS NULL"),
		"pending_leads":       count("SELECT COUNT(*) FROM job_leads WHERE status = 'pending' AND deleted_at IS NULL"),
		"sent_leads":          count("SELECT COUNT(*) FROM job_leads WHERE status = 'sent' AND deleted_at IS NULL"),
		"accepted_leads":      count("SELECT COUNT(*) FROM job_leads WHERE status = 'accepted' AND deleted_at IS NULL"),
		"declined_leads":      count("SELECT COUNT(*) FROM job_leads WHERE status = 'declined' AND deleted_at IS NULL"),
		"expired_leads":       count("SELECT COUNT(*) FROM job_leads WHERE status = 'expired' AND deleted_at IS NULL"),
		"auto_assigned":       count("SELECT COUNT(*) FROM job_leads WHERE assigned_by = 'auto' AND deleted_at IS NULL"),
		"manual_assigned":     count("SELECT COUNT(*) FROM job_leads WHERE assigned_by = 'admin' AND deleted_at IS NULL"),
		"total_fee_collected": totalFeeCollected,
		"currency":            "INR",
	})
}

// FinalizeAssignment finalizes a lead, matching the Pandit and Yajman and setting event to Booked.
// @Summary Finalize lead assignment
// @Description Transition lead to accepted, create match and conversation workspace, and log the platform fee.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body FinalizeAssignmentRequest true "Finalize data"
// @Success 200 {object} response.DataResponse "Assignment finalized"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/leads/finalize [post]
func (h *Handler) FinalizeAssignment(w http.ResponseWriter, r *http.Request) {
	var req FinalizeAssignmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	leadID, err := uuid.Parse(req.LeadID)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid lead_id", r.URL.Path))
		return
	}

	if h.assignmentEngine == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("assignment engine not configured", r.URL.Path))
		return
	}

	// 1. Fetch current status and event ID before finalization
	var currentStatus string
	var eventID uuid.UUID
	err = h.pool.QueryRow(r.Context(), "SELECT status, event_id FROM job_leads WHERE id = $1 AND deleted_at IS NULL", leadID).Scan(&currentStatus, &eventID)
	if err != nil {
		if err == pgx.ErrNoRows {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("lead not found", r.URL.Path))
		} else {
			h.internal(w, r, "fetch lead details", err)
		}
		return
	}

	// 2. Perform finalization (marks event as Booked, creates match/conversation)
	if err := h.assignmentEngine.FinalizeAssignment(r.Context(), leadID); err != nil {
		h.internal(w, r, "finalize assignment", err)
		return
	}

	// 3. Write transition log
	changedBy, _ := r.Context().Value(adminUsernameKey).(string)
	if changedBy == "" {
		changedBy = "system"
	}

	fromStatusLogged := currentStatus
	if fromStatusLogged == "sent" {
		fromStatusLogged = "pushed"
	}

	_, _ = h.pool.Exec(r.Context(), `
		INSERT INTO event_assignment_logs (lead_id, event_id, changed_by, from_status, to_status, notes)
		VALUES ($1, $2, $3, $4, 'assigned', '')
	`, leadID, eventID, changedBy, fromStatusLogged)

	response.WriteData(w, http.StatusOK, map[string]any{"status": "finalized"})
}

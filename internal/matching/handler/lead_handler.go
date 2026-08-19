package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"

	"github.com/medha/backend/internal/admin/assignment"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// LeadHandler handles Pandit-facing job lead endpoints.
type LeadHandler struct {
	pool   *pgxpool.Pool
	engine *assignment.Engine
	logger *slog.Logger
}

// NewLeadHandler creates a new LeadHandler.
func NewLeadHandler(pool *pgxpool.Pool, engine *assignment.Engine, logger *slog.Logger) *LeadHandler {
	return &LeadHandler{pool: pool, engine: engine, logger: logger}
}

// ListMyLeads returns job leads for the authenticated Pandit.
// @Summary List my leads
// @Description Pandit views their incoming job leads.
// @Tags lead
// @Security BearerAuth
// @Produce json
// @Success 200 {object} response.DataResponse "Leads list"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/leads/mine [get]
func (h *LeadHandler) ListMyLeads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	panditID, err := middleware.UserIDFromContext(ctx)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	rows, err := h.pool.Query(ctx, `
		SELECT jl.id, jl.event_id, jl.yajman_id, jl.source, jl.score, jl.status,
		       jl.platform_fee, jl.created_at,
		       COALESCE(y.first_name, '') AS yajman_first, COALESCE(y.last_name, '') AS yajman_last,
		       COALESCE(e.ceremony_type::text, '') AS ceremony_type,
		       COALESCE(e.address, '') AS event_address,
		       e.event_date
		FROM job_leads jl
		LEFT JOIN users y ON y.id = jl.yajman_id
		JOIN events e ON e.id = jl.event_id
		WHERE jl.pandit_id = $1
		  AND jl.deleted_at IS NULL
		  AND e.deleted_at IS NULL
		  AND e.status IN ('Active', 'Pending', 'Pushed')
		ORDER BY jl.created_at DESC
		LIMIT 50
	`, panditID)
	if err != nil {
		h.logger.Error("list my leads failed", "error", err, "pandit_id", panditID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("failed to fetch leads", r.URL.Path))
		return
	}
	defer rows.Close()

	leads := make([]map[string]any, 0)
	for rows.Next() {
		var id, eventID, yajmanID uuid.UUID
		var source, status, yajmanFirst, yajmanLast, ceremonyType, eventAddress string
		var scoreVal, platformFee float64
		var createdAt, eventDate int64

		if err := rows.Scan(
			&id, &eventID, &yajmanID, &source, &scoreVal, &status,
			&platformFee, &createdAt,
			&yajmanFirst, &yajmanLast,
			&ceremonyType, &eventAddress, &eventDate,
		); err != nil {
			h.logger.Error("scan lead failed", "error", err)
			continue
		}

		var respYajmanName string
		var respYajmanID *uuid.UUID = nil
		// ponytail: only disclose Yajman identity after Admin finalizes (accepted).
		// "interested" is a self-service Pandit tap — no fee paid yet, details stay masked.
		if status == "accepted" {
			respYajmanName = yajmanFirst + " " + yajmanLast
			respYajmanID = &yajmanID
		} else {
			respYajmanName = "Yajman"
		}

		displayStatus := status
		if status == "sent" {
			displayStatus = "Active"
		}

		leadItem := map[string]any{
			"id":            id,
			"event_id":      eventID,
			"source":        source,
			"score":         scoreVal,
			"status":        displayStatus,
			"lead_status":   displayStatus,
			"platform_fee":  platformFee,
			"ceremony_type": ceremonyType,
			"event_address": eventAddress,
			"event_date":    eventDate,
			"created_at":    createdAt,
			"yajman_name":   respYajmanName,
		}
		if respYajmanID != nil {
			leadItem["yajman_id"] = *respYajmanID
		}
		leads = append(leads, leadItem)
	}

	response.WriteData(w, http.StatusOK, leads)
}

// AcceptLead marks a lead as accepted (fee charged).
// @Summary Accept a job lead
// @Description Pandit accepts a job lead. Platform fee is charged.
// @Tags lead
// @Security BearerAuth
// @Produce json
// @Param id path string true "Lead ID"
// @Success 200 {object} response.DataResponse "Lead accepted"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/leads/{id}/accept [put]
func (h *LeadHandler) AcceptLead(w http.ResponseWriter, r *http.Request) {
	panditID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	leadIDStr := chi.URLParam(r, "id")
	leadID, err := uuid.Parse(leadIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid lead id", r.URL.Path))
		return
	}

	if err := h.engine.AcceptLead(r.Context(), leadID, panditID); err != nil {
		h.logger.Error("accept lead failed", "error", err, "lead_id", leadID, "pandit_id", panditID)
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(err.Error(), r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{"status": "interested"})
}

// DeclineLead marks a lead as declined (no fee).
// @Summary Decline a job lead
// @Description Pandit declines a job lead. No fee is charged.
// @Tags lead
// @Security BearerAuth
// @Produce json
// @Param id path string true "Lead ID"
// @Success 200 {object} response.DataResponse "Lead declined"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/leads/{id}/decline [put]
func (h *LeadHandler) DeclineLead(w http.ResponseWriter, r *http.Request) {
	panditID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	leadIDStr := chi.URLParam(r, "id")
	leadID, err := uuid.Parse(leadIDStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid lead id", r.URL.Path))
		return
	}

	if err := h.engine.DeclineLead(r.Context(), leadID, panditID); err != nil {
		h.logger.Error("decline lead failed", "error", err, "lead_id", leadID, "pandit_id", panditID)
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(err.Error(), r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{"status": "declined"})
}

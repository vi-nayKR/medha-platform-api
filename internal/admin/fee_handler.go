package admin

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// FeeConfigResponse represents the JSON output for a fee configuration.
type FeeConfigResponse struct {
	ID           uuid.UUID `json:"id"`
	CeremonyType string    `json:"ceremony_type"`
	FeeAmount    float64   `json:"fee_amount"`
	FeeType      string    `json:"fee_type"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    int64     `json:"created_at"`
	UpdatedAt    int64     `json:"updated_at"`
}

// CreateFeeConfigRequest is the admin payload to create a fee config.
type CreateFeeConfigRequest struct {
	CeremonyType string  `json:"ceremony_type"`
	FeeAmount    float64 `json:"fee_amount"`
	FeeType      string  `json:"fee_type"`
}

// UpdateFeeConfigRequest is the admin payload to update a fee config.
type UpdateFeeConfigRequest struct {
	CeremonyType string  `json:"ceremony_type"`
	FeeAmount    float64 `json:"fee_amount"`
	FeeType      string  `json:"fee_type"`
	IsActive     bool    `json:"is_active"`
}

// ListFeeConfigs returns all fee configurations.
// @Summary List fee configurations
// @Description Fetch all platform fee configurations.
// @Tags admin
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.DataResponse{data=[]FeeConfigResponse} "Fee configs"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/fees [get]
func (h *Handler) ListFeeConfigs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.pool.Query(ctx, `
		SELECT id, ceremony_type, fee_amount, fee_type, is_active, created_at, updated_at
		FROM platform_fee_config
		ORDER BY created_at DESC
	`)
	if err != nil {
		h.internal(w, r, "list fee configs", err)
		return
	}
	defer rows.Close()

	configs := make([]FeeConfigResponse, 0)
	for rows.Next() {
		var c FeeConfigResponse
		if err := rows.Scan(&c.ID, &c.CeremonyType, &c.FeeAmount, &c.FeeType, &c.IsActive, &c.CreatedAt, &c.UpdatedAt); err != nil {
			h.internal(w, r, "scan fee config", err)
			return
		}
		configs = append(configs, c)
	}

	response.WriteData(w, http.StatusOK, configs)
}

// CreateFeeConfig creates a new fee configuration.
// @Summary Create fee config
// @Description Create a new platform fee configuration.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body CreateFeeConfigRequest true "Fee config data"
// @Success 201 {object} response.DataResponse "Fee config created"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router /api/v2/admin/fees [post]
func (h *Handler) CreateFeeConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateFeeConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.FeeAmount < 0 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("fee_amount must be non-negative", r.URL.Path))
		return
	}
	if req.CeremonyType == "" {
		req.CeremonyType = "*"
	}
	if req.FeeType == "" {
		req.FeeType = "fixed"
	}
	if req.FeeType != "fixed" && req.FeeType != "percentage" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("fee_type must be 'fixed' or 'percentage'", r.URL.Path))
		return
	}

	id := uuid.New()
	now := time.Now().Unix()

	_, err := h.pool.Exec(ctx, `
		INSERT INTO platform_fee_config (id, ceremony_type, fee_amount, fee_type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, id, req.CeremonyType, req.FeeAmount, req.FeeType, now)
	if err != nil {
		h.internal(w, r, "create fee config", err)
		return
	}

	response.WriteData(w, http.StatusCreated, map[string]any{"id": id})
}

// UpdateFeeConfig updates an existing fee configuration.
// @Summary Update fee config
// @Description Update an existing platform fee configuration.
// @Tags admin
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Fee Config ID"
// @Param body body UpdateFeeConfigRequest true "Update data"
// @Success 200 {object} response.DataResponse "Updated"
// @Failure 400 {object} apierrors.ProblemDetail "Bad request"
// @Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure 404 {object} apierrors.ProblemDetail "Not found"
// @Router /api/v2/admin/fees/{id} [put]
func (h *Handler) UpdateFeeConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid id", r.URL.Path))
		return
	}

	var req UpdateFeeConfigRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.FeeAmount < 0 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("fee_amount must be non-negative", r.URL.Path))
		return
	}

	now := time.Now().Unix()
	tag, err := h.pool.Exec(ctx, `
		UPDATE platform_fee_config
		SET ceremony_type = $2, fee_amount = $3, fee_type = $4, is_active = $5, updated_at = $6
		WHERE id = $1
	`, id, req.CeremonyType, req.FeeAmount, req.FeeType, req.IsActive, now)
	if err != nil {
		h.internal(w, r, "update fee config", err)
		return
	}
	if tag.RowsAffected() == 0 {
		apierrors.WriteProblemDetail(w, apierrors.NotFound("fee config not found", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]any{"status": "updated"})
}

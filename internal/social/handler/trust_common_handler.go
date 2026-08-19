package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/social/domain"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// BadgeResponse represents a pandit's badge in API responses.
type BadgeResponse struct {
	PanditID            uuid.UUID `json:"pandit_id"`
	BadgeTier           string    `json:"badge_tier"`
	BadgeIcon           string    `json:"badge_icon"`
	BadgeLabel          string    `json:"badge_label"`
	CeremoniesCompleted int       `json:"ceremonies_completed"`
	AverageRating       float64   `json:"average_rating"`
	TotalFeedbackCount  int       `json:"total_feedback_count"`
}

// GetBadge handles GET /api/v2/pandit/{id}/badge.
func (h *TrustHandler) GetBadge(w http.ResponseWriter, r *http.Request) {
	panditID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The pandit ID provided is not valid.", r.URL.Path))
		return
	}

	badge, err := h.trustService.GetBadge(r.Context(), panditID)
	if err != nil {
		if errors.Is(err, domain.ErrBadgeNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This user's badge could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get badge failed", "error", err, "pandit_id", panditID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load badge details. Please try again later.", r.URL.Path))
		return
	}

	icon, label := badge.BadgeTier.Display()
	response.WriteData(w, http.StatusOK, BadgeResponse{
		PanditID:            badge.PanditID,
		BadgeTier:           badge.BadgeTier.String(),
		BadgeIcon:           icon,
		BadgeLabel:          label,
		CeremoniesCompleted: badge.CeremoniesCompleted,
		AverageRating:       badge.AverageRating,
		TotalFeedbackCount:  badge.TotalFeedbackCount,
	})
}

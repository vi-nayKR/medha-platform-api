package handler

import (
	"net/http"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

var (
	_ = response.DataResponse{}
	_ = apierrors.ProblemDetail{}
)

// SubmitFeedbackV2 handles POST /api/v2/match/{id}/feedback.
//
// @Summary      Submit feedback for a match
// @Description  Allows a yajman to rate and comment on a pandit's service after match completion. Returns updated badge info.
// @Tags         trust-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path string true "Match UUID"
// @Param body body SubmitFeedbackRequest true "Rating and comment"
// @Success      201 {object} response.DataResponse{data=interface{}} "Feedback submitted"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request — match not completed"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not the yajman"
// @Failure      404 {object} apierrors.ProblemDetail "Match not found"
// @Failure      409 {object} apierrors.ProblemDetail "Feedback already submitted"
// @Router       /api/v2/match/{id}/feedback [post]
func (h *TrustHandler) SubmitFeedbackV2(w http.ResponseWriter, r *http.Request) {
	h.SubmitFeedback(w, r)
}

// GetBadgeV2 handles GET /api/v2/pandit/{id}/badge.
//
// @Summary      Get pandit badge and reputation
// @Description  Returns the current badge tier, average rating, and ceremony counts for a pandit.
// @Tags         trust-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Pandit User UUID"
// @Success      200 {object} response.DataResponse{data=BadgeResponse} "Badge data"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Badge not found"
// @Router       /api/v2/pandit/{id}/badge [get]
func (h *TrustHandler) GetBadgeV2(w http.ResponseWriter, r *http.Request) {
	h.GetBadge(w, r)
}


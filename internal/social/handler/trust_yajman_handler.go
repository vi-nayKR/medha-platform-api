package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	matchingdomain "github.com/medha/backend/internal/matching/domain"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/server/middleware"
	"github.com/medha/backend/internal/social/domain"
	socialservice "github.com/medha/backend/internal/social/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// SubmitFeedbackRequest is the request body for POST /api/v2/match/{id}/feedback.
type SubmitFeedbackRequest struct {
	Rating  int    `json:"rating" validate:"required,min=1,max=5"`
	Comment string `json:"comment"`
}

// FeedbackResponse represents feedback in API responses.
type FeedbackResponse struct {
	ID        uuid.UUID `json:"id"`
	MatchID   uuid.UUID `json:"match_id"`
	YajmanID  uuid.UUID `json:"yajman_id"`
	PanditID  uuid.UUID `json:"pandit_id"`
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	CreatedAt string    `json:"created_at"`
}

// SubmitFeedback handles POST /api/v2/match/{id}/feedback.
func (h *TrustHandler) SubmitFeedback(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	matchID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The match ID provided is not valid.", r.URL.Path))
		return
	}

	var req SubmitFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid feedback details.", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please provide a rating between 1 and 5.",
			r.URL.Path,
		))
		return
	}

	feedback, badge, err := h.trustService.SubmitFeedback(r.Context(), socialservice.SubmitFeedbackParams{
		MatchID:  matchID,
		YajmanID: userID,
		Rating:   req.Rating,
		Comment:  req.Comment,
	})
	if err != nil {
		if errors.Is(err, matchingdomain.ErrMatchNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This match could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotYajmanInMatch) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"Only the user who booked the service can submit feedback.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrMatchNotCompleted) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Feedback can only be submitted after the service is completed.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrFeedbackAlreadyExists) {
			apierrors.WriteProblemDetail(w, apierrors.Conflict(
				"You have already submitted feedback for this match.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("submit feedback failed", "error", err, "match_id", matchID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to submit your feedback. Please try again later.", r.URL.Path))
		return
	}

	result := map[string]any{
		"feedback": FeedbackResponse{
			ID:        feedback.ID,
			MatchID:   feedback.MatchID,
			YajmanID:  feedback.YajmanID,
			PanditID:  feedback.PanditID,
			Rating:    feedback.Rating,
			Comment:   feedback.Comment,
			CreatedAt: epoch.ToTime(feedback.CreatedAt).Format("2006-01-02T15:04:05Z07:00"),
		},
	}
	if badge != nil {
		icon, label := badge.BadgeTier.Display()
		result["badge"] = BadgeResponse{
			PanditID:            badge.PanditID,
			BadgeTier:           badge.BadgeTier.String(),
			BadgeIcon:           icon,
			BadgeLabel:          label,
			CeremoniesCompleted: badge.CeremoniesCompleted,
			AverageRating:       badge.AverageRating,
			TotalFeedbackCount:  badge.TotalFeedbackCount,
		}
	}

	response.WriteData(w, http.StatusCreated, result)
}

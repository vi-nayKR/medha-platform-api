package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	eventdomain "github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/interest/domain"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// RespondToInterestRequest is the request body for PUT /api/v2/interest/{id}/respond.
type RespondToInterestRequest struct {
	Accept bool `json:"accept"`
}

// RespondToInterest handles PUT /api/v2/interest/{id}/respond.
func (h *InterestHandler) RespondToInterest(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	interestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid interest ID", r.URL.Path))
		return
	}

	var req RespondToInterestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid request body", r.URL.Path))
		return
	}

	interest, err := h.interestService.RespondToInterest(r.Context(), interestID, userID, req.Accept)
	if err != nil {
		if errors.Is(err, domain.ErrInterestNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This interest could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotEventOwner) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You can only respond to interests for your own events.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInterestAlreadyResponded) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusConflict,
				"https://medha.app/errors/conflict",
				"Conflict",
				"This interest has already been resolved.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("respond to interest failed", "error", err, "interest_id", interestID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to respond to interest. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toInterestResponse(interest))
}

// ListEventInterests handles GET /api/v2/event/{id}/interest.
func (h *InterestHandler) ListEventInterests(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	eventID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid event ID", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	interests, nextCursor, err := h.interestService.ListEventInterests(r.Context(), eventID, userID, cursor, limit)
	if err != nil {
		if errors.Is(err, eventdomain.ErrEventNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The event could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, eventdomain.ErrEventNotOwned) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You can only view interests for your own events.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("list event interests failed", "error", err, "event_id", eventID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load interests for this event. Please try again later.", r.URL.Path))
		return
	}

	dtos := make([]InterestDetailResponse, len(interests))
	for i, it := range interests {
		dtos[i] = toInterestDetailResponse(it)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, dtos, cursorPtr)
}

// AcceptInterest handles PUT /api/v2/interest/{id}/accept.
func (h *InterestHandler) AcceptInterest(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	interestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid interest ID", r.URL.Path))
		return
	}

	interest, err := h.interestService.AcceptInterest(r.Context(), interestID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrInterestNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This interest could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotEventOwner) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You can only accept interests for your own events.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInterestAlreadyResponded) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusConflict,
				"https://medha.app/errors/conflict",
				"Conflict",
				"This interest has already been resolved or accepted.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("accept interest failed", "error", err, "interest_id", interestID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to accept interest. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toInterestResponse(interest))
}

// ConfirmBooking handles PUT /api/v2/interest/{id}/book.
func (h *InterestHandler) ConfirmBooking(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	interestID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid interest ID", r.URL.Path))
		return
	}

	interest, err := h.interestService.ConfirmBooking(r.Context(), interestID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrInterestNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This interest could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotEventOwner) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You can only confirm bookings for your own events.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInterestAlreadyResponded) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusConflict,
				"https://medha.app/errors/conflict",
				"Conflict",
				"This interest has already been resolved, rejected, or booked.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("confirm booking failed", "error", err, "interest_id", interestID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to confirm booking. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toInterestResponse(interest))
}

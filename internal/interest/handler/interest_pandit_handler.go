package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	eventdomain "github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/interest/domain"
	interestservice "github.com/medha/backend/internal/interest/service"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// ExpressInterestRequest is the request body for POST /api/v2/interest.
type ExpressInterestRequest struct {
	EventID string `json:"event_id" validate:"required,uuid"`
	Message string `json:"message"`
}

// ExpressInterest handles POST /api/v2/interest.
func (h *InterestHandler) ExpressInterest(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req ExpressInterestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid request body", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid event ID format", r.URL.Path))
		return
	}

	eventID, _ := uuid.Parse(req.EventID)

	interest, err := h.interestService.ExpressInterest(r.Context(), interestservice.ExpressInterestParams{
		PanditID: userID,
		EventID:  eventID,
		Message:  req.Message,
	})
	if err != nil {
		if errors.Is(err, eventdomain.ErrEventNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The event could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrInterestAlreadyExists) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusConflict,
				"https://medha.app/errors/already-expressed-interest",
				"Conflict",
				"You have already expressed interest in this event.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrEventNotAcceptingInterests) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("Cannot express interest in this event in its current status.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrCannotInterestOwnEvent) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("You cannot express interest in your own event.", r.URL.Path))
			return
		}
		h.logger.Error("express interest failed", "error", err, "pandit_id", userID, "event_id", eventID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to express interest. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusCreated, toInterestResponse(interest))
}

// WithdrawInterest handles PUT /api/v2/interest/{id}/withdraw.
func (h *InterestHandler) WithdrawInterest(w http.ResponseWriter, r *http.Request) {
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

	interest, err := h.interestService.WithdrawInterest(r.Context(), interestID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrInterestNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This interest could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrNotInterestOwner) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You are not authorized to withdraw this interest.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInterestAlreadyResponded) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusConflict,
				"https://medha.app/errors/conflict",
				"Conflict",
				"This interest has already been processed or cannot be withdrawn at this stage.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("withdraw interest failed", "error", err, "interest_id", interestID, "pandit_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to withdraw interest. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toInterestResponse(interest))
}

// ListMyInterests handles GET /api/v2/interest/mine.
func (h *InterestHandler) ListMyInterests(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	interests, nextCursor, err := h.interestService.ListMyInterests(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list my interests failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load your interests. Please try again later.", r.URL.Path))
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

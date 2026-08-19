package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/event/domain"
	eventservice "github.com/medha/backend/internal/event/service"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// --- Request/Response DTOs ---

// CreateEventRequest is the request body for POST /api/v2/event.
type CreateEventRequest struct {
	CeremonyType              string  `json:"ceremony_type" validate:"required" example:"custom"`
	CustomCeremonyName        string  `json:"custom_ceremony_name,omitempty" example:"Gudli Pooja"`
	CustomCeremonyDescription string  `json:"custom_ceremony_description,omitempty" example:"A family ritual performed before the main ceremony; include samagri, preferred vidhi, and regional custom notes here."`
	EventDate                 string  `json:"event_date"    validate:"required" example:"2026-06-15"`
	Latitude                  float64 `json:"latitude"      validate:"required,min=-90,max=90" example:"12.9716"`
	Longitude                 float64 `json:"longitude"     validate:"required,min=-180,max=180" example:"77.5946"`
	Address                   string  `json:"address"       validate:"required" example:"12 MG Road, Bengaluru"`
	Description               string  `json:"description" example:"Please arrive by 7 AM. Samagri will be arranged by the family."`
}

// UpdateEventRequest is the request body for PUT /api/v2/event/{id}.
type UpdateEventRequest struct {
	CeremonyType              *string  `json:"ceremony_type" example:"custom"`
	CustomCeremonyName        *string  `json:"custom_ceremony_name,omitempty" example:"Gudli Pooja"`
	CustomCeremonyDescription *string  `json:"custom_ceremony_description,omitempty" example:"Updated ritual notes or vidhi details."`
	EventDate                 *string  `json:"event_date" example:"2026-06-16"`
	Latitude                  *float64 `json:"latitude" example:"12.9716"`
	Longitude                 *float64 `json:"longitude" example:"77.5946"`
	Address                   *string  `json:"address" example:"12 MG Road, Bengaluru"`
	Description               *string  `json:"description" example:"Updated logistics note."`
}

// CreateEvent handles POST /api/v2/event.
func (h *EventHandler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("Please log in to continue.", r.URL.Path))
		return
	}

	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid event details.", r.URL.Path))
		return
	}

	req.CeremonyType = NormalizeCeremonyType(req.CeremonyType)

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please fill in all required fields: ceremony type, event date, location, and address.",
			r.URL.Path,
		))
		return
	}

	// Parse event date
	eventDate, err := time.Parse("2006-01-02", req.EventDate)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please enter a valid event date (e.g. 2026-05-15).",
			r.URL.Path,
		))
		return
	}

	event, err := h.eventService.CreateEvent(r.Context(), eventservice.CreateEventParams{
		YajmanID:                  userID,
		CeremonyType:              req.CeremonyType,
		CustomCeremonyName:        req.CustomCeremonyName,
		CustomCeremonyDescription: req.CustomCeremonyDescription,
		EventDate:                 eventDate,
		Latitude:                  req.Latitude,
		Longitude:                 req.Longitude,
		Address:                   req.Address,
		Description:               req.Description,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCeremonyType) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"The ceremony type provided is not valid. Please select from the available options.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrEventDateInPast) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"The event date must be set in the future. Please choose an upcoming date.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInvalidCustomCeremony) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Custom ceremonies require custom_ceremony_name (2-120 characters) and custom_ceremony_description (up to 1000 characters).",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("create event failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to create the event. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusCreated, h.toEventResponse(r.Context(), event))
}

// UpdateEvent handles PUT /api/v2/event/{id}.
func (h *EventHandler) UpdateEvent(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("invalid request body", r.URL.Path))
		return
	}

	if req.CeremonyType != nil && *req.CeremonyType != "" {
		normalized := NormalizeCeremonyType(*req.CeremonyType)
		req.CeremonyType = &normalized
	}

	var eventDate *time.Time
	if req.EventDate != nil && *req.EventDate != "" {
		parsedDate, err := time.Parse("2006-01-02", *req.EventDate)
		if err != nil {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"event_date must be in YYYY-MM-DD format",
				r.URL.Path,
			))
			return
		}
		eventDate = &parsedDate
	}

	event, err := h.eventService.UpdateEvent(r.Context(), eventservice.UpdateEventParams{
		EventID:                   eventID,
		YajmanID:                  userID,
		CeremonyType:              req.CeremonyType,
		CustomCeremonyName:        req.CustomCeremonyName,
		CustomCeremonyDescription: req.CustomCeremonyDescription,
		EventDate:                 eventDate,
		Latitude:                  req.Latitude,
		Longitude:                 req.Longitude,
		Address:                   req.Address,
		Description:               req.Description,
	})
	if err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The requested event could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrEventNotOwned) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You are not authorized to modify this event.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInvalidCeremonyType) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"The ceremony type provided is not valid. Please select from the available options.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrEventDateInPast) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"The event date must be set in the future. Please choose an upcoming date.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrEventNotActive) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"This event is no longer accepting changes.",
				r.URL.Path,
			))
			return
		}
		if errors.Is(err, domain.ErrInvalidCustomCeremony) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Custom ceremonies require custom_ceremony_name (2-120 characters) and custom_ceremony_description (up to 1000 characters).",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("update event failed", "error", err, "event_id", eventID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to update the event. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, h.toEventResponse(r.Context(), event))
}

// CancelEvent handles DELETE /api/v2/event/{id}.
func (h *EventHandler) CancelEvent(w http.ResponseWriter, r *http.Request) {
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

	if err := h.eventService.CancelEvent(r.Context(), eventID, userID); err != nil {
		if errors.Is(err, domain.ErrEventNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This event could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrEventNotOwned) {
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"You can only cancel your own events.",
				r.URL.Path,
			))
			return
		}
		h.logger.Error("cancel event failed", "error", err, "event_id", eventID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to cancel the event. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "event cancelled"})
}

// ListMyEvents handles GET /api/v2/event/mine.
func (h *EventHandler) ListMyEvents(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	events, nextCursor, err := h.eventService.ListMyEvents(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list my events failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load your events. Please try again later.", r.URL.Path))
		return
	}

	eventResponses := make([]EventResponse, len(events))
	for i, e := range events {
		eventResponses[i] = h.toEventResponse(r.Context(), e)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, eventResponses, cursorPtr)
}

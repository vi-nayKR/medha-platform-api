package handler

import (
	"net/http"
	"strings"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// Keep swag-referenced imports used.
var (
	_ = response.DataResponse{}
	_ = apierrors.ProblemDetail{}
)

// V2 Swagger Annotations for Event Endpoints
// These are thin wrappers around the existing EventHandler methods that simply
// provide V2 swagger annotations. The actual handler logic is unchanged.

// CreateEventV2 handles POST /api/v2/event.
//
// @Summary      Create an event
// @Description  Creates a new ceremony event at the given location. Use ceremony_type="custom" with custom_ceremony_name and custom_ceremony_description for rituals not present in the catalog, such as Gudli Pooja.
// @Tags         event-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body CreateEventRequest true "Event details"
// @Success      201 {object} response.DataResponse{data=EventResponse} "Event created"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/event [post]
func (h *EventHandler) CreateEventV2(w http.ResponseWriter, r *http.Request) {
	h.CreateEvent(w, r)
}

// GetEventV2 handles GET /api/v2/event/{id}.
//
// @Summary      Get event by ID
// @Description  Returns event details by UUID.
// @Tags         event-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Event UUID"
// @Success      200 {object} response.DataResponse{data=EventResponse} "Event details"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid event ID"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Event not found"
// @Router       /api/v2/event/{id} [get]
func (h *EventHandler) GetEventV2(w http.ResponseWriter, r *http.Request) {
	h.GetEvent(w, r)
}

// UpdateEventV2 handles PUT /api/v2/event/{id}.
//
// @Summary      Update event
// @Description  Updates an existing event. Only the event owner can update it. All fields are optional.
// @Tags         event-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Event UUID"
// @Param body body UpdateEventRequest true "Fields to update"
// @Success      200 {object} response.DataResponse{data=EventResponse} "Event updated"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not event owner"
// @Failure      404 {object} apierrors.ProblemDetail "Event not found"
// @Router       /api/v2/event/{id} [put]
func (h *EventHandler) UpdateEventV2(w http.ResponseWriter, r *http.Request) {
	h.UpdateEvent(w, r)
}

// CancelEventV2 handles DELETE /api/v2/event/{id}.
//
// @Summary      Cancel event
// @Description  Cancels an existing event. Only the event owner can cancel it.
// @Tags         event-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Event UUID"
// @Success      200 {object} response.DataResponse{data=interface{}} "Event cancelled"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — not event owner"
// @Failure      404 {object} apierrors.ProblemDetail "Event not found"
// @Router       /api/v2/event/{id} [delete]
func (h *EventHandler) CancelEventV2(w http.ResponseWriter, r *http.Request) {
	h.CancelEvent(w, r)
}

// ListMyEventsV2 handles GET /api/v2/event/mine.
//
// @Summary      List my events
// @Description  Returns paginated list of events created by the authenticated user. Supports cursor-based pagination.
// @Tags         event-v2
// @Produce      json
// @Security     BearerAuth
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]EventResponse} "My events"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/event/mine [get]
func (h *EventHandler) ListMyEventsV2(w http.ResponseWriter, r *http.Request) {
	h.ListMyEvents(w, r)
}

// ListNearbyEventsV2 handles GET /api/v2/event/nearby.
//
// @Summary      Find nearby events
// @Description  Returns events near the given coordinates. Supports radius-based search (lat/lng/radius_km) or bounding box (min_lat/max_lat/min_lng/max_lng).
// @Description  NOTE: distance_km is calculated relative to the authenticated user's stored home location if available.
// @Tags         event-v2
// @Produce      json
// @Security     BearerAuth
// @Param        lat       query number false "Latitude"
// @Param        lng       query number false "Longitude"
// @Param        radius_km query int    false "Search radius in km (default 25)"
// @Param        min_lat   query number false "Bounding box min latitude"
// @Param        max_lat   query number false "Bounding box max latitude"
// @Param        min_lng   query number false "Bounding box min longitude"
// @Param        max_lng   query number false "Bounding box max longitude"
// @Param        cursor    query string false "Pagination cursor"
// @Param        limit     query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]EventNearbyResponse} "Nearby events"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request — missing coordinates"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/event/nearby [get]
func (h *EventHandler) ListNearbyEventsV2(w http.ResponseWriter, r *http.Request) {
	h.ListNearbyEvents(w, r)
}

// ListCeremoniesV2 handles GET /api/v2/ceremony.
//
// @Summary      List or search ceremony types
// @Description  Returns all available ceremony types with display names, logos, and categories. Supports optional fuzzy search via ?q= parameter that matches against slug, display name, category, and description.
// @Tags         event-v2
// @Produce      json
// @Param        q query string false "Search query for fuzzy filtering (e.g. 'shraadh', 'puja', 'wedding')"
// @Success      200 {object} response.DataResponse{data=[]CeremonyDTO} "Ceremony catalog"
// @Router       /api/v2/ceremony [get]
func (h *EventHandler) ListCeremoniesV2(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	if query != "" {
		// Fuzzy search
		results, err := h.eventService.SearchCeremonies(r.Context(), query)
		if err != nil {
			h.logger.Error("search ceremonies failed", "error", err, "query", query)
			apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to search ceremony types. Please try again later.", r.URL.Path))
			return
		}
		dtos := make([]CeremonyDTO, len(results))
		for i, c := range results {
			imageURL, logoURL, imageURLNoBg := h.resolveCeremonyURLs(c)
			dtos[i] = CeremonyDTO{
				ID:           c.ID.String(),
				Slug:         c.Slug,
				DisplayName:  c.DisplayName,
				ImageURL:     imageURL,
				LogoURL:      logoURL,
				Category:     c.Category,
				Description:  c.Description,
				ImageURLNoBg: imageURLNoBg,
				DisplayOrder: c.DisplayOrder,
				IsActive:     c.IsActive,
			}
		}
		response.WriteData(w, http.StatusOK, dtos)
		return
	}

	// Full list (no search query)
	h.ListCeremonies(w, r)
}

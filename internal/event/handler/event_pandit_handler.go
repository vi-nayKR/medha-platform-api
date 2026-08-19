package handler

import (
	"context"
	"math"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// EventNearbyResponse adds distance + yajman info to EventResponse.
type EventNearbyResponse struct {
	EventResponse
	DistanceKM      float64 `json:"distance_km"`
	YajmanFirstName string  `json:"yajman_first_name"`
	YajmanLastName  string  `json:"yajman_last_name"`
	YajmanPhotoURL  *string `json:"yajman_photo_url"`
}

// ListNearbyEvents handles GET /api/v2/event/nearby.
func (h *EventHandler) ListNearbyEvents(w http.ResponseWriter, r *http.Request) {
	minLatStr := r.URL.Query().Get("min_lat")
	maxLatStr := r.URL.Query().Get("max_lat")
	minLngStr := r.URL.Query().Get("min_lng")
	maxLngStr := r.URL.Query().Get("max_lng")

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	var events []*domain.EventWithDistance
	var nextCursor string
	var err error

	// Determine UserID if authenticated
	var userIDPtr *uuid.UUID
	if uid, err := middleware.UserIDFromContext(r.Context()); err == nil {
		userIDPtr = &uid
	}

	// If all bounding box params are present, use BB search (Map View)
	if minLatStr != "" && maxLatStr != "" && minLngStr != "" && maxLngStr != "" {
		minLat, err1 := strconv.ParseFloat(minLatStr, 64)
		maxLat, err2 := strconv.ParseFloat(maxLatStr, 64)
		minLng, err3 := strconv.ParseFloat(minLngStr, 64)
		maxLng, err4 := strconv.ParseFloat(maxLngStr, 64)

		if err1 == nil && err2 == nil && err3 == nil && err4 == nil {
			events, nextCursor, err = h.eventService.ListInBoundingBox(r.Context(), minLat, maxLat, minLng, maxLng, userIDPtr, cursor, limit)
		} else {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid location coordinates.", r.URL.Path))
			return
		}
	} else {
		// Fallback to radius-based search (List View)
		latStr := r.URL.Query().Get("lat")
		lngStr := r.URL.Query().Get("lng")

		if latStr == "" || lngStr == "" {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"Please provide your location to find nearby events.",
				r.URL.Path,
			))
			return
		}

		lat, err1 := strconv.ParseFloat(latStr, 64)
		lng, err2 := strconv.ParseFloat(lngStr, 64)
		if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid location coordinates.", r.URL.Path))
			return
		}

		// List view defaults to 20km radius
		radiusKM := parseIntParam(r, "radius_km", 0)
		if radiusKM == 0 {
			radiusKM = parseIntParam(r, "radius", 20)
		}

		events, nextCursor, err = h.eventService.ListNearbyEvents(r.Context(), lat, lng, radiusKM, userIDPtr, cursor, limit)
	}

	if err != nil {
		h.logger.Error("list nearby events failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to find nearby events. Please try again later.", r.URL.Path))
		return
	}

	eventResponses := make([]EventNearbyResponse, len(events))
	for i, e := range events {
		eventResponses[i] = h.toEventNearbyResponse(r.Context(), e)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, eventResponses, cursorPtr)
}

func (h *EventHandler) toEventNearbyResponse(ctx context.Context, event *domain.EventWithDistance) EventNearbyResponse {
	resp := EventNearbyResponse{
		EventResponse:   h.toEventResponse(ctx, &event.Event),
		DistanceKM:      math.Round(event.DistanceKM*100) / 100, // 2 decimal places
		YajmanFirstName: "Yajman",
		YajmanLastName:  "",
	}

	// Always mask photo URL in public listings
	resp.YajmanPhotoURL = nil

	return resp
}

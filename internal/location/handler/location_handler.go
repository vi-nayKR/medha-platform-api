package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	_ "github.com/medha/backend/internal/location/domain"
	locationservice "github.com/medha/backend/internal/location/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// LocationHandler handles map location HTTP endpoints.
type LocationHandler struct {
	mapService *locationservice.MapMyIndiaService
	logger     *slog.Logger
}

// NewLocationHandler creates a new LocationHandler.
func NewLocationHandler(mapService *locationservice.MapMyIndiaService, logger *slog.Logger) *LocationHandler {
	return &LocationHandler{
		mapService: mapService,
		logger:     logger,
	}
}

// AutocompleteV2 handles GET /api/v2/location/suggest.
//
// @Summary      Location auto-suggest
// @Description  Returns up to 10 place suggestions matching the query string using MapMyIndia Atlas API. Requires a valid JWT bearer token.
// @Tags         location-v2
// @Produce      json
// @Security     BearerAuth
// @Param        q      query string true  "Search query (e.g. 'Aruna Hospital', 'Bangalore')"
// @Param        region query string false "Region code (default IND)"
// @Success      200 {object} response.DataResponse{data=map[string]any} "Autocomplete suggestions"
// @Failure      400 {object} apierrors.ProblemDetail "Missing query parameter"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      502 {object} apierrors.ProblemDetail "Location service unavailable"
// @Router       /api/v2/location/suggest [get]
func (h *LocationHandler) AutocompleteV2(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Query parameter 'q' is required (e.g. ?q=Bangalore).",
			r.URL.Path,
		))
		return
	}
	region := strings.TrimSpace(r.URL.Query().Get("region"))

	// If region is specified but is not a valid 3-letter ISO country code (e.g., if a user passes a pincode like '560004'),
	// append it to the query to make the search work, and default the region restriction to 'IND'.
	if region != "" && !isValidRegionCode(region) {
		query = query + " " + region
		region = "IND"
	}

	result, err := h.mapService.Autocomplete(query, region)
	if err != nil {
		h.logger.Error("autocomplete v2 failed", "error", err, "query", query)
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusServiceUnavailable,
			"https://medha.app/errors/location-service-unavailable",
			"Location Service Unavailable",
			"The location auto-suggest service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
		return
	}

	response.WriteData(w, http.StatusOK, result)
}

// RouteV2 handles GET /api/v2/location/route.
//
// @Summary      Get driving route waypoints
// @Description  Returns step-by-step driving directions between coordinates. Requires a valid JWT bearer token.
// @Tags         location-v2
// @Produce      json
// @Security     BearerAuth
// @Param        waypoints query string true "Semicolon-separated lng,lat pairs"
// @Param        alternatives query string false "Return alternatives (1=yes, default 0)"
// @Success      200 {object} response.DataResponse{data=map[string]any} "Driving route"
// @Failure      400 {object} apierrors.ProblemDetail "Missing query parameter"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      502 {object} apierrors.ProblemDetail "Location service unavailable"
// @Router       /api/v2/location/route [get]
func (h *LocationHandler) RouteV2(w http.ResponseWriter, r *http.Request) {
	waypoints := strings.TrimSpace(r.URL.Query().Get("waypoints"))
	if waypoints == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Query parameter 'waypoints' is required (e.g. ?waypoints=77.5728,12.9371;77.1086,13.3374).",
			r.URL.Path,
		))
		return
	}
	alternatives := r.URL.Query().Get("alternatives")

	result, err := h.mapService.Route(waypoints, alternatives)
	if err != nil {
		h.logger.Error("route v2 failed", "error", err, "waypoints", waypoints)
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusBadGateway,
			"https://medha.app/errors/location-service-unavailable",
			"Location Service Unavailable",
			"The routing service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
		return
	}

	response.WriteData(w, http.StatusOK, result)
}

// TokenV2 handles GET /api/v2/location/token.
//
// @Summary      Get MapMyIndia access token
// @Description  Returns a valid access token for client-side MapMyIndia SDK use. Requires a valid JWT bearer token.
// @Tags         location-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} response.DataResponse{data=map[string]string} "Access token"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      502 {object} apierrors.ProblemDetail "Location service unavailable"
// @Router       /api/v2/location/token [get]
func (h *LocationHandler) TokenV2(w http.ResponseWriter, r *http.Request) {
	tok, err := h.mapService.GetToken()
	if err != nil {
		h.logger.Error("failed to get map token v2", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusBadGateway,
			"https://medha.app/errors/location-service-unavailable",
			"Location Service Unavailable",
			"The map token service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{
		"access_token": tok,
	})
}

// NearbyV2 handles GET /api/v2/location/nearby.
//
// @Summary      Search for nearby POIs
// @Description  Returns nearby points of interest (temples, venues) using MapMyIndia Atlas. Requires a valid JWT bearer token.
// @Tags         location-v2
// @Produce      json
// @Security     BearerAuth
// @Param        lat query number true "Latitude"
// @Param        lng query number true "Longitude"
// @Param        keywords query string false "Comma-separated keywords" default(temple,venue)
// @Param        radius query int false "Radius in meters" default(5000)
// @Success      200 {object} response.DataResponse{data=map[string]any} "Nearby POIs"
// @Failure      400 {object} apierrors.ProblemDetail "Missing query parameter"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      502 {object} apierrors.ProblemDetail "Location service unavailable"
// @Router       /api/v2/location/nearby [get]
func (h *LocationHandler) NearbyV2(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")
	keywords := r.URL.Query().Get("keywords")
	if keywords == "" {
		keywords = "temple,venue" // Default for Medha use case
	}

	if latStr == "" || lngStr == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Query parameters 'lat' and 'lng' are required.",
			r.URL.Path,
		))
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Invalid 'lat' parameter. Must be a valid float.",
			r.URL.Path,
		))
		return
	}

	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Invalid 'lng' parameter. Must be a valid float.",
			r.URL.Path,
		))
		return
	}

	radius := 5000 // default 5km
	if r.URL.Query().Get("radius") != "" {
		if r, err := strconv.Atoi(r.URL.Query().Get("radius")); err == nil {
			radius = r
		}
	}

	result, err := h.mapService.Nearby(keywords, lat, lng, radius)
	if err != nil {
		h.logger.Error("nearby search v2 failed", "error", err, "keywords", keywords)
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusBadGateway,
			"https://medha.app/errors/location-service-unavailable",
			"Location Service Unavailable",
			"The nearby search service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
		return
	}

	response.WriteData(w, http.StatusOK, result)
}

// ReverseGeocodeV2 handles GET /api/v2/location/reverse.
//
// @Summary      Reverse Geocode
// @Description  Returns the formatted address and location details for given latitude and longitude coordinates. Requires a valid JWT bearer token.
// @Tags         location-v2
// @Produce      json
// @Security     BearerAuth
// @Param        lat query number true "Latitude"
// @Param        lng query number true "Longitude"
// @Success      200 {object} response.DataResponse{data=domain.RevGeocodeResponse} "Reverse Geocode Results"
// @Failure      400 {object} apierrors.ProblemDetail "Missing or invalid query parameter"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      502 {object} apierrors.ProblemDetail "Location service unavailable"
// @Router       /api/v2/location/reverse [get]
func (h *LocationHandler) ReverseGeocodeV2(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")

	if latStr == "" || lngStr == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Query parameters 'lat' and 'lng' are required.",
			r.URL.Path,
		))
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Invalid 'lat' parameter. Must be a valid float.",
			r.URL.Path,
		))
		return
	}

	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Invalid 'lng' parameter. Must be a valid float.",
			r.URL.Path,
		))
		return
	}

	result, err := h.mapService.ReverseGeocode(lat, lng)
	if err != nil {
		h.logger.Error("reverse geocode v2 failed", "error", err, "lat", lat, "lng", lng)
		apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
			http.StatusBadGateway,
			"https://medha.app/errors/location-service-unavailable",
			"Location Service Unavailable",
			"The reverse geocoding service is temporarily unavailable. Please try again later.",
			r.URL.Path,
		))
		return
	}

	// Passing through the exact MapMyIndia structure wrapped in our standard JSON response
	response.WriteData(w, http.StatusOK, result)
}

// isValidRegionCode returns true if the region string is a valid 3-letter ISO country code.
func isValidRegionCode(region string) bool {
	if len(region) != 3 {
		return false
	}
	for i := 0; i < len(region); i++ {
		c := region[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

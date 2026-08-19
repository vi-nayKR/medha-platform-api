package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/medha/backend/internal/city/domain"
	"github.com/medha/backend/internal/city/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

var _ = domain.City{}

type CityHandler struct {
	citySvc *service.CityService
	logger  *slog.Logger
}

func NewCityHandler(citySvc *service.CityService, logger *slog.Logger) *CityHandler {
	return &CityHandler{
		citySvc: citySvc,
		logger:  logger,
	}
}

// ListNearbyCities handles GET /api/v2/cities/nearby
//
// @Summary      Get nearby seeded cities
// @Description  Returns a list of seeded cities within a given radius, sorted by distance.
// @Tags         cities
// @Produce      json
// @Param        lat    query number true  "Latitude"
// @Param        lng    query number true  "Longitude"
// @Param        radius query int    false "Radius in km (default 200)"
// @Param        limit  query int    false "Max results (default 15)"
// @Success      200 {object} response.DataResponse{data=[]domain.NearbyCityResult} "Nearby cities"
// @Failure      400 {object} apierrors.ProblemDetail "Missing or invalid parameters"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/cities/nearby [get]
func (h *CityHandler) ListNearbyCities(w http.ResponseWriter, r *http.Request) {
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

	radiusKm := 200
	if r.URL.Query().Get("radius") != "" {
		if val, err := strconv.Atoi(r.URL.Query().Get("radius")); err == nil {
			radiusKm = val
		}
	}

	limit := 15
	if r.URL.Query().Get("limit") != "" {
		if val, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
			limit = val
		}
	}

	excludeClosest := true
	if r.URL.Query().Get("exclude_closest") != "" {
		if val, err := strconv.ParseBool(r.URL.Query().Get("exclude_closest")); err == nil {
			excludeClosest = val
		}
	}

	results, err := h.citySvc.ListNearbyCities(r.Context(), lat, lng, radiusKm, limit, excludeClosest)
	if err != nil {
		h.logger.Error("list nearby cities failed", "error", err, "lat", lat, "lng", lng)
		apierrors.WriteProblemDetail(w, apierrors.InternalError(
			"Failed to fetch nearby cities.",
			r.URL.Path,
		))
		return
	}

	response.WriteData(w, http.StatusOK, results)
}

// SearchCities handles GET /api/v2/cities/search
//
// @Summary      Search cities by name
// @Description  Searches seeded cities by name. Falls back to MapMyIndia autocomplete if not found in database.
// @Tags         cities
// @Produce      json
// @Param        q     query string true  "Search query (min 2 characters)"
// @Param        limit query int    false "Max results (default 10)"
// @Success      200 {object} response.DataResponse{data=[]domain.City} "Matching cities"
// @Failure      400 {object} apierrors.ProblemDetail "Missing or invalid query parameter"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/cities/search [get]
func (h *CityHandler) SearchCities(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Query parameter 'q' must be at least 2 characters.",
			r.URL.Path,
		))
		return
	}

	limit := 10
	if r.URL.Query().Get("limit") != "" {
		if val, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
			limit = val
		}
	}

	results, err := h.citySvc.SearchCities(r.Context(), query, limit)
	if err != nil {
		h.logger.Error("search cities failed", "error", err, "query", query)
		apierrors.WriteProblemDetail(w, apierrors.InternalError(
			"Failed to search cities.",
			r.URL.Path,
		))
		return
	}

	response.WriteData(w, http.StatusOK, results)
}

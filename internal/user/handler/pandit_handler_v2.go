package handler

import (
	"net/http"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

var (
	_ = response.ListResponse{}
	_ = apierrors.ProblemDetail{}
)

// GetPanditByIDV2 handles GET /api/v2/pandit/{id}.
//
// @Summary      Get pandit by user ID
// @Description  Returns the professional profile of a specific pandit by their user UUID.
// @Tags         pandit-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "User UUID"
// @Success      200 {object} response.DataResponse{data=PanditProfileResponse} "Pandit profile"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Pandit not found"
// @Router       /api/v2/pandit/{id} [get]
func (h *PanditHandler) GetPanditByIDV2(w http.ResponseWriter, r *http.Request) {
	h.GetPanditByID(w, r)
}

// ListNearbyPanditsV2 handles GET /api/v2/pandit/nearby.
//
// @Summary      Find nearby pandits
// @Description  Returns a paginated list of pandits near a location. Supports filtering by availability, tradition, and ceremony type.
// @Tags         pandit-v2
// @Produce      json
// @Security     BearerAuth
// @Param        lat            query number  true  "Latitude"
// @Param        lng            query number  true  "Longitude"
// @Param        radius_km      query int     false "Radius in km (default 25)"
// @Param        available_only query boolean false "Filter to available only"
// @Param        parampara      query string  false "Filter by tradition"
// @Param        ceremony_type  query string  false "Comma-separated ceremony types"
// @Param        cursor         query string  false "Pagination cursor"
// @Param        limit          query int     false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]PanditNearbyResponse} "Nearby pandits"
// @Failure      400 {object} apierrors.ProblemDetail "Missing lat/lng"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/pandit/nearby [get]
func (h *PanditHandler) ListNearbyPanditsV2(w http.ResponseWriter, r *http.Request) {
	h.ListNearbyPandits(w, r)
}

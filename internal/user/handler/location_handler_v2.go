package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"

	"github.com/medha/backend/internal/server/middleware"
	userdomain "github.com/medha/backend/internal/user/domain"
	userservice "github.com/medha/backend/internal/user/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// LocationHandlerV2 handles V2 user-location HTTP endpoints.
type LocationHandlerV2 struct {
	userService *userservice.UserService
	validate    *validator.Validate
	logger      *slog.Logger
}

// NewLocationHandlerV2 creates a new LocationHandlerV2.
func NewLocationHandlerV2(userService *userservice.UserService, logger *slog.Logger) *LocationHandlerV2 {
	return &LocationHandlerV2{
		userService: userService,
		validate:    validator.New(),
		logger:      logger,
	}
}

// UpdateLocationRequestV2 is the request body for PUT /api/v2/user/location.
type UpdateLocationRequestV2 struct {
	Latitude  float64 `json:"latitude"  validate:"required,min=-90,max=90"`
	Longitude float64 `json:"longitude" validate:"required,min=-180,max=180"`
}

// LocationResponseV2 is the response body for PUT /api/v2/user/location.
type LocationResponseV2 struct {
	Latitude  float64 `json:"latitude"  example:"12.9371"`
	Longitude float64 `json:"longitude" example:"77.5728"`
}

// UpdateLocation handles user location updates.
func (h *LocationHandlerV2) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req UpdateLocationRequestV2
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid location data.", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please provide valid location coordinates.",
			r.URL.Path,
		))
		return
	}

	if err := h.userService.UpdateLocationV2(r.Context(), userID, req.Latitude, req.Longitude); err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("update location failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to update your location. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, LocationResponseV2(req))
}

// GetLocation returns the current user's stored location.
//
// @Summary      Get user location
// @Description  Returns the currently stored home location for the authenticated user. Used to seed the map on startup.
// @Tags         user-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} response.DataResponse{data=LocationResponseV2} "User location"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Location not found"
// @Router       /api/v2/user/me/location [get]
func (h *LocationHandlerV2) GetLocation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	user, err := h.userService.GetByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get location failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to retrieve your location. Please try again later.", r.URL.Path))
		return
	}

	if user.Location == nil {
		// Return 200 with null or empty if no location stored, or 404?
		// Bug report says 200 { "data": { "latitude": 13.33, "longitude": 77.11 } }
		// If no location, we should probably return a clear state.
		response.WriteData(w, http.StatusOK, nil)
		return
	}

	response.WriteData(w, http.StatusOK, LocationResponseV2{
		Latitude:  user.Location.Latitude,
		Longitude: user.Location.Longitude,
	})
}

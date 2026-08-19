package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/server/middleware"
	"github.com/medha/backend/internal/user/domain"
	userservice "github.com/medha/backend/internal/user/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// PanditHandler handles pandit profile-related HTTP endpoints.
type PanditHandler struct {
	panditService *userservice.PanditService
	validate      *validator.Validate
	logger        *slog.Logger
}

// NewPanditHandler creates a new PanditHandler.
func NewPanditHandler(panditService *userservice.PanditService, logger *slog.Logger) *PanditHandler {
	return &PanditHandler{
		panditService: panditService,
		validate:      validator.New(),
		logger:        logger,
	}
}

// --- Request/Response DTOs ---

// PanditProfileRequest is the request body for PUT /api/v2/pandit/profile.
type PanditProfileRequest struct {
	Parampara               string   `json:"parampara"`
	VedaAffiliation         string   `json:"veda_affiliation"`
	CeremonySpecializations []string `json:"ceremony_specializations"`
	Languages               []string `json:"languages"`
	ServiceRadiusKM         int      `json:"service_radius_km"`
	AvailabilityStatus      string   `json:"availability_status" validate:"omitempty,oneof=available unavailable"`
	About                   string   `json:"about"`
}

// PanditProfileResponse represents a pandit profile in API responses.
type PanditProfileResponse struct {
	ID                      uuid.UUID `json:"id"`
	UserID                  uuid.UUID `json:"user_id"`
	Parampara               string    `json:"parampara"`
	VedaAffiliation         string    `json:"veda_affiliation"`
	CeremonySpecializations []string  `json:"ceremony_specializations"`
	Languages               []string  `json:"languages"`
	ServiceRadiusKM         int       `json:"service_radius_km"`
	AvailabilityStatus      string    `json:"availability_status"`
	About                   string    `json:"about"`
	// Joined user data
	FirstName       string  `json:"first_name,omitempty"`
	LastName        string  `json:"last_name,omitempty"`
	ProfilePhotoURL *string `json:"profile_photo_url,omitempty"`
	PhoneNumber     string  `json:"phone_number,omitempty"`
}

// PanditNearbyResponse adds distance info and connection context to PanditProfileResponse.
type PanditNearbyResponse struct {
	PanditProfileResponse
	DistanceKM       float64    `json:"distance_km"`
	ConnectionStatus string     `json:"connection_status"` // "not_connected" | "pending" | "connected"
	ConversationID   *uuid.UUID `json:"conversation_id,omitempty"`
}

// --- Handlers ---

// GetMyProfile handles GET /api/v2/pandit/profile.
// Summary Get my pandit profile
// Description Returns the professional pandit profile of the currently authenticated user.
// Tags Pandits
// Accept json
// Produce json
// Security bearerAuth
// Success 200 {object} response.DataResponse{data=PanditProfileResponse} "Pandit profile found"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 404 {object} apierrors.ProblemDetail "Profile not found"
// Router /api/v2/pandit/profile [get]
func (h *PanditHandler) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	profile, user, err := h.panditService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrPanditProfileNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your pandit profile has not been set up yet.", r.URL.Path))
			return
		}
		h.logger.Error("get pandit profile failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load your pandit profile. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toPanditProfileResponse(profile, user, true))
}

// UpdateMyProfile handles PUT /api/v2/pandit/profile.
// Summary Create or update pandit profile
// Description Creates a new pandit profile or updates an existing one for the authenticated user.
// Tags Pandits
// Accept json
// Produce json
// Security bearerAuth
// Param request body PanditProfileRequest true "Pandit profile details"
// Success 200 {object} response.DataResponse{data=PanditProfileResponse} "Profile updated"
// Failure 400 {object} apierrors.ProblemDetail "Bad request"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/pandit/profile [put]
func (h *PanditHandler) UpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req PanditProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid profile details.", r.URL.Path))
		return
	}

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"availability_status must be 'available' or 'unavailable' if provided",
			r.URL.Path,
		))
		return
	}

	availStatus := domain.AvailabilityAvailable
	if req.AvailabilityStatus != "" {
		availStatus = domain.AvailabilityStatus(req.AvailabilityStatus)
	}

	serviceRadius := req.ServiceRadiusKM
	if serviceRadius <= 0 {
		serviceRadius = 25
	}

	profile := &domain.PanditProfile{
		Parampara:               req.Parampara,
		VedaAffiliation:         req.VedaAffiliation,
		CeremonySpecializations: req.CeremonySpecializations,
		Languages:               req.Languages,
		ServiceRadiusKM:         serviceRadius,
		AvailabilityStatus:      availStatus,
		About:                   req.About,
	}

	updated, err := h.panditService.CreateOrUpdate(r.Context(), userID, profile)
	if err != nil {
		h.logger.Error("update pandit profile failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to update your pandit profile. Please try again later.", r.URL.Path))
		return
	}

	// Simple response without user join (since it's the current user)
	resp := PanditProfileResponse{
		ID:                      updated.ID,
		UserID:                  updated.UserID,
		Parampara:               updated.Parampara,
		VedaAffiliation:         updated.VedaAffiliation,
		CeremonySpecializations: updated.CeremonySpecializations,
		Languages:               updated.Languages,
		ServiceRadiusKM:         updated.ServiceRadiusKM,
		AvailabilityStatus:      updated.AvailabilityStatus.String(),
		About:                   updated.About,
	}

	response.WriteData(w, http.StatusOK, resp)
}

// GetPanditByID handles GET /api/v2/pandit/{id}.
// Summary Get pandit by user ID
// Description Returns the professional profile of a specific pandit by their user UUID.
// Tags Pandits
// Accept json
// Produce json
// Security bearerAuth
// Param id path string true "User UUID"
// Success 200 {object} response.DataResponse{data=PanditProfileResponse} "Pandit found"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 404 {object} apierrors.ProblemDetail "Pandit not found"
// Router /api/v2/pandit/{id} [get]
func (h *PanditHandler) GetPanditByID(w http.ResponseWriter, r *http.Request) {
	panditUserID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The pandit ID provided is not valid.", r.URL.Path))
		return
	}

	profile, user, err := h.panditService.GetByUserID(r.Context(), panditUserID)
	if err != nil {
		if errors.Is(err, domain.ErrPanditProfileNotFound) || errors.Is(err, domain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This pandit could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get pandit by id failed", "error", err, "pandit_user_id", panditUserID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load pandit details. Please try again later.", r.URL.Path))
		return
	}

	viewerID, err := middleware.UserIDFromContext(r.Context())
	allowed := false
	if err == nil {
		allowed, _ = h.panditService.CanViewContact(r.Context(), viewerID, panditUserID)
	}

	response.WriteData(w, http.StatusOK, toPanditProfileResponse(profile, user, allowed))
}

// ListNearbyPandits handles GET /api/v2/pandit/nearby.
// Summary Find nearby pandits
// Description Returns a paginated list of pandits near a location with filtering support.
// Tags Pandits
// Accept json
// Produce json
// Security bearerAuth
// Param lat query number true "Latitude"
// Param lng query number true "Longitude"
// Param radius_km query int false "Radius in KM" default(25)
// Param available_only query boolean false "Filter available only"
// Param parampara query string false "Filter by tradition"
// Param ceremony_type query string false "Comma-separated ceremony types"
// Param cursor query string false "Pagination cursor"
// Param limit query int false "Pagination limit" default(20)
// Success 200 {object} response.ListResponse{data=[]PanditNearbyResponse} "Nearby pandits"
// Failure 400 {object} apierrors.ProblemDetail "Bad request"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/pandit/nearby [get]
func (h *PanditHandler) ListNearbyPandits(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")

	if latStr == "" || lngStr == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please provide your location to find nearby pandits.",
			r.URL.Path,
		))
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil || lat < -90 || lat > 90 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid latitude.", r.URL.Path))
		return
	}

	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil || lng < -180 || lng > 180 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid longitude.", r.URL.Path))
		return
	}

	radiusKM := parseIntParam(r, "radius_km", 25)
	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	// Parse filters
	filters := domain.PanditFilters{
		AvailableOnly: r.URL.Query().Get("available_only") == "true",
		Parampara:     r.URL.Query().Get("parampara"),
	}
	if ctStr := r.URL.Query().Get("ceremony_type"); ctStr != "" {
		filters.CeremonyTypes = strings.Split(ctStr, ",")
	}

	// Extract viewerID from JWT (nil-safe — viewer may be anonymous on v1 endpoint)
	var viewerIDPtr *uuid.UUID
	viewerID, viewerErr := middleware.UserIDFromContext(r.Context())
	if viewerErr == nil {
		viewerIDPtr = &viewerID
	}

	profiles, nextCursor, err := h.panditService.ListNearby(r.Context(), lat, lng, radiusKM, filters, cursor, limit, viewerIDPtr)
	if err != nil {
		h.logger.Error("list nearby pandits failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to find nearby pandits. Please try again later.", r.URL.Path))
		return
	}

	responses := make([]PanditNearbyResponse, len(profiles))
	for i, p := range profiles {
		allowed := false
		if viewerIDPtr != nil {
			allowed, _ = h.panditService.CanViewContact(r.Context(), viewerID, p.UserID)
		}
		connStatus := p.ConnectionStatus
		if connStatus == "" {
			connStatus = "not_connected"
		}
		resp := PanditNearbyResponse{
			PanditProfileResponse: toPanditProfileResponse(&p.PanditProfile, p.User, allowed),
			DistanceKM:            math.Round(p.DistanceKM*100) / 100,
			ConnectionStatus:      connStatus,
			ConversationID:        p.ConversationID,
		}
		responses[i] = resp
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, responses, cursorPtr)
}

// --- Helpers ---

func toPanditProfileResponse(profile *domain.PanditProfile, user *domain.User, allowed bool) PanditProfileResponse {
	resp := PanditProfileResponse{
		ID:                      profile.ID,
		UserID:                  profile.UserID,
		Parampara:               profile.Parampara,
		VedaAffiliation:         profile.VedaAffiliation,
		CeremonySpecializations: profile.CeremonySpecializations,
		Languages:               profile.Languages,
		ServiceRadiusKM:         profile.ServiceRadiusKM,
		AvailabilityStatus:      profile.AvailabilityStatus.String(),
		About:                   profile.About,
	}

	if user != nil {
		resp.FirstName = user.FirstName
		resp.LastName = user.LastName
		if allowed {
			resp.PhoneNumber = user.Phone
		} else {
			resp.PhoneNumber = ""
		}
		if user.ProfilePhotoURL != "" {
			resp.ProfilePhotoURL = &user.ProfilePhotoURL
		}
	}

	return resp
}

func parseIntParam(r *http.Request, key string, defaultVal int) int {
	valStr := r.URL.Query().Get(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil || val <= 0 {
		return defaultVal
	}
	return val
}

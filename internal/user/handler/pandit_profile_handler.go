package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/medha/backend/internal/server/middleware"
	userdomain "github.com/medha/backend/internal/user/domain"
	userservice "github.com/medha/backend/internal/user/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// --- Pandit Profile V2 ---

// SetupPanditProfileRequestDTO is the request body for PUT /api/v2/pandit/profile.
//
// ceremony_specializations must be one or more of:
// shraadh, grihapravesh, vivah, satyanarayan, mundan, antim_sanskar, vastu_shanti, naamkaran, upanayana
type SetupPanditProfileRequestDTO struct {
	Parampara               string   `json:"parampara"                validate:"omitempty,max=100"    example:"Smartha"`
	VedaAffiliation         string   `json:"veda_affiliation"         validate:"omitempty,max=100"    example:"Rigveda"`
	CeremonySpecializations []string `json:"ceremony_specializations" validate:"omitempty"            example:"[\"vivah\",\"grihapravesh\"]"`
	Languages               []string `json:"languages"                validate:"omitempty"            example:"[\"Hindi\",\"Sanskrit\"]"`
	ServiceRadiusKM         int      `json:"service_radius_km"        validate:"omitempty,min=1,max=500" example:"30"`
	AvailabilityStatus      string   `json:"availability_status"      validate:"omitempty,oneof=available unavailable" example:"available"`
	About                   string   `json:"about"                    validate:"omitempty,max=1000"   example:"Experienced pandit with 15+ years."`
}

// SetupPanditProfile handles PUT /api/v2/pandit/profile.
//
// @Summary      Setup or update pandit profile
// @Description  Creates or updates the professional profile of the authenticated pandit. Only users with role=pandit can call this endpoint. All fields are optional — send only what you want to update.
// @Tags         pandit-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body SetupPanditProfileRequestDTO true "Pandit profile data"
// @Success      200 {object} response.DataResponse{data=userservice.PanditProfileFullResponse} "Profile updated"
// @Failure      400 {object} apierrors.ProblemDetail "Validation error — invalid availability_status, service_radius_km out of range, or field too long"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized — missing or invalid JWT"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — only pandit accounts can update a pandit profile"
// @Failure      404 {object} apierrors.ProblemDetail "User not found"
// @Failure      500 {object} apierrors.ProblemDetail "Internal server error"
// @Router       /api/v2/pandit/profile [put]
func (h *ProfileHandlerV2) SetupPanditProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req SetupPanditProfileRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid profile details.", r.URL.Path))
		return
	}

	// Sanitize double-serialized arrays (common in mobile clients)
	req.CeremonySpecializations = sanitizeArrayStrings(req.CeremonySpecializations)
	req.Languages = sanitizeArrayStrings(req.Languages)

	if err := h.validate.Struct(req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please check your profile details. Service radius should be between 1-500 km.",
			r.URL.Path,
		))
		return
	}

	// Validate ceremony specializations against known list
	for _, ct := range req.CeremonySpecializations {
		if !h.isValidCeremonyType(r.Context(), ct) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest(
				"One or more ceremony types are invalid. Please select from the available options.",
				r.URL.Path,
			))
			return
		}
	}

	resp, err := h.userService.SetupPanditProfileV2(r.Context(), userID, userservice.SetupPanditProfileInput{
		Parampara:               req.Parampara,
		VedaAffiliation:         req.VedaAffiliation,
		CeremonySpecializations: req.CeremonySpecializations,
		Languages:               req.Languages,
		ServiceRadiusKM:         req.ServiceRadiusKM,
		AvailabilityStatus:      req.AvailabilityStatus,
		About:                   req.About,
	})
	if err != nil {
		switch {
		case errors.Is(err, userdomain.ErrUserNotFound):
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
		case errors.Is(err, userdomain.ErrInvalidUserRole):
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"Only pandit accounts can set up a professional profile. Please update your role to Pandit first.",
				r.URL.Path,
			))
		default:
			h.logger.Error("setup pandit profile v2 failed", "error", err, "user_id", userID)
			apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to set up your professional profile. Please try again later.", r.URL.Path))
		}
		return
	}

	response.WriteData(w, http.StatusOK, resp)
}

// GetPanditProfile handles GET /api/v2/pandit/profile.
//
// @Summary      Get pandit profile
// @Description  Returns the authenticated pandit's full professional profile merged with their user info. Returns 403 if the user is not a pandit and 404 if no profile has been set up yet.
// @Tags         pandit-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} response.DataResponse{data=userservice.PanditProfileFullResponse} "Pandit profile"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized — missing or invalid JWT"
// @Failure      403 {object} apierrors.ProblemDetail "Forbidden — user is not a pandit"
// @Failure      404 {object} apierrors.ProblemDetail "Pandit profile not found"
// @Failure      500 {object} apierrors.ProblemDetail "Internal server error"
// @Router       /api/v2/pandit/profile [get]
func (h *ProfileHandlerV2) GetPanditProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	resp, err := h.userService.GetPanditProfileV2(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, userdomain.ErrUserNotFound):
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
		case errors.Is(err, userdomain.ErrInvalidUserRole):
			apierrors.WriteProblemDetail(w, apierrors.NewProblemDetail(
				http.StatusForbidden,
				"https://medha.app/errors/forbidden",
				"Forbidden",
				"This feature is only for Pandits.",
				r.URL.Path,
			))
		case errors.Is(err, userdomain.ErrPanditProfileNotFound):
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your professional profile has not been set up yet.", r.URL.Path))
		default:
			h.logger.Error("get pandit profile v2 failed", "error", err, "user_id", userID)
			apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load your professional profile. Please try again later.", r.URL.Path))
		}
		return
	}

	response.WriteData(w, http.StatusOK, resp)
}

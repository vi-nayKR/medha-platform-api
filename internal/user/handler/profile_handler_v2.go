package handler

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	"github.com/medha/backend/internal/config"
	"github.com/medha/backend/internal/server/middleware"
	userdomain "github.com/medha/backend/internal/user/domain"
	userservice "github.com/medha/backend/internal/user/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// ProfileHandlerV2 handles V2 user profile HTTP endpoints.
type ProfileHandlerV2 struct {
	userService *userservice.UserService
	cfg         *config.Config
	validate    *validator.Validate
	logger      *slog.Logger
}

// NewProfileHandlerV2 creates a new ProfileHandlerV2.
func NewProfileHandlerV2(userService *userservice.UserService, cfg *config.Config, logger *slog.Logger) *ProfileHandlerV2 {
	return &ProfileHandlerV2{
		userService: userService,
		cfg:         cfg,
		validate:    validator.New(),
		logger:      logger,
	}
}

// SetupProfileRequestDTO is the request body for POST /api/v2/user/profile.
type SetupProfileRequestDTO struct {
	FirstName string `json:"first_name" validate:"required,min=1,max=50" example:"Synthetic"`
	LastName  string `json:"last_name"  validate:"required,min=1,max=50" example:"User"`
	Role      string `json:"role"       validate:"required,oneof=yajman pandit" example:"yajman"`
	Username  string `json:"username"   validate:"omitempty,min=3,max=30" example:"syntheticuser"`
	Email     string `json:"email"      validate:"omitempty,email" example:"synthetic@example.com"`
}

// LocationUpdateRequestV2 is the request body for PUT /api/v2/user/me/location.
type LocationUpdateRequestV2 struct {
	Latitude  float64 `json:"latitude"  validate:"required,min=-90,max=90" example:"12.9716"`
	Longitude float64 `json:"longitude" validate:"required,min=-180,max=180" example:"77.5946"`
}

// SetupProfile handles POST /api/v2/user/me/profile.
//
// @Summary      Setup user profile
// @Description  Sets the user's first name, last name, and role, marking the profile as complete.
// @Tags         user-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body SetupProfileRequestDTO true "Profile data"
// @Success      200 {object} response.DataResponse{data=userservice.ProfileResponse} "Profile set up"
// @Failure      400 {object} apierrors.ProblemDetail "Validation error"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "User not found"
// @Router       /api/v2/user/me/profile [post]
func (h *ProfileHandlerV2) SetupProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req SetupProfileRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide valid profile details.", r.URL.Path))
		return
	}

	h.logger.Info("HANDLER TRACE: SetupProfile decoded",
		"first_name", req.FirstName,
		"last_name", req.LastName,
		"username", req.Username,
		"email", req.Email,
	)

	if err := h.validate.Struct(req); err != nil {
		var validationErrs validator.ValidationErrors
		if errors.As(err, &validationErrs) {
			for _, fieldErr := range validationErrs {
				switch fieldErr.Field() {
				case "Role":
					apierrors.WriteProblemDetail(w, apierrors.BadRequest(
						"Please select a valid role (yajman or pandit).",
						r.URL.Path,
					))
					return
				case "FirstName":
					apierrors.WriteProblemDetail(w, apierrors.BadRequest(
						"First name is required.",
						r.URL.Path,
					))
					return
				case "LastName":
					apierrors.WriteProblemDetail(w, apierrors.BadRequest(
						"Last name is required.",
						r.URL.Path,
					))
					return
				case "Username":
					apierrors.WriteProblemDetail(w, apierrors.BadRequest(
						"Username must be between 3 and 30 characters.",
						r.URL.Path,
					))
					return
				case "Email":
					apierrors.WriteProblemDetail(w, apierrors.BadRequest(
						"Please enter a valid email address.",
						r.URL.Path,
					))
					return
				}
			}
		}
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(
			"Please fill in all required fields (first name, last name, and role).",
			r.URL.Path,
		))
		return
	}

	profileResp, err := h.userService.SetupProfile(r.Context(), userID, userservice.SetupProfileInput{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Role:      req.Role,
		Username:  req.Username,
		Email:     req.Email,
	})
	if err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, userdomain.ErrUsernameTaken) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("Unfortunately, that username is already taken. Please try another.", r.URL.Path))
			return
		}
		h.logger.Error("setup profile failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to set up your profile. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, profileResp)
}

// GetProfile handles GET /api/v2/user/me/profile.
//
// @Summary      Get user profile
// @Description  Returns the current user's profile including pandit profile if applicable.
// @Tags         user-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} response.DataResponse{data=userservice.ProfileResponse} "User profile"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "User not found"
// @Router       /api/v2/user/me/profile [get]
func (h *ProfileHandlerV2) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	profileResp, err := h.userService.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get profile failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load your profile. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, profileResp)
}

// UpdateLocation handles PUT /api/v2/user/me/location.
//
// @Summary      Update user location
// @Description  Updates the user's geographic location natively translating to PostGIS coordinates. Returns the full profile.
// @Tags         user-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body LocationUpdateRequestV2 true "Location data"
// @Success      200 {object} response.DataResponse{data=userservice.ProfileResponse} "Location updated"
// @Failure      400 {object} apierrors.ProblemDetail "Validation error"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "User not found"
// @Router       /api/v2/user/me/location [put]
func (h *ProfileHandlerV2) UpdateLocation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req LocationUpdateRequestV2
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

	_, err = h.userService.UpdateLocation(r.Context(), userID, req.Latitude, req.Longitude)
	if err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Your profile could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("update location failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to update your location. Please try again later.", r.URL.Path))
		return
	}

	profileResp, err := h.userService.GetProfile(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get profile after location update", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Location updated, but we couldn't reload your profile. Please refresh.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, profileResp)
}

// CheckUsernameExistsResponse is the response body for GET /api/v2/user/check-username.
type CheckUsernameExistsResponse struct {
	Exists bool `json:"exists"`
}

// CheckUsername handles GET /api/v2/user/check-username.
//
// @Summary      Check username availability
// @Description  Checks if a username is already taken. This endpoint is public.
// @Tags         user-v2
// @Produce      json
// @Param        username query string true "Username to check"
// @Success      200 {object} response.DataResponse{data=CheckUsernameExistsResponse} "Username check result"
// @Failure      400 {object} apierrors.ProblemDetail "Validation error"
// @Router       /api/v2/user/check-username [get]
func (h *ProfileHandlerV2) CheckUsername(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a username to check.", r.URL.Path))
		return
	}

	if len(username) < 3 || len(username) > 30 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Username must be between 3 and 30 characters.", r.URL.Path))
		return
	}

	exists, err := h.userService.CheckUsernameExists(r.Context(), username)
	if err != nil {
		h.logger.Error("check username failed", "error", err, "username", username)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to check username availability. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, CheckUsernameExistsResponse{
		Exists: exists,
	})
}

// --- End of Username Check, Pandit Profile removed to pandit_profile_handler.go ---

// isValidCeremonyType checks if a string is one of the known ceremony types by querying the database.
func (h *ProfileHandlerV2) isValidCeremonyType(ctx context.Context, ct string) bool {
	if ct == "custom" {
		return true
	}
	exists, err := h.userService.VerifyCeremonySpecialization(ctx, ct)
	if err != nil {
		h.logger.Error("failed to verify ceremony specialization", "error", err, "slug", ct)
		return false
	}
	return exists
}

// sanitizeArrayStrings removes backslashes, double quotes, and square brackets from a slice of strings.
func sanitizeArrayStrings(strs []string) []string {
	if strs == nil {
		return nil
	}
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		cleaned := s
		cleaned = strings.ReplaceAll(cleaned, "[", "")
		cleaned = strings.ReplaceAll(cleaned, "]", "")
		cleaned = strings.ReplaceAll(cleaned, "\\", "")
		cleaned = strings.ReplaceAll(cleaned, "\"", "")
		cleaned = strings.TrimSpace(cleaned)
		if cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

// GetPublicProfile handles GET /api/v2/public/profile/{username}.
//
// @Summary      Get public profile details
// @Description  Returns safe public details of a user profile by username. This endpoint is public (no JWT required).
// @Tags         user-v2
// @Produce      json
// @Param        username path string true "Username"
// @Success      200 {object} response.DataResponse{data=userservice.PublicProfileResponse} "Public profile details"
// @Failure      400 {object} apierrors.ProblemDetail "Validation error"
// @Failure      404 {object} apierrors.ProblemDetail "Profile not found"
// @Failure      500 {object} apierrors.ProblemDetail "Internal server error"
// @Router       /api/v2/public/profile/{username} [get]
func (h *ProfileHandlerV2) GetPublicProfile(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Username path parameter is required.", r.URL.Path))
		return
	}

	profile, err := h.userService.GetPublicProfileByUsername(r.Context(), username, h.cfg.ShareBaseURL)
	if err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("The profile you are looking for could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get public profile failed", "error", err, "username", username)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load the profile. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, profile)
}

// RenderPublicProfilePage handles GET /p/{username}.
// Serves a beautiful web page with Open Graph and Twitter Card tags for social media app link sharing.
// Includes javascript to redirect to the medha:// app deep link.
func (h *ProfileHandlerV2) RenderPublicProfilePage(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = publicProfileTemplate.Execute(w, publicProfilePageData{
			IsError:     true,
			ErrorHeader: "Bad Request",
			ErrorText:   "Please provide a valid username in the URL link.",
		})
		return
	}

	profile, err := h.userService.GetPublicProfileByUsername(r.Context(), username, h.cfg.ShareBaseURL)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if errors.Is(err, userdomain.ErrUserNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = publicProfileTemplate.Execute(w, publicProfilePageData{
				IsError:     true,
				ErrorHeader: "Profile Not Found",
				ErrorText:   "The profile you are looking for could not be found. It may have been deleted or the username might be incorrect.",
			})
			return
		}
		h.logger.Error("render public profile page failed", "error", err, "username", username)
		w.WriteHeader(http.StatusInternalServerError)
		_ = publicProfileTemplate.Execute(w, publicProfilePageData{
			IsError:     true,
			ErrorHeader: "Internal Error",
			ErrorText:   "We encountered an issue loading this profile. Please try again later.",
		})
		return
	}

	// Prepare template data
	var specs []string
	if profile.PanditProfile != nil && profile.PanditProfile.CeremonySpecializations != nil {
		specs = make([]string, 0, len(profile.PanditProfile.CeremonySpecializations))
		for _, rawSpec := range profile.PanditProfile.CeremonySpecializations {
			if cleanSpec, ok := ceremonyDisplayNames[rawSpec]; ok {
				specs = append(specs, cleanSpec)
			} else {
				specs = append(specs, strings.Title(rawSpec))
			}
		}
	}

	var locationText string
	if profile.City != "" || profile.State != "" {
		if profile.City != "" && profile.State != "" {
			locationText = profile.City + ", " + profile.State
		} else if profile.City != "" {
			locationText = profile.City
		} else {
			locationText = profile.State
		}
	}

	initials := "M"
	firstInit := ""
	lastInit := ""
	if len(profile.FirstName) > 0 {
		firstInit = string([]rune(profile.FirstName)[0])
	}
	if len(profile.LastName) > 0 {
		lastInit = string([]rune(profile.LastName)[0])
	}
	if firstInit != "" || lastInit != "" {
		initials = strings.ToUpper(firstInit + lastInit)
	}

	data := publicProfilePageData{
		IsError:         false,
		ID:              profile.ID.String(),
		Username:        profile.Username,
		FirstName:       profile.FirstName,
		LastName:        profile.LastName,
		Role:            profile.Role,
		ProfilePhotoURL: profile.ProfilePhotoURL,
		Initials:        initials,
		BadgeTier:       profile.BadgeTier,
		BadgeLabel:      profile.BadgeLabel,
		BadgeIcon:       profile.BadgeIcon,
		About:           getAboutSafe(profile.PanditProfile),
		LocationText:    locationText,
		ShareURL:        profile.ShareURL,
		Specializations: specs,
		PanditProfile:   profile.PanditProfile,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := publicProfileTemplate.Execute(w, data); err != nil {
		h.logger.Error("execute public profile template failed", "error", err, "username", username)
	}
}

// getAboutSafe returns About info safely (nil-guard helper).
func getAboutSafe(p *userservice.PanditProfileV2) string {
	if p == nil {
		return ""
	}
	return p.About
}

// DeleteAccount handles DELETE /api/v2/user/me.
//
// @Summary      Delete user account
// @Description  Permanently deletes the authenticated user's account and all associated data.
// @Tags         user-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} map[string]string "Account deleted successfully"
// @Failure      401 {object} map[string]any "Unauthorized"
// @Failure      500 {object} map[string]any "Failed to delete account. Please try again."
// @Router       /api/v2/user/me [delete]
func (h *ProfileHandlerV2) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": http.StatusUnauthorized,
			"message":    "Unauthorized",
		})
		return
	}

	if err := h.userService.DeleteAccount(r.Context(), userID); err != nil {
		if errors.Is(err, userdomain.ErrUserNotFound) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statusCode": http.StatusNotFound,
				"message":    "User not found",
			})
			return
		}
		h.logger.Error("delete account failed", "error", err, "user_id", userID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"statusCode": http.StatusInternalServerError,
			"message":    "Failed to delete account. Please try again.",
		})
		return
	}

	resp := map[string]string{
		"data":    "Account deleted successfully",
		"message": "OK",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

type publicProfilePageData struct {
	IsError         bool
	ErrorHeader     string
	ErrorText       string
	ID              string
	Username        string
	FirstName       string
	LastName        string
	Role            string
	ProfilePhotoURL string
	Initials        string
	BadgeTier       string
	BadgeLabel      string
	BadgeIcon       string
	About           string
	LocationText    string
	ShareURL        string
	Specializations []string
	PanditProfile   *userservice.PanditProfileV2
}

var ceremonyDisplayNames = map[string]string{
	"shraadh":       "Shraadh",
	"grihapravesh":  "Grihapravesh",
	"vivah":         "Vivah",
	"satyanarayan":  "Satyanarayan",
	"mundan":        "Mundan",
	"antim_sanskar": "Antim Sanskar",
	"vastu_shanti":  "Vastu Shanti",
	"naamkaran":     "Naamkaran",
	"upanayana":     "Upanayana",
}

var publicProfileTemplate = template.Must(template.New("public_profile").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{if .IsError}}{{.ErrorHeader}}{{else}}{{.FirstName}} {{.LastName}} (@{{.Username}}) on Medha{{end}}</title>
    
    {{if not .IsError}}
    <!-- Open Graph / Facebook Meta Tags -->
    <meta property="og:title" content="{{.FirstName}} {{.LastName}} (@{{.Username}}) on Medha" />
    <meta property="og:type" content="profile" />
    <meta property="og:url" content="{{.ShareURL}}" />
    <meta property="og:image" content="{{.ProfilePhotoURL}}" />
    <meta property="og:description" content="{{if eq .Role "pandit"}}{{.BadgeIcon}} {{.BadgeLabel}}{{if .PanditProfile}}{{if .PanditProfile.Parampara}} | {{.PanditProfile.Parampara}}{{end}}{{if .PanditProfile.VedaAffiliation}} | {{.PanditProfile.VedaAffiliation}}{{end}}{{end}} - Connect with {{.FirstName}} on Medha, the sacred bridge connecting Pandits and Yajmans.{{else}}Connect with {{.FirstName}} on Medha, the sacred bridge connecting Pandits and Yajmans.{{end}}" />
    
    <!-- Twitter Card Meta Tags -->
    <meta name="twitter:card" content="summary_large_image" />
    <meta name="twitter:title" content="{{.FirstName}} {{.LastName}} (@{{.Username}}) on Medha" />
    <meta name="twitter:image" content="{{.ProfilePhotoURL}}" />
    <meta name="twitter:description" content="{{if eq .Role "pandit"}}{{.BadgeIcon}} {{.BadgeLabel}} | Connect with {{.FirstName}} on Medha.{{else}}Connect with {{.FirstName}} on Medha.{{end}}" />
    {{end}}

    <!-- Google Fonts -->
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&family=Inter:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    
    <style>
        :root {
            --bg-primary: #0b0f19;
            --bg-secondary: rgba(255, 255, 255, 0.03);
            --border-glow: rgba(249, 115, 22, 0.2);
            --primary-orange: #f97316;
            --secondary-orange: #ea580c;
            --text-primary: #f8fafc;
            --text-secondary: #94a3b8;
            --gold: #f59e0b;
        }
        
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        
        body {
            font-family: 'Inter', sans-serif;
            background-color: var(--bg-primary);
            color: var(--text-primary);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            padding: 24px;
            overflow-x: hidden;
            position: relative;
        }

        /* Ambient mesh background effects */
        body::before {
            content: '';
            position: absolute;
            width: 400px;
            height: 400px;
            background: radial-gradient(circle, rgba(249, 115, 22, 0.12) 0%, rgba(249, 115, 22, 0) 70%);
            top: -100px;
            left: -100px;
            z-index: 0;
        }
        
        body::after {
            content: '';
            position: absolute;
            width: 450px;
            height: 450px;
            background: radial-gradient(circle, rgba(245, 158, 11, 0.08) 0%, rgba(245, 158, 11, 0) 70%);
            bottom: -100px;
            right: -100px;
            z-index: 0;
        }

        /* Glassmorphism Card Container */
        .card {
            background: rgba(17, 24, 39, 0.75);
            backdrop-filter: blur(20px);
            -webkit-backdrop-filter: blur(20px);
            border: 1px solid rgba(255, 255, 255, 0.08);
            border-radius: 24px;
            width: 100%;
            max-width: 440px;
            padding: 40px 32px;
            text-align: center;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
            z-index: 10;
            position: relative;
        }
        
        .card::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            height: 4px;
            background: linear-gradient(90deg, var(--primary-orange), var(--gold));
            border-radius: 24px 24px 0 0;
        }

        /* Medha Logo / Header */
        .header {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 8px;
            margin-bottom: 32px;
        }

        .logo-emblem {
            font-size: 24px;
        }

        .brand-name {
            font-family: 'Outfit', sans-serif;
            font-size: 24px;
            font-weight: 700;
            background: linear-gradient(135deg, var(--primary-orange), var(--gold));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            letter-spacing: 0.5px;
        }
        
        /* Avatar styles */
        .avatar-container {
            position: relative;
            width: 110px;
            height: 110px;
            margin: 0 auto 20px;
        }

        .avatar {
            width: 100%;
            height: 100%;
            border-radius: 50%;
            object-fit: cover;
            border: 3px solid rgba(249, 115, 22, 0.3);
            padding: 4px;
            background: rgba(15, 23, 42, 0.6);
            box-shadow: 0 8px 20px rgba(0, 0, 0, 0.3);
        }
        
        .avatar-placeholder {
            width: 100%;
            height: 100%;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-family: 'Outfit', sans-serif;
            font-size: 38px;
            font-weight: 600;
            color: #ffffff;
            background: linear-gradient(135deg, var(--primary-orange), var(--gold));
            border: 3px solid rgba(255, 255, 255, 0.1);
            box-shadow: 0 8px 20px rgba(249, 115, 22, 0.25);
        }

        /* Profile Details */
        .name {
            font-family: 'Outfit', sans-serif;
            font-size: 22px;
            font-weight: 600;
            color: var(--text-primary);
            margin-bottom: 4px;
        }
        
        .username {
            font-size: 14px;
            color: var(--text-secondary);
            margin-bottom: 16px;
        }

        /* Role & Badge */
        .badge-row {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 8px;
            margin-bottom: 24px;
        }

        .badge-pill {
            background: rgba(249, 115, 22, 0.1);
            border: 1px solid rgba(249, 115, 22, 0.2);
            padding: 6px 12px;
            border-radius: 20px;
            font-size: 11px;
            font-weight: 600;
            color: var(--primary-orange);
            text-transform: uppercase;
            letter-spacing: 1px;
        }

        .dharmic-badge {
            background: rgba(245, 158, 11, 0.1);
            border: 1px solid rgba(245, 158, 11, 0.2);
            padding: 6px 12px;
            border-radius: 20px;
            font-size: 11px;
            font-weight: 600;
            color: var(--gold);
        }

        /* Pandit Details Grid */
        .details-grid {
            text-align: left;
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid rgba(255, 255, 255, 0.05);
            border-radius: 16px;
            padding: 20px;
            margin-bottom: 28px;
        }

        .details-row {
            display: flex;
            justify-content: space-between;
            margin-bottom: 12px;
            font-size: 14px;
        }

        .details-row:last-child {
            margin-bottom: 0;
        }

        .details-label {
            color: var(--text-secondary);
            font-weight: 500;
        }

        .details-value {
            color: var(--text-primary);
            font-weight: 600;
            max-width: 60%;
            text-align: right;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .specializations {
            display: flex;
            flex-wrap: wrap;
            gap: 6px;
            margin-top: 14px;
            padding-top: 14px;
            border-top: 1px solid rgba(255, 255, 255, 0.05);
        }

        .spec-tag {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid rgba(255, 255, 255, 0.08);
            color: var(--text-primary);
            padding: 4px 10px;
            border-radius: 12px;
            font-size: 12px;
            font-weight: 500;
        }

        /* About / Bio */
        .about-section {
            font-size: 13px;
            line-height: 1.6;
            color: var(--text-secondary);
            margin-bottom: 24px;
            text-align: left;
            background: rgba(255, 255, 255, 0.01);
            padding: 12px 16px;
            border-left: 2px solid var(--primary-orange);
            border-radius: 0 12px 12px 0;
        }

        /* Call To Actions */
        .btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            width: 100%;
            background: linear-gradient(135deg, var(--primary-orange), var(--secondary-orange));
            color: #ffffff;
            font-family: 'Outfit', sans-serif;
            font-size: 16px;
            font-weight: 600;
            padding: 16px 24px;
            border-radius: 16px;
            text-decoration: none;
            border: none;
            cursor: pointer;
            box-shadow: 0 10px 20px -5px rgba(234, 88, 12, 0.3);
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            margin-bottom: 16px;
        }

        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 15px 25px -5px rgba(234, 88, 12, 0.45);
            background: linear-gradient(135deg, #f97316, #f59e0b);
        }

        .btn:active {
            transform: translateY(0);
        }

        .app-store-text {
            font-size: 12px;
            color: var(--text-secondary);
        }

        .app-store-link {
            color: var(--primary-orange);
            text-decoration: none;
            font-weight: 600;
            transition: color 0.2s;
        }

        .app-store-link:hover {
            color: var(--gold);
        }

        /* Animations */
        @keyframes fadeIn {
            from { opacity: 0; transform: translateY(10px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .card {
            animation: fadeIn 0.6s cubic-bezier(0.16, 1, 0.3, 1) forwards;
        }

        /* Styling for 404/Error View */
        .error-title {
            font-family: 'Outfit', sans-serif;
            font-size: 32px;
            font-weight: 700;
            color: var(--primary-orange);
            margin-bottom: 12px;
        }

        .error-desc {
            color: var(--text-secondary);
            font-size: 14px;
            line-height: 1.6;
            margin-bottom: 24px;
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="header">
            <span class="logo-emblem">🕉️</span>
            <span class="brand-name">Medha</span>
        </div>
        
        {{if .IsError}}
            <div class="error-title">🕉️</div>
            <div class="error-title">{{.ErrorHeader}}</div>
            <p class="error-desc">{{.ErrorText}}</p>
            <a href="https://medha.app" class="btn">Go to Medha Website</a>
        {{else}}
            <div class="avatar-container">
                {{if .ProfilePhotoURL}}
                    <img class="avatar" src="{{.ProfilePhotoURL}}" alt="{{.FirstName}}'s profile image">
                {{else}}
                    <div class="avatar-placeholder">{{.Initials}}</div>
                {{end}}
            </div>
            
            <h1 class="name">{{.FirstName}} {{.LastName}}</h1>
            <p class="username">@{{.Username}}</p>
            
            <div class="badge-row">
                <span class="badge-pill">{{.Role}}</span>
                {{if .BadgeLabel}}
                    <span class="dharmic-badge">{{.BadgeIcon}} {{.BadgeLabel}}</span>
                {{end}}
            </div>

            {{if .About}}
                <div class="about-section">
                    {{.About}}
                </div>
            {{end}}
            
            {{if eq .Role "pandit"}}
                {{if .PanditProfile}}
                    <div class="details-grid">
                        {{if .PanditProfile.Parampara}}
                            <div class="details-row">
                                <span class="details-label">Parampara</span>
                                <span class="details-value">{{.PanditProfile.Parampara}}</span>
                            </div>
                        {{end}}
                        {{if .PanditProfile.VedaAffiliation}}
                            <div class="details-row">
                                <span class="details-label">Veda Affiliation</span>
                                <span class="details-value">{{.PanditProfile.VedaAffiliation}}</span>
                            </div>
                        {{end}}
                        {{if .LocationText}}
                            <div class="details-row">
                                <span class="details-label">Location</span>
                                <span class="details-value">{{.LocationText}}</span>
                            </div>
                        {{end}}
                        
                        {{if .Specializations}}
                            <div class="specializations">
                                {{range .Specializations}}
                                    <span class="spec-tag">{{.}}</span>
                                {{end}}
                            </div>
                        {{end}}
                    </div>
                {{end}}
            {{else}}
                {{if .LocationText}}
                    <div class="details-grid">
                        <div class="details-row">
                            <span class="details-label">Location</span>
                            <span class="details-value">{{.LocationText}}</span>
                        </div>
                    </div>
                {{end}}
            {{end}}
            
            <a href="medha://profile/{{.Username}}" class="btn" id="open-app-btn">Open in Medha App</a>
            
            <p class="app-store-text">
                Don't have the app? 
                <a class="app-store-link" href="https://medha.app">Download Medha</a>
            </p>

            <script>
                // Deep link auto-redirection on load
                (function() {
                    var username = "{{.Username}}";
                    var deepLink = "medha://profile/" + encodeURIComponent(username);
                    
                    // Attempt to redirect immediately
                    window.location.href = deepLink;
                })();
            </script>
        {{end}}
    </div>
</body>
</html>`))

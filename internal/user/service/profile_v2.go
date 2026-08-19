package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/user/domain"
)

// SetupProfileInput holds the data for the POST /api/v2/user/profile endpoint.
type SetupProfileInput struct {
	FirstName string `json:"first_name" validate:"required,min=1,max=50"`
	LastName  string `json:"last_name"  validate:"required,min=1,max=50"`
	Role      string `json:"role"       validate:"required,oneof=yajman pandit"`
	Username  string `json:"username"   validate:"omitempty,min=3,max=30"`
	Email     string `json:"email"      validate:"omitempty,email"`
}

// ProfileResponse is the response for GET/POST /api/v2/user/profile.
type ProfileResponse struct {
	ID                uuid.UUID        `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Phone             string           `json:"phone,omitempty" example:"+919999999999"`
	Email             string           `json:"email,omitempty" example:"user@example.com"`
	Username          string           `json:"username,omitempty" example:"syntheticuser"`
	FirstName         string           `json:"first_name" example:"Synthetic"`
	LastName          string           `json:"last_name" example:"User"`
	Role              string           `json:"role,omitempty" example:"yajman"`
	ProfilePhotoURL   string           `json:"profile_photo_url,omitempty" example:"https://cdn.medha.app/photos/abc.jpg"`
	IsProfileComplete bool             `json:"is_profile_complete" example:"true"`
	IsPhoneVerified   bool             `json:"is_phone_verified" example:"true"`
	Latitude          float64          `json:"latitude,omitempty" example:"12.9716"`
	Longitude         float64          `json:"longitude,omitempty" example:"77.5946"`
	PanditProfile     *PanditProfileV2 `json:"pandit_profile"`
}

// PanditProfileV2 is a simplified pandit profile for the V2 API.
type PanditProfileV2 struct {
	Parampara               string   `json:"parampara,omitempty"`
	VedaAffiliation         string   `json:"veda_affiliation,omitempty"`
	CeremonySpecializations []string `json:"ceremony_specializations,omitempty"`
	Languages               []string `json:"languages,omitempty"`
	ServiceRadiusKM         int      `json:"service_radius_km,omitempty"`
	AvailabilityStatus      string   `json:"availability_status,omitempty"`
	About                   string   `json:"about,omitempty"`
}

// reservedUsernames may never be claimed by a human-registered account —
// they're set aside for platform/system publishers (see system_publishers).
var reservedUsernames = map[string]bool{
	"medha":     true,
	"medhaapp":  true,
	"medha-app": true,
	"admin":     true,
	"support":   true,
}

// SetupProfile updates the user's profile and marks it complete.
func (s *UserService) SetupProfile(ctx context.Context, userID uuid.UUID, input SetupProfileInput) (ProfileResponse, error) {
	if input.Username != "" && reservedUsernames[strings.ToLower(input.Username)] {
		return ProfileResponse{}, domain.ErrUsernameTaken
	}

	s.logger.Info("SVC TRACE: SetupProfile input",
		"userID", userID,
		"username", input.Username,
		"email", input.Email,
	)
	if err := s.userRepo.SetupProfile(ctx, userID, input.FirstName, input.LastName, input.Role, input.Username, input.Email); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return ProfileResponse{}, domain.ErrUserNotFound
		}
		return ProfileResponse{}, fmt.Errorf("setup profile: %w", err)
	}

	// If pandit, ensure a pandit_profile row exists
	if input.Role == "pandit" {
		if ppRepo, ok := s.userRepo.(panditProfileCreator); ok {
			_ = ppRepo.EnsurePanditProfile(ctx, userID)
		}
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return ProfileResponse{}, fmt.Errorf("get user after setup: %w", err)
	}

	s.logger.Info("profile setup complete",
		"user_id", userID,
		"role", input.Role,
		"username", user.Username,
		"email", user.Email,
	)
	return toProfileResponse(user, nil), nil
}

// GetProfile fetches a user's profile (including pandit profile if pandit).
func (s *UserService) GetProfile(ctx context.Context, userID uuid.UUID) (ProfileResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return ProfileResponse{}, domain.ErrUserNotFound
		}
		return ProfileResponse{}, fmt.Errorf("get profile: %w", err)
	}

	var panditResp *PanditProfileV2
	if user.Role == authdomain.RolePandit {
		pp, err := s.panditRepo.GetByUserID(ctx, userID)
		if err == nil {
			panditResp = &PanditProfileV2{
				Parampara:               pp.Parampara,
				VedaAffiliation:         pp.VedaAffiliation,
				CeremonySpecializations: pp.CeremonySpecializations,
				Languages:               pp.Languages,
				ServiceRadiusKM:         pp.ServiceRadiusKM,
				AvailabilityStatus:      pp.AvailabilityStatus.String(),
				About:                   pp.About,
			}
		}
	}

	return toProfileResponse(user, panditResp), nil
}

// panditProfileCreator is an optional interface that UserRepository implementations
// may satisfy to allow creating empty pandit_profile rows.
type panditProfileCreator interface {
	EnsurePanditProfile(ctx context.Context, userID uuid.UUID) error
}

// toProfileResponse converts a domain User to a ProfileResponse.
func toProfileResponse(user *domain.User, pandit *PanditProfileV2) ProfileResponse {
	profileComplete := user.ProfileComplete ||
		(user.FirstName != "" && user.LastName != "" && user.Role.IsProfileRole())

	resp := ProfileResponse{
		ID:                user.ID,
		Phone:             user.Phone,
		Email:             user.Email,
		Username:          user.Username,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		Role:              user.Role.String(),
		ProfilePhotoURL:   user.ProfilePhotoURL,
		IsProfileComplete: profileComplete,
		IsPhoneVerified:   user.PhoneVerified,
		PanditProfile:     pandit,
	}

	if user.Location != nil {
		resp.Latitude = user.Location.Latitude
		resp.Longitude = user.Location.Longitude
	}

	return resp
}

// SetupPanditProfileInput holds the data for PUT /api/v2/pandit/profile.
type SetupPanditProfileInput struct {
	Parampara               string   `json:"parampara"`
	VedaAffiliation         string   `json:"veda_affiliation"`
	CeremonySpecializations []string `json:"ceremony_specializations"`
	Languages               []string `json:"languages"`
	ServiceRadiusKM         int      `json:"service_radius_km"`
	AvailabilityStatus      string   `json:"availability_status"`
	About                   string   `json:"about"`
}

// PanditProfileFullResponse is the response for GET/PUT /api/v2/pandit/profile.
// It merges user info with the pandit professional details.
type PanditProfileFullResponse struct {
	UserID                  uuid.UUID `json:"user_id"                            example:"550e8400-e29b-41d4-a716-446655440000"`
	FirstName               string    `json:"first_name"                         example:"Ramesh"`
	LastName                string    `json:"last_name"                          example:"Sharma"`
	ProfilePhotoURL         string    `json:"profile_photo_url,omitempty"        example:"https://cdn.medha.app/photos/abc.jpg"`
	Parampara               string    `json:"parampara,omitempty"                example:"Smartha"`
	VedaAffiliation         string    `json:"veda_affiliation,omitempty"         example:"Rigveda"`
	CeremonySpecializations []string  `json:"ceremony_specializations"           example:"[\"vivah\",\"grihapravesh\"]"`
	Languages               []string  `json:"languages"                          example:"[\"Hindi\",\"Sanskrit\"]"`
	ServiceRadiusKM         int       `json:"service_radius_km"                  example:"30"`
	AvailabilityStatus      string    `json:"availability_status"               example:"available"`
	About                   string    `json:"about,omitempty"                    example:"Experienced pandit with 15+ years."`
	IsProfileComplete       bool      `json:"is_profile_complete"               example:"true"`
}

// SetupPanditProfileV2 creates or updates the pandit's professional profile.
// Returns ErrUserNotFound, ErrInvalidUserRole, or wrapped DB errors.
func (s *UserService) SetupPanditProfileV2(ctx context.Context, userID uuid.UUID, input SetupPanditProfileInput) (PanditProfileFullResponse, error) {
	// Verify user exists and has pandit role
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return PanditProfileFullResponse{}, domain.ErrUserNotFound
		}
		return PanditProfileFullResponse{}, fmt.Errorf("get user: %w", err)
	}

	if user.Role != authdomain.RolePandit {
		return PanditProfileFullResponse{}, domain.ErrInvalidUserRole
	}

	// Resolve defaults
	availStatus := domain.AvailabilityAvailable
	if input.AvailabilityStatus != "" {
		availStatus = domain.AvailabilityStatus(input.AvailabilityStatus)
	}
	serviceRadius := input.ServiceRadiusKM
	if serviceRadius <= 0 {
		serviceRadius = 25
	}
	if input.CeremonySpecializations == nil {
		input.CeremonySpecializations = []string{}
	}
	if input.Languages == nil {
		input.Languages = []string{}
	}

	profile := &domain.PanditProfile{
		UserID:                  userID,
		Parampara:               input.Parampara,
		VedaAffiliation:         input.VedaAffiliation,
		CeremonySpecializations: input.CeremonySpecializations,
		Languages:               input.Languages,
		ServiceRadiusKM:         serviceRadius,
		AvailabilityStatus:      availStatus,
		About:                   input.About,
	}

	// Try to get existing, create if absent, update if present
	existing, err := s.panditRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrPanditProfileNotFound) {
			if createErr := s.panditRepo.Create(ctx, profile); createErr != nil {
				return PanditProfileFullResponse{}, fmt.Errorf("create pandit profile: %w", createErr)
			}
		} else {
			return PanditProfileFullResponse{}, fmt.Errorf("get pandit profile: %w", err)
		}
	} else {
		existing.Parampara = profile.Parampara
		existing.VedaAffiliation = profile.VedaAffiliation
		existing.CeremonySpecializations = profile.CeremonySpecializations
		existing.Languages = profile.Languages
		existing.ServiceRadiusKM = profile.ServiceRadiusKM
		existing.AvailabilityStatus = profile.AvailabilityStatus
		existing.About = profile.About
		if updateErr := s.panditRepo.Update(ctx, existing); updateErr != nil {
			return PanditProfileFullResponse{}, fmt.Errorf("update pandit profile: %w", updateErr)
		}
	}

	// Fetch updated to return canonical state
	updated, err := s.panditRepo.GetByUserID(ctx, userID)
	if err != nil {
		return PanditProfileFullResponse{}, fmt.Errorf("fetch pandit profile after upsert: %w", err)
	}

	s.logger.Info("pandit profile v2 upserted", "user_id", userID)
	return toPanditProfileFullResponse(user, updated), nil
}

// GetPanditProfileV2 fetches the pandit's professional profile merged with user info.
// Returns ErrUserNotFound or ErrPanditProfileNotFound as sentinel errors.
func (s *UserService) GetPanditProfileV2(ctx context.Context, userID uuid.UUID) (PanditProfileFullResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return PanditProfileFullResponse{}, domain.ErrUserNotFound
		}
		return PanditProfileFullResponse{}, fmt.Errorf("get user: %w", err)
	}

	if user.Role != authdomain.RolePandit {
		return PanditProfileFullResponse{}, domain.ErrInvalidUserRole
	}

	pp, err := s.panditRepo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrPanditProfileNotFound) {
			return PanditProfileFullResponse{}, domain.ErrPanditProfileNotFound
		}
		return PanditProfileFullResponse{}, fmt.Errorf("get pandit profile: %w", err)
	}

	return toPanditProfileFullResponse(user, pp), nil
}

// toPanditProfileFullResponse merges a User and PanditProfile into the V2 response DTO.
func toPanditProfileFullResponse(user *domain.User, pp *domain.PanditProfile) PanditProfileFullResponse {
	profileComplete := user.ProfileComplete ||
		(user.FirstName != "" && user.LastName != "" && user.Role.IsProfileRole())

	resp := PanditProfileFullResponse{
		UserID:                  user.ID,
		FirstName:               user.FirstName,
		LastName:                user.LastName,
		ProfilePhotoURL:         user.ProfilePhotoURL,
		Parampara:               pp.Parampara,
		VedaAffiliation:         pp.VedaAffiliation,
		CeremonySpecializations: pp.CeremonySpecializations,
		Languages:               pp.Languages,
		ServiceRadiusKM:         pp.ServiceRadiusKM,
		AvailabilityStatus:      pp.AvailabilityStatus.String(),
		About:                   pp.About,
		IsProfileComplete:       profileComplete,
	}
	if resp.CeremonySpecializations == nil {
		resp.CeremonySpecializations = []string{}
	}
	if resp.Languages == nil {
		resp.Languages = []string{}
	}
	return resp
}

// PublicProfileResponse is a safe, limited response returned by public endpoints.
type PublicProfileResponse struct {
	ID              uuid.UUID        `json:"id"`
	Username        string           `json:"username"`
	FirstName       string           `json:"first_name"`
	LastName        string           `json:"last_name"`
	Role            string           `json:"role"`
	ProfilePhotoURL string           `json:"profile_photo_url,omitempty"`
	City            string           `json:"city,omitempty"`
	State           string           `json:"state,omitempty"`
	BadgeTier       string           `json:"badge_tier,omitempty"`
	BadgeLabel      string           `json:"badge_label,omitempty"`
	BadgeIcon       string           `json:"badge_icon,omitempty"`
	PanditProfile   *PanditProfileV2 `json:"pandit_profile,omitempty"`
	ShareURL        string           `json:"share_url"`
}

// GetPublicProfileByUsername retrieves a public user profile by username.
func (s *UserService) GetPublicProfileByUsername(ctx context.Context, username string, shareBaseURL string) (PublicProfileResponse, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return PublicProfileResponse{}, domain.ErrUserNotFound
		}
		return PublicProfileResponse{}, fmt.Errorf("get user by username: %w", err)
	}

	var panditResp *PanditProfileV2
	var badgeTier, badgeLabel, badgeIcon string

	if user.Role == authdomain.RolePandit {
		pp, err := s.panditRepo.GetByUserID(ctx, user.ID)
		if err == nil {
			panditResp = &PanditProfileV2{
				Parampara:               pp.Parampara,
				VedaAffiliation:         pp.VedaAffiliation,
				CeremonySpecializations: pp.CeremonySpecializations,
				Languages:               pp.Languages,
				ServiceRadiusKM:         pp.ServiceRadiusKM,
				AvailabilityStatus:      pp.AvailabilityStatus.String(),
				About:                   pp.About,
			}
		}

		tier, err := s.userRepo.GetBadgeTier(ctx, user.ID)
		if err == nil {
			badgeTier = tier
			// Map to display values matching trust.go logic
			switch tier {
			case "pandit_ji":
				badgeLabel, badgeIcon = "Pandit ji", "🔶"
			case "puja_praveen":
				badgeLabel, badgeIcon = "Puja Praveen", "🟠"
			case "karma_kandi":
				badgeLabel, badgeIcon = "Karma Kandi", "🟤"
			case "yajna_maharshi":
				badgeLabel, badgeIcon = "Yajna Maharshi", "⭐"
			case "dharma_ratna":
				badgeLabel, badgeIcon = "Dharma Ratna", "💎"
			default:
				badgeLabel, badgeIcon = "Pandit ji", "🔶"
			}
		} else {
			badgeTier, badgeLabel, badgeIcon = "pandit_ji", "Pandit ji", "🔶"
		}
	}

	shareURL := fmt.Sprintf("%s/p/%s", strings.TrimSuffix(shareBaseURL, "/"), user.Username)

	resp := PublicProfileResponse{
		ID:              user.ID,
		Username:        user.Username,
		FirstName:       user.FirstName,
		LastName:        user.LastName,
		Role:            user.Role.String(),
		ProfilePhotoURL: user.ProfilePhotoURL,
		City:            user.City,
		State:           user.State,
		BadgeTier:       badgeTier,
		BadgeLabel:      badgeLabel,
		BadgeIcon:       badgeIcon,
		PanditProfile:   panditResp,
		ShareURL:        shareURL,
	}

	return resp, nil
}

// VerifyCeremonySpecialization verifies if the ceremony specialization is valid.
func (s *UserService) VerifyCeremonySpecialization(ctx context.Context, slug string) (bool, error) {
	return s.panditRepo.VerifyCeremonySlugExists(ctx, slug)
}

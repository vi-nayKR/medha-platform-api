package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/infra/storage"
	locationservice "github.com/medha/backend/internal/location/service"
	"github.com/medha/backend/internal/user/domain"
)

// UserService handles user-related business logic.
type UserService struct {
	userRepo   domain.UserRepository
	panditRepo domain.PanditProfileRepository
	mapService *locationservice.MapMyIndiaService
	s3Client   *storage.S3Client
	logger     *slog.Logger
}

// NewUserService creates a new UserService.
func NewUserService(userRepo domain.UserRepository, panditRepo domain.PanditProfileRepository, mapService *locationservice.MapMyIndiaService, logger *slog.Logger) *UserService {
	return &UserService{
		userRepo:   userRepo,
		panditRepo: panditRepo,
		mapService: mapService,
		logger:     logger,
	}
}

// SetS3Client injects the S3 client dependency.
func (s *UserService) SetS3Client(s3Client *storage.S3Client) {
	s.s3Client = s3Client
}

// DeleteAccount permanently deletes the authenticated user's account and all associated data.
func (s *UserService) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	s.logger.Info("starting permanent account deletion", "user_id", userID)

	// 1. Delete all user files from S3 storage if the client is configured
	if s.s3Client != nil {
		if err := s.s3Client.DeleteUserDirectory(ctx, userID.String()); err != nil {
			s.logger.Error("failed to delete user files from storage during account deletion", "user_id", userID, "error", err)
			// We continue with database deletion even if S3 cleanup fails, so user is deleted
		}
	}

	// 2. Delete user from database (cascades to all user-related data)
	if err := s.userRepo.Delete(ctx, userID); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return domain.ErrUserNotFound
		}
		s.logger.Error("failed to delete user from database during account deletion", "user_id", userID, "error", err)
		return fmt.Errorf("delete user from database: %w", err)
	}

	s.logger.Info("account deleted successfully", "user_id", userID)
	return nil
}

// UpdateRole updates a user's role. Idempotent: setting the same role is a no-op success.
func (s *UserService) UpdateRole(ctx context.Context, userID uuid.UUID, role string) (*domain.User, error) {
	// Validate role
	newRole := authdomain.UserRole(role)
	if !newRole.IsValid() {
		return nil, domain.ErrInvalidUserRole
	}

	// Get user
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	// Idempotent check
	if user.Role == newRole {
		return user, nil
	}

	// Update role
	user.Role = newRole
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update user role: %w", err)
	}

	s.logger.Info("user role updated",
		"user_id", userID,
		"new_role", role,
	)

	return user, nil
}

// GetByID retrieves a user by their ID.
func (s *UserService) GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return user, nil
}

// geocodeLocation reverse geocodes latitude/longitude coordinates to extract city, state, and pincode.
func (s *UserService) geocodeLocation(ctx context.Context, lat, lng float64) (string, string, string) {
	if s.mapService == nil {
		s.logger.Warn("Map service not configured, skipping reverse geocoding")
		return "", "", ""
	}

	result, err := s.mapService.ReverseGeocode(lat, lng)
	if err != nil {
		s.logger.Error("failed to reverse geocode location", "lat", lat, "lng", lng, "error", err)
		return "", "", ""
	}

	if len(result.Results) > 0 {
		first := result.Results[0]
		city := first.City
		if city == "" {
			city = first.District // Fallback to District
		}
		if city == "" {
			city = first.Locality // Fallback to Locality
		}
		return city, first.State, first.Pincode
	}

	return "", "", ""
}

// UpdateLocation updates a user's geographic location.
func (s *UserService) UpdateLocation(ctx context.Context, userID uuid.UUID, latitude, longitude float64) (*domain.User, error) {
	city, state, pincode := s.geocodeLocation(ctx, latitude, longitude)

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	user.Location = &authdomain.GeoPoint{
		Latitude:  latitude,
		Longitude: longitude,
	}
	user.City = city
	user.State = state
	user.Pincode = pincode

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("update user location: %w", err)
	}

	s.logger.Info("user location updated",
		"user_id", userID,
		"latitude", latitude,
		"longitude", longitude,
		"city", city,
		"state", state,
	)

	return user, nil
}

// UpdateLocationV2 stores the user's location using a targeted SQL UPDATE (v2 API).
// Unlike UpdateLocation it does not perform a full user fetch+rewrite.
func (s *UserService) UpdateLocationV2(ctx context.Context, userID uuid.UUID, latitude, longitude float64) error {
	city, state, pincode := s.geocodeLocation(ctx, latitude, longitude)

	if err := s.userRepo.UpdateLocation(ctx, userID, latitude, longitude, city, state, pincode); err != nil {
		return fmt.Errorf("update location v2: %w", err)
	}
	s.logger.Info("user location updated (v2)", "user_id", userID, "lat", latitude, "lng", longitude, "city", city)
	return nil
}

// UpdateProfilePhoto updates the user's profile photo URL.
func (s *UserService) UpdateProfilePhoto(ctx context.Context, userID uuid.UUID, photoURL string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	user.ProfilePhotoURL = photoURL

	if err := s.userRepo.Update(ctx, user); err != nil {
		return fmt.Errorf("update profile photo: %w", err)
	}

	s.logger.Info("user profile photo updated",
		"user_id", userID,
	)

	return nil
}

// CheckUsernameExists natively queries the database to see if a username is taken.
func (s *UserService) CheckUsernameExists(ctx context.Context, username string) (bool, error) {
	exists, err := s.userRepo.CheckUsernameExists(ctx, username)
	if err != nil {
		return false, fmt.Errorf("check username exists: %w", err)
	}
	return exists, nil
}

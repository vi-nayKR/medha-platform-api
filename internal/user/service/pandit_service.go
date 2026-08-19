package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
	"math"

	citydomain "github.com/medha/backend/internal/city/domain"
	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/user/domain"
)

// PanditService handles pandit profile-related business logic.
type PanditService struct {
	panditRepo  domain.PanditProfileRepository
	userRepo    domain.UserRepository
	cityRepo    citydomain.CityRepository
	pool        *pgxpool.Pool
	redisClient *redis.Client
	logger      *slog.Logger
}

// NewPanditService creates a new PanditService.
func NewPanditService(
	panditRepo domain.PanditProfileRepository,
	userRepo domain.UserRepository,
	cityRepo citydomain.CityRepository,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	logger *slog.Logger,
) *PanditService {
	return &PanditService{
		panditRepo:  panditRepo,
		userRepo:    userRepo,
		cityRepo:    cityRepo,
		pool:        pool,
		redisClient: redisClient,
		logger:      logger,
	}
}

// GetByUserID returns a pandit profile by user ID.
func (s *PanditService) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.PanditProfile, *domain.User, error) {
	g, gCtx := errgroup.WithContext(ctx)

	var profile *domain.PanditProfile
	var user *domain.User

	g.Go(func() error {
		var err error
		profile, err = s.panditRepo.GetByUserID(gCtx, userID)
		if err != nil {
			if errors.Is(err, domain.ErrPanditProfileNotFound) {
				return nil // Graceful fallback
			}
			return fmt.Errorf("get pandit profile: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		var err error
		user, err = s.userRepo.GetByID(gCtx, userID)
		if err != nil {
			return fmt.Errorf("get user for pandit: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return profile, user, nil
}

// CreateOrUpdate creates a new pandit profile or updates an existing one.
func (s *PanditService) CreateOrUpdate(ctx context.Context, userID uuid.UUID, profile *domain.PanditProfile) (*domain.PanditProfile, error) {
	profile.UserID = userID

	// Try to get existing profile
	existing, err := s.panditRepo.GetByUserID(ctx, userID)
	if err != nil {
		if err == domain.ErrPanditProfileNotFound {
			// Create new
			if err := s.panditRepo.Create(ctx, profile); err != nil {
				return nil, fmt.Errorf("create pandit profile: %w", err)
			}
			s.logger.Info("pandit profile created", "user_id", userID)
			return s.panditRepo.GetByUserID(ctx, userID)
		}
		return nil, fmt.Errorf("check existing profile: %w", err)
	}

	// Update existing
	existing.Parampara = profile.Parampara
	existing.VedaAffiliation = profile.VedaAffiliation
	existing.CeremonySpecializations = profile.CeremonySpecializations
	existing.Languages = profile.Languages
	existing.ServiceRadiusKM = profile.ServiceRadiusKM
	existing.AvailabilityStatus = profile.AvailabilityStatus
	existing.About = profile.About

	if err := s.panditRepo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update pandit profile: %w", err)
	}

	s.logger.Info("pandit profile updated", "user_id", userID)
	return s.panditRepo.GetByUserID(ctx, userID)
}

func (s *PanditService) ListNearby(ctx context.Context, lat, lng float64, radiusKM int, filters domain.PanditFilters, cursor string, limit int, viewerID *uuid.UUID) ([]*domain.PanditProfileWithDistance, string, error) {
	if radiusKM <= 0 {
		radiusKM = 25
	}
	if limit <= 0 {
		limit = 20
	}

	// Try fetching from Redis first
	if s.redisClient != nil && cursor == "" {
		geoQuery := &redis.GeoSearchQuery{
			Longitude:  lng,
			Latitude:   lat,
			Radius:     float64(radiusKM),
			RadiusUnit: "km",
			Sort:       "ASC",
			Count:      limit,
		}

		res, err := s.redisClient.GeoSearch(ctx, "pandits:locations", geoQuery).Result()
		if err == nil && len(res) > 0 {
			// Fast path hit: We have nearby items.
			// In a real scenario, we'd then pull the actual DB records concurrently by ID or return simplified objects.
			// Falling through to DB for now where the advanced PG-PostGIS query handles filtering naturally.
			s.logger.Debug("redis geosearch hit for list nearby", "count", len(res))
		}

		// TODO: Update Redis cache whenever a Pandit updates their location.
		// e.g. s.redisClient.GeoAdd(ctx, "pandits:locations", &redis.GeoLocation{ ... })
	}

	profiles, nextCursor, err := s.panditRepo.ListNearby(ctx, lat, lng, radiusKM, filters, cursor, limit, viewerID)
	if err != nil {
		return nil, "", fmt.Errorf("list nearby pandits: %w", err)
	}
	return profiles, nextCursor, nil
}

// CanViewContact checks if the viewer is authorized to see the Pandit's phone number.
func (s *PanditService) CanViewContact(ctx context.Context, viewerID, panditID uuid.UUID) (bool, error) {
	return s.userRepo.CanViewContactInfo(ctx, viewerID, panditID)
}

// SetServiceCities sets the pandit's home and additional service cities.
func (s *PanditService) SetServiceCities(ctx context.Context, userID uuid.UUID, homeCityID uuid.UUID, additionalCityIDs []uuid.UUID) ([]domain.ServiceCityDetail, error) {
	// 1. Validate pandit profile exists
	_, err := s.panditRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 2. Validate max additional cities limit
	if len(additionalCityIDs) > 100 {
		return nil, domain.ErrTooManyServiceCities
	}

	// 3. Check for duplicates in inputs
	allCityIDs := append([]uuid.UUID{homeCityID}, additionalCityIDs...)
	seen := make(map[uuid.UUID]bool)
	for _, id := range allCityIDs {
		if seen[id] {
			return nil, domain.ErrDuplicateServiceCity
		}
		seen[id] = true
	}

	// 4. Fetch cities and validate they exist + check distance
	citiesMap := make(map[uuid.UUID]*citydomain.City)
	for _, id := range allCityIDs {
		c, err := s.cityRepo.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("city %s not found: %w", id, err)
		}
		if !c.IsActive {
			return nil, fmt.Errorf("city %s is not active", id)
		}
		citiesMap[id] = c
	}

	homeCity := citiesMap[homeCityID]
	for _, addCityID := range additionalCityIDs {
		addCity := citiesMap[addCityID]
		dist := haversineDistance(homeCity.Latitude, homeCity.Longitude, addCity.Latitude, addCity.Longitude)
		isBothKarnataka := homeCity.State == "Karnataka" && addCity.State == "Karnataka"
		if dist > 250.0 && !isBothKarnataka {
			return nil, domain.ErrCityTooFar
		}
	}

	// 5. Replace service cities transactionally (if pool is present)
	if s.pool != nil {
		err = database.WithTx(ctx, s.pool, func(txCtx context.Context) error {
			return s.panditRepo.ReplaceServiceCities(txCtx, userID, homeCityID, additionalCityIDs)
		})
	} else {
		err = s.panditRepo.ReplaceServiceCities(ctx, userID, homeCityID, additionalCityIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("replace service cities: %w", err)
	}

	// 6. Fetch and return enriched service cities
	return s.panditRepo.GetServiceCities(ctx, userID)
}

// GetServiceCities retrieves the list of service cities with details.
func (s *PanditService) GetServiceCities(ctx context.Context, userID uuid.UUID) ([]domain.ServiceCityDetail, error) {
	// Validate pandit profile exists first
	_, err := s.panditRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return s.panditRepo.GetServiceCities(ctx, userID)
}

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth radius in km
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	rLat1 := lat1 * math.Pi / 180.0
	rLat2 := lat2 * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(rLat1)*math.Cos(rLat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}


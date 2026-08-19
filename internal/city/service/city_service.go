package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/medha/backend/internal/city/domain"
	locationdomain "github.com/medha/backend/internal/location/domain"
	locationservice "github.com/medha/backend/internal/location/service"
)

type CityService struct {
	cityRepo   domain.CityRepository
	mapService *locationservice.MapMyIndiaService
	logger     *slog.Logger
}

func NewCityService(cityRepo domain.CityRepository, mapService *locationservice.MapMyIndiaService, logger *slog.Logger) *CityService {
	return &CityService{
		cityRepo:   cityRepo,
		mapService: mapService,
		logger:     logger,
	}
}

func (s *CityService) ListNearbyCities(ctx context.Context, lat, lng float64, radiusKm, limit int, excludeClosest bool) ([]domain.NearbyCityResult, error) {
	if radiusKm <= 0 {
		radiusKm = 200 // default 200 km
	}
	if limit <= 0 {
		limit = 15 // default 15 results
	}

	// Fetch limit + 3 in case we filter out multiple duplicates
	results, err := s.cityRepo.ListNearby(ctx, lat, lng, radiusKm, limit+3)
	if err != nil {
		return nil, fmt.Errorf("list nearby cities service: %w", err)
	}

	// Filter out the current location's city (the closest one, plus any duplicates/variations)
	if excludeClosest && len(results) > 0 {
		closestCity := results[0]
		closestName := strings.ToLower(closestCity.City)
		if closestName == "" {
			closestName = strings.ToLower(closestCity.Name)
		}

		var filtered []domain.NearbyCityResult
		for _, r := range results {
			rName := strings.ToLower(r.City)
			if rName == "" {
				rName = strings.ToLower(r.Name)
			}

			// Check if same city or spelling variation (e.g., Tumkur/Tumakuru, Bangalore/Bengaluru)
			isSameCity := rName == closestName ||
				(strings.HasPrefix(rName, "tumk") && strings.HasPrefix(closestName, "tumk")) ||
				(strings.HasPrefix(rName, "beng") && strings.HasPrefix(closestName, "beng")) ||
				(strings.HasPrefix(rName, "bang") && strings.HasPrefix(closestName, "beng")) ||
				(strings.HasPrefix(rName, "beng") && strings.HasPrefix(closestName, "bang"))

			// Exclude if it is within 15km of the coordinates (current city area) or matches the closest city
			if r.DistanceKm < 15.0 || isSameCity {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}

	// Truncate to the requested limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (s *CityService) SearchCities(ctx context.Context, query string, limit int) ([]domain.City, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return []domain.City{}, nil
	}

	if limit <= 0 {
		limit = 10
	}

	// 1. Search database first
	dbCities, err := s.cityRepo.Search(ctx, query, limit)
	if err != nil {
		s.logger.Error("failed to search cities in db", "query", query, "error", err)
	}

	// If we have database results, return them
	if len(dbCities) > 0 {
		return dbCities, nil
	}

	// 2. Fall back to MapMyIndia if DB search has no matches
	if s.mapService == nil {
		s.logger.Warn("map service not configured, skipping fallback search")
		return []domain.City{}, nil
	}

	s.logger.Info("no cities found in DB, falling back to MapMyIndia autocomplete", "query", query)
	resp, err := s.mapService.Autocomplete(query, "IND")
	if err != nil {
		return nil, fmt.Errorf("mapmyindia autocomplete: %w", err)
	}

	var results []domain.City
	for _, sug := range resp.Suggestions {
		// Only keep suggestions representing actual cities or localities (filter out POIs/streets/houses)
		lowerType := strings.ToLower(sug.Type)
		if strings.Contains(lowerType, "poi") || 
		   strings.Contains(lowerType, "street") || 
		   strings.Contains(lowerType, "house") {
			continue
		}

		// We extract the place name, actual city name, and state.
		placeName, cityName, stateName := parseCityNameAndState(sug)
		if placeName == "" || stateName == "" {
			continue
		}

		// Check if this city already exists in the database by (name, state)
		city, err := s.cityRepo.GetByName(ctx, placeName, stateName)
		if err != nil && !strings.Contains(err.Error(), "city not found") {
			s.logger.Error("error checking city by name", "name", placeName, "state", stateName, "error", err)
		}

		if city == nil {
			// Create a new city record
			newCity := &domain.City{
				ID:        uuid.New(),
				Name:      placeName,
				City:      cityName,
				State:     stateName,
				Latitude:  sug.Latitude,
				Longitude: sug.Longitude,
				IsActive:  true,
			}
			err = s.cityRepo.Create(ctx, newCity)
			if err != nil {
				s.logger.Error("failed to auto-create city from map result", "name", cityName, "state", stateName, "error", err)
				continue
			}
			city = newCity
		}

		results = append(results, *city)
		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

func (s *CityService) GetCityByID(ctx context.Context, id uuid.UUID) (*domain.City, error) {
	return s.cityRepo.GetByID(ctx, id)
}

func parseCityNameAndState(result locationdomain.AutocompleteResult) (string, string, string) {
	placeName := result.PlaceName
	stateName := "Karnataka"
	cityName := ""

	parts := strings.Split(result.PlaceAddress, ",")
	var cleanParts []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			cleanParts = append(cleanParts, trimmed)
		}
	}

	n := len(cleanParts)
	if n > 0 {
		stateIdx := -1
		cityIdx := -1

		for i := n - 1; i >= 0; i-- {
			p := cleanParts[i]
			if strings.EqualFold(p, "India") || strings.EqualFold(p, "IND") {
				continue
			}
			if isPincode(p) {
				continue
			}
			if stateIdx == -1 {
				stateIdx = i
				stateName = p
				continue
			}
			if cityIdx == -1 {
				cityIdx = i
				cityName = p
				break
			}
		}
	}

	if cityName == "" {
		cityName = placeName
	}

	// Normalize some common city names
	if strings.EqualFold(cityName, "Bangalore") || strings.EqualFold(cityName, "Bengaluru Urban") {
		cityName = "Bengaluru"
	}
	if strings.EqualFold(cityName, "Mysore") {
		cityName = "Mysuru"
	}
	if strings.EqualFold(cityName, "Tumkur") {
		cityName = "Tumakuru"
	}

	return placeName, cityName, stateName
}

func isPincode(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

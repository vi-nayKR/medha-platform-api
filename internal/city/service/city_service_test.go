package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/medha/backend/internal/city/domain"
	"github.com/medha/backend/internal/city/service"
)

type mockCityRepo struct {
	cities        map[uuid.UUID]*domain.City
	nearbyResults []domain.NearbyCityResult
}

func (m *mockCityRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.City, error) {
	c, ok := m.cities[id]
	if !ok {
		return nil, domain.ErrCityNotFound
	}
	return c, nil
}

func (m *mockCityRepo) GetByName(ctx context.Context, name, state string) (*domain.City, error) {
	for _, c := range m.cities {
		if c.Name == name && c.State == state {
			return c, nil
		}
	}
	return nil, domain.ErrCityNotFound
}

func (m *mockCityRepo) ListNearby(ctx context.Context, lat, lng float64, radiusKm int, limit int) ([]domain.NearbyCityResult, error) {
	return m.nearbyResults, nil
}

func (m *mockCityRepo) Search(ctx context.Context, query string, limit int) ([]domain.City, error) {
	var matches []domain.City
	for _, c := range m.cities {
		if c.Name == query {
			matches = append(matches, *c)
		}
	}
	return matches, nil
}

func (m *mockCityRepo) Create(ctx context.Context, city *domain.City) error {
	m.cities[city.ID] = city
	return nil
}

func (m *mockCityRepo) ExistsByIDs(ctx context.Context, ids []uuid.UUID) (bool, error) {
	for _, id := range ids {
		if _, ok := m.cities[id]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func TestCityService_ListNearbyCities(t *testing.T) {
	mockRepo := &mockCityRepo{
		nearbyResults: []domain.NearbyCityResult{
			{
				ID:         uuid.New(),
				Name:       "Bengaluru", // current city
				State:      "Karnataka",
				Latitude:   12.9716,
				Longitude:  77.5946,
				DistanceKm: 1.2,
			},
			{
				ID:         uuid.New(),
				Name:       "Tumkur", // nearby city
				State:      "Karnataka",
				Latitude:   13.3409,
				Longitude:  77.1010,
				DistanceKm: 65.5,
			},
		},
	}

	svc := service.NewCityService(mockRepo, nil, nil)
	results, err := svc.ListNearbyCities(context.Background(), 12.97, 77.59, 200, 10, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "Tumkur" {
		t.Errorf("expected Tumkur, got %s", results[0].Name)
	}
}

func TestCityService_SearchCities_DBHit(t *testing.T) {
	cityID := uuid.New()
	mockRepo := &mockCityRepo{
		cities: map[uuid.UUID]*domain.City{
			cityID: {
				ID:       cityID,
				Name:     "Tumkur",
				State:    "Karnataka",
				IsActive: true,
			},
		},
	}

	svc := service.NewCityService(mockRepo, nil, nil)
	results, err := svc.SearchCities(context.Background(), "Tumkur", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if results[0].Name != "Tumkur" {
		t.Errorf("expected Tumkur, got %s", results[0].Name)
	}
}

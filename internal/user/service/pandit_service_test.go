package service_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	citydomain "github.com/medha/backend/internal/city/domain"
	"github.com/medha/backend/internal/user/domain"
	"github.com/medha/backend/internal/user/service"
)

type mockPanditRepoForCities struct {
	domain.PanditProfileRepository
	profiles      map[uuid.UUID]*domain.PanditProfile
	serviceCities map[uuid.UUID][]domain.ServiceCityDetail
}

func (m *mockPanditRepoForCities) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.PanditProfile, error) {
	p, ok := m.profiles[userID]
	if !ok {
		return nil, domain.ErrPanditProfileNotFound
	}
	return p, nil
}

func (m *mockPanditRepoForCities) ReplaceServiceCities(ctx context.Context, userID uuid.UUID, homeCityID uuid.UUID, additionalCityIDs []uuid.UUID) error {
	var details []domain.ServiceCityDetail
	details = append(details, domain.ServiceCityDetail{
		CityID: homeCityID,
		IsHome: true,
	})
	for _, addID := range additionalCityIDs {
		details = append(details, domain.ServiceCityDetail{
			CityID: addID,
			IsHome: false,
		})
	}
	m.serviceCities[userID] = details
	return nil
}

func (m *mockPanditRepoForCities) GetServiceCities(ctx context.Context, userID uuid.UUID) ([]domain.ServiceCityDetail, error) {
	return m.serviceCities[userID], nil
}

type mockCityRepoForCities struct {
	citydomain.CityRepository
	cities map[uuid.UUID]*citydomain.City
}

func (m *mockCityRepoForCities) GetByID(ctx context.Context, id uuid.UUID) (*citydomain.City, error) {
	c, ok := m.cities[id]
	if !ok {
		return nil, citydomain.ErrCityNotFound
	}
	return c, nil
}

func TestPanditService_SetServiceCities(t *testing.T) {
	userID := uuid.New()
	homeCityID := uuid.New()
	addCityID1 := uuid.New()
	addCityID2 := uuid.New()

	pRepo := &mockPanditRepoForCities{
		profiles: map[uuid.UUID]*domain.PanditProfile{
			userID: {
				UserID: userID,
			},
		},
		serviceCities: make(map[uuid.UUID][]domain.ServiceCityDetail),
	}

	cRepo := &mockCityRepoForCities{
		cities: map[uuid.UUID]*citydomain.City{
			homeCityID: {
				ID:        homeCityID,
				Name:      "Tumkur",
				Latitude:  13.3409,
				Longitude: 77.1010,
				IsActive:  true,
			},
			addCityID1: {
				ID:        addCityID1,
				Name:      "Bengaluru",
				Latitude:  12.9716,
				Longitude: 77.5946,
				IsActive:  true,
			},
			addCityID2: {
				ID:        addCityID2,
				Name:      "Kolar",
				Latitude:  13.1357,
				Longitude: 78.1292,
				IsActive:  true,
			},
		},
	}

	svc := service.NewPanditService(pRepo, nil, cRepo, nil, nil, slog.Default())

	t.Run("Success Happy Path", func(t *testing.T) {
		res, err := svc.SetServiceCities(context.Background(), userID, homeCityID, []uuid.UUID{addCityID1, addCityID2})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(res) != 3 {
			t.Errorf("expected 3 service cities, got %d", len(res))
		}
	})

	t.Run("Too Many Cities", func(t *testing.T) {
		tooManyIDs := make([]uuid.UUID, 101)
		for i := 0; i < 101; i++ {
			tooManyIDs[i] = uuid.New()
		}
		_, err := svc.SetServiceCities(context.Background(), userID, homeCityID, tooManyIDs)
		if !errors.Is(err, domain.ErrTooManyServiceCities) {
			t.Errorf("expected ErrTooManyServiceCities, got %v", err)
		}
	})

	t.Run("Duplicate City", func(t *testing.T) {
		_, err := svc.SetServiceCities(context.Background(), userID, homeCityID, []uuid.UUID{homeCityID})
		if !errors.Is(err, domain.ErrDuplicateServiceCity) {
			t.Errorf("expected ErrDuplicateServiceCity, got %v", err)
		}
	})
}

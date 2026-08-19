package domain

import (
	"context"

	"github.com/google/uuid"
)

type CityRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*City, error)
	GetByName(ctx context.Context, name, state string) (*City, error)
	ListNearby(ctx context.Context, lat, lng float64, radiusKm int, limit int) ([]NearbyCityResult, error)
	Search(ctx context.Context, query string, limit int) ([]City, error)
	Create(ctx context.Context, city *City) error
	ExistsByIDs(ctx context.Context, ids []uuid.UUID) (bool, error)
}

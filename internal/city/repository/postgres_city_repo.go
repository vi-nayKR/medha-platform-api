package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/city/domain"
	"github.com/medha/backend/internal/infra/database"
)

type PostgresCityRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCityRepository(pool *pgxpool.Pool) *PostgresCityRepository {
	return &PostgresCityRepository{pool: pool}
}

func (r *PostgresCityRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.City, error) {
	query := `
		SELECT id, name, city, state, 
		       ST_Y(location::geometry) AS latitude, 
		       ST_X(location::geometry) AS longitude, 
		       is_active, created_at, updated_at
		FROM cities
		WHERE id = $1
	`
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id)
	
	var city domain.City
	err := row.Scan(
		&city.ID,
		&city.Name,
		&city.City,
		&city.State,
		&city.Latitude,
		&city.Longitude,
		&city.IsActive,
		&city.CreatedAt,
		&city.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCityNotFound
		}
		return nil, fmt.Errorf("get city by id: %w", err)
	}
	return &city, nil
}

func (r *PostgresCityRepository) GetByName(ctx context.Context, name, state string) (*domain.City, error) {
	query := `
		SELECT id, name, city, state, 
		       ST_Y(location::geometry) AS latitude, 
		       ST_X(location::geometry) AS longitude, 
		       is_active, created_at, updated_at
		FROM cities
		WHERE name = $1 AND state = $2
	`
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, name, state)
	
	var city domain.City
	err := row.Scan(
		&city.ID,
		&city.Name,
		&city.City,
		&city.State,
		&city.Latitude,
		&city.Longitude,
		&city.IsActive,
		&city.CreatedAt,
		&city.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrCityNotFound
		}
		return nil, fmt.Errorf("get city by name: %w", err)
	}
	return &city, nil
}

func (r *PostgresCityRepository) ListNearby(ctx context.Context, lat, lng float64, radiusKm int, limit int) ([]domain.NearbyCityResult, error) {
	query := `
		SELECT id, name, city, state,
		       ST_Y(location::geometry) AS latitude,
		       ST_X(location::geometry) AS longitude,
		       ST_Distance(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) / 1000.0 AS distance_km
		FROM cities
		WHERE is_active = true
		  AND ST_DWithin(location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
		ORDER BY distance_km ASC
		LIMIT $4
	`
	// $1 = lng, $2 = lat, $3 = radius in meters, $4 = limit
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, lng, lat, radiusKm*1000, limit)
	if err != nil {
		return nil, fmt.Errorf("list nearby cities: %w", err)
	}
	defer rows.Close()

	var results []domain.NearbyCityResult
	for rows.Next() {
		var res domain.NearbyCityResult
		err := rows.Scan(
			&res.ID,
			&res.Name,
			&res.City,
			&res.State,
			&res.Latitude,
			&res.Longitude,
			&res.DistanceKm,
		)
		if err != nil {
			return nil, fmt.Errorf("scan nearby city: %w", err)
		}
		results = append(results, res)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing nearby cities: %w", err)
	}

	return results, nil
}

func (r *PostgresCityRepository) Search(ctx context.Context, query string, limit int) ([]domain.City, error) {
	sqlQuery := `
		SELECT id, name, city, state,
		       ST_Y(location::geometry) AS latitude,
		       ST_X(location::geometry) AS longitude,
		       is_active, created_at, updated_at
		FROM cities
		WHERE is_active = true
		  AND name ILIKE '%' || $1 || '%'
		ORDER BY name ASC
		LIMIT $2
	`
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, sqlQuery, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search cities: %w", err)
	}
	defer rows.Close()

	var results []domain.City
	for rows.Next() {
		var city domain.City
		err := rows.Scan(
			&city.ID,
			&city.Name,
			&city.City,
			&city.State,
			&city.Latitude,
			&city.Longitude,
			&city.IsActive,
			&city.CreatedAt,
			&city.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan city from search: %w", err)
		}
		results = append(results, city)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error searching cities: %w", err)
	}

	return results, nil
}

func (r *PostgresCityRepository) Create(ctx context.Context, city *domain.City) error {
	query := `
		INSERT INTO cities (id, name, city, state, location, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography, $7, $8, $9)
		ON CONFLICT (name, state) DO NOTHING
	`
	now := time.Now().Unix()
	if city.ID == uuid.Nil {
		city.ID = uuid.New()
	}
	city.CreatedAt = now
	city.UpdatedAt = now
	city.IsActive = true

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		city.ID,
		city.Name,
		city.City,
		city.State,
		city.Longitude, // ST_MakePoint longitude first
		city.Latitude,  // ST_MakePoint latitude second
		city.IsActive,
		city.CreatedAt,
		city.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create city: %w", err)
	}
	return nil
}

func (r *PostgresCityRepository) ExistsByIDs(ctx context.Context, ids []uuid.UUID) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}

	query := `
		SELECT COUNT(*) 
		FROM cities
		WHERE id = ANY($1) AND is_active = true
	`
	var count int
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, ids).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check cities exist by ids: %w", err)
	}
	
	// We want to count distinct IDs in input, wait!
	// If the list of IDs contains duplicates, count might not match len(ids) if we do direct comparison.
	// But the service layer should filter/validate uniqueness of input IDs, so length matches.
	// Just to be safe, let's remove duplicates in caller or count unique matching cities.
	// Actually, if we do: id = ANY($1), and there are duplicate IDs in $1, SQL will count each city row matching any of the IDs,
	// so since `id` is primary key, it will count unique cities.
	// So we can count unique IDs in `ids` slice and compare with `count`.
	uniqueIDs := make(map[uuid.UUID]struct{})
	for _, id := range ids {
		uniqueIDs[id] = struct{}{}
	}
	return count == len(uniqueIDs), nil
}

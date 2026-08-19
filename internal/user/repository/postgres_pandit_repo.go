package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/user/domain"
)

// PostgresPanditProfileRepository implements domain.PanditProfileRepository using pgxpool.
type PostgresPanditProfileRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPanditProfileRepository creates a new PostgresPanditProfileRepository.
func NewPostgresPanditProfileRepository(pool *pgxpool.Pool) *PostgresPanditProfileRepository {
	return &PostgresPanditProfileRepository{pool: pool}
}

// Create inserts a new pandit profile.
func (r *PostgresPanditProfileRepository) Create(ctx context.Context, profile *domain.PanditProfile) error {
	query := `
		INSERT INTO pandit_profiles (
			id, user_id, parampara, veda_affiliation, ceremony_specializations,
			languages, service_radius_km, availability_status, about
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	if profile.ID == uuid.Nil {
		profile.ID = uuid.New()
	}

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		profile.ID,
		profile.UserID,
		profile.Parampara,
		profile.VedaAffiliation,
		profile.CeremonySpecializations,
		profile.Languages,
		profile.ServiceRadiusKM,
		profile.AvailabilityStatus.String(),
		profile.About,
	)
	if err != nil {
		// Check for unique constraint violation on user_id
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrPanditProfileAlreadyExists
		}
		return fmt.Errorf("create pandit profile: %w", err)
	}

	return nil
}

// GetByUserID returns a non-deleted pandit profile by user ID.
func (r *PostgresPanditProfileRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.PanditProfile, error) {
	query := `
		SELECT id, user_id, parampara, veda_affiliation, ceremony_specializations,
			   languages, service_radius_km, availability_status, about,
			   created_at, updated_at, deleted_at
		FROM pandit_profiles
		WHERE user_id = $1 AND deleted_at IS NULL
	`

	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, userID)

	var p domain.PanditProfile
	var availStatus string
	var aboutPtr *string
	err := row.Scan(
		&p.ID,
		&p.UserID,
		&p.Parampara,
		&p.VedaAffiliation,
		&p.CeremonySpecializations,
		&p.Languages,
		&p.ServiceRadiusKM,
		&availStatus,
		&aboutPtr,
		&p.CreatedAt,
		&p.UpdatedAt,
		&p.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPanditProfileNotFound
		}
		return nil, fmt.Errorf("get pandit profile by user id: %w", err)
	}

	if aboutPtr != nil {
		p.About = *aboutPtr
	}
	p.AvailabilityStatus = domain.AvailabilityStatus(availStatus)
	return &p, nil
}

// Update modifies an existing non-deleted pandit profile.
func (r *PostgresPanditProfileRepository) Update(ctx context.Context, profile *domain.PanditProfile) error {
	query := `
		UPDATE pandit_profiles
		SET parampara = $2, veda_affiliation = $3, ceremony_specializations = $4,
			languages = $5, service_radius_km = $6, availability_status = $7, about = $8
		WHERE user_id = $1 AND deleted_at IS NULL
	`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		profile.UserID,
		profile.Parampara,
		profile.VedaAffiliation,
		profile.CeremonySpecializations,
		profile.Languages,
		profile.ServiceRadiusKM,
		profile.AvailabilityStatus.String(),
		profile.About,
	)
	if err != nil {
		return fmt.Errorf("update pandit profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPanditProfileNotFound
	}

	return nil
}

// SoftDelete marks a pandit profile as deleted by setting deleted_at.
func (r *PostgresPanditProfileRepository) SoftDelete(ctx context.Context, userID uuid.UUID) error {
	query := `UPDATE pandit_profiles SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE user_id = $1 AND deleted_at IS NULL`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("soft delete pandit profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPanditProfileNotFound
	}

	return nil
}

// ListNearby returns pandit profiles within a given radius, sorted by distance (ascending).
// Uses ST_DWithin on users.location with a JOIN to pandit_profiles.
// Supports optional filtering by ceremony types, parampara, and availability.
// Cursor-based pagination using composite key (distance, profile_id) for stable keyset ordering.
// When viewerID is non-nil, adds connection_status and conversation_id per pandit.
func (r *PostgresPanditProfileRepository) ListNearby(
	ctx context.Context,
	lat, lng float64,
	radiusKM int,
	filters domain.PanditFilters,
	cursor string,
	limit int,
	viewerID *uuid.UUID,
) ([]*domain.PanditProfileWithDistance, string, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Convert radius from km to meters for ST_DWithin (geography type uses meters)
	radiusMeters := radiusKM * 1000

	// Build dynamic query with optional filters
	var conditions []string
	args := []interface{}{lng, lat, radiusMeters}
	argIdx := 4

	// Base conditions (always applied)
	conditions = append(conditions,
		"u.role = 'pandit'",
		"u.deleted_at IS NULL",
		"u.location IS NOT NULL",
		"pp.deleted_at IS NULL",
		"ST_DWithin(u.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)",
	)

	// Optional: ceremony type filter
	if len(filters.CeremonyTypes) > 0 {
		conditions = append(conditions, fmt.Sprintf("pp.ceremony_specializations @> $%d::text[]", argIdx))
		args = append(args, filters.CeremonyTypes)
		argIdx++
	}

	// Optional: parampara filter
	if filters.Parampara != "" {
		conditions = append(conditions, fmt.Sprintf("pp.parampara = $%d", argIdx))
		args = append(args, filters.Parampara)
		argIdx++
	}

	// Optional: availability filter
	if filters.AvailableOnly {
		conditions = append(conditions, "pp.availability_status = 'available'")
	}

	// Cursor-based pagination: composite key (distance, profile_id) for stable ordering
	if cursor != "" {
		cursorDist, cursorID, err := decodeCursor(cursor)
		if err == nil {
			// Keyset: (distance > cursorDist) OR (distance = cursorDist AND pp.id > cursorID)
			conditions = append(conditions, fmt.Sprintf(
				"(ST_Distance(u.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) / 1000 > $%d "+
					"OR (ST_Distance(u.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) / 1000 = $%d AND pp.id > $%d))",
				argIdx, argIdx, argIdx+1))
			args = append(args, cursorDist, cursorID)
			argIdx += 2
		}
	}

	// Build connection context joins when viewerID is set
	connJoins := ""
	connSelect := "'' AS connection_status, NULL::uuid AS conversation_id"

	if viewerID != nil {
		// First Flow: conversation already exists between viewer+pandit (any type)
		// We execute the subquery once per row using a LATERAL join.
		connJoins = fmt.Sprintf(`
			LEFT JOIN LATERAL (
				SELECT cv.id AS conversation_id
				FROM conversations cv
				WHERE cv.type = 'pandit_yajman'
				  AND ((cv.user_one_id = $%d AND cv.user_two_id = pp.user_id) OR (cv.user_one_id = pp.user_id AND cv.user_two_id = $%d))
				LIMIT 1
			) conv ON TRUE
		`, argIdx, argIdx)
		args = append(args, *viewerID)
		argIdx++

		// Unified status: conversation wins
		connSelect = `
			CASE
				WHEN conv.conversation_id IS NOT NULL THEN 'connected'
				ELSE 'not_connected'
			END AS connection_status,
			conv.conversation_id AS conversation_id`
	}

	query := fmt.Sprintf(`
		SELECT
			pp.id, pp.user_id, pp.parampara, pp.veda_affiliation,
			pp.ceremony_specializations, pp.languages, pp.service_radius_km,
			pp.availability_status, pp.about, pp.created_at, pp.updated_at,
			u.id, u.first_name, u.last_name, u.profile_photo_url,
			ST_Distance(u.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) / 1000 AS distance_km,
			%s
		FROM pandit_profiles pp
			INNER JOIN users u ON u.id = pp.user_id
			%s
		WHERE %s
		ORDER BY distance_km ASC, pp.id ASC
		LIMIT $%d
	`, connSelect, connJoins, strings.Join(conditions, " AND "), argIdx)
	args = append(args, limit+1) // fetch one extra to determine if there's a next page

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list nearby pandit profiles: %w", err)
	}
	defer rows.Close()

	var results []*domain.PanditProfileWithDistance
	for rows.Next() {
		var p domain.PanditProfile
		var u authdomain.User
		var availStatus string
		var profilePhotoURL *string
		var distanceKM float64
		var aboutPtr *string
		var connStatus string
		var convID *uuid.UUID

		err := rows.Scan(
			&p.ID, &p.UserID, &p.Parampara, &p.VedaAffiliation,
			&p.CeremonySpecializations, &p.Languages, &p.ServiceRadiusKM,
			&availStatus, &aboutPtr, &p.CreatedAt, &p.UpdatedAt,
			&u.ID, &u.FirstName, &u.LastName, &profilePhotoURL,
			&distanceKM,
			&connStatus, &convID,
		)
		if err != nil {
			return nil, "", fmt.Errorf("scan pandit profile row: %w", err)
		}

		if profilePhotoURL != nil {
			u.ProfilePhotoURL = *profilePhotoURL
		}
		if aboutPtr != nil {
			p.About = *aboutPtr
		}
		if connStatus == "" {
			connStatus = "not_connected"
		}
		p.AvailabilityStatus = domain.AvailabilityStatus(availStatus)
		results = append(results, &domain.PanditProfileWithDistance{
			PanditProfile:    p,
			User:             &u,
			DistanceKM:       distanceKM,
			ConnectionStatus: connStatus,
			ConversationID:   convID,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate pandit profile rows: %w", err)
	}

	// Determine next cursor using composite key (distance, profile_id)
	var nextCursor string
	if len(results) > limit {
		// We fetched limit+1, so there's a next page
		results = results[:limit]
		last := results[len(results)-1]
		nextCursor = encodeCursor(last.DistanceKM, last.ID)
	}

	return results, nextCursor, nil
}

// encodeCursor encodes a composite (distance, id) cursor as a base64 string.
func encodeCursor(distance float64, id uuid.UUID) string {
	payload := strconv.FormatFloat(distance, 'f', 6, 64) + "|" + id.String()
	return base64.URLEncoding.EncodeToString([]byte(payload))
}

// decodeCursor decodes a base64 composite cursor back to (distance, id).
func decodeCursor(cursor string) (float64, uuid.UUID, error) {
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, uuid.Nil, err
	}
	parts := strings.SplitN(string(data), "|", 2)
	if len(parts) != 2 {
		return 0, uuid.Nil, fmt.Errorf("invalid cursor format")
	}
	dist, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, uuid.Nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return 0, uuid.Nil, err
	}
	return dist, id, nil
}

// ReplaceServiceCities transactionally replaces a pandit's home and additional service cities.
func (r *PostgresPanditProfileRepository) ReplaceServiceCities(ctx context.Context, userID uuid.UUID, homeCityID uuid.UUID, additionalCityIDs []uuid.UUID) error {
	exec := database.GetExecutor(ctx, r.pool)

	// Delete existing service cities for this user
	_, err := exec.Exec(ctx, `DELETE FROM pandit_service_cities WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("delete old service cities: %w", err)
	}

	now := epoch.Now()

	// Insert home city
	_, err = exec.Exec(ctx, `
		INSERT INTO pandit_service_cities (id, user_id, city_id, is_home, created_at)
		VALUES ($1, $2, $3, true, $4)
	`, uuid.New(), userID, homeCityID, now)
	if err != nil {
		return fmt.Errorf("insert home city: %w", err)
	}

	// Insert additional cities
	for _, cityID := range additionalCityIDs {
		_, err = exec.Exec(ctx, `
			INSERT INTO pandit_service_cities (id, user_id, city_id, is_home, created_at)
			VALUES ($1, $2, $3, false, $4)
		`, uuid.New(), userID, cityID, now)
		if err != nil {
			return fmt.Errorf("insert additional city %s: %w", cityID, err)
		}
	}

	return nil
}

// GetServiceCities retrieves the list of service cities with name and distance details.
func (r *PostgresPanditProfileRepository) GetServiceCities(ctx context.Context, userID uuid.UUID) ([]domain.ServiceCityDetail, error) {
	exec := database.GetExecutor(ctx, r.pool)
	query := `
		SELECT
			c.id, c.name, c.state,
			ST_Y(c.location::geometry) AS latitude,
			ST_X(c.location::geometry) AS longitude,
			psc.is_home,
			CASE
				WHEN psc.is_home THEN 0
				ELSE COALESCE(
					ST_Distance(c.location, home_city.location) / 1000.0,
					0
				)
			END AS distance_km
		FROM pandit_service_cities psc
		JOIN cities c ON c.id = psc.city_id
		LEFT JOIN (
			SELECT c2.location
			FROM pandit_service_cities psc2
			JOIN cities c2 ON c2.id = psc2.city_id
			WHERE psc2.user_id = $1 AND psc2.is_home = true
		) home_city ON true
		WHERE psc.user_id = $1
		ORDER BY psc.is_home DESC, distance_km ASC
	`
	rows, err := exec.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query service cities: %w", err)
	}
	defer rows.Close()

	list := []domain.ServiceCityDetail{}
	for rows.Next() {
		var d domain.ServiceCityDetail
		err := rows.Scan(
			&d.CityID,
			&d.CityName,
			&d.StateName,
			&d.Latitude,
			&d.Longitude,
			&d.IsHome,
			&d.DistanceKm,
		)
		if err != nil {
			return nil, fmt.Errorf("scan service city detail: %w", err)
		}
		list = append(list, d)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service city rows: %w", err)
	}

	return list, nil
}

// VerifyCeremonySlugExists checks if the ceremony slug exists in the database.
func (r *PostgresPanditProfileRepository) VerifyCeremonySlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS (
			SELECT 1 FROM festival_logos
			WHERE REPLACE(slug, '-', '_') = REPLACE($1, '-', '_') AND is_active = true
		)
	`
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, slug).Scan(&exists)
	return exists, err
}



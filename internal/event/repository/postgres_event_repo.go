package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/event/domain"
	"github.com/medha/backend/internal/infra/database"
)

// PostgresEventRepository implements domain.EventRepository using pgxpool.
type PostgresEventRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresEventRepository creates a new PostgresEventRepository.
func NewPostgresEventRepository(pool *pgxpool.Pool) *PostgresEventRepository {
	return &PostgresEventRepository{pool: pool}
}

// Create inserts a new event.
func (r *PostgresEventRepository) Create(ctx context.Context, event *domain.Event) error {
	if event.PlatformFee == nil {
		var fee float64
		err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, `
			SELECT fee_amount FROM platform_fee_config
			WHERE (ceremony_type = $1 OR ceremony_type = '*') AND is_active = true
			ORDER BY CASE WHEN ceremony_type = '*' THEN 1 ELSE 0 END, created_at DESC
			LIMIT 1
		`, event.CeremonyType.String()).Scan(&fee)
		if err == nil {
			event.PlatformFee = &fee
		} else {
			defaultFee := 49.00
			event.PlatformFee = &defaultFee
		}
	}

	query := `
		INSERT INTO events (
			id, yajman_id, ceremony_type, custom_ceremony_name, custom_ceremony_description,
			event_date, location, address, description, status, platform_fee
		)
		VALUES ($1, $2, $3, $4, $5, $6, ST_SetSRID(ST_MakePoint($7, $8), 4326)::geography, $9, $10, $11, $12)
	`

	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}

	var lng, lat *float64
	if event.Location != nil {
		lng = &event.Location.Longitude
		lat = &event.Location.Latitude
	}

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		event.ID,
		event.YajmanID,
		event.CeremonyType.String(),
		nullableString(event.CustomCeremonyName),
		nullableString(event.CustomCeremonyDescription),
		event.EventDate,
		lng,
		lat,
		event.Address,
		nullableString(event.Description),
		event.Status.String(),
		event.PlatformFee,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

// GetByID returns a non-deleted event by primary key.
func (r *PostgresEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	query := `
		SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
		       ST_Y(e.location::geometry) AS latitude, ST_X(e.location::geometry) AS longitude,
		       e.address, e.description, e.custom_ceremony_name, e.custom_ceremony_description, e.status,
		       e.created_at, e.updated_at, e.deleted_at,
		       c.image_url_no_bg AS ceremony_logo_url,
		       p.phone AS pandit_phone_number,
		       NULLIF(TRIM(COALESCE(p.first_name, '') || ' ' || COALESCE(p.last_name, '')), '') AS pandit_name,
		       e.platform_fee,
		       conv.id AS conversation_id,
		       y.phone AS yajman_phone_number
		FROM events e
		LEFT JOIN users y ON y.id = e.yajman_id
		LEFT JOIN festival_logos c ON c.slug = CASE e.ceremony_type::text
			WHEN 'shraadh' THEN 'shraddha'
			WHEN 'grihapravesh' THEN 'griha-pravesh'
			WHEN 'satyanarayan' THEN 'satyanarayan-puja'
			WHEN 'vastu_shanti' THEN 'vastu-shanti-puja'
			WHEN 'naamkaran' THEN 'namakarana'
			ELSE replace(e.ceremony_type::text, '_', '-')
		END
		LEFT JOIN matches m ON m.event_id = e.id AND m.status IN ('matched', 'active', 'completed')
		LEFT JOIN users p ON p.id = m.pandit_id
		LEFT JOIN conversations conv ON conv.match_id = m.id
		WHERE e.id = $1 AND e.deleted_at IS NULL
	`

	return r.scanEvent(ctx, query, id)
}

// Update modifies an existing non-deleted event.
func (r *PostgresEventRepository) Update(ctx context.Context, event *domain.Event) error {
	query := `
		UPDATE events
		SET ceremony_type = $2,
		    custom_ceremony_name = $3,
		    custom_ceremony_description = $4,
		    event_date = $5,
		    location = ST_SetSRID(ST_MakePoint($6, $7), 4326)::geography,
		    address = $8,
		    description = $9,
		    status = $10
		WHERE id = $1 AND deleted_at IS NULL
	`

	var lng, lat *float64
	if event.Location != nil {
		lng = &event.Location.Longitude
		lat = &event.Location.Latitude
	}

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		event.ID,
		event.CeremonyType.String(),
		nullableString(event.CustomCeremonyName),
		nullableString(event.CustomCeremonyDescription),
		event.EventDate,
		lng,
		lat,
		event.Address,
		nullableString(event.Description),
		event.Status.String(),
	)
	if err != nil {
		return fmt.Errorf("update event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventNotFound
	}

	return nil
}

// UpdateStatus updates the status of an event.
func (r *PostgresEventRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.EventStatus) error {
	query := `UPDATE events SET status = $2 WHERE id = $1 AND deleted_at IS NULL`
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, id, status.String())
	if err != nil {
		return fmt.Errorf("update event status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventNotFound
	}
	return nil
}

// SoftDelete marks an event as deleted by setting deleted_at.
func (r *PostgresEventRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE events SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT, status = 'Cancelled' WHERE id = $1 AND deleted_at IS NULL`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("soft delete event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEventNotFound
	}

	return nil
}

// ListByYajmanID returns all non-deleted events for a given yajman.
func (r *PostgresEventRepository) ListByYajmanID(ctx context.Context, yajmanID uuid.UUID, cursor string, limit int) ([]*domain.Event, string, error) {
	if limit <= 0 {
		limit = 20
	}

	// Decode cursor (created_at, id)
	var cursorTime int64
	var cursorID uuid.UUID
	if cursor != "" {
		var err error
		cursorTime, cursorID, err = decodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
	}

	// Fetch limit+1 to determine if there's a next page
	fetchLimit := limit + 1

	var query string
	var args []any

	if cursor == "" {
		query = `
			SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
			       ST_Y(e.location::geometry) AS latitude, ST_X(e.location::geometry) AS longitude,
			       e.address, e.description, e.custom_ceremony_name, e.custom_ceremony_description, e.status,
			       e.created_at, e.updated_at, e.deleted_at,
			       c.image_url_no_bg AS ceremony_logo_url,
			       p.phone AS pandit_phone_number,
			       NULLIF(TRIM(COALESCE(p.first_name, '') || ' ' || COALESCE(p.last_name, '')), '') AS pandit_name,
			       e.platform_fee
			FROM events e
			LEFT JOIN festival_logos c ON c.slug = CASE e.ceremony_type::text
				WHEN 'shraadh' THEN 'shraddha'
				WHEN 'grihapravesh' THEN 'griha-pravesh'
				WHEN 'satyanarayan' THEN 'satyanarayan-puja'
				WHEN 'vastu_shanti' THEN 'vastu-shanti-puja'
				WHEN 'naamkaran' THEN 'namakarana'
				ELSE replace(e.ceremony_type::text, '_', '-')
			END
			LEFT JOIN matches m ON m.event_id = e.id AND m.status IN ('matched', 'active', 'completed')
			LEFT JOIN users p ON p.id = m.pandit_id
			WHERE e.yajman_id = $1 AND e.deleted_at IS NULL
			ORDER BY e.event_date ASC, e.id ASC
			LIMIT $2
		`
		args = []any{yajmanID, fetchLimit}
	} else {
		query = `
			SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
			       ST_Y(e.location::geometry) AS latitude, ST_X(e.location::geometry) AS longitude,
			       e.address, e.description, e.custom_ceremony_name, e.custom_ceremony_description, e.status,
			       e.created_at, e.updated_at, e.deleted_at,
			       c.image_url_no_bg AS ceremony_logo_url,
			       p.phone AS pandit_phone_number,
			       NULLIF(TRIM(COALESCE(p.first_name, '') || ' ' || COALESCE(p.last_name, '')), '') AS pandit_name,
			       e.platform_fee
			FROM events e
			LEFT JOIN festival_logos c ON c.slug = CASE e.ceremony_type::text
				WHEN 'shraadh' THEN 'shraddha'
				WHEN 'grihapravesh' THEN 'griha-pravesh'
				WHEN 'satyanarayan' THEN 'satyanarayan-puja'
				WHEN 'vastu_shanti' THEN 'vastu-shanti-puja'
				WHEN 'naamkaran' THEN 'namakarana'
				ELSE replace(e.ceremony_type::text, '_', '-')
			END
			LEFT JOIN matches m ON m.event_id = e.id AND m.status IN ('matched', 'active', 'completed')
			LEFT JOIN users p ON p.id = m.pandit_id
			WHERE e.yajman_id = $1 AND e.deleted_at IS NULL
			  AND (e.event_date, e.id) > ($3, $4)
			ORDER BY e.event_date ASC, e.id ASC
			LIMIT $2
		`
		args = []any{yajmanID, fetchLimit, cursorTime, cursorID}
	}

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list events by yajman: %w", err)
	}
	defer rows.Close()

	var events []*domain.Event
	for rows.Next() {
		event, err := scanEventRow(rows)
		if err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}

	// Check for next page
	var nextCursor string
	if len(events) > limit {
		events = events[:limit]
		last := events[limit-1]
		nextCursor = encodeCursor(last.EventDate, last.ID)
	}

	return events, nextCursor, nil
}

// ListNearby returns active events within a given radius, sorted by distance.
func (r *PostgresEventRepository) ListNearby(ctx context.Context, lat, lng float64, radiusKM int, refLat, refLng *float64, excludePanditID *uuid.UUID, cursor string, limit int) ([]*domain.EventWithDistance, string, error) {
	if limit <= 0 {
		limit = 20
	}

	radiusMeters := radiusKM * 1000
	fetchLimit := limit + 1

	// Reference point for distance calculation (defaults to search center)
	distLat := lat
	distLng := lng
	if refLat != nil && refLng != nil {
		distLat = *refLat
		distLng = *refLng
	}

	// Decode distance cursor
	var cursorDist float64
	var cursorID uuid.UUID
	if cursor != "" {
		var err error
		cursorDist, cursorID, err = decodeDistanceCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
	}

	var panditIDVal any = nil
	if excludePanditID != nil && *excludePanditID != uuid.Nil {
		panditIDVal = *excludePanditID
	}

	var query string
	var args []any

	if cursor == "" {
		query = `
			SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
			       ST_Y(e.location::geometry) AS latitude, ST_X(e.location::geometry) AS longitude,
			       e.address, e.description, e.custom_ceremony_name, e.custom_ceremony_description, e.status,
			       e.created_at, e.updated_at, e.deleted_at,
			       ST_Distance(e.location, ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography) / 1000 AS distance_km,
			       u.first_name, u.last_name, u.profile_photo_url,
			       c.image_url_no_bg AS ceremony_logo_url,
			       e.platform_fee
			FROM events e
			    INNER JOIN users u ON u.id = e.yajman_id
			    LEFT JOIN festival_logos c ON c.slug = CASE e.ceremony_type::text
					WHEN 'shraadh' THEN 'shraddha'
					WHEN 'grihapravesh' THEN 'griha-pravesh'
					WHEN 'satyanarayan' THEN 'satyanarayan-puja'
					WHEN 'vastu_shanti' THEN 'vastu-shanti-puja'
					WHEN 'naamkaran' THEN 'namakarana'
					ELSE replace(e.ceremony_type::text, '_', '-')
				END
			WHERE e.status IN ('Active', 'Pending', 'Pushed')
			    AND e.deleted_at IS NULL
			    AND u.deleted_at IS NULL
			    AND e.location IS NOT NULL
			    AND ST_DWithin(e.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
			    AND ($7::uuid IS NULL OR NOT EXISTS (
			        SELECT 1 FROM interests i
			        WHERE i.event_id = e.id AND i.pandit_id = $7
			    ))
			ORDER BY distance_km ASC, e.id ASC
			LIMIT $6
		`
		args = []any{lng, lat, radiusMeters, distLng, distLat, fetchLimit, panditIDVal}
	} else {
		query = `
			SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
			       ST_Y(e.location::geometry) AS latitude, ST_X(e.location::geometry) AS longitude,
			       e.address, e.description, e.custom_ceremony_name, e.custom_ceremony_description, e.status,
			       e.created_at, e.updated_at, e.deleted_at,
			       ST_Distance(e.location, ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography) / 1000 AS distance_km,
			       u.first_name, u.last_name, u.profile_photo_url,
			       c.image_url_no_bg AS ceremony_logo_url,
			       e.platform_fee
			FROM events e
			    INNER JOIN users u ON u.id = e.yajman_id
			    LEFT JOIN festival_logos c ON c.slug = CASE e.ceremony_type::text
					WHEN 'shraadh' THEN 'shraddha'
					WHEN 'grihapravesh' THEN 'griha-pravesh'
					WHEN 'satyanarayan' THEN 'satyanarayan-puja'
					WHEN 'vastu_shanti' THEN 'vastu-shanti-puja'
					WHEN 'naamkaran' THEN 'namakarana'
					ELSE replace(e.ceremony_type::text, '_', '-')
				END
			WHERE e.status IN ('Active', 'Pending', 'Pushed')
			    AND e.deleted_at IS NULL
			    AND u.deleted_at IS NULL
			    AND e.location IS NOT NULL
			    AND ST_DWithin(e.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
			    AND (ST_Distance(e.location, ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography) / 1000, e.id) > ($7, $8)
			    AND ($9::uuid IS NULL OR NOT EXISTS (
			        SELECT 1 FROM interests i
			        WHERE i.event_id = e.id AND i.pandit_id = $9
			    ))
			ORDER BY distance_km ASC, e.id ASC
			LIMIT $6
		`
		args = []any{lng, lat, radiusMeters, distLng, distLat, fetchLimit, cursorDist, cursorID, panditIDVal}
	}

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list nearby events: %w", err)
	}
	defer rows.Close()

	var events []*domain.EventWithDistance
	for rows.Next() {
		var (
			event       domain.Event
			latitude    *float64
			longitude   *float64
			description *string
			customName  *string
			customDesc  *string
			deletedAt   *int64
			distanceKM  float64
			firstName   string
			lastName    string
			photoURL    *string
		)

		err := rows.Scan(
			&event.ID, &event.YajmanID, &event.CeremonyType, &event.EventDate,
			&latitude, &longitude,
			&event.Address, &description, &customName, &customDesc, &event.Status,
			&event.CreatedAt, &event.UpdatedAt, &deletedAt,
			&distanceKM,
			&firstName, &lastName, &photoURL, &event.CeremonyLogoURL,
			&event.PlatformFee,
		)
		if err != nil {
			return nil, "", fmt.Errorf("scan nearby event: %w", err)
		}

		event.DeletedAt = deletedAt
		if description != nil {
			event.Description = *description
		}
		if customName != nil {
			event.CustomCeremonyName = *customName
		}
		if customDesc != nil {
			event.CustomCeremonyDescription = *customDesc
		}
		if latitude != nil && longitude != nil {
			event.Location = &authdomain.GeoPoint{
				Latitude:  *latitude,
				Longitude: *longitude,
			}
		}

		events = append(events, &domain.EventWithDistance{
			Event:           event,
			DistanceKM:      distanceKM,
			YajmanFirstName: firstName,
			YajmanLastName:  lastName,
			YajmanPhotoURL:  photoURL,
		})
	}

	// Check for next page
	var nextCursor string
	if len(events) > limit {
		events = events[:limit]
		last := events[limit-1]
		nextCursor = encodeDistanceCursor(last.DistanceKM, last.ID)
	}

	return events, nextCursor, nil
}

// ListInBoundingBox returns active events within a rectangular geographic area.
func (r *PostgresEventRepository) ListInBoundingBox(ctx context.Context, minLat, maxLat, minLng, maxLng float64, refLat, refLng *float64, excludePanditID *uuid.UUID, cursor string, limit int) ([]*domain.EventWithDistance, string, error) {
	if limit <= 0 {
		limit = 100 // Higher limit for map views usually
	}

	fetchLimit := limit + 1

	// Reference point for distance calculation
	distLat := (minLat + maxLat) / 2
	distLng := (minLng + maxLng) / 2
	if refLat != nil && refLng != nil {
		distLat = *refLat
		distLng = *refLng
	}

	// Use ID-based cursor for simplicity in BB search
	var cursorID uuid.UUID
	if cursor != "" {
		var err error
		cursorID, err = uuid.Parse(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
	}

	var panditIDVal any = nil
	if excludePanditID != nil && *excludePanditID != uuid.Nil {
		panditIDVal = *excludePanditID
	}

	var query string
	var args []any

	// Query for events within bounding box. Sorted by ID for cursor consistency.
	// We use e.location::geometry @ ST_MakeEnvelope($1, $2, $3, $4, 4326) for index speed.
	query = `
		SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
		       ST_Y(e.location::geometry) AS latitude, ST_X(e.location::geometry) AS longitude,
		       e.address, e.description, e.custom_ceremony_name, e.custom_ceremony_description, e.status,
		       e.created_at, e.updated_at, e.deleted_at,
		       ST_Distance(e.location, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography) / 1000 AS distance_km,
		       u.first_name, u.last_name, u.profile_photo_url,
		       c.image_url_no_bg AS ceremony_logo_url,
		       e.platform_fee
		FROM events e
		    INNER JOIN users u ON u.id = e.yajman_id
		    LEFT JOIN festival_logos c ON c.slug = CASE e.ceremony_type::text
				WHEN 'shraadh' THEN 'shraddha'
				WHEN 'grihapravesh' THEN 'griha-pravesh'
				WHEN 'satyanarayan' THEN 'satyanarayan-puja'
				WHEN 'vastu_shanti' THEN 'vastu-shanti-puja'
				WHEN 'naamkaran' THEN 'namakarana'
				ELSE replace(e.ceremony_type::text, '_', '-')
			END
		WHERE e.status IN ('Active', 'Pending', 'Pushed')
		    AND e.deleted_at IS NULL
		    AND u.deleted_at IS NULL
		    AND e.location IS NOT NULL
		    AND e.location::geometry @ ST_MakeEnvelope($1, $2, $3, $4, 4326)
	`

	args = []any{minLng, minLat, maxLng, maxLat, distLng, distLat}

	if cursor != "" {
		query += " AND e.id > $7 "
		args = append(args, cursorID)
	}

	// Now add the excludePanditID check
	// The next placeholder index will be len(args) + 1
	excludeIdx := len(args) + 1
	query += fmt.Sprintf(` AND ($%d::uuid IS NULL OR NOT EXISTS (
		SELECT 1 FROM interests i
		WHERE i.event_id = e.id AND i.pandit_id = $%d
	)) `, excludeIdx, excludeIdx)
	args = append(args, panditIDVal)

	// Now append the limit parameter, which is fetchLimit
	limitIdx := len(args) + 1
	args = append(args, fetchLimit)
	query += fmt.Sprintf(" ORDER BY e.id ASC LIMIT $%d", limitIdx)

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list events in bounding box: %w", err)
	}
	defer rows.Close()

	var events []*domain.EventWithDistance
	for rows.Next() {
		var (
			event       domain.Event
			latitude    *float64
			longitude   *float64
			description *string
			customName  *string
			customDesc  *string
			deletedAt   *int64
			distanceKM  float64
			firstName   string
			lastName    string
			photoURL    *string
		)

		err := rows.Scan(
			&event.ID, &event.YajmanID, &event.CeremonyType, &event.EventDate,
			&latitude, &longitude,
			&event.Address, &description, &customName, &customDesc, &event.Status,
			&event.CreatedAt, &event.UpdatedAt, &deletedAt,
			&distanceKM,
			&firstName, &lastName, &photoURL, &event.CeremonyLogoURL,
			&event.PlatformFee,
		)
		if err != nil {
			return nil, "", fmt.Errorf("scan event: %w", err)
		}

		event.DeletedAt = deletedAt
		if description != nil {
			event.Description = *description
		}
		if customName != nil {
			event.CustomCeremonyName = *customName
		}
		if customDesc != nil {
			event.CustomCeremonyDescription = *customDesc
		}
		if latitude != nil && longitude != nil {
			event.Location = &authdomain.GeoPoint{
				Latitude:  *latitude,
				Longitude: *longitude,
			}
		}

		events = append(events, &domain.EventWithDistance{
			Event:           event,
			DistanceKM:      distanceKM,
			YajmanFirstName: firstName,
			YajmanLastName:  lastName,
			YajmanPhotoURL:  photoURL,
		})
	}

	var nextCursor string
	if len(events) > limit {
		events = events[:limit]
		nextCursor = events[limit-1].ID.String()
	}

	return events, nextCursor, nil
}

// ListCeremonies returns all entries in the ceremony catalog.
func (r *PostgresEventRepository) ListCeremonies(ctx context.Context) ([]*domain.Ceremony, error) {
	query := `
		SELECT id, slug, name, COALESCE(image_url_no_bg, ''), COALESCE(category, 'general'), description, COALESCE(image_url_no_bg, ''), display_order, is_active, EXTRACT(EPOCH FROM created_at)::BIGINT
		FROM festival_logos
		WHERE is_active = true
		ORDER BY display_order ASC, name ASC
	`

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list ceremonies: %w", err)
	}
	defer rows.Close()

	var ceremonies []*domain.Ceremony
	for rows.Next() {
		var c domain.Ceremony
		var desc *string
		if err := rows.Scan(&c.ID, &c.Slug, &c.DisplayName, &c.ImageURL, &c.Category, &desc, &c.ImageURLNoBg, &c.DisplayOrder, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ceremony: %w", err)
		}
		if desc != nil {
			c.Description = *desc
		}
		c.LogoURL = c.ImageURL
		ceremonies = append(ceremonies, &c)
	}

	return ceremonies, nil
}

// SearchCeremonies returns ceremonies matching the query (case-insensitive ILIKE on slug, display_name, category, description).
func (r *PostgresEventRepository) SearchCeremonies(ctx context.Context, query string) ([]*domain.Ceremony, error) {
	sqlQuery := `
		SELECT id, slug, name, COALESCE(image_url_no_bg, ''), COALESCE(category, 'general'), description, COALESCE(image_url_no_bg, ''), display_order, is_active, EXTRACT(EPOCH FROM created_at)::BIGINT
		FROM festival_logos
		WHERE is_active = true
		  AND (slug ILIKE '%' || $1 || '%'
		   OR name ILIKE '%' || $1 || '%'
		   OR category ILIKE '%' || $1 || '%'
		   OR description ILIKE '%' || $1 || '%')
		ORDER BY display_order ASC, name ASC
	`

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, sqlQuery, query)
	if err != nil {
		return nil, fmt.Errorf("search ceremonies: %w", err)
	}
	defer rows.Close()

	var ceremonies []*domain.Ceremony
	for rows.Next() {
		var c domain.Ceremony
		var desc *string
		if err := rows.Scan(&c.ID, &c.Slug, &c.DisplayName, &c.ImageURL, &c.Category, &desc, &c.ImageURLNoBg, &c.DisplayOrder, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ceremony: %w", err)
		}
		if desc != nil {
			c.Description = *desc
		}
		c.LogoURL = c.ImageURL
		ceremonies = append(ceremonies, &c)
	}

	return ceremonies, nil
}

// FindNearbyPandits returns pandits (role='pandit') within radiusKM of the given coordinates.
// Uses ST_DWithin on users.location for PostGIS spatial index efficiency.
func (r *PostgresEventRepository) FindNearbyPandits(ctx context.Context, lat, lng float64, radiusKM int) ([]domain.NearbyPandit, error) {
	radiusMeters := radiusKM * 1000
	query := `
		SELECT u.id, u.first_name, u.last_name,
		       ST_Distance(u.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) / 1000 AS distance_km
		FROM users u
		WHERE u.role = 'pandit'
		  AND u.deleted_at IS NULL
		  AND u.location IS NOT NULL
		  AND ST_DWithin(u.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
		ORDER BY distance_km ASC
	`
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, lng, lat, radiusMeters)
	if err != nil {
		return nil, fmt.Errorf("find nearby pandits: %w", err)
	}
	defer rows.Close()

	var pandits []domain.NearbyPandit
	for rows.Next() {
		var p domain.NearbyPandit
		if err := rows.Scan(&p.UserID, &p.FirstName, &p.LastName, &p.DistanceKM); err != nil {
			return nil, fmt.Errorf("scan nearby pandit: %w", err)
		}
		pandits = append(pandits, p)
	}
	return pandits, nil
}

// --- internal helpers ---

func (r *PostgresEventRepository) scanEvent(ctx context.Context, query string, args ...any) (*domain.Event, error) {
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, args...)
	event, err := scanEventFromRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrEventNotFound
		}
		return nil, err
	}
	return event, nil
}

func scanEventFromRow(row pgx.Row) (*domain.Event, error) {
	var (
		event             domain.Event
		latitude          *float64
		longitude         *float64
		description       *string
		customName        *string
		customDesc        *string
		deletedAt         *int64
		conversationID    *uuid.UUID
		yajmanPhoneNumber *string
	)

	err := row.Scan(
		&event.ID, &event.YajmanID, &event.CeremonyType, &event.EventDate,
		&latitude, &longitude,
		&event.Address, &description, &customName, &customDesc, &event.Status,
		&event.CreatedAt, &event.UpdatedAt, &deletedAt,
		&event.CeremonyLogoURL, &event.PanditPhoneNumber, &event.PanditName,
		&event.PlatformFee, &conversationID, &yajmanPhoneNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("scan event: %w", err)
	}

	event.DeletedAt = deletedAt
	event.ConversationID = conversationID
	event.YajmanPhoneNumber = yajmanPhoneNumber
	if description != nil {
		event.Description = *description
	}
	if customName != nil {
		event.CustomCeremonyName = *customName
	}
	if customDesc != nil {
		event.CustomCeremonyDescription = *customDesc
	}
	if latitude != nil && longitude != nil {
		event.Location = &authdomain.GeoPoint{
			Latitude:  *latitude,
			Longitude: *longitude,
		}
	}

	return &event, nil
}

func scanEventRow(rows pgx.Rows) (*domain.Event, error) {
	var (
		event       domain.Event
		latitude    *float64
		longitude   *float64
		description *string
		customName  *string
		customDesc  *string
		deletedAt   *int64
	)

	err := rows.Scan(
		&event.ID, &event.YajmanID, &event.CeremonyType, &event.EventDate,
		&latitude, &longitude,
		&event.Address, &description, &customName, &customDesc, &event.Status,
		&event.CreatedAt, &event.UpdatedAt, &deletedAt,
		&event.CeremonyLogoURL, &event.PanditPhoneNumber, &event.PanditName,
		&event.PlatformFee,
	)
	if err != nil {
		return nil, fmt.Errorf("scan event row: %w", err)
	}

	event.DeletedAt = deletedAt
	if description != nil {
		event.Description = *description
	}
	if customName != nil {
		event.CustomCeremonyName = *customName
	}
	if customDesc != nil {
		event.CustomCeremonyDescription = *customDesc
	}
	if latitude != nil && longitude != nil {
		event.Location = &authdomain.GeoPoint{
			Latitude:  *latitude,
			Longitude: *longitude,
		}
	}

	return &event, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- Cursor encoding/decoding ---

type cursorData struct {
	T  int64     `json:"t"`
	ID uuid.UUID `json:"id"`
}

type distanceCursorData struct {
	D  float64   `json:"d"`
	ID uuid.UUID `json:"id"`
}

func encodeCursor(t int64, id uuid.UUID) string {
	data, _ := json.Marshal(cursorData{T: t, ID: id})
	return base64.URLEncoding.EncodeToString(data)
}

func decodeCursor(cursor string) (int64, uuid.UUID, error) {
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, uuid.Nil, err
	}
	var cd cursorData
	if err := json.Unmarshal(data, &cd); err != nil {
		return 0, uuid.Nil, err
	}
	return cd.T, cd.ID, nil
}

func encodeDistanceCursor(distance float64, id uuid.UUID) string {
	data, _ := json.Marshal(distanceCursorData{D: distance, ID: id})
	return base64.URLEncoding.EncodeToString(data)
}

func decodeDistanceCursor(cursor string) (float64, uuid.UUID, error) {
	data, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, uuid.Nil, err
	}
	var cd distanceCursorData
	if err := json.Unmarshal(data, &cd); err != nil {
		return 0, uuid.Nil, err
	}
	return cd.D, cd.ID, nil
}

// MarkPastEventsCompleted transitions all non-deleted events whose event_date has
// passed from Active/Pending/Booked to Completed. Returns the IDs of updated events.
// Intended to be called by the EventCompletionWorker background job.
func (r *PostgresEventRepository) MarkPastEventsCompleted(ctx context.Context) ([]uuid.UUID, error) {
	query := `
		UPDATE events
		SET status = 'Completed',
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE status IN ('Active', 'Pending', 'Booked', 'Created')
		  AND event_date < (EXTRACT(EPOCH FROM NOW()))::BIGINT
		  AND deleted_at IS NULL
		RETURNING id
	`
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("mark past events completed: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan completed event id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

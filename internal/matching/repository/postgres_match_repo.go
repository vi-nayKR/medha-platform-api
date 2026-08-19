package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/matching/domain"
)

// PostgresMatchRepository implements domain.MatchRepository using pgx.
type PostgresMatchRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresMatchRepository creates a new PostgresMatchRepository.
func NewPostgresMatchRepository(pool *pgxpool.Pool) *PostgresMatchRepository {
	return &PostgresMatchRepository{pool: pool}
}

// Create inserts a new match.
func (r *PostgresMatchRepository) Create(ctx context.Context, match *domain.Match) error {
	query := `
		INSERT INTO matches (id, yajman_id, pandit_id, event_id, status, matched_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		match.ID, match.YajmanID, match.PanditID, match.EventID,
		match.Status, match.MatchedAt, match.CreatedAt, match.UpdatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return domain.ErrMatchAlreadyExists
		}
		return fmt.Errorf("insert match: %w", err)
	}
	return nil
}

// GetByID returns a match by ID.
func (r *PostgresMatchRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Match, error) {
	query := `
		SELECT id, yajman_id, pandit_id, event_id, status, matched_at, created_at, updated_at
		FROM matches WHERE id = $1
	`
	var m domain.Match
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id).Scan(
		&m.ID, &m.YajmanID, &m.PanditID, &m.EventID,
		&m.Status, &m.MatchedAt, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrMatchNotFound
		}
		return nil, fmt.Errorf("get match by id: %w", err)
	}
	return &m, nil
}

// GetByIDWithDetails returns a match with joined user/event details.
func (r *PostgresMatchRepository) GetByIDWithDetails(ctx context.Context, id uuid.UUID) (*domain.MatchWithDetails, error) {
	query := `
		SELECT
			m.id, m.yajman_id, m.pandit_id, m.event_id, m.status, m.matched_at, m.created_at, m.updated_at,
			uy.first_name, uy.last_name, uy.profile_photo_url, uy.phone,
			up.first_name, up.last_name, up.profile_photo_url, up.phone,
			e.ceremony_type::text, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address
		FROM matches m
		JOIN users uy ON m.yajman_id = uy.id
		JOIN users up ON m.pandit_id = up.id
		JOIN events e ON m.event_id = e.id
		WHERE m.id = $1
	`
	var md domain.MatchWithDetails
	var yPhoto, pPhoto *string
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id).Scan(
		&md.ID, &md.YajmanID, &md.PanditID, &md.EventID,
		&md.Status, &md.MatchedAt, &md.CreatedAt, &md.UpdatedAt,
		&md.YajmanFirstName, &md.YajmanLastName, &yPhoto, &md.YajmanPhone,
		&md.PanditFirstName, &md.PanditLastName, &pPhoto, &md.PanditPhone,
		&md.CeremonyType, &md.EventDate, &md.EventAddress,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrMatchNotFound
		}
		return nil, fmt.Errorf("get match with details: %w", err)
	}
	md.YajmanPhotoURL = yPhoto
	md.PanditPhotoURL = pPhoto
	return &md, nil
}

// UpdateStatus updates the status of a match.
func (r *PostgresMatchRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.MatchStatus) error {
	query := `UPDATE matches SET status = $1, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $2`
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("update match status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMatchNotFound
	}
	return nil
}

// ListByUserID returns all matches for a user (as yajman or pandit).
func (r *PostgresMatchRepository) ListByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*domain.MatchWithDetails, string, error) {
	var cursorTime int64
	var cursorID uuid.UUID
	hasCursor := false

	if cursor != "" {
		parsed, err := parseCursor(cursor)
		if err == nil {
			cursorTime = parsed.time
			cursorID = parsed.id
			hasCursor = true
		}
	}

	query := `
		SELECT
			m.id, m.yajman_id, m.pandit_id, m.event_id, m.status, m.matched_at, m.created_at, m.updated_at,
			uy.first_name, uy.last_name, uy.profile_photo_url, uy.phone,
			up.first_name, up.last_name, up.profile_photo_url, up.phone,
			e.ceremony_type::text, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address
		FROM matches m
		JOIN users uy ON m.yajman_id = uy.id
		JOIN users up ON m.pandit_id = up.id
		JOIN events e ON m.event_id = e.id
		WHERE (m.yajman_id = $1 OR m.pandit_id = $1)
	`
	args := []any{userID}

	if hasCursor {
		query += ` AND (m.matched_at, m.id) < ($2, $3)`
		args = append(args, cursorTime, cursorID)
	}

	query += ` ORDER BY m.matched_at DESC, m.id DESC LIMIT $` + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit+1)

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list matches by user: %w", err)
	}
	defer rows.Close()

	var matches []*domain.MatchWithDetails
	for rows.Next() {
		var md domain.MatchWithDetails
		var yPhoto, pPhoto *string
		if err := rows.Scan(
			&md.ID, &md.YajmanID, &md.PanditID, &md.EventID,
			&md.Status, &md.MatchedAt, &md.CreatedAt, &md.UpdatedAt,
			&md.YajmanFirstName, &md.YajmanLastName, &yPhoto, &md.YajmanPhone,
			&md.PanditFirstName, &md.PanditLastName, &pPhoto, &md.PanditPhone,
			&md.CeremonyType, &md.EventDate, &md.EventAddress,
		); err != nil {
			return nil, "", fmt.Errorf("scan match: %w", err)
		}
		md.YajmanPhotoURL = yPhoto
		md.PanditPhotoURL = pPhoto
		matches = append(matches, &md)
	}

	var nextCursor string
	if len(matches) > limit {
		matches = matches[:limit]
		last := matches[limit-1]
		nextCursor = encodeCursor(last.MatchedAt, last.ID)
	}

	return matches, nextCursor, nil
}

// ListByEventID returns all matches for an event.
func (r *PostgresMatchRepository) ListByEventID(ctx context.Context, eventID uuid.UUID) ([]*domain.MatchWithDetails, error) {
	query := `
		SELECT
			m.id, m.yajman_id, m.pandit_id, m.event_id, m.status, m.matched_at, m.created_at, m.updated_at,
			uy.first_name, uy.last_name, uy.profile_photo_url, uy.phone,
			up.first_name, up.last_name, up.profile_photo_url, up.phone,
			e.ceremony_type::text, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address
		FROM matches m
		JOIN users uy ON m.yajman_id = uy.id
		JOIN users up ON m.pandit_id = up.id
		JOIN events e ON m.event_id = e.id
		WHERE m.event_id = $1
		ORDER BY m.matched_at DESC
	`
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("list matches by event: %w", err)
	}
	defer rows.Close()

	var matches []*domain.MatchWithDetails
	for rows.Next() {
		var md domain.MatchWithDetails
		var yPhoto, pPhoto *string
		if err := rows.Scan(
			&md.ID, &md.YajmanID, &md.PanditID, &md.EventID,
			&md.Status, &md.MatchedAt, &md.CreatedAt, &md.UpdatedAt,
			&md.YajmanFirstName, &md.YajmanLastName, &yPhoto, &md.YajmanPhone,
			&md.PanditFirstName, &md.PanditLastName, &pPhoto, &md.PanditPhone,
			&md.CeremonyType, &md.EventDate, &md.EventAddress,
		); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}
		md.YajmanPhotoURL = yPhoto
		md.PanditPhotoURL = pPhoto
		matches = append(matches, &md)
	}

	return matches, nil
}

// CountByEventID returns the number of active matches for an event.
func (r *PostgresMatchRepository) CountByEventID(ctx context.Context, eventID uuid.UUID) (int, error) {
	var count int
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT COUNT(*) FROM matches WHERE event_id = $1 AND status IN ('matched', 'active')`,
		eventID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count matches by event: %w", err)
	}
	return count, nil
}

// GetByPanditAndEvent returns the match for a specific pandit+event pair.
func (r *PostgresMatchRepository) GetByPanditAndEvent(ctx context.Context, panditID, eventID uuid.UUID) (*domain.Match, error) {
	query := `
		SELECT id, yajman_id, pandit_id, event_id, status, matched_at, created_at, updated_at
		FROM matches WHERE pandit_id = $1 AND event_id = $2
	`
	var m domain.Match
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, panditID, eventID).Scan(
		&m.ID, &m.YajmanID, &m.PanditID, &m.EventID,
		&m.Status, &m.MatchedAt, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrMatchNotFound
		}
		return nil, fmt.Errorf("get match by pandit and event: %w", err)
	}
	return &m, nil
}

// --- Cursor helpers ---

type cursorData struct {
	time int64
	id   uuid.UUID
}

func parseCursor(cursor string) (cursorData, error) {
	// Format: "RFC3339Nano|UUID"
	parts := splitCursor(cursor)
	if len(parts) != 2 {
		return cursorData{}, fmt.Errorf("invalid cursor format")
	}
	t, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return cursorData{}, fmt.Errorf("parse cursor time: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return cursorData{}, fmt.Errorf("parse cursor id: %w", err)
	}
	return cursorData{time: t, id: id}, nil
}

func encodeCursor(t int64, id uuid.UUID) string {
	return strconv.FormatInt(t, 10) + "|" + id.String()
}

func splitCursor(cursor string) []string {
	idx := len(cursor) - 37 // UUID is 36 chars + 1 separator
	if idx < 0 {
		return nil
	}
	return []string{cursor[:idx], cursor[idx+1:]}
}

// isDuplicateKeyError checks for PostgreSQL unique violation (23505).
func isDuplicateKeyError(err error) bool {
	return err != nil && !errors.Is(err, pgx.ErrNoRows) &&
		(fmt.Sprintf("%v", err) != "" && containsDuplicateKey(err.Error()))
}

func containsDuplicateKey(s string) bool {
	return len(s) > 0 && (contains(s, "23505") || contains(s, "duplicate key"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// MarkMatchesCompletedForEvents transitions all active matches for a given
// set of event IDs to 'completed'. Called by the EventCompletionWorker after
// events are marked Completed. Returns the count of updated rows.
func (r *PostgresMatchRepository) MarkMatchesCompletedForEvents(ctx context.Context, eventIDs []uuid.UUID) (int64, error) {
	if len(eventIDs) == 0 {
		return 0, nil
	}
	query := `
		UPDATE matches
		SET status = 'completed',
		    updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE event_id = ANY($1)
		  AND status IN ('created', 'matched', 'active')
	`
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, eventIDs)
	if err != nil {
		return 0, fmt.Errorf("mark matches completed for events: %w", err)
	}
	return tag.RowsAffected(), nil
}

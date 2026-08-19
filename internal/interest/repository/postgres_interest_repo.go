package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/interest/domain"
)

// PostgresInterestRepository implements domain.InterestRepository using pgxpool.
type PostgresInterestRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresInterestRepository creates a new PostgresInterestRepository.
func NewPostgresInterestRepository(pool *pgxpool.Pool) *PostgresInterestRepository {
	return &PostgresInterestRepository{pool: pool}
}

// Create inserts a new interest.
func (r *PostgresInterestRepository) Create(ctx context.Context, interest *domain.Interest) error {
	query := `
		INSERT INTO interests (id, pandit_id, event_id, message, status)
		VALUES ($1, $2, $3, $4, $5)
	`

	if interest.ID == uuid.Nil {
		interest.ID = uuid.New()
	}

	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		interest.ID,
		interest.PanditID,
		interest.EventID,
		nullableString(interest.Message),
		interest.Status.String(),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrInterestAlreadyExists
		}
		return fmt.Errorf("create interest: %w", err)
	}

	return nil
}

// GetByID returns an interest by its ID.
func (r *PostgresInterestRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Interest, error) {
	query := `
		SELECT id, pandit_id, event_id, message, status, created_at, updated_at
		FROM interests
		WHERE id = $1
	`

	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id)
	return scanInterest(row)
}

// UpdateStatus updates the status of an interest.
func (r *PostgresInterestRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.InterestStatus) error {
	query := `UPDATE interests SET status = $2 WHERE id = $1`

	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, id, status.String())
	if err != nil {
		return fmt.Errorf("update interest status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInterestNotFound
	}

	return nil
}

// GetByPanditAndEvent returns the interest for a specific pandit+event pair.
func (r *PostgresInterestRepository) GetByPanditAndEvent(ctx context.Context, panditID, eventID uuid.UUID) (*domain.Interest, error) {
	query := `
		SELECT id, pandit_id, event_id, message, status, created_at, updated_at
		FROM interests
		WHERE pandit_id = $1 AND event_id = $2
	`

	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, panditID, eventID)
	return scanInterest(row)
}

// ListByEventID returns all interests for an event, with pandit details.
func (r *PostgresInterestRepository) ListByEventID(ctx context.Context, eventID uuid.UUID, cursor string, limit int) ([]*domain.InterestWithDetails, string, error) {
	if limit <= 0 {
		limit = 20
	}
	fetchLimit := limit + 1

	var cursorTime int64
	var cursorID uuid.UUID
	if cursor != "" {
		var err error
		cursorTime, cursorID, err = decodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
	}

	var query string
	var args []any

	// The LEFT JOIN on conversations finds the auto-created workspace between this pandit and the
	// event's yajman (First Flow). We match by both participants to be exact.
	convSubquery := `
		SELECT cv.id
		FROM conversations cv
		WHERE cv.type = 'pandit_yajman'
		  AND ((cv.user_one_id = i.pandit_id AND cv.user_two_id = e.yajman_id) OR (cv.user_one_id = e.yajman_id AND cv.user_two_id = i.pandit_id))
		LIMIT 1`

	if cursor == "" {
		query = fmt.Sprintf(`
			SELECT i.id, i.pandit_id, i.event_id, i.message, i.status,
			       i.created_at, i.updated_at,
			       u.first_name, u.last_name, u.profile_photo_url,
			       e.ceremony_type, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address,
			       c.image_url_no_bg AS ceremony_logo_url,
			       y.phone AS yajman_phone_number,
			       (%s) AS conversation_id
			FROM interests i
			    INNER JOIN users u ON u.id = i.pandit_id
			    INNER JOIN events e ON e.id = i.event_id
			    INNER JOIN users y ON y.id = e.yajman_id
			    LEFT JOIN festival_logos c ON c.slug = e.ceremony_type::text
			WHERE i.event_id = $1
			ORDER BY i.created_at DESC, i.id ASC
			LIMIT $2
		`, convSubquery)
		args = []any{eventID, fetchLimit}
	} else {
		query = fmt.Sprintf(`
			SELECT i.id, i.pandit_id, i.event_id, i.message, i.status,
			       i.created_at, i.updated_at,
			       u.first_name, u.last_name, u.profile_photo_url,
			       e.ceremony_type, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address,
			       c.image_url_no_bg AS ceremony_logo_url,
			       y.phone AS yajman_phone_number,
			       (%s) AS conversation_id
			FROM interests i
			    INNER JOIN users u ON u.id = i.pandit_id
			    INNER JOIN events e ON e.id = i.event_id
			    INNER JOIN users y ON y.id = e.yajman_id
			    LEFT JOIN festival_logos c ON c.slug = e.ceremony_type::text
			WHERE i.event_id = $1
			    AND (i.created_at, i.id) < ($3, $4)
			ORDER BY i.created_at DESC, i.id ASC
			LIMIT $2
		`, convSubquery)
		args = []any{eventID, fetchLimit, cursorTime, cursorID}
	}

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list interests by event: %w", err)
	}
	defer rows.Close()

	results, err := scanInterestWithDetails(rows)
	if err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return results, nextCursor, nil
}

// ListByPanditID returns all interests for a pandit, with event details.
func (r *PostgresInterestRepository) ListByPanditID(ctx context.Context, panditID uuid.UUID, cursor string, limit int) ([]*domain.InterestWithDetails, string, error) {
	if limit <= 0 {
		limit = 20
	}
	fetchLimit := limit + 1

	var cursorTime int64
	var cursorID uuid.UUID
	if cursor != "" {
		var err error
		cursorTime, cursorID, err = decodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
	}

	var query string
	var args []any

	// Subquery to find the auto-created conversation for this pandit+yajman pair (First Flow)
	convSubquery := `
		SELECT cv.id
		FROM conversations cv
		WHERE cv.type = 'pandit_yajman'
		  AND ((cv.user_one_id = i.pandit_id AND cv.user_two_id = e.yajman_id) OR (cv.user_one_id = e.yajman_id AND cv.user_two_id = i.pandit_id))
		LIMIT 1`

	if cursor == "" {
		query = fmt.Sprintf(`
			SELECT i.id, i.pandit_id, i.event_id, i.message, i.status,
			       i.created_at, i.updated_at,
			       u.first_name, u.last_name, u.profile_photo_url,
			       e.ceremony_type, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address,
			       c.image_url_no_bg AS ceremony_logo_url,
			       y.phone AS yajman_phone_number,
			       (%s) AS conversation_id
			FROM interests i
			    INNER JOIN users u ON u.id = i.pandit_id
			    INNER JOIN events e ON e.id = i.event_id
			    INNER JOIN users y ON y.id = e.yajman_id
			    LEFT JOIN festival_logos c ON c.slug = e.ceremony_type::text
			WHERE i.pandit_id = $1
			ORDER BY i.created_at DESC, i.id ASC
			LIMIT $2
		`, convSubquery)
		args = []any{panditID, fetchLimit}
	} else {
		query = fmt.Sprintf(`
			SELECT i.id, i.pandit_id, i.event_id, i.message, i.status,
			       i.created_at, i.updated_at,
			       u.first_name, u.last_name, u.profile_photo_url,
			       e.ceremony_type, to_char(to_timestamp(e.event_date), 'YYYY-MM-DD'), e.address,
			       c.image_url_no_bg AS ceremony_logo_url,
			       y.phone AS yajman_phone_number,
			       (%s) AS conversation_id
			FROM interests i
			    INNER JOIN users u ON u.id = i.pandit_id
			    INNER JOIN events e ON e.id = i.event_id
			    INNER JOIN users y ON y.id = e.yajman_id
			    LEFT JOIN festival_logos c ON c.slug = e.ceremony_type::text
			WHERE i.pandit_id = $1
			    AND (i.created_at, i.id) < ($3, $4)
			ORDER BY i.created_at DESC, i.id ASC
			LIMIT $2
		`, convSubquery)
		args = []any{panditID, fetchLimit, cursorTime, cursorID}
	}

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list interests by pandit: %w", err)
	}
	defer rows.Close()

	results, err := scanInterestWithDetails(rows)
	if err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(results) > limit {
		results = results[:limit]
		last := results[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return results, nextCursor, nil
}

// CountByEventID returns the number of interests for a given event.
func (r *PostgresInterestRepository) CountByEventID(ctx context.Context, eventID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM interests WHERE event_id = $1`
	var count int
	if err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, eventID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count interests: %w", err)
	}
	return count, nil
}

// BulkRejectByEventID updates all pending/connected interests for an event to rejected, excluding a specific pandit.
func (r *PostgresInterestRepository) BulkRejectByEventID(ctx context.Context, eventID uuid.UUID, excludePanditID uuid.UUID) error {
	query := `
		UPDATE interests
		SET status = $3, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE event_id = $1 AND pandit_id != $2 AND status IN ('pending', 'connected')
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, eventID, excludePanditID, domain.InterestStatusRejected.String())
	if err != nil {
		return fmt.Errorf("bulk reject interests: %w", err)
	}
	return nil
}

// BulkCancelByEventID updates all active interests for an event to event_cancelled.
func (r *PostgresInterestRepository) BulkCancelByEventID(ctx context.Context, eventID uuid.UUID) error {
	query := `
		UPDATE interests
		SET status = $2, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE event_id = $1 AND status IN ('pending', 'connected', 'accepted')
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, eventID, domain.InterestStatusEventCancelled.String())
	if err != nil {
		return fmt.Errorf("bulk cancel interests: %w", err)
	}
	return nil
}

// BulkCompleteByEventID updates the accepted interest for an event to completed.
func (r *PostgresInterestRepository) BulkCompleteByEventID(ctx context.Context, eventID uuid.UUID) error {
	query := `
		UPDATE interests
		SET status = $2, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
		WHERE event_id = $1 AND status = 'accepted'
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, eventID, domain.InterestStatusCompleted.String())
	if err != nil {
		return fmt.Errorf("bulk complete interests: %w", err)
	}
	return nil
}

// CountActiveByEventID returns the count of interests in pending or connected status for an event.
func (r *PostgresInterestRepository) CountActiveByEventID(ctx context.Context, eventID uuid.UUID) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM interests
		WHERE event_id = $1 AND status IN ('pending', 'connected')
	`
	var count int
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, eventID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active interests: %w", err)
	}
	return count, nil
}

// --- helpers ---

func scanInterest(row pgx.Row) (*domain.Interest, error) {
	var (
		interest domain.Interest
		message  *string
	)

	err := row.Scan(
		&interest.ID, &interest.PanditID, &interest.EventID,
		&message, &interest.Status,
		&interest.CreatedAt, &interest.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInterestNotFound
		}
		return nil, fmt.Errorf("scan interest: %w", err)
	}

	if message != nil {
		interest.Message = *message
	}

	return &interest, nil
}

func scanInterestWithDetails(rows pgx.Rows) ([]*domain.InterestWithDetails, error) {
	var results []*domain.InterestWithDetails
	for rows.Next() {
		var (
			interest          domain.Interest
			message           *string
			firstName         string
			lastName          string
			photoURL          *string
			ceremonyType      string
			eventDate         string
			address           string
			ceremonyLogoURL   *string
			yajmanPhoneNumber *string
			conversationID    *uuid.UUID
		)

		err := rows.Scan(
			&interest.ID, &interest.PanditID, &interest.EventID,
			&message, &interest.Status,
			&interest.CreatedAt, &interest.UpdatedAt,
			&firstName, &lastName, &photoURL,
			&ceremonyType, &eventDate, &address,
			&ceremonyLogoURL, &yajmanPhoneNumber,
			&conversationID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan interest with details: %w", err)
		}

		if message != nil {
			interest.Message = *message
		}
		interest.ConversationID = conversationID

		results = append(results, &domain.InterestWithDetails{
			Interest:          interest,
			PanditFirstName:   firstName,
			PanditLastName:    lastName,
			PanditPhotoURL:    photoURL,
			CeremonyType:      ceremonyType,
			CeremonyLogoURL:   ceremonyLogoURL,
			EventDate:         eventDate,
			EventAddress:      address,
			YajmanPhoneNumber: yajmanPhoneNumber,
			ConversationID:    conversationID,
		})
	}

	return results, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type cursorData struct {
	T  int64     `json:"t"`
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

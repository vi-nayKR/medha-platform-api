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
	"github.com/medha/backend/internal/notification/domain"
)

// PostgresNotificationRepository implements domain.NotificationRepository.
type PostgresNotificationRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresNotificationRepository creates a new PostgresNotificationRepository.
func NewPostgresNotificationRepository(pool *pgxpool.Pool) *PostgresNotificationRepository {
	return &PostgresNotificationRepository{pool: pool}
}

// Create inserts a new notification.
func (r *PostgresNotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	query := `
		INSERT INTO notifications (id, user_id, type, title, body, data, read, delivered_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		n.ID, n.UserID, n.Type, n.Title, n.Body, n.Data, n.Read, n.DeliveredAt, n.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// GetByID returns a notification by ID.
func (r *PostgresNotificationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	query := `
		SELECT id, user_id, type, title, body, data, read, delivered_at, created_at
		FROM notifications WHERE id = $1
	`
	var n domain.Notification
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id).Scan(
		&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Data,
		&n.Read, &n.DeliveredAt, &n.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotificationNotFound
		}
		return nil, fmt.Errorf("get notification: %w", err)
	}
	return &n, nil
}

// ListByUserID returns notifications with cursor-based pagination.
func (r *PostgresNotificationRepository) ListByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*domain.Notification, string, error) {
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
		SELECT id, user_id, type, title, body, data, read, delivered_at, created_at
		FROM notifications
		WHERE user_id = $1
	`
	args := []any{userID}

	if hasCursor {
		query += ` AND (created_at, id) < ($2, $3)`
		args = append(args, cursorTime, cursorID)
	}

	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*domain.Notification
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(
			&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Data,
			&n.Read, &n.DeliveredAt, &n.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("scan notification: %w", err)
		}
		notifications = append(notifications, &n)
	}

	var nextCursor string
	if len(notifications) > limit {
		notifications = notifications[:limit]
		last := notifications[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return notifications, nextCursor, nil
}

// MarkRead marks a single notification as read.
func (r *PostgresNotificationRepository) MarkRead(ctx context.Context, id uuid.UUID) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE notifications SET read = TRUE WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotificationNotFound
	}
	return nil
}

// MarkAllRead marks all notifications for a user as read.
func (r *PostgresNotificationRepository) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE notifications SET read = TRUE WHERE user_id = $1 AND read = FALSE`, userID,
	)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}
	return nil
}

// CountUnread returns the number of unread notifications.
func (r *PostgresNotificationRepository) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = FALSE`, userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return count, nil
}

// SaveDeviceToken stores or updates a device token (FCM/APNs) for a user.
func (r *PostgresNotificationRepository) SaveDeviceToken(ctx context.Context, userID uuid.UUID, token, deviceID, platform string) error {
	query := `
		INSERT INTO fcm_tokens (id, user_id, token, device_id, platform, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (token) DO UPDATE SET user_id = $2, device_id = $4, platform = $5, is_active = $6, updated_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, uuid.New(), userID, token, deviceID, platform, true)
	if err != nil {
		return fmt.Errorf("save device token: %w", err)
	}
	return nil
}

// GetActiveDeviceTokens returns all active device tokens for a user.
func (r *PostgresNotificationRepository) GetActiveDeviceTokens(ctx context.Context, userID uuid.UUID) ([]domain.DeviceToken, error) {
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx,
		`SELECT token, platform, is_active FROM fcm_tokens WHERE user_id = $1 AND is_active = TRUE`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get active device tokens: %w", err)
	}
	defer rows.Close()

	var tokens []domain.DeviceToken
	for rows.Next() {
		var t domain.DeviceToken
		if err := rows.Scan(&t.Token, &t.Platform, &t.IsActive); err != nil {
			return nil, fmt.Errorf("scan device token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// MarkDeviceTokenInactive marks a token as inactive.
func (r *PostgresNotificationRepository) MarkDeviceTokenInactive(ctx context.Context, token string) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, `UPDATE fcm_tokens SET is_active = FALSE WHERE token = $1`, token)
	if err != nil {
		return fmt.Errorf("mark device token inactive: %w", err)
	}
	return nil
}

// SaveNotificationLog records that a risk-tier notification was sent for an event.
func (r *PostgresNotificationRepository) SaveNotificationLog(ctx context.Context, eventID uuid.UUID, tier string) error {
	query := `
		INSERT INTO notification_log (id, event_id, tier)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id, tier) DO NOTHING
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query, uuid.New(), eventID, tier)
	if err != nil {
		return fmt.Errorf("save notification log: %w", err)
	}
	return nil
}

// HasNotificationLog checks if a risk-tier notification was already sent for an event.
func (r *PostgresNotificationRepository) HasNotificationLog(ctx context.Context, eventID uuid.UUID, tier string) (bool, error) {
	var exists bool
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM notification_log WHERE event_id = $1 AND tier = $2)`,
		eventID, tier,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check notification log: %w", err)
	}
	return exists, nil
}

// GetEventsAtRisk returns active events with zero interests whose event_date is within daysThreshold days from now.
func (r *PostgresNotificationRepository) GetEventsAtRisk(ctx context.Context, daysThreshold int) ([]domain.RiskEvent, error) {
	query := `
		SELECT e.id, e.yajman_id, e.ceremony_type, e.event_date,
		       ((e.event_date - (EXTRACT(EPOCH FROM CURRENT_DATE))::BIGINT) / 86400)::INT AS days_until,
		       COALESCE(ic.cnt, 0) AS interest_count
		FROM events e
		LEFT JOIN (
			SELECT event_id, COUNT(*) AS cnt FROM interests GROUP BY event_id
		) ic ON ic.event_id = e.id
		WHERE e.deleted_at IS NULL
		  AND e.status IN ('Created', 'Active')
		  AND e.event_date >= (EXTRACT(EPOCH FROM CURRENT_DATE))::BIGINT
		  AND e.event_date <= (EXTRACT(EPOCH FROM CURRENT_DATE))::BIGINT + ($1 * 86400)
		  AND COALESCE(ic.cnt, 0) = 0
		ORDER BY e.event_date ASC
	`
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, daysThreshold)
	if err != nil {
		return nil, fmt.Errorf("get events at risk: %w", err)
	}
	defer rows.Close()

	var events []domain.RiskEvent
	for rows.Next() {
		var ev domain.RiskEvent
		if err := rows.Scan(&ev.EventID, &ev.YajmanID, &ev.CeremonyType, &ev.EventDate, &ev.DaysUntil, &ev.InterestCount); err != nil {
			return nil, fmt.Errorf("scan risk event: %w", err)
		}
		events = append(events, ev)
	}
	return events, nil
}

// --- Cursor helpers ---

type cursorData struct {
	time int64
	id   uuid.UUID
}

func parseCursor(cursor string) (cursorData, error) {
	idx := len(cursor) - 37
	if idx < 0 {
		return cursorData{}, fmt.Errorf("invalid cursor")
	}
	parts := []string{cursor[:idx], cursor[idx+1:]}
	t, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return cursorData{}, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return cursorData{}, err
	}
	return cursorData{time: t, id: id}, nil
}

func encodeCursor(t int64, id uuid.UUID) string {
	return strconv.FormatInt(t, 10) + "|" + id.String()
}

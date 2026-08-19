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
	"github.com/medha/backend/internal/messaging/domain"
	"github.com/medha/backend/internal/platform/epoch"
)

// PostgresMessagingRepository implements domain.MessagingRepository.
type PostgresMessagingRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresMessagingRepository creates a new PostgresMessagingRepository.
func NewPostgresMessagingRepository(pool *pgxpool.Pool) *PostgresMessagingRepository {
	return &PostgresMessagingRepository{pool: pool}
}

// CreateConversation creates a conversation in the database.
func (r *PostgresMessagingRepository) CreateConversation(ctx context.Context, conv *domain.Conversation) error {
	exec := database.GetExecutor(ctx, r.pool)

	now := epoch.Now()
	var userOneID uuid.UUID
	var userTwoID *uuid.UUID

	if len(conv.Participants) > 0 {
		userOneID = conv.Participants[0].UserID
	}
	if len(conv.Participants) > 1 {
		userTwoID = &conv.Participants[1].UserID
	}

	query := `
		INSERT INTO conversations (id, type, event_id, match_id, title, is_active, user_one_id, user_two_id, user_one_last_read_at, user_two_last_read_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, 0, $9, $9)
	`
	_, err := exec.Exec(ctx, query,
		conv.ID, conv.Type, conv.EventID, conv.MatchID, conv.Title, conv.IsActive, userOneID, userTwoID, now,
	)
	if err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}
	conv.CreatedAt = now
	conv.UpdatedAt = now

	return nil
}

// GetConversation returns a conversation by ID, including its participants.
func (r *PostgresMessagingRepository) GetConversation(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	exec := database.GetExecutor(ctx, r.pool)

	// Fetch conversation
	query := `
		SELECT id, type, event_id, match_id, title, is_active, last_message_at, created_at, updated_at,
		       user_one_id, user_two_id, user_one_last_read_at, user_two_last_read_at
		FROM conversations WHERE id = $1
	`
	var conv domain.Conversation
	var userOneID uuid.UUID
	var userTwoID *uuid.UUID
	var userOneReadAt, userTwoReadAt int64
	err := exec.QueryRow(ctx, query, id).Scan(
		&conv.ID, &conv.Type, &conv.EventID, &conv.MatchID, &conv.Title,
		&conv.IsActive, &conv.LastMessageAt, &conv.CreatedAt, &conv.UpdatedAt,
		&userOneID, &userTwoID, &userOneReadAt, &userTwoReadAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConversationNotFound
		}
		return nil, fmt.Errorf("get conversation: %w", err)
	}

	// Fetch user details for participants
	participants, err := r.fetchParticipants(ctx, userOneID, userTwoID, userOneReadAt, userTwoReadAt, conv.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("fetch participants: %w", err)
	}
	conv.Participants = participants

	return &conv, nil
}

func (r *PostgresMessagingRepository) fetchParticipants(ctx context.Context, userOneID uuid.UUID, userTwoID *uuid.UUID, userOneReadAt, userTwoReadAt, joinedAt int64) ([]domain.Participant, error) {
	exec := database.GetExecutor(ctx, r.pool)
	ids := []uuid.UUID{userOneID}
	if userTwoID != nil {
		ids = append(ids, *userTwoID)
	}

	query := `
		SELECT id, COALESCE(first_name, '') AS first_name, COALESCE(last_name, '') AS last_name,
		       COALESCE(role::TEXT, '') AS role, COALESCE(profile_photo_url, '') AS photo_url
		FROM users
		WHERE id = ANY($1)
	`
	rows, err := exec.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	userMap := make(map[uuid.UUID]domain.Participant)
	for rows.Next() {
		var userID uuid.UUID
		var p domain.Participant
		if err := rows.Scan(&userID, &p.FirstName, &p.LastName, &p.Role, &p.PhotoURL); err != nil {
			return nil, err
		}
		p.UserID = userID
		p.JoinedAt = joinedAt
		if userID == userOneID {
			p.IsAdmin = true
			p.LastReadAt = userOneReadAt
		} else {
			p.IsAdmin = false
			p.LastReadAt = userTwoReadAt
		}
		userMap[userID] = p
	}

	res := make([]domain.Participant, 0, len(ids))
	if p, ok := userMap[userOneID]; ok {
		res = append(res, p)
	}
	if userTwoID != nil {
		if p, ok := userMap[*userTwoID]; ok {
			res = append(res, p)
		}
	}
	return res, nil
}

// ListUserConversations returns conversations for a user ordered by last_message_at DESC.
func (r *PostgresMessagingRepository) ListUserConversations(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]domain.Conversation, string, error) {
	exec := database.GetExecutor(ctx, r.pool)

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
		SELECT c.id, c.type, c.event_id, c.match_id, c.title, c.is_active, c.last_message_at, c.created_at, c.updated_at,
		       c.user_one_id, c.user_two_id, c.user_one_last_read_at, c.user_two_last_read_at
		FROM conversations c
		WHERE (c.user_one_id = $1 OR c.user_two_id = $1)
	`
	args := []any{userID}

	if hasCursor {
		query += ` AND (COALESCE(c.last_message_at, c.created_at), c.id) < ($2, $3)`
		args = append(args, cursorTime, cursorID)
	}

	query += fmt.Sprintf(` ORDER BY COALESCE(c.last_message_at, c.created_at) DESC, c.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var conversations []domain.Conversation
	type extraFields struct {
		userOneID     uuid.UUID
		userTwoID     *uuid.UUID
		userOneReadAt int64
		userTwoReadAt int64
	}
	var extras []extraFields

	for rows.Next() {
		var conv domain.Conversation
		var ex extraFields
		if err := rows.Scan(
			&conv.ID, &conv.Type, &conv.EventID, &conv.MatchID, &conv.Title,
			&conv.IsActive, &conv.LastMessageAt, &conv.CreatedAt, &conv.UpdatedAt,
			&ex.userOneID, &ex.userTwoID, &ex.userOneReadAt, &ex.userTwoReadAt,
		); err != nil {
			return nil, "", fmt.Errorf("scan conversation: %w", err)
		}
		conversations = append(conversations, conv)
		extras = append(extras, ex)
	}

	var nextCursor string
	if len(conversations) > limit {
		conversations = conversations[:limit]
		extras = extras[:limit]
		last := conversations[limit-1]
		ts := last.CreatedAt
		if last.LastMessageAt != nil {
			ts = *last.LastMessageAt
		}
		nextCursor = encodeCursor(ts, last.ID)
	}

	if len(conversations) == 0 {
		return conversations, nextCursor, nil
	}

	// Bulk load participants
	userIDsMap := make(map[uuid.UUID]bool)
	for _, ex := range extras {
		userIDsMap[ex.userOneID] = true
		if ex.userTwoID != nil {
			userIDsMap[*ex.userTwoID] = true
		}
	}

	userIDs := make([]uuid.UUID, 0, len(userIDsMap))
	for id := range userIDsMap {
		userIDs = append(userIDs, id)
	}

	uQuery := `
		SELECT id, COALESCE(first_name, '') AS first_name, COALESCE(last_name, '') AS last_name,
		       COALESCE(role::TEXT, '') AS role, COALESCE(profile_photo_url, '') AS photo_url
		FROM users
		WHERE id = ANY($1)
	`
	uRows, err := exec.Query(ctx, uQuery, userIDs)
	if err != nil {
		return nil, "", fmt.Errorf("bulk load user details: %w", err)
	}
	defer uRows.Close()

	type rawUser struct {
		firstName string
		lastName  string
		role      string
		photoURL  string
	}
	rawUsers := make(map[uuid.UUID]rawUser)
	for uRows.Next() {
		var uid uuid.UUID
		var u rawUser
		if err := uRows.Scan(&uid, &u.firstName, &u.lastName, &u.role, &u.photoURL); err != nil {
			return nil, "", err
		}
		rawUsers[uid] = u
	}

	for i := range conversations {
		ex := extras[i]
		pts := make([]domain.Participant, 0, 2)
		if u, ok := rawUsers[ex.userOneID]; ok {
			pts = append(pts, domain.Participant{
				UserID:     ex.userOneID,
				IsAdmin:    true,
				LastReadAt: ex.userOneReadAt,
				JoinedAt:   conversations[i].CreatedAt,
				FirstName:  u.firstName,
				LastName:   u.lastName,
				Role:       u.role,
				PhotoURL:   u.photoURL,
			})
		}
		if ex.userTwoID != nil {
			if u, ok := rawUsers[*ex.userTwoID]; ok {
				pts = append(pts, domain.Participant{
					UserID:     *ex.userTwoID,
					IsAdmin:    false,
					LastReadAt: ex.userTwoReadAt,
					JoinedAt:   conversations[i].CreatedAt,
					FirstName:  u.firstName,
					LastName:   u.lastName,
					Role:       u.role,
					PhotoURL:   u.photoURL,
				})
			}
		}
		conversations[i].Participants = pts
	}

	return conversations, nextCursor, nil
}

// IsParticipant checks whether a user is a participant in a conversation.
func (r *PostgresMessagingRepository) IsParticipant(ctx context.Context, conversationID, userID uuid.UUID) (bool, error) {
	exec := database.GetExecutor(ctx, r.pool)
	var exists bool
	err := exec.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversations WHERE id = $1 AND (user_one_id = $2 OR user_two_id = $2))`,
		conversationID, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check participant: %w", err)
	}
	return exists, nil
}

// FindDirectConversation finds an existing 1-on-1 conversation between two users.
func (r *PostgresMessagingRepository) FindDirectConversation(ctx context.Context, userA, userB uuid.UUID, convType domain.ConversationType) (*domain.Conversation, error) {
	exec := database.GetExecutor(ctx, r.pool)

	query := `
		SELECT id
		FROM conversations
		WHERE type = $3 AND is_active = TRUE
		  AND ((user_one_id = $1 AND user_two_id = $2) OR (user_one_id = $2 AND user_two_id = $1))
		LIMIT 1
	`
	var convID uuid.UUID
	err := exec.QueryRow(ctx, query, userA, userB, convType).Scan(&convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Not found — not an error
		}
		return nil, fmt.Errorf("find direct conversation: %w", err)
	}

	return r.GetConversation(ctx, convID)
}

// CreateMessage inserts a message and updates conversation.last_message_at.
func (r *PostgresMessagingRepository) CreateMessage(ctx context.Context, msg *domain.Message) error {
	return database.WithTx(ctx, r.pool, func(txCtx context.Context) error {
		exec := database.GetExecutor(txCtx, r.pool)

		now := epoch.Now()
		query := `
			INSERT INTO messages (id, conversation_id, sender_id, content_type, content, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`
		metadata := msg.Metadata
		if metadata == nil {
			metadata = []byte("{}")
		}
		_, err := exec.Exec(txCtx, query,
			msg.ID, msg.ConversationID, msg.SenderID, msg.ContentType, msg.Content, metadata, now,
		)
		if err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		msg.CreatedAt = now

		// Update conversation.last_message_at
		_, err = exec.Exec(txCtx,
			`UPDATE conversations SET last_message_at = $1 WHERE id = $2`,
			now, msg.ConversationID,
		)
		if err != nil {
			return fmt.Errorf("update last_message_at: %w", err)
		}

		return nil
	})
}

// ListMessages returns messages in a conversation with cursor pagination.
func (r *PostgresMessagingRepository) ListMessages(ctx context.Context, conversationID uuid.UUID, cursor string, limit int) ([]domain.Message, string, error) {
	exec := database.GetExecutor(ctx, r.pool)

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
		SELECT m.id, m.conversation_id, m.sender_id, m.content_type, m.content, m.metadata, m.created_at,
		       COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '') AS sender_name
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.conversation_id = $1
	`
	args := []any{conversationID}

	if hasCursor {
		query += ` AND (m.created_at, m.id) < ($2, $3)`
		args = append(args, cursorTime, cursorID)
	}

	query += fmt.Sprintf(` ORDER BY m.created_at DESC, m.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.SenderID, &m.ContentType, &m.Content, &m.Metadata, &m.CreatedAt,
			&m.SenderName,
		); err != nil {
			return nil, "", fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}

	var nextCursor string
	if len(messages) > limit {
		messages = messages[:limit]
		last := messages[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return messages, nextCursor, nil
}

// MarkRead updates the participant's last_read_at.
func (r *PostgresMessagingRepository) MarkRead(ctx context.Context, conversationID, userID uuid.UUID, upToTimestamp int64) error {
	exec := database.GetExecutor(ctx, r.pool)
	tag, err := exec.Exec(ctx,
		`UPDATE conversations
		 SET user_one_last_read_at = CASE WHEN user_one_id = $3 THEN $1 ELSE user_one_last_read_at END,
		     user_two_last_read_at = CASE WHEN user_two_id = $3 THEN $1 ELSE user_two_last_read_at END
		 WHERE id = $2 AND (user_one_id = $3 OR user_two_id = $3)`,
		upToTimestamp, conversationID, userID,
	)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotParticipant
	}
	return nil
}

// GetUnreadCounts returns unread message counts per conversation for a user.
func (r *PostgresMessagingRepository) GetUnreadCounts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error) {
	exec := database.GetExecutor(ctx, r.pool)

	query := `
		SELECT c.id, COUNT(m.id)
		FROM conversations c
		INNER JOIN messages m ON m.conversation_id = c.id 
		                     AND m.created_at > CASE WHEN c.user_one_id = $1 THEN c.user_one_last_read_at ELSE c.user_two_last_read_at END
		WHERE (c.user_one_id = $1 OR c.user_two_id = $1) AND m.sender_id != $1
		GROUP BY c.id
	`
	rows, err := exec.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("get unread counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var convID uuid.UUID
		var count int
		if err := rows.Scan(&convID, &count); err != nil {
			return nil, fmt.Errorf("scan unread count: %w", err)
		}
		counts[convID] = count
	}
	return counts, nil
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

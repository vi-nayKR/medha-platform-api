package domain

import (
	"context"

	"github.com/google/uuid"
)

// MessagingRepository defines the port for messaging data access.
type MessagingRepository interface {
	// ── Conversations ────────────────────────────────────────

	// CreateConversation creates a new conversation with participants.
	CreateConversation(ctx context.Context, conv *Conversation) error

	// GetConversation returns a conversation by ID, including participants.
	GetConversation(ctx context.Context, id uuid.UUID) (*Conversation, error)

	// ListUserConversations returns conversations for a user with cursor pagination.
	// Results are ordered by last_message_at DESC (most recent first).
	ListUserConversations(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]Conversation, string, error)

	// IsParticipant checks whether a user is a participant in a conversation.
	IsParticipant(ctx context.Context, conversationID, userID uuid.UUID) (bool, error)

	// FindDirectConversation finds an existing 1-on-1 conversation between two users of a given type.
	FindDirectConversation(ctx context.Context, userA, userB uuid.UUID, convType ConversationType) (*Conversation, error)

	// ── Messages ─────────────────────────────────────────────

	// CreateMessage inserts a new message and updates last_message_at on the conversation.
	CreateMessage(ctx context.Context, msg *Message) error

	// ListMessages returns messages in a conversation with cursor pagination.
	// Results are ordered by created_at DESC (newest first).
	ListMessages(ctx context.Context, conversationID uuid.UUID, cursor string, limit int) ([]Message, string, error)

	// ── Read tracking ────────────────────────────────────────

	// MarkRead updates the participant's last_read_at to the given timestamp.
	MarkRead(ctx context.Context, conversationID, userID uuid.UUID, upToTimestamp int64) error

	// GetUnreadCounts returns a map of conversation_id → unread message count for a user.
	GetUnreadCounts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error)
}

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/messaging/domain"
	notifdomain "github.com/medha/backend/internal/notification/domain"
	notifservice "github.com/medha/backend/internal/notification/service"
	"github.com/medha/backend/internal/platform/epoch"
)

// ChatPublisher defines the interface for publishing real-time chat messages.
type ChatPublisher interface {
	PublishChat(ctx context.Context, conversationID uuid.UUID, payload []byte) error
}

// MessagingService handles messaging business logic.
type MessagingService struct {
	repo      domain.MessagingRepository
	publisher ChatPublisher                     // real-time delivery via Redis
	notifSvc  *notifservice.NotificationService // push notifications for offline users
	logger    *slog.Logger
}

// NewMessagingService creates a new MessagingService.
func NewMessagingService(
	repo domain.MessagingRepository,
	publisher ChatPublisher,
	notifSvc *notifservice.NotificationService,
	logger *slog.Logger,
) *MessagingService {
	return &MessagingService{
		repo:      repo,
		publisher: publisher,
		notifSvc:  notifSvc,
		logger:    logger,
	}
}

// CreateConversationParams contains the parameters for creating a conversation.
type CreateConversationParams struct {
	Type           domain.ConversationType
	ParticipantIDs []uuid.UUID // other participant IDs (caller is auto-added)
	EventID        *uuid.UUID
	MatchID        *uuid.UUID
	Title          string
}

// CreateConversation creates a new conversation. If a direct conversation between
// the same users already exists, it returns the existing one.
func (s *MessagingService) CreateConversation(ctx context.Context, callerID uuid.UUID, params CreateConversationParams) (*domain.Conversation, error) {
	if !params.Type.IsValid() {
		return nil, fmt.Errorf("invalid conversation type: %s", params.Type)
	}

	// For direct conversations (non-help), enforce 2-participant limit and dedup check
	if params.Type != domain.ConvHelp {
		if len(params.ParticipantIDs) != 1 {
			return nil, fmt.Errorf("direct conversations must have exactly one other participant")
		}
		// Check if conversation already exists between these users
		existing, err := s.repo.FindDirectConversation(ctx, callerID, params.ParticipantIDs[0], params.Type)
		if err != nil {
			return nil, fmt.Errorf("check existing conversation: %w", err)
		}
		if existing != nil {
			s.logger.Info("returning existing conversation", "conversation_id", existing.ID)
			return existing, nil
		}
	}

	// Build participant list (caller + other participants)
	allIDs := append([]uuid.UUID{callerID}, params.ParticipantIDs...)
	participants := make([]domain.Participant, len(allIDs))
	for i, id := range allIDs {
		participants[i] = domain.Participant{
			UserID:  id,
			IsAdmin: i == 0, // creator is admin
		}
	}

	conv := &domain.Conversation{
		ID:           uuid.New(),
		Type:         params.Type,
		EventID:      params.EventID,
		MatchID:      params.MatchID,
		Title:        params.Title,
		IsActive:     true,
		Participants: participants,
	}

	if err := s.repo.CreateConversation(ctx, conv); err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}

	s.logger.Info("conversation created",
		"conversation_id", conv.ID,
		"type", conv.Type,
		"participant_count", len(conv.Participants),
	)

	// Re-fetch to get joined user info (names, roles)
	full, err := s.repo.GetConversation(ctx, conv.ID)
	if err != nil {
		// Non-fatal — return what we have
		s.logger.Warn("failed to re-fetch conversation", "error", err)
		return conv, nil
	}

	return full, nil
}

// GetConversation returns a conversation if the caller is a participant.
func (s *MessagingService) GetConversation(ctx context.Context, callerID, conversationID uuid.UUID) (*domain.Conversation, error) {
	ok, err := s.repo.IsParticipant(ctx, conversationID, callerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrNotParticipant
	}
	return s.repo.GetConversation(ctx, conversationID)
}

// ListConversations returns conversations for a user with cursor pagination.
func (s *MessagingService) ListConversations(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]domain.Conversation, string, error) {
	convs, nextCursor, err := s.repo.ListUserConversations(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list conversations: %w", err)
	}

	// Attach unread counts
	unreadCounts, err := s.repo.GetUnreadCounts(ctx, userID)
	if err != nil {
		s.logger.Warn("failed to get unread counts", "error", err)
		// Non-fatal
	}

	// We can't embed unread counts into the Conversation struct directly,
	// so the handler will handle that via a separate map. This is fine.
	_ = unreadCounts

	return convs, nextCursor, nil
}

// SendMessageParams contains the parameters for sending a message.
type SendMessageParams struct {
	ConversationID uuid.UUID
	SenderID       uuid.UUID
	ContentType    domain.MessageContentType
	Content        string
	Metadata       json.RawMessage
}

// SendMessage sends a message to a conversation, persists it, and fans out via Pub/Sub.
func (s *MessagingService) SendMessage(ctx context.Context, params SendMessageParams) (*domain.Message, error) {
	params.Content = strings.TrimSpace(params.Content)
	if params.Content == "" {
		return nil, fmt.Errorf("message content is required")
	}

	// Only text messages are supported for now
	if params.ContentType != domain.ContentText && params.ContentType != domain.ContentSystem {
		params.ContentType = domain.ContentText
	}

	// Verify sender is a participant
	ok, err := s.repo.IsParticipant(ctx, params.ConversationID, params.SenderID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, domain.ErrNotParticipant
	}

	msg := &domain.Message{
		ID:             uuid.New(),
		ConversationID: params.ConversationID,
		SenderID:       params.SenderID,
		ContentType:    params.ContentType,
		Content:        params.Content,
		Metadata:       params.Metadata,
	}

	if err := s.repo.CreateMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}

	conv, err := s.repo.GetConversation(ctx, params.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("load conversation after message create: %w", err)
	}
	recipientIDs := make([]uuid.UUID, 0, len(conv.Participants))
	for _, p := range conv.Participants {
		recipientIDs = append(recipientIDs, p.UserID)
		if p.UserID == msg.SenderID {
			msg.SenderName = strings.TrimSpace(p.FirstName + " " + p.LastName)
		}
	}

	s.logger.Debug("message persisted",
		"message_id", msg.ID,
		"conversation_id", msg.ConversationID,
		"sender_id", msg.SenderID,
	)

	// Publish to Redis for real-time delivery
	if s.publisher != nil {
		go func() {
			publishCtx, cancel := detachedTimeout(ctx, 10*time.Second)
			defer cancel()

			wsMsg := map[string]any{
				"type": "chat.message",
				"payload": map[string]any{
					"id":              msg.ID,
					"conversation_id": msg.ConversationID,
					"sender_id":       msg.SenderID,
					"sender_name":     msg.SenderName,
					"content_type":    msg.ContentType,
					"content":         msg.Content,
					"metadata":        msg.Metadata,
					"created_at":      msg.CreatedAt,
					"recipient_ids":   recipientIDs,
				},
			}
			data, err := json.Marshal(wsMsg)
			if err != nil {
				s.logger.Error("failed to marshal chat message", "error", err)
				return
			}
			if err := s.publisher.PublishChat(publishCtx, msg.ConversationID, data); err != nil {
				s.logger.Error("failed to publish chat message", "error", err, "conversation_id", msg.ConversationID)
			}
		}()
	}

	// Send push notification to other participants who may be offline
	if s.notifSvc != nil {
		go func() {
			pushCtx, cancel := detachedTimeout(ctx, 30*time.Second)
			defer cancel()

			s.sendOfflinePush(pushCtx, msg)
		}()
	}

	return msg, nil
}

// sendOfflinePush sends a push notification to all participants except the sender.
func (s *MessagingService) sendOfflinePush(ctx context.Context, msg *domain.Message) {
	conv, err := s.repo.GetConversation(ctx, msg.ConversationID)
	if err != nil {
		s.logger.Error("failed to load conversation for push", "error", err)
		return
	}

	for _, p := range conv.Participants {
		if p.UserID == msg.SenderID {
			continue
		}
		if p.Muted {
			continue
		}

		_, _ = s.notifSvc.CreateNotification(ctx, notifservice.CreateNotificationParams{
			UserID: p.UserID,
			Type:   notifdomain.NotifChatMessage,
			Title:  "New Message",
			Body:   truncate(msg.Content, 100),
			Data: map[string]string{
				"conversation_id": msg.ConversationID.String(),
				"sender_id":       msg.SenderID.String(),
				"type":            "chat_message",
			},
		})
	}
}

// ListMessages returns messages in a conversation with cursor pagination.
func (s *MessagingService) ListMessages(ctx context.Context, callerID, conversationID uuid.UUID, cursor string, limit int) ([]domain.Message, string, error) {
	ok, err := s.repo.IsParticipant(ctx, conversationID, callerID)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", domain.ErrNotParticipant
	}

	return s.repo.ListMessages(ctx, conversationID, cursor, limit)
}

// MarkRead marks all messages in a conversation as read up to now.
func (s *MessagingService) MarkRead(ctx context.Context, callerID, conversationID uuid.UUID) error {
	return s.repo.MarkRead(ctx, conversationID, callerID, epoch.Now())
}

// GetUnreadCounts returns unread counts per conversation for a user.
func (s *MessagingService) GetUnreadCounts(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error) {
	return s.repo.GetUnreadCounts(ctx, userID)
}

// ListParticipantIDs returns conversation participant IDs after verifying the
// caller is a participant.
func (s *MessagingService) ListParticipantIDs(ctx context.Context, conversationID, callerID uuid.UUID) ([]uuid.UUID, error) {
	conv, err := s.GetConversation(ctx, callerID, conversationID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(conv.Participants))
	for _, p := range conv.Participants {
		ids = append(ids, p.UserID)
	}
	return ids, nil
}

// truncate shortens a string to maxLen and appends "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

func detachedTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

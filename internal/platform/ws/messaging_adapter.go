package ws

import (
	"context"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/messaging/domain"
	messagingservice "github.com/medha/backend/internal/messaging/service"
)

// MessagingAdapter bridges the ws.Hub MessageHandler interface to the messaging service.
// It translates inbound WebSocket payloads into service calls.
type MessagingAdapter struct {
	msgSvc *messagingservice.MessagingService
}

// NewMessagingAdapter creates a new MessagingAdapter.
func NewMessagingAdapter(msgSvc *messagingservice.MessagingService) *MessagingAdapter {
	return &MessagingAdapter{msgSvc: msgSvc}
}

// HandleWSChatSend processes an inbound "chat.send" message from a WebSocket client.
func (a *MessagingAdapter) HandleWSChatSend(ctx context.Context, senderID uuid.UUID, payload ChatSendPayload) error {
	contentType := domain.ContentText
	if payload.ContentType != "" {
		ct := domain.MessageContentType(payload.ContentType)
		if ct.IsValid() {
			contentType = ct
		}
	}

	_, err := a.msgSvc.SendMessage(ctx, messagingservice.SendMessageParams{
		ConversationID: payload.ConversationID,
		SenderID:       senderID,
		ContentType:    contentType,
		Content:        payload.Content,
	})
	return err
}

// HandleWSChatTyping validates that the user is in the conversation and returns
// the participant IDs that should receive the transient typing event.
func (a *MessagingAdapter) HandleWSChatTyping(ctx context.Context, userID uuid.UUID, payload ChatTypingPayload) ([]uuid.UUID, error) {
	return a.msgSvc.ListParticipantIDs(ctx, payload.ConversationID, userID)
}

// HandleWSChatRead processes an inbound "chat.read" message from a WebSocket client.
func (a *MessagingAdapter) HandleWSChatRead(ctx context.Context, userID uuid.UUID, payload ChatReadPayload) error {
	return a.msgSvc.MarkRead(ctx, userID, payload.ConversationID)
}

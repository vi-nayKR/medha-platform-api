package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RedisSubscriber listens to Redis Pub/Sub channels and forwards messages to the Hub.
type RedisSubscriber struct {
	redis  *redis.Client
	hub    *Hub
	logger *slog.Logger
}

// NewRedisSubscriber creates a new RedisSubscriber.
func NewRedisSubscriber(redis *redis.Client, hub *Hub, logger *slog.Logger) *RedisSubscriber {
	return &RedisSubscriber{
		redis:  redis,
		hub:    hub,
		logger: logger,
	}
}

// StartGlobalSubscriber starts a single goroutine that listens for ALL notifications
// and chat messages using a pattern subscription and routes them to the Hub.
func (s *RedisSubscriber) StartGlobalSubscriber(ctx context.Context) error {
	// Subscribe to both notification and chat channels
	pubsub := s.redis.PSubscribe(ctx, "notifications:*", "chat:*")

	_, err := pubsub.Receive(ctx)
	if err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("psubscribe: %w", err)
	}

	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()

		s.logger.Info("global redis subscriber started", "patterns", []string{"notifications:*", "chat:*"})

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				if strings.HasPrefix(msg.Channel, "notifications:") {
					s.handleNotification(msg)
				} else if strings.HasPrefix(msg.Channel, "chat:") {
					s.handleChatMessage(msg)
				}
			}
		}
	}()

	return nil
}

// handleNotification routes a notification message to a specific user.
func (s *RedisSubscriber) handleNotification(msg *redis.Message) {
	// Pattern message: channel name is notifications:<uuid>
	userIDStr, ok := strings.CutPrefix(msg.Channel, "notifications:")
	if !ok || userIDStr == "" {
		s.logger.Error("failed to parse channel name", "channel", msg.Channel)
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		s.logger.Error("invalid uuid in channel name", "channel", msg.Channel, "error", err)
		return
	}

	s.logger.Debug("routing notification from redis", "user_id", userID)

	// Forward to Hub
	if err := s.hub.SendToUser(userID, []byte(msg.Payload)); err != nil {
		// User not connected to this instance, ignore
		s.logger.Debug("user not connected to this instance", "user_id", userID)
	}
}

// handleChatMessage routes a chat message to all connected participants of a conversation.
// The payload contains the conversation_id, which we use to look up connected participants.
func (s *RedisSubscriber) handleChatMessage(msg *redis.Message) {
	// Parse the envelope to extract conversation participants info
	// The payload is a full Envelope with type "chat.message" and the message data
	var envelope struct {
		Type    string `json:"type"`
		Payload struct {
			ConversationID uuid.UUID   `json:"conversation_id"`
			SenderID       uuid.UUID   `json:"sender_id"`
			RecipientIDs   []uuid.UUID `json:"recipient_ids"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
		s.logger.Error("failed to parse chat message payload", "error", err)
		return
	}

	s.logger.Debug("routing chat message from redis",
		"conversation_id", envelope.Payload.ConversationID,
		"sender_id", envelope.Payload.SenderID,
	)

	for _, userID := range envelope.Payload.RecipientIDs {
		if err := s.hub.SendToUser(userID, []byte(msg.Payload)); err != nil {
			s.logger.Debug("chat recipient not connected to this instance", "user_id", userID)
		}
	}
}

package ws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/medha/backend/internal/notification/domain"
)

// RedisPublisher handles publishing notifications and chat messages to Redis Pub/Sub.
type RedisPublisher struct {
	redis *redis.Client
}

// NewRedisPublisher creates a new RedisPublisher.
func NewRedisPublisher(redis *redis.Client) *RedisPublisher {
	return &RedisPublisher{
		redis: redis,
	}
}

// Publish sends a notification to the user's specific Redis channel.
func (p *RedisPublisher) Publish(ctx context.Context, userID uuid.UUID, notification *domain.Notification) error {
	channel := fmt.Sprintf("notifications:%s", userID)

	// In a real implementation, you'd use a DO matching NotificationResponse from handler.go
	// For simplicity, we'll serialize the domain object or a map.
	// Reusing the structure from handler.go to ensure client compatibility.
	msg := map[string]any{
		"id":         notification.ID,
		"type":       notification.Type,
		"title":      notification.Title,
		"body":       notification.Body,
		"data":       notification.Data,
		"read":       notification.Read,
		"created_at": notification.CreatedAt,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	if err := p.redis.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("redis publish: %w", err)
	}

	return nil
}

// PublishChat sends a chat message to a conversation-specific Redis channel.
// All API instances subscribed to this channel will deliver the message to connected participants.
func (p *RedisPublisher) PublishChat(ctx context.Context, conversationID uuid.UUID, payload []byte) error {
	channel := fmt.Sprintf("chat:%s", conversationID)

	if err := p.redis.Publish(ctx, channel, payload).Err(); err != nil {
		return fmt.Errorf("redis publish chat: %w", err)
	}

	return nil
}

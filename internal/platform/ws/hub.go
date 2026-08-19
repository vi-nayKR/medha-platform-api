package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// InboundMessage represents a message received from a client via WebSocket.
type InboundMessage struct {
	Conn     *Conn
	Envelope Envelope
}

// Envelope is the standard wire format for all WebSocket messages.
type Envelope struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	RequestID string          `json:"request_id,omitempty"`
}

// ChatSendPayload is the payload for "chat.send" messages from the client.
type ChatSendPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Content        string    `json:"content"`
	ContentType    string    `json:"content_type"`
}

// ChatTypingPayload is the payload for "chat.typing" messages.
type ChatTypingPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	IsTyping       bool      `json:"is_typing"`
}

// ChatReadPayload is the payload for "chat.read" messages.
type ChatReadPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	UpTo           int64     `json:"up_to"`
}

// MessageHandler is called by the Hub when a chat message is received.
// The implementation is provided by the messaging service layer.
type MessageHandler interface {
	HandleWSChatSend(ctx context.Context, senderID uuid.UUID, payload ChatSendPayload) error
	HandleWSChatTyping(ctx context.Context, userID uuid.UUID, payload ChatTypingPayload) ([]uuid.UUID, error)
	HandleWSChatRead(ctx context.Context, userID uuid.UUID, payload ChatReadPayload) error
}

// Hub maintains the set of active connections and broadcasts messages to them.
type Hub struct {
	// Registered connections by UserID.
	// Map: UserID -> map of connections (a user can have multiple devices/tabs)
	userConnections map[uuid.UUID]map[*Conn]bool

	// Inbound messages from the connections.
	broadcast chan []byte

	// Inbound chat messages from client connections.
	inbound chan *InboundMessage

	// Register requests from the connections.
	register chan *Conn

	// Unregister requests from connections.
	unregister chan *Conn

	// Optional message handler for processing inbound chat messages.
	msgHandler MessageHandler

	mu     sync.RWMutex
	logger *slog.Logger
}

// NewHub creates a new Hub.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		broadcast:       make(chan []byte),
		inbound:         make(chan *InboundMessage, 256),
		register:        make(chan *Conn),
		unregister:      make(chan *Conn),
		userConnections: make(map[uuid.UUID]map[*Conn]bool),
		logger:          logger,
	}
}

// SetMessageHandler sets the handler for inbound chat messages.
// Must be called before Run().
func (h *Hub) SetMessageHandler(handler MessageHandler) {
	h.msgHandler = handler
}

// Run starts the Hub main loop.
func (h *Hub) Run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			if _, ok := h.userConnections[conn.userID]; !ok {
				h.userConnections[conn.userID] = make(map[*Conn]bool)
			}
			h.userConnections[conn.userID][conn] = true
			h.mu.Unlock()
			h.logger.Info("websocket connection registered", "user_id", conn.userID)

		case conn := <-h.unregister:
			h.mu.Lock()
			if conns, ok := h.userConnections[conn.userID]; ok {
				if _, ok := conns[conn]; ok {
					delete(conns, conn)
					close(conn.send)
					if len(conns) == 0 {
						delete(h.userConnections, conn.userID)
					}
					h.logger.Info("websocket connection unregistered", "user_id", conn.userID)
				}
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			// Note: This broadcast is for system-wide messages.
			// For user-specific notifications, use SendToUser.
			h.mu.RLock()
			for userID, conns := range h.userConnections {
				for conn := range conns {
					select {
					case conn.send <- message:
					default:
						h.logger.Warn("dropping broadcast message", "user_id", userID)
					}
				}
			}
			h.mu.RUnlock()

		case msg := <-h.inbound:
			go h.handleInbound(msg)
		}
	}
}

// handleInbound routes inbound client messages to the appropriate handler.
func (h *Hub) handleInbound(msg *InboundMessage) {
	if h.msgHandler == nil {
		h.logger.Debug("no message handler configured, ignoring inbound message", "type", msg.Envelope.Type)
		msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
			"code":    "handler_unavailable",
			"message": "Realtime messaging is not available right now.",
		})
		return
	}

	ctx := context.Background()

	switch msg.Envelope.Type {
	case "chat.send":
		var payload ChatSendPayload
		if err := json.Unmarshal(msg.Envelope.Payload, &payload); err != nil {
			h.logger.Warn("invalid chat.send payload", "error", err, "user_id", msg.Conn.userID)
			msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
				"code":    "invalid_payload",
				"message": "Invalid chat.send payload.",
			})
			return
		}
		if err := h.msgHandler.HandleWSChatSend(ctx, msg.Conn.userID, payload); err != nil {
			h.logger.Error("chat.send handler failed", "error", err, "user_id", msg.Conn.userID)
			msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
				"code":    "send_failed",
				"message": "Unable to send message.",
			})
			return
		}
		msg.Conn.sendEnvelope("chat.sent", msg.Envelope.RequestID, map[string]any{
			"conversation_id": payload.ConversationID,
		})

	case "chat.typing":
		var payload ChatTypingPayload
		if err := json.Unmarshal(msg.Envelope.Payload, &payload); err != nil {
			msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
				"code":    "invalid_payload",
				"message": "Invalid chat.typing payload.",
			})
			return
		}
		recipients, err := h.msgHandler.HandleWSChatTyping(ctx, msg.Conn.userID, payload)
		if err != nil {
			h.logger.Error("chat.typing handler failed", "error", err, "user_id", msg.Conn.userID)
			msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
				"code":    "typing_failed",
				"message": "Unable to send typing indicator.",
			})
			return
		}
		typingMsg, _ := json.Marshal(Envelope{
			Type: "chat.typing",
			Payload: mustMarshal(map[string]any{
				"conversation_id": payload.ConversationID,
				"user_id":         msg.Conn.userID,
				"is_typing":       payload.IsTyping,
			}),
		})
		for _, userID := range recipients {
			if userID == msg.Conn.userID {
				continue
			}
			if err := h.SendToUser(userID, typingMsg); err != nil {
				h.logger.Debug("typing recipient not connected", "user_id", userID)
			}
		}

	case "chat.read":
		var payload ChatReadPayload
		if err := json.Unmarshal(msg.Envelope.Payload, &payload); err != nil {
			msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
				"code":    "invalid_payload",
				"message": "Invalid chat.read payload.",
			})
			return
		}
		if err := h.msgHandler.HandleWSChatRead(ctx, msg.Conn.userID, payload); err != nil {
			h.logger.Error("chat.read handler failed", "error", err, "user_id", msg.Conn.userID)
			msg.Conn.sendEnvelope("error", msg.Envelope.RequestID, map[string]any{
				"code":    "read_failed",
				"message": "Unable to mark the conversation as read.",
			})
			return
		}
		msg.Conn.sendEnvelope("chat.read", msg.Envelope.RequestID, map[string]any{
			"conversation_id": payload.ConversationID,
		})

	default:
		h.logger.Debug("unknown inbound message type", "type", msg.Envelope.Type, "user_id", msg.Conn.userID)
	}
}

// SendToUser sends a message to all active connections for a specific user.
func (h *Hub) SendToUser(userID uuid.UUID, message []byte) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conns, ok := h.userConnections[userID]
	if !ok {
		return fmt.Errorf("user %s not connected", userID)
	}

	for conn := range conns {
		select {
		case conn.send <- message:
		default:
			h.logger.Warn("dropping notification message", "user_id", userID)
		}
	}

	return nil
}

// UserHasConnections returns true if the user has at least one active WebSocket connection.
func (h *Hub) UserHasConnections(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.userConnections[userID]
	return ok
}

// mustMarshal is a helper that marshals JSON and panics on error (for static payloads only).
func mustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustMarshal: %v", err))
	}
	return data
}

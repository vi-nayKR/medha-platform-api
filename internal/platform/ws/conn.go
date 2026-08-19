package ws

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 4096
)

// Conn is a middleman between the WebSocket connection and the hub.
type Conn struct {
	hub *Hub

	// The UserID this connection belongs to.
	userID uuid.UUID

	// The WebSocket connection.
	ws *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte

	logger *slog.Logger
}

// readPump pumps messages from the WebSocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Conn) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("websocket read error", "error", err)
			}
			break
		}

		// Parse envelope and route to hub's inbound channel
		var envelope Envelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			c.logger.Warn("invalid ws message format", "error", err, "user_id", c.userID)
			c.sendEnvelope("error", "", map[string]any{
				"code":    "invalid_json",
				"message": "Message must be a valid websocket envelope.",
			})
			continue
		}

		// Route recognized message types to the inbound handler
		switch envelope.Type {
		case "chat.send", "chat.typing", "chat.read":
			select {
			case c.hub.inbound <- &InboundMessage{Conn: c, Envelope: envelope}:
			default:
				c.logger.Warn("inbound channel full, dropping message", "type", envelope.Type, "user_id", c.userID)
			}
		default:
			c.logger.Debug("unknown ws message type from client", "type", envelope.Type, "user_id", c.userID)
			c.sendEnvelope("error", envelope.RequestID, map[string]any{
				"code":    "unknown_type",
				"message": "Unsupported websocket message type.",
			})
		}
	}
}

func (c *Conn) sendEnvelope(messageType, requestID string, payload any) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		c.logger.Error("failed to marshal websocket envelope payload", "error", err, "type", messageType)
		return
	}
	raw, err := json.Marshal(Envelope{
		Type:      messageType,
		RequestID: requestID,
		Payload:   rawPayload,
	})
	if err != nil {
		c.logger.Error("failed to marshal websocket envelope", "error", err, "type", messageType)
		return
	}
	select {
	case c.send <- raw:
	default:
		c.logger.Warn("websocket send buffer full, dropping envelope", "type", messageType, "user_id", c.userID)
	}
}

// writePump pumps messages from the hub to the WebSocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued chat messages to the current WebSocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

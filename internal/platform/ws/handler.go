package ws

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	authservice "github.com/medha/backend/internal/auth/service"
	apierrors "github.com/medha/backend/pkg/errors"
)

func originAllowed(origin string, isDev bool) bool {
	if origin == "" {
		return true
	}
	if isDev {
		return origin == "https://admin-dev.medha.dev" || origin == "https://dev.medha.dev"
	}
	return origin == "https://admin.medha.dev" ||
		origin == "https://medha.dev" ||
		origin == "https://www.medha.dev"
}

// ServeWS handles WebSocket requests from the peer.
func ServeWS(hub *Hub, authSvc *authservice.AuthService, logger *slog.Logger, isDev bool) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(r.Header.Get("Origin"), isDev)
		},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// WebSocket doesn't support custom headers on initial connection in many clients,
		// so we accept the token as a query parameter.
		token := r.URL.Query().Get("token")
		if token == "" {
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("token required", r.URL.Path))
			return
		}

		// Validate token
		claims, err := authSvc.ValidateJWT(token)
		if err != nil {
			logger.Warn("websocket auth failed", "error", err)
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("invalid token", r.URL.Path))
			return
		}

		// Upgrade connection
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("websocket upgrade failed", "error", err)
			// Upgrade already sent a response, so we just return.
			return
		}

		c := &Conn{
			hub:    hub,
			userID: claims.UserID,
			ws:     conn,
			send:   make(chan []byte, 256),
			logger: logger,
		}

		c.hub.register <- c

		// Allow collection of memory referenced by the caller by doing all work in
		// new goroutines.
		go c.writePump()
		go c.readPump()
	}
}

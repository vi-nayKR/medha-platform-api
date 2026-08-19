package handler

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/notification/domain"
	"github.com/medha/backend/internal/notification/service"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// FCMPlaceholderHandler provides stub endpoints to test FCM flows for domains that don't exist yet (Chat, Direct Connection).
type FCMPlaceholderHandler struct {
	notifSvc *service.NotificationService
}

// NewFCMPlaceholderHandler creates a new FCMPlaceholderHandler.
func NewFCMPlaceholderHandler(notifSvc *service.NotificationService) *FCMPlaceholderHandler {
	return &FCMPlaceholderHandler{notifSvc: notifSvc}
}

// connectionReq body
type connectionReq struct {
	TargetUserID uuid.UUID `json:"target_user_id"`
}

// RequestConnection handles POST /api/v2/connection/request (Placeholder)
// Flow 2c: Connection Request
func (h *FCMPlaceholderHandler) RequestConnection(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())

	var req connectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}

	_, _ = h.notifSvc.CreateNotification(r.Context(), service.CreateNotificationParams{
		UserID: req.TargetUserID,
		Type:   domain.NotifConnectionRequest,
		Title:  "🤝 New Connection Request",
		Body:   "Someone wants to connect with you.",
		Data: map[string]string{
			"source_user_id": userID.String(),
			"type":           "connection_request",
		},
	})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "connection request sent"})
}

// ConfirmConnection handles POST /api/v2/connection (Placeholder)
// Flow 1b: Connection Confirmed
func (h *FCMPlaceholderHandler) ConfirmConnection(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())

	var req connectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}

	_, _ = h.notifSvc.CreateNotification(r.Context(), service.CreateNotificationParams{
		UserID: req.TargetUserID,
		Type:   domain.NotifConnectionConfirmed,
		Title:  "🎉 Connection Accepted!",
		Body:   "You are now connected.",
		Data: map[string]string{
			"source_user_id": userID.String(),
			"type":           "connection_confirmed",
		},
	})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "connection confirmed"})
}

// chatMsgReq body
type chatMsgReq struct {
	TargetUserID uuid.UUID `json:"target_user_id"`
	Message      string    `json:"message"`
}

// SendMessage handles POST /api/v2/chat/message (Placeholder)
// Flow 3a: Chat Message
func (h *FCMPlaceholderHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.UserIDFromContext(r.Context())

	var req chatMsgReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}

	_, _ = h.notifSvc.CreateNotification(r.Context(), service.CreateNotificationParams{
		UserID: req.TargetUserID,
		Type:   domain.NotifChatMessage,
		Title:  "New Message",
		Body:   req.Message,
		Data: map[string]string{
			"source_user_id": userID.String(),
			"type":           "chat_message",
		},
	})
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "chat message sent"})
}

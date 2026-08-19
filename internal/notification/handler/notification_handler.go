package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/notification/domain"
	notifservice "github.com/medha/backend/internal/notification/service"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// NotificationHandler handles notification HTTP endpoints.
type NotificationHandler struct {
	notifService *notifservice.NotificationService
	logger       *slog.Logger
}

// NewNotificationHandler creates a new NotificationHandler.
func NewNotificationHandler(notifService *notifservice.NotificationService, logger *slog.Logger) *NotificationHandler {
	return &NotificationHandler{
		notifService: notifService,
		logger:       logger,
	}
}

// --- Request/Response DTOs ---

// RegisterDeviceTokenRequest is the request body for POST /api/v2/notification/device-token.
type RegisterDeviceTokenRequest struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id" validate:"required"`
	Platform string `json:"platform" validate:"required,oneof=android ios"`
}

// UnregisterDeviceTokenRequest is the request body for DELETE /api/v2/notification/device-token.
type UnregisterDeviceTokenRequest struct {
	Token string `json:"token" validate:"required"`
}

// NotificationResponse represents a notification in API responses.
type NotificationResponse struct {
	ID          uuid.UUID `json:"id"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Data        any       `json:"data"`
	Read        bool      `json:"read"`
	DeliveredAt *string   `json:"delivered_at"`
	CreatedAt   string    `json:"created_at"`
}

// --- Handlers ---

// ListNotifications handles GET /api/v2/notification.
// Summary List my notifications
// Description Returns a paginated list of notifications for the authenticated user, including unread count.
// Tags Notifications
// Accept json
// Produce json
// Security bearerAuth
// Param cursor query string false "Pagination cursor"
// Param limit query int false "Pagination limit" default(20)
// Success 200 {object} response.DataResponse{data=[]NotificationResponse,unread_count=int} "List of notifications"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/notification [get]
func (h *NotificationHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	notifications, nextCursor, unreadCount, err := h.notifService.ListNotifications(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list notifications failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load notifications. Please try again later.", r.URL.Path))
		return
	}

	responses := make([]NotificationResponse, len(notifications))
	for i, n := range notifications {
		responses[i] = toNotificationResponse(n)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}

	// Custom response with unread count
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":         responses,
		"cursor":       cursorPtr,
		"unread_count": unreadCount,
	})
}

// MarkRead handles PUT /api/v2/notification/{id}/read.
// Summary Mark notification as read
// Description Updates a notification's status to read.
// Tags Notifications
// Accept json
// Produce json
// Security bearerAuth
// Param id path string true "Notification UUID"
// Success 200 {object} response.DataResponse{data=map[string]string} "Marked as read"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Failure 404 {object} apierrors.ProblemDetail "Notification not found"
// Router /api/v2/notification/{id}/read [put]
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	notifID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The notification ID provided is not valid.", r.URL.Path))
		return
	}

	if err := h.notifService.MarkRead(r.Context(), notifID, userID); err != nil {
		if errors.Is(err, domain.ErrNotificationNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This notification could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("mark read failed", "error", err, "notification_id", notifID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to mark notification as read. Please try again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "notification marked as read"})
}

// MarkAllRead handles PUT /api/v2/notification/read-all.
// Summary Mark all notifications as read
// Description Updates all of the user's unread notifications to read status.
// Tags Notifications
// Accept json
// Produce json
// Security bearerAuth
// Success 200 {object} response.DataResponse{data=map[string]string} "All marked as read"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/notification/read-all [put]
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	if err := h.notifService.MarkAllRead(r.Context(), userID); err != nil {
		h.logger.Error("mark all read failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to mark all notifications as read. Please try again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "all notifications marked as read"})
}

// RegisterDeviceToken handles POST /api/v2/notification/device-token.
// Summary Register a device token (FCM or APNs)
// Description Associates a push token with the user's account and platform.
// Tags Notifications
// Accept json
// Produce json
// Security bearerAuth
// Param request body RegisterDeviceTokenRequest true "Token details"
// Success 200 {object} response.DataResponse{data=map[string]string} "Token registered"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/notification/device-token [post]
func (h *NotificationHandler) RegisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req RegisterDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}

	if req.Token == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(domain.ErrDeviceTokenRequired.Error()+".", r.URL.Path))
		return
	}

	if req.Platform != "android" && req.Platform != "ios" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Device platform must be either 'android' or 'ios'.", r.URL.Path))
		return
	}

	if err := h.notifService.RegisterDeviceToken(r.Context(), userID, req.Token, req.DeviceID, req.Platform); err != nil {
		h.logger.Error("register device token failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to register your device for notifications. Please try again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "device token registered"})
}

// UnregisterDeviceToken handles DELETE /api/v2/notification/device-token.
// Summary Unregister a device token
// Description Marks a device token as inactive.
// Tags Notifications
// Accept json
// Produce json
// Security bearerAuth
// Param request body UnregisterDeviceTokenRequest true "Token to unregister"
// Success 200 {object} response.DataResponse{data=map[string]string} "Token unregistered"
// Failure 401 {object} apierrors.ProblemDetail "Unauthorized"
// Router /api/v2/notification/device-token [delete]
func (h *NotificationHandler) UnregisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	// Auth check: only token owner should be able to unregister
	_, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req UnregisterDeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request.", r.URL.Path))
		return
	}

	if req.Token == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("A device token is required to unregister.", r.URL.Path))
		return
	}

	if err := h.notifService.UnregisterDeviceToken(r.Context(), req.Token); err != nil {
		h.logger.Error("unregister device token failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to unregister your device. Please try again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "device token unregistered"})
}

// --- Helpers ---

func toNotificationResponse(n *domain.Notification) NotificationResponse {
	resp := NotificationResponse{
		ID:        n.ID,
		Type:      n.Type.String(),
		Title:     n.Title,
		Body:      n.Body,
		Data:      n.Data,
		Read:      n.Read,
		CreatedAt: epoch.ToTime(n.CreatedAt).Format("2006-01-02T15:04:05Z07:00"),
	}
	if n.DeliveredAt != nil {
		t := epoch.ToTime(*n.DeliveredAt).Format("2006-01-02T15:04:05Z07:00")
		resp.DeliveredAt = &t
	}
	return resp
}

func parseIntParam(r *http.Request, key string, defaultVal int) int {
	valStr := r.URL.Query().Get(key)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil || val <= 0 {
		return defaultVal
	}
	return val
}

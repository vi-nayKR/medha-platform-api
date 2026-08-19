package handler

import (
	"net/http"

	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

var (
	_ = response.DataResponse{}
	_ = apierrors.ProblemDetail{}
)

// ListNotificationsV2 handles GET /api/v2/notification.
//
// @Summary      List my notifications
// @Description  Returns a paginated list of notifications for the authenticated user, including unread count.
// @Tags         notification-v2
// @Produce      json
// @Security     BearerAuth
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.DataResponse{data=[]NotificationResponse} "Notifications"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/notification [get]
func (h *NotificationHandler) ListNotificationsV2(w http.ResponseWriter, r *http.Request) {
	h.ListNotifications(w, r)
}

// MarkReadV2 handles PUT /api/v2/notification/{id}/read.
//
// @Summary      Mark notification as read
// @Description  Updates a single notification's status to read.
// @Tags         notification-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Notification UUID"
// @Success      200 {object} response.DataResponse{data=interface{}} "Marked as read"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Notification not found"
// @Router       /api/v2/notification/{id}/read [put]
func (h *NotificationHandler) MarkReadV2(w http.ResponseWriter, r *http.Request) {
	h.MarkRead(w, r)
}

// MarkAllReadV2 handles PUT /api/v2/notification/read-all.
//
// @Summary      Mark all notifications as read
// @Description  Marks all of the authenticated user's unread notifications as read.
// @Tags         notification-v2
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} response.DataResponse{data=interface{}} "All marked as read"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/notification/read-all [put]
func (h *NotificationHandler) MarkAllReadV2(w http.ResponseWriter, r *http.Request) {
	h.MarkAllRead(w, r)
}

// RegisterDeviceTokenV2 handles POST /api/v2/notification/device-token.
//
// @Summary      Register device push token
// @Description  Associates a push token (FCM or APNs) with the user's account for the given platform.
// @Tags         notification-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body RegisterDeviceTokenRequest true "Token details"
// @Success      200 {object} response.DataResponse{data=interface{}} "Token registered"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/notification/device-token [post]
func (h *NotificationHandler) RegisterDeviceTokenV2(w http.ResponseWriter, r *http.Request) {
	h.RegisterDeviceToken(w, r)
}

// UnregisterDeviceTokenV2 handles DELETE /api/v2/notification/device-token.
//
// @Summary      Unregister device push token
// @Description  Marks a device push token as inactive.
// @Tags         notification-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body UnregisterDeviceTokenRequest true "Token to unregister"
// @Success      200 {object} response.DataResponse{data=interface{}} "Token unregistered"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/notification/device-token [delete]
func (h *NotificationHandler) UnregisterDeviceTokenV2(w http.ResponseWriter, r *http.Request) {
	h.UnregisterDeviceToken(w, r)
}


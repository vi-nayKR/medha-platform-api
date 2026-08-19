package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/notification/domain"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/platform/worker"
	pushservice "github.com/medha/backend/internal/push/service"
)

// NotificationPublisher defines the port for real-time notification delivery.
type NotificationPublisher interface {
	Publish(ctx context.Context, userID uuid.UUID, notification *domain.Notification) error
}

// NotificationService handles notification business logic.
type NotificationService struct {
	notifRepo  domain.NotificationRepository
	publisher  NotificationPublisher   // optional real-time publisher
	pushSvc    pushservice.PushService // optional push service
	workerPool *worker.Pool            // worker pool for async push dispatch
	logger     *slog.Logger
}

// NewNotificationService creates a new NotificationService.
func NewNotificationService(notifRepo domain.NotificationRepository, publisher NotificationPublisher, pushSvc pushservice.PushService, workerPool *worker.Pool, logger *slog.Logger) *NotificationService {
	return &NotificationService{
		notifRepo:  notifRepo,
		publisher:  publisher,
		pushSvc:    pushSvc,
		workerPool: workerPool,
		logger:     logger,
	}
}

// CreateNotificationParams contains the parameters for creating a notification.
type CreateNotificationParams struct {
	UserID uuid.UUID
	Type   domain.NotificationType
	Title  string
	Body   string
	Data   map[string]string // deep-link payload
}

// CreateNotification creates and persists a notification.
func (s *NotificationService) CreateNotification(ctx context.Context, params CreateNotificationParams) (*domain.Notification, error) {
	data, _ := json.Marshal(params.Data)
	if params.Data == nil {
		data = []byte("{}")
	}

	notif := &domain.Notification{
		ID:        uuid.New(),
		UserID:    params.UserID,
		Type:      params.Type,
		Title:     params.Title,
		Body:      params.Body,
		Data:      data,
		Read:      false,
		CreatedAt: epoch.Now(),
	}

	if err := s.notifRepo.Create(ctx, notif); err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}

	s.logger.Info("notification created",
		"notification_id", notif.ID,
		"user_id", params.UserID,
		"type", params.Type,
		"priority", params.Type.Priority(),
	)

	// Publish to real-time delivery if publisher is configured
	if s.publisher != nil {
		go func() {
			publishCtx, cancel := detachedTimeout(ctx, 10*time.Second)
			defer cancel()

			if err := s.publisher.Publish(publishCtx, params.UserID, notif); err != nil {
				s.logger.Error("failed to publish real-time notification",
					"error", err,
					"notification_id", notif.ID,
					"user_id", params.UserID,
				)
			}
		}()
	}

	// Send push notification if push service is configured
	if s.pushSvc != nil {
		dispatchJob := func(wCtx context.Context) error {
			pushCtx, cancel := detachedTimeout(wCtx, 30*time.Second)
			defer cancel()

			err := s.pushSvc.SendToUser(pushCtx, params.UserID, pushservice.PushNotification{
				Title: params.Title,
				Body:  params.Body,
				Data:  params.Data,
			})
			if err != nil {
				s.logger.Error("failed to send push notification",
					"error", err,
					"user_id", params.UserID,
				)
				return err
			}
			return nil
		}

		enqueued := false
		if s.workerPool != nil {
			enqueued = s.workerPool.TryEnqueue(dispatchJob)
		}

		if !enqueued {
			if s.workerPool != nil {
				s.logger.Warn("push notification worker pool queue is full — falling back to raw goroutine", "user_id", params.UserID)
			}
			go func() {
				_ = dispatchJob(ctx)
			}()
		}
	}

	return notif, nil
}

func detachedTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}

// ListNotifications returns notifications for a user with cursor pagination.
func (s *NotificationService) ListNotifications(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]*domain.Notification, string, int, error) {
	notifications, nextCursor, err := s.notifRepo.ListByUserID(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list notifications: %w", err)
	}

	unreadCount, err := s.notifRepo.CountUnread(ctx, userID)
	if err != nil {
		s.logger.Error("failed to count unread", "error", err, "user_id", userID)
		// Don't fail, just return 0
	}

	return notifications, nextCursor, unreadCount, nil
}

// MarkRead marks a single notification as read.
func (s *NotificationService) MarkRead(ctx context.Context, notifID, userID uuid.UUID) error {
	// Verify ownership
	notif, err := s.notifRepo.GetByID(ctx, notifID)
	if err != nil {
		return fmt.Errorf("get notification: %w", err)
	}
	if notif.UserID != userID {
		return domain.ErrNotificationNotFound // don't reveal existence
	}
	return s.notifRepo.MarkRead(ctx, notifID)
}

// MarkAllRead marks all notifications for a user as read.
func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.notifRepo.MarkAllRead(ctx, userID)
}

// RegisterFCMToken stores an FCM token for push notification delivery.
func (s *NotificationService) RegisterFCMToken(ctx context.Context, userID uuid.UUID, token, deviceID string) error {
	// For legacy calls, we assume android platform
	if err := s.notifRepo.SaveDeviceToken(ctx, userID, token, deviceID, "android"); err != nil {
		return fmt.Errorf("save device token: %w", err)
	}
	s.logger.Info("fcm token registered", "user_id", userID)
	return nil
}

// RegisterDeviceToken stores a device token (FCM/APNs) for push notification delivery.
func (s *NotificationService) RegisterDeviceToken(ctx context.Context, userID uuid.UUID, token, deviceID, platform string) error {
	if err := s.notifRepo.SaveDeviceToken(ctx, userID, token, deviceID, platform); err != nil {
		return fmt.Errorf("save device token: %w", err)
	}
	s.logger.Info("device token registered", "user_id", userID, "platform", platform)
	return nil
}

// UnregisterFCMToken removes an FCM token (marks it inactive).
func (s *NotificationService) UnregisterFCMToken(ctx context.Context, token string) error {
	return s.notifRepo.MarkDeviceTokenInactive(ctx, token)
}

// UnregisterDeviceToken removes a device token (marks it inactive).
func (s *NotificationService) UnregisterDeviceToken(ctx context.Context, token string) error {
	return s.notifRepo.MarkDeviceTokenInactive(ctx, token)
}

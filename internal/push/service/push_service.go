package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/google/uuid"
	"google.golang.org/api/option"

	"github.com/medha/backend/internal/config"
	"github.com/medha/backend/internal/notification/domain"
)

// PushNotification represents the content of a push notification.
type PushNotification struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	ImageURL string            `json:"image_url,omitempty"`
}

// PushService defines the interface for sending multi-platform push notifications.
type PushService interface {
	SendToUser(ctx context.Context, userID uuid.UUID, n PushNotification) error
	SendToDevice(ctx context.Context, token string, title string, body string, data map[string]string) error
}

type pushServiceImpl struct {
	notifRepo domain.NotificationRepository
	cfg       *config.Config
	fcmClient *messaging.Client
	logger    *slog.Logger
}

// NewPushService creates a PushService backed by Firebase Admin SDK.
// If Firebase credentials are not configured, FCM sends are no-ops (logged only).
func NewPushService(notifRepo domain.NotificationRepository, cfg *config.Config, logger *slog.Logger) PushService {
	svc := &pushServiceImpl{
		notifRepo: notifRepo,
		cfg:       cfg,
		logger:    logger,
	}

	// Try to initialize Firebase
	fcmClient, err := initFirebase(cfg, logger)
	if err != nil {
		logger.Warn("firebase init failed — FCM push disabled (sends will be logged only)", "error", err)
	} else if fcmClient != nil {
		svc.fcmClient = fcmClient
		logger.Info("firebase messaging client initialized — FCM push enabled")
	} else {
		logger.Info("firebase credentials not configured — FCM push disabled")
	}

	return svc
}

// initFirebase creates a Firebase messaging.Client from credentials.
// Returns (nil, nil) if no credentials are configured.
func initFirebase(cfg *config.Config, logger *slog.Logger) (*messaging.Client, error) {
	ctx := context.Background()
	var app *firebase.App
	var err error

	if cfg.FirebaseCredentialsPath != "" {
		// Load from file path
		app, err = firebase.NewApp(ctx, nil, option.WithAuthCredentialsFile(option.ServiceAccount, cfg.FirebaseCredentialsPath))
		if err != nil {
			return nil, fmt.Errorf("firebase from file: %w", err)
		}
		logger.Info("firebase app initialized from credentials file", "path", cfg.FirebaseCredentialsPath)
	} else if cfg.FirebaseServiceAccountJSON != "" {
		// Load from base64-encoded JSON string
		jsonBytes, decErr := base64.StdEncoding.DecodeString(cfg.FirebaseServiceAccountJSON)
		if decErr != nil {
			// Try raw JSON (not base64-encoded)
			jsonBytes = []byte(cfg.FirebaseServiceAccountJSON)
		}

		// Extract and log the project_id for verification (FCM tokens are project-scoped)
		var saCreds struct {
			ProjectID string `json:"project_id"`
		}
		if jsonErr := json.Unmarshal(jsonBytes, &saCreds); jsonErr == nil && saCreds.ProjectID != "" {
			logger.Info("firebase service account project_id", "project_id", saCreds.ProjectID)
		} else {
			logger.Warn("could not extract project_id from firebase service account JSON — verify credentials are correct")
		}

		app, err = firebase.NewApp(ctx, nil, option.WithAuthCredentialsJSON(option.ServiceAccount, jsonBytes))
		if err != nil {
			return nil, fmt.Errorf("firebase from json: %w", err)
		}
		logger.Info("firebase app initialized from service account JSON env var")
	} else {
		// No credentials configured — FCM disabled
		return nil, nil
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase messaging client: %w", err)
	}
	return client, nil
}

// SendToUser sends a push notification to all active device tokens for a user.
func (s *pushServiceImpl) SendToUser(ctx context.Context, userID uuid.UUID, n PushNotification) error {
	tokens, err := s.notifRepo.GetActiveDeviceTokens(ctx, userID)
	if err != nil {
		s.logger.Error("push: failed to get active device tokens",
			"error", err,
			"user_id", userID,
		)
		return fmt.Errorf("get active tokens: %w", err)
	}

	if len(tokens) == 0 {
		s.logger.Warn("push: no active device tokens for user — push skipped",
			"user_id", userID,
			"title", n.Title,
		)
		return nil
	}

	s.logger.Info("push: sending to user",
		"user_id", userID,
		"token_count", len(tokens),
		"title", n.Title,
	)

	for _, t := range tokens {
		go func(token domain.DeviceToken) {
			s.logger.Debug("push: sending to device",
				"user_id", userID,
				"platform", token.Platform,
				"token", truncateToken(token.Token),
			)
			sendErr := s.SendToDevice(context.Background(), token.Token, n.Title, n.Body, n.Data)
			if sendErr != nil {
				s.logger.Error("push: failed to send to device",
					"error", sendErr,
					"user_id", userID,
					"platform", token.Platform,
					"token", truncateToken(token.Token),
				)
			} else {
				s.logger.Info("push: successfully sent to device",
					"user_id", userID,
					"platform", token.Platform,
					"token", truncateToken(token.Token),
				)
			}
		}(t)
	}

	return nil
}

// SendToDevice sends a push notification to a single device token via FCM.
func (s *pushServiceImpl) SendToDevice(ctx context.Context, token string, title string, body string, data map[string]string) error {
	if s.fcmClient == nil {
		// FCM not configured — log and return
		s.logger.Warn("FCM send (no-op — credentials not configured)",
			"token", truncateToken(token),
			"title", title,
		)
		return nil
	}

	msg := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Data: data,
		Android: &messaging.AndroidConfig{
			Priority: "high",
			Notification: &messaging.AndroidNotification{
				Sound:       "default",
				ClickAction: "FLUTTER_NOTIFICATION_CLICK",
				ChannelID:   "medha_events",
			},
		},
		APNS: &messaging.APNSConfig{
			Payload: &messaging.APNSPayload{
				Aps: &messaging.Aps{
					Sound:            "default",
					MutableContent:   true,
					ContentAvailable: true,
				},
			},
		},
	}

	resp, err := s.fcmClient.Send(ctx, msg)
	if err != nil {
		// Check if the token is invalid/unregistered
		if messaging.IsUnregistered(err) ||
			messaging.IsInvalidArgument(err) {
			s.logger.Warn("FCM token invalid, marking inactive",
				"token", truncateToken(token),
				"error", err,
			)
			// Mark the token as inactive asynchronously
			go func() {
				if markErr := s.notifRepo.MarkDeviceTokenInactive(context.Background(), token); markErr != nil {
					s.logger.Error("failed to mark invalid token inactive", "error", markErr)
				}
			}()
		}
		return fmt.Errorf("fcm send: %w", err)
	}

	s.logger.Debug("FCM message sent",
		"message_id", resp,
		"token", truncateToken(token),
		"title", title,
	)
	return nil
}

// truncateToken returns the first 12 chars of a token for safe logging.
func truncateToken(t string) string {
	if len(t) > 12 {
		return t[:12] + "..."
	}
	return t
}

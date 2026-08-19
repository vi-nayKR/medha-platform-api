package handler

import (
	"log/slog"

	"github.com/go-playground/validator/v10"

	eventservice "github.com/medha/backend/internal/event/service"
	"github.com/medha/backend/internal/infra/storage"
)

// EventHandler handles event-related HTTP endpoints.
type EventHandler struct {
	eventService *eventservice.EventService
	s3Client     *storage.S3Client
	validate     *validator.Validate
	logger       *slog.Logger
}

// NewEventHandler creates a new EventHandler.
func NewEventHandler(eventService *eventservice.EventService, s3Client *storage.S3Client, logger *slog.Logger) *EventHandler {
	return &EventHandler{
		eventService: eventService,
		s3Client:     s3Client,
		validate:     validator.New(),
		logger:       logger,
	}
}

// Handlers and DTOs segregated to event_yajman_handler.go, event_pandit_handler.go, and event_common_handler.go

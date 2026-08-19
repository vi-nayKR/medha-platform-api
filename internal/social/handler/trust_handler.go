package handler

import (
	"log/slog"

	"github.com/go-playground/validator/v10"

	socialservice "github.com/medha/backend/internal/social/service"
)

// TrustHandler handles feedback and badge HTTP endpoints.
type TrustHandler struct {
	trustService *socialservice.TrustService
	validate     *validator.Validate
	logger       *slog.Logger
}

// NewTrustHandler creates a new TrustHandler.
func NewTrustHandler(trustService *socialservice.TrustService, logger *slog.Logger) *TrustHandler {
	return &TrustHandler{
		trustService: trustService,
		validate:     validator.New(),
		logger:       logger,
	}
}

// Handlers and DTOs segregated to trust_yajman_handler.go and trust_common_handler.go

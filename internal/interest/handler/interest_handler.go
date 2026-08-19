package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/interest/domain"
	interestservice "github.com/medha/backend/internal/interest/service"
	"github.com/medha/backend/internal/platform/epoch"
)

// InterestHandler handles interest-related HTTP endpoints.
type InterestHandler struct {
	interestService *interestservice.InterestService
	validate        *validator.Validate
	logger          *slog.Logger
}

// NewInterestHandler creates a new InterestHandler.
func NewInterestHandler(interestService *interestservice.InterestService, logger *slog.Logger) *InterestHandler {
	return &InterestHandler{
		interestService: interestService,
		validate:        validator.New(),
		logger:          logger,
	}
}

// --- Request/Response DTOs ---

// InterestResponse represents an interest in API responses.
type InterestResponse struct {
	ID               uuid.UUID  `json:"id"`
	PanditID         uuid.UUID  `json:"pandit_id"`
	EventID          uuid.UUID  `json:"event_id"`
	Message          string     `json:"message"`
	Status           string     `json:"status"`
	ConnectionStatus string     `json:"connection_status"` // "pending" | "connected"
	ConversationID   *uuid.UUID `json:"conversation_id,omitempty"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
}

// InterestDetailResponse adds joined pandit/event details.
type InterestDetailResponse struct {
	InterestResponse
	PanditFirstName   string  `json:"pandit_first_name,omitempty"`
	PanditLastName    string  `json:"pandit_last_name,omitempty"`
	PanditPhotoURL    *string `json:"pandit_photo_url,omitempty"`
	CeremonyType      string  `json:"ceremony_type,omitempty"`
	CeremonyLogoURL   *string `json:"ceremony_logo_url,omitempty"`
	EventDate         string  `json:"event_date,omitempty"`
	EventAddress      string  `json:"event_address,omitempty"`
	YajmanPhoneNumber *string `json:"yajman_phone_number,omitempty"`
}

func toInterestResponse(interest *domain.Interest) InterestResponse {
	connStatus := "pending"
	if interest.ConversationID != nil {
		connStatus = "connected"
	}
	return InterestResponse{
		ID:               interest.ID,
		PanditID:         interest.PanditID,
		EventID:          interest.EventID,
		Message:          interest.Message,
		Status:           interest.Status.String(),
		ConnectionStatus: connStatus,
		ConversationID:   interest.ConversationID,
		CreatedAt:        epoch.ToTime(interest.CreatedAt).Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        epoch.ToTime(interest.UpdatedAt).Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toInterestDetailResponse(interest *domain.InterestWithDetails) InterestDetailResponse {
	connStatus := "pending"
	convID := interest.ConversationID
	if convID != nil {
		connStatus = "connected"
	}

	var yajmanPhone *string = nil
	if interest.Status == domain.InterestStatusAccepted || interest.Status == domain.InterestStatusConnected || interest.Status == domain.InterestStatusCompleted || convID != nil {
		yajmanPhone = interest.YajmanPhoneNumber
	}

	resp := InterestDetailResponse{
		InterestResponse:  toInterestResponse(&interest.Interest),
		PanditFirstName:   interest.PanditFirstName,
		PanditLastName:    interest.PanditLastName,
		PanditPhotoURL:    interest.PanditPhotoURL,
		CeremonyType:      interest.CeremonyType,
		CeremonyLogoURL:   interest.CeremonyLogoURL,
		EventDate:         interest.EventDate,
		EventAddress:      interest.EventAddress,
		YajmanPhoneNumber: yajmanPhone,
	}
	resp.ConnectionStatus = connStatus
	resp.ConversationID = convID

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

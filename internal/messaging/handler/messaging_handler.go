package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/messaging/domain"
	messagingservice "github.com/medha/backend/internal/messaging/service"
	"github.com/medha/backend/internal/server/middleware"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// MessagingHandler handles messaging HTTP endpoints.
type MessagingHandler struct {
	msgSvc *messagingservice.MessagingService
	logger *slog.Logger
}

// NewMessagingHandler creates a new MessagingHandler.
func NewMessagingHandler(msgSvc *messagingservice.MessagingService, logger *slog.Logger) *MessagingHandler {
	return &MessagingHandler{
		msgSvc: msgSvc,
		logger: logger,
	}
}

// --- Request/Response DTOs ---

// CreateConversationRequest is the request body for POST /api/v2/conversation.
type CreateConversationRequest struct {
	Type           string      `json:"type"`
	ParticipantIDs []uuid.UUID `json:"participant_ids"`
	EventID        *uuid.UUID  `json:"event_id,omitempty"`
	MatchID        *uuid.UUID  `json:"match_id,omitempty"`
	Title          string      `json:"title,omitempty"`
}

// SendMessageRequest is the request body for POST /api/v2/conversation/{id}/message.
type SendMessageRequest struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"` // defaults to "text"
}

// ConversationResponse is the API response for a conversation.
type ConversationResponse struct {
	ID            uuid.UUID             `json:"id"`
	Type          string                `json:"type"`
	Title         string                `json:"title"`
	EventID       *uuid.UUID            `json:"event_id,omitempty"`
	MatchID       *uuid.UUID            `json:"match_id,omitempty"`
	IsActive      bool                  `json:"is_active"`
	LastMessageAt *int64                `json:"last_message_at,omitempty"`
	Participants  []ParticipantResponse `json:"participants"`
	UnreadCount   int                   `json:"unread_count"`
	CreatedAt     int64                 `json:"created_at"`
}

// ParticipantResponse is the API response for a conversation participant.
type ParticipantResponse struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	PhotoURL string    `json:"profile_photo_url,omitempty"`
}

// MessageResponse is the API response for a message.
type MessageResponse struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	SenderID       uuid.UUID       `json:"sender_id"`
	SenderName     string          `json:"sender_name"`
	ContentType    string          `json:"content_type"`
	Content        string          `json:"content"`
	Metadata       json.RawMessage `json:"metadata,omitempty" swaggertype:"object"`
	CreatedAt      int64           `json:"created_at"`
}

// --- Handlers ---

// CreateConversation handles POST /api/v2/conversation.
//
// @Summary      Create a conversation
// @Description  Starts a new conversation thread between participants. If a direct conversation already exists, returns the existing one.
// @Tags         messaging
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param body body CreateConversationRequest true "Conversation details"
// @Success      201 {object} response.DataResponse{data=ConversationResponse} "Created"
// @Success      200 {object} response.DataResponse{data=ConversationResponse} "Existing conversation returned"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/conversation [post]
func (h *MessagingHandler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req CreateConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request body.", r.URL.Path))
		return
	}

	convType := domain.ConversationType(req.Type)
	if !convType.IsValid() {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid conversation type. Must be pandit_pandit, pandit_yajman, or help.", r.URL.Path))
		return
	}

	if convType != domain.ConvHelp && len(req.ParticipantIDs) == 0 {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("At least one participant is required.", r.URL.Path))
		return
	}

	conv, err := h.msgSvc.CreateConversation(r.Context(), userID, messagingservice.CreateConversationParams{
		Type:           convType,
		ParticipantIDs: req.ParticipantIDs,
		EventID:        req.EventID,
		MatchID:        req.MatchID,
		Title:          req.Title,
	})
	if err != nil {
		h.logger.Error("create conversation failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to create conversation. Please try again.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusCreated, toConversationResponse(conv, 0))
}

// ListConversations handles GET /api/v2/conversation.
//
// @Summary      List my conversations
// @Description  Returns the user's conversation inbox sorted by most recent activity.
// @Tags         messaging
// @Produce      json
// @Security     BearerAuth
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]ConversationResponse} "Conversations"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/conversation [get]
func (h *MessagingHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 20)

	convs, nextCursor, err := h.msgSvc.ListConversations(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list conversations failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load conversations.", r.URL.Path))
		return
	}

	// Get unread counts
	unreadCounts, err := h.msgSvc.GetUnreadCounts(r.Context(), userID)
	if err != nil {
		h.logger.Warn("failed to get unread counts", "error", err)
		unreadCounts = map[uuid.UUID]int{}
	}

	responses := make([]ConversationResponse, len(convs))
	for i, c := range convs {
		responses[i] = toConversationResponse(&c, unreadCounts[c.ID])
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, responses, cursorPtr)
}

// GetConversation handles GET /api/v2/conversation/{id}.
//
// @Summary      Get conversation details
// @Description  Returns a conversation with its participants. Caller must be a participant.
// @Tags         messaging
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Conversation UUID"
// @Success      200 {object} response.DataResponse{data=ConversationResponse} "Conversation"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Not a participant"
// @Failure      404 {object} apierrors.ProblemDetail "Not found"
// @Router       /api/v2/conversation/{id} [get]
func (h *MessagingHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid conversation ID.", r.URL.Path))
		return
	}

	conv, err := h.msgSvc.GetConversation(r.Context(), userID, convID)
	if err != nil {
		if errors.Is(err, domain.ErrNotParticipant) {
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("You are not a participant in this conversation.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrConversationNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Conversation not found.", r.URL.Path))
			return
		}
		h.logger.Error("get conversation failed", "error", err)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load conversation.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, toConversationResponse(conv, 0))
}

// ListMessages handles GET /api/v2/conversation/{id}/message.
//
// @Summary      List messages in a conversation
// @Description  Returns paginated messages in a conversation, newest first. Caller must be a participant.
// @Tags         messaging
// @Produce      json
// @Security     BearerAuth
// @Param        id     path  string true  "Conversation UUID"
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 25)"
// @Success      200 {object} response.ListResponse{data=[]MessageResponse} "Messages"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Not a participant"
// @Router       /api/v2/conversation/{id}/message [get]
func (h *MessagingHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid conversation ID.", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseIntParam(r, "limit", 25)

	messages, nextCursor, err := h.msgSvc.ListMessages(r.Context(), userID, convID, cursor, limit)
	if err != nil {
		if errors.Is(err, domain.ErrNotParticipant) {
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("You are not a participant in this conversation.", r.URL.Path))
			return
		}
		h.logger.Error("list messages failed", "error", err, "conversation_id", convID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load messages.", r.URL.Path))
		return
	}

	responses := make([]MessageResponse, len(messages))
	for i, m := range messages {
		responses[i] = toMessageResponse(&m)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, responses, cursorPtr)
}

// SendMessage handles POST /api/v2/conversation/{id}/message.
//
// @Summary      Send a message (REST fallback)
// @Description  Sends a message to a conversation via REST. For real-time use WebSocket instead.
// @Tags         messaging
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path string              true "Conversation UUID"
// @Param body body SendMessageRequest   true "Message content"
// @Success      201 {object} response.DataResponse{data=MessageResponse} "Message sent"
// @Failure      400 {object} apierrors.ProblemDetail "Bad request"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      403 {object} apierrors.ProblemDetail "Not a participant"
// @Router       /api/v2/conversation/{id}/message [post]
func (h *MessagingHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid conversation ID.", r.URL.Path))
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Please provide a valid request body.", r.URL.Path))
		return
	}

	if req.Content == "" {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Message content is required.", r.URL.Path))
		return
	}

	contentType := domain.ContentText
	if req.ContentType != "" {
		ct := domain.MessageContentType(req.ContentType)
		if ct.IsValid() {
			contentType = ct
		}
	}

	msg, err := h.msgSvc.SendMessage(r.Context(), messagingservice.SendMessageParams{
		ConversationID: convID,
		SenderID:       userID,
		ContentType:    contentType,
		Content:        req.Content,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotParticipant) {
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("You are not a participant in this conversation.", r.URL.Path))
			return
		}
		h.logger.Error("send message failed", "error", err, "conversation_id", convID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to send message.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusCreated, toMessageResponse(msg))
}

// MarkRead handles PUT /api/v2/conversation/{id}/read.
//
// @Summary      Mark conversation as read
// @Description  Marks all messages in the conversation as read for the caller.
// @Tags         messaging
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Conversation UUID"
// @Success      200 {object} response.DataResponse{data=interface{}} "Marked as read"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/conversation/{id}/read [put]
func (h *MessagingHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	convID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid conversation ID.", r.URL.Path))
		return
	}

	if err := h.msgSvc.MarkRead(r.Context(), userID, convID); err != nil {
		if errors.Is(err, domain.ErrNotParticipant) {
			apierrors.WriteProblemDetail(w, apierrors.Unauthorized("You are not a participant in this conversation.", r.URL.Path))
			return
		}
		h.logger.Error("mark read failed", "error", err, "conversation_id", convID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to mark as read.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "conversation marked as read"})
}

// StartHelpConversation handles POST /api/v2/help.
//
// @Summary      Start a help conversation
// @Description  Creates a new help/support conversation for the authenticated user.
// @Tags         messaging
// @Produce      json
// @Security     BearerAuth
// @Success      201 {object} response.DataResponse{data=ConversationResponse} "Help conversation created"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/help [post]
func (h *MessagingHandler) StartHelpConversation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	conv, err := h.msgSvc.CreateConversation(r.Context(), userID, messagingservice.CreateConversationParams{
		Type:  domain.ConvHelp,
		Title: "Help & Support",
	})
	if err != nil {
		h.logger.Error("create help conversation failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to start help conversation.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusCreated, toConversationResponse(conv, 0))
}

// --- Helpers ---

func toConversationResponse(conv *domain.Conversation, unreadCount int) ConversationResponse {
	participants := make([]ParticipantResponse, len(conv.Participants))
	for i, p := range conv.Participants {
		name := p.FirstName
		if p.LastName != "" {
			name += " " + p.LastName
		}
		participants[i] = ParticipantResponse{
			UserID:   p.UserID,
			Name:     name,
			Role:     p.Role,
			PhotoURL: p.PhotoURL,
		}
	}

	return ConversationResponse{
		ID:            conv.ID,
		Type:          string(conv.Type),
		Title:         conv.Title,
		EventID:       conv.EventID,
		MatchID:       conv.MatchID,
		IsActive:      conv.IsActive,
		LastMessageAt: conv.LastMessageAt,
		Participants:  participants,
		UnreadCount:   unreadCount,
		CreatedAt:     conv.CreatedAt,
	}
}

func toMessageResponse(msg *domain.Message) MessageResponse {
	return MessageResponse{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		SenderName:     msg.SenderName,
		ContentType:    string(msg.ContentType),
		Content:        msg.Content,
		Metadata:       msg.Metadata,
		CreatedAt:      msg.CreatedAt,
	}
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


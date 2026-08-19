package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/server/middleware"
	"github.com/medha/backend/internal/story/domain"
	"github.com/medha/backend/internal/story/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// Handler handles public-facing story feed HTTP endpoints.
type Handler struct {
	svc    *service.StoryService
	logger *slog.Logger
}

// NewHandler creates a new story Handler.
func NewHandler(svc *service.StoryService, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// StoryResponse is the public API representation of an official story.
// The author is always the publisher — never a human user's identity.
type StoryResponse struct {
	ID           uuid.UUID      `json:"id"`
	Title        string         `json:"title,omitempty"`
	Caption      string         `json:"caption,omitempty"`
	MediaType    string         `json:"media_type"`
	MediaURL     string         `json:"media_url"`
	ThumbnailURL string         `json:"thumbnail_url,omitempty"`
	ActionURL    string         `json:"action_url,omitempty"`
	ActionLabel  string         `json:"action_label,omitempty"`
	Priority     int            `json:"priority"`
	PublishedAt  *int64         `json:"published_at,omitempty"`
	ExpiresAt    int64          `json:"expires_at"`
	Author       StoryAuthorDTO `json:"author"`
}

// StoryAuthorDTO is the explicit system-author representation.
type StoryAuthorDTO struct {
	Type        string    `json:"type"`
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	DisplayName string    `json:"displayName"`
	Username    string    `json:"username"`
	AvatarURL   string    `json:"avatarUrl,omitempty"`
	Verified    bool      `json:"verified"`
}

func toStoryResponse(sp *domain.StoryWithPublisher) StoryResponse {
	return StoryResponse{
		ID:           sp.ID,
		Title:        sp.Title,
		Caption:      sp.Caption,
		MediaType:    sp.MediaType,
		MediaURL:     sp.MediaURL,
		ThumbnailURL: sp.ThumbnailURL,
		ActionURL:    sp.ActionURL,
		ActionLabel:  sp.ActionLabel,
		Priority:     sp.Priority,
		PublishedAt:  sp.PublishedAt,
		ExpiresAt:    sp.ExpiresAt,
		Author: StoryAuthorDTO{
			Type:        "system",
			ID:          sp.PublisherID,
			Key:         sp.PublisherKey,
			DisplayName: sp.PublisherDisplayName,
			Username:    sp.PublisherUsername,
			AvatarURL:   sp.PublisherAvatarURL,
			Verified:    sp.PublisherVerified,
		},
	}
}

// ListFeed handles GET /api/v2/stories.
//
// @Summary      List active official stories for the viewer
// @Description  Returns active, eligible official stories for the authenticated viewer, deterministically ordered.
// @Tags         stories
// @Produce      json
// @Security     BearerAuth
// @Param        cursor query string false "Pagination cursor"
// @Param        limit  query int    false "Page size (default 20)"
// @Success      200 {object} response.ListResponse{data=[]StoryResponse} "Stories"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/stories [get]
func (h *Handler) ListFeed(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	stories, nextCursor, err := h.svc.GetFeed(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list story feed failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load stories. Please try again later.", r.URL.Path))
		return
	}

	out := make([]StoryResponse, len(stories))
	for i, sp := range stories {
		out[i] = toStoryResponse(sp)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, out, cursorPtr)
}

// RecordView handles POST /api/v2/stories/{id}/view.
//
// @Summary      Record a story view
// @Description  Idempotently records that the authenticated viewer has seen this story.
// @Tags         stories
// @Produce      json
// @Security     BearerAuth
// @Param        id path string true "Story UUID"
// @Success      204 "View recorded"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Story not found or not eligible"
// @Router       /api/v2/stories/{id}/view [post]
func (h *Handler) RecordView(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	storyID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid story ID format.", r.URL.Path))
		return
	}

	if err := h.svc.RecordView(r.Context(), userID, storyID); err != nil {
		if err == domain.ErrStoryNotFound {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Story not found.", r.URL.Path))
			return
		}
		h.logger.Error("record story view failed", "error", err, "user_id", userID, "story_id", storyID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to record view.", r.URL.Path))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

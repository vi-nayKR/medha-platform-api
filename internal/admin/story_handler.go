package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/story/domain"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// AdminStoryRequest is the request body for creating/updating an official
// story. Deliberately excludes publisher_id, status, and audit fields —
// those are never taken from client input (no mass assignment).
type AdminStoryRequest struct {
	Title         string         `json:"title"`
	Caption       string         `json:"caption"`
	MediaType     string         `json:"media_type"`
	MediaURL      string         `json:"media_url"`
	ThumbnailURL  string         `json:"thumbnail_url"`
	ActionURL     string         `json:"action_url"`
	ActionLabel   string         `json:"action_label"`
	AudienceScope string         `json:"audience_scope"`
	AudienceRules map[string]any `json:"audience_rules"`
	Priority      int            `json:"priority"`
	StartsAt      int64          `json:"starts_at"`
	ExpiresAt     int64          `json:"expires_at"`
}

type AdminStoryResponse struct {
	ID             uuid.UUID `json:"id"`
	PublisherKey   string    `json:"publisher_key"`
	Title          string    `json:"title,omitempty"`
	Caption        string    `json:"caption,omitempty"`
	MediaType      string    `json:"media_type"`
	MediaURL       string    `json:"media_url"`
	ThumbnailURL   string    `json:"thumbnail_url,omitempty"`
	ActionURL      string    `json:"action_url,omitempty"`
	ActionLabel    string    `json:"action_label,omitempty"`
	AudienceScope  string    `json:"audience_scope"`
	Status         string    `json:"status"`
	Priority       int       `json:"priority"`
	StartsAt       int64     `json:"starts_at"`
	ExpiresAt      int64     `json:"expires_at"`
	CreatedByAdmin string    `json:"created_by_admin,omitempty"`
	CreatedAt      int64     `json:"created_at"`
	UpdatedAt      int64     `json:"updated_at"`
	PublishedAt    *int64    `json:"published_at,omitempty"`
}

func toAdminStoryResponse(sp *domain.StoryWithPublisher) AdminStoryResponse {
	return AdminStoryResponse{
		ID:             sp.ID,
		PublisherKey:   sp.PublisherKey,
		Title:          sp.Title,
		Caption:        sp.Caption,
		MediaType:      sp.MediaType,
		MediaURL:       sp.MediaURL,
		ThumbnailURL:   sp.ThumbnailURL,
		ActionURL:      sp.ActionURL,
		ActionLabel:    sp.ActionLabel,
		AudienceScope:  string(sp.AudienceScope),
		Status:         string(sp.Status),
		Priority:       sp.Priority,
		StartsAt:       sp.StartsAt,
		ExpiresAt:      sp.ExpiresAt,
		CreatedByAdmin: sp.CreatedByAdmin,
		CreatedAt:      sp.CreatedAt,
		UpdatedAt:      sp.UpdatedAt,
		PublishedAt:    sp.PublishedAt,
	}
}

func (h *Handler) adminUsernameFromContext(ctx context.Context) string {
	username, _ := ctx.Value(adminUsernameKey).(string)
	return username
}

func toCreateStoryParams(req AdminStoryRequest) (domain.CreateStoryParams, error) {
	rulesJSON, err := marshalAudienceRules(req.AudienceRules)
	if err != nil {
		return domain.CreateStoryParams{}, err
	}
	rules, err := domain.ParseAudienceRules(rulesJSON)
	if err != nil {
		return domain.CreateStoryParams{}, err
	}
	return domain.CreateStoryParams{
		Title:         req.Title,
		Caption:       req.Caption,
		MediaType:     req.MediaType,
		MediaURL:      req.MediaURL,
		ThumbnailURL:  req.ThumbnailURL,
		ActionURL:     req.ActionURL,
		ActionLabel:   req.ActionLabel,
		AudienceScope: domain.AudienceScope(req.AudienceScope),
		AudienceRules: rules,
		Priority:      req.Priority,
		StartsAt:      req.StartsAt,
		ExpiresAt:     req.ExpiresAt,
	}, nil
}

func marshalAudienceRules(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func (h *Handler) writeStoryServiceError(w http.ResponseWriter, r *http.Request, action string, err error) {
	switch {
	case errors.Is(err, domain.ErrStoryNotFound):
		apierrors.WriteProblemDetail(w, apierrors.NotFound("Story not found.", r.URL.Path))
	case errors.Is(err, domain.ErrInvalidStoryWindow):
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("expires_at must be after starts_at.", r.URL.Path))
	case errors.Is(err, domain.ErrInvalidAudienceRule):
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid audience_scope or audience_rules.", r.URL.Path))
	case errors.Is(err, domain.ErrInvalidMediaType):
		apierrors.WriteProblemDetail(w, apierrors.BadRequest(err.Error(), r.URL.Path))
	default:
		h.internal(w, r, action, err)
	}
}

// CreateStory handles POST /api/v2/admin/stories.
// @Summary Create an official story (draft)
// @Tags admin-stories
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body AdminStoryRequest true "Story data"
// @Success 201 {object} response.DataResponse{data=AdminStoryResponse} "Story created"
// @Router /api/v2/admin/stories [post]
func (h *Handler) CreateStory(w http.ResponseWriter, r *http.Request) {
	if h.storyService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Story service is not initialized", r.URL.Path))
		return
	}
	var req AdminStoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	params, err := toCreateStoryParams(req)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid audience_rules.", r.URL.Path))
		return
	}

	story, err := h.storyService.CreateStory(r.Context(), h.adminUsernameFromContext(r.Context()), params)
	if err != nil {
		h.writeStoryServiceError(w, r, "create story", err)
		return
	}
	full, err := h.storyService.GetByID(r.Context(), story.ID)
	if err != nil {
		h.writeStoryServiceError(w, r, "get created story", err)
		return
	}
	response.WriteData(w, http.StatusCreated, toAdminStoryResponse(full))
}

// ListStories handles GET /api/v2/admin/stories.
// @Summary List all official stories
// @Tags admin-stories
// @Security AdminToken
// @Produce json
// @Success 200 {object} response.ListResponse{data=[]AdminStoryResponse} "Stories"
// @Router /api/v2/admin/stories [get]
func (h *Handler) ListStories(w http.ResponseWriter, r *http.Request) {
	if h.storyService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Story service is not initialized", r.URL.Path))
		return
	}
	cursor := r.URL.Query().Get("cursor")
	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	stories, nextCursor, err := h.storyService.ListForAdmin(r.Context(), cursor, limit)
	if err != nil {
		h.writeStoryServiceError(w, r, "list stories", err)
		return
	}
	out := make([]AdminStoryResponse, len(stories))
	for i, sp := range stories {
		out[i] = toAdminStoryResponse(sp)
	}
	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, out, cursorPtr)
}

// GetStory handles GET /api/v2/admin/stories/{id}.
// @Summary Get an official story
// @Tags admin-stories
// @Security AdminToken
// @Produce json
// @Param id path string true "Story UUID"
// @Success 200 {object} response.DataResponse{data=AdminStoryResponse} "Story"
// @Router /api/v2/admin/stories/{id} [get]
func (h *Handler) GetStory(w http.ResponseWriter, r *http.Request) {
	if h.storyService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Story service is not initialized", r.URL.Path))
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	sp, err := h.storyService.GetByID(r.Context(), id)
	if err != nil {
		h.writeStoryServiceError(w, r, "get story", err)
		return
	}
	response.WriteData(w, http.StatusOK, toAdminStoryResponse(sp))
}

// UpdateStory handles PUT /api/v2/admin/stories/{id}.
// @Summary Update an official story's content/targeting/window
// @Tags admin-stories
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Story UUID"
// @Param body body AdminStoryRequest true "Story data"
// @Success 200 {object} response.DataResponse{data=AdminStoryResponse} "Story updated"
// @Router /api/v2/admin/stories/{id} [put]
func (h *Handler) UpdateStory(w http.ResponseWriter, r *http.Request) {
	if h.storyService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Story service is not initialized", r.URL.Path))
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	var req AdminStoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	params, err := toCreateStoryParams(req)
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid audience_rules.", r.URL.Path))
		return
	}
	sp, err := h.storyService.UpdateStory(r.Context(), id, params)
	if err != nil {
		h.writeStoryServiceError(w, r, "update story", err)
		return
	}
	response.WriteData(w, http.StatusOK, toAdminStoryResponse(sp))
}

// PublishStory handles POST /api/v2/admin/stories/{id}/publish.
// @Summary Publish (activate) a story
// @Tags admin-stories
// @Security AdminToken
// @Produce json
// @Param id path string true "Story UUID"
// @Success 204 "Story published"
// @Router /api/v2/admin/stories/{id}/publish [post]
func (h *Handler) PublishStory(w http.ResponseWriter, r *http.Request) {
	h.storyTransition(w, r, h.storyService.Publish)
}

// PauseStory handles POST /api/v2/admin/stories/{id}/pause.
// @Summary Pause a story
// @Tags admin-stories
// @Security AdminToken
// @Produce json
// @Param id path string true "Story UUID"
// @Success 204 "Story paused"
// @Router /api/v2/admin/stories/{id}/pause [post]
func (h *Handler) PauseStory(w http.ResponseWriter, r *http.Request) {
	h.storyTransition(w, r, h.storyService.Pause)
}

// ArchiveStory handles POST /api/v2/admin/stories/{id}/archive.
// @Summary Archive a story
// @Tags admin-stories
// @Security AdminToken
// @Produce json
// @Param id path string true "Story UUID"
// @Success 204 "Story archived"
// @Router /api/v2/admin/stories/{id}/archive [post]
func (h *Handler) ArchiveStory(w http.ResponseWriter, r *http.Request) {
	h.storyTransition(w, r, h.storyService.Archive)
}

func (h *Handler) storyTransition(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, adminUsername string, id uuid.UUID) error) {
	if h.storyService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Story service is not initialized", r.URL.Path))
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := fn(r.Context(), h.adminUsernameFromContext(r.Context()), id); err != nil {
		h.writeStoryServiceError(w, r, "change story status", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteStory handles DELETE /api/v2/admin/stories/{id}.
// @Summary Delete (soft-delete) a story
// @Tags admin-stories
// @Security AdminToken
// @Produce json
// @Param id path string true "Story UUID"
// @Success 204 "Story deleted"
// @Router /api/v2/admin/stories/{id} [delete]
func (h *Handler) DeleteStory(w http.ResponseWriter, r *http.Request) {
	if h.storyService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Story service is not initialized", r.URL.Path))
		return
	}
	id, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}
	if err := h.storyService.Delete(r.Context(), h.adminUsernameFromContext(r.Context()), id); err != nil {
		h.writeStoryServiceError(w, r, "delete story", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

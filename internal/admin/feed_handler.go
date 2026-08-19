package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/infra/storage"
	"github.com/medha/backend/internal/social/domain"
	socialservice "github.com/medha/backend/internal/social/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// medhaAppSystemUserID looks up the Medha App official system user by its
// stable username — never a hardcoded UUID, since that identity is a
// per-environment generated row (see migration 00047). Cached for the life
// of the process: the row is never re-created while the server is running.
func (h *Handler) medhaAppSystemUserID(ctx context.Context) (uuid.UUID, error) {
	h.medhaSystemUserOnce.Do(func() {
		err := h.pool.QueryRow(ctx, `SELECT id FROM users WHERE username = 'medhaapp' AND auth_provider = 'system'`).Scan(&h.medhaSystemUserID)
		if err != nil {
			h.medhaSystemUserErr = fmt.Errorf("lookup medha app system user: %w", err)
		}
	})
	return h.medhaSystemUserID, h.medhaSystemUserErr
}

type MedhaFeedPostRequest struct {
	Content   string   `json:"content"`
	ImageURLs []string `json:"image_urls"`
	Tags      []string `json:"tags"`
}

type MedhaFeedPostResponse struct {
	ID              uuid.UUID `json:"id"`
	AuthorID        uuid.UUID `json:"author_id"`
	AuthorFirstName string    `json:"author_first_name"`
	AuthorLastName  string    `json:"author_last_name"`
	AuthorPhotoURL  *string   `json:"author_photo_url"`
	AuthorRole      string    `json:"author_role"`
	AuthorUsername  string    `json:"author_username"`
	Content         string    `json:"content"`
	ImageURLs       []string  `json:"image_urls"`
	Tags            []string  `json:"tags"`
	LikeCount       int       `json:"like_count"`
	CommentCount    int       `json:"comment_count"`
	CreatedAt       int64     `json:"created_at"`
	UpdatedAt       int64     `json:"updated_at"`
}

func (h *Handler) toMedhaFeedPostResponse(p *domain.PostWithAuthor) MedhaFeedPostResponse {
	imageURLs := make([]string, len(p.ImageURLs))
	for i, u := range p.ImageURLs {
		if h.s3Client != nil {
			imageURLs[i] = h.s3Client.ResolveURLForBucket(storage.BucketFeeds, u)
		} else {
			imageURLs[i] = u
		}
	}

	var photoURL *string
	if p.AuthorPhotoURL != nil && *p.AuthorPhotoURL != "" {
		var resolved string
		if h.s3Client != nil {
			resolved = h.s3Client.ResolveURLForBucket(storage.BucketProfiles, *p.AuthorPhotoURL)
		} else {
			resolved = *p.AuthorPhotoURL
		}
		photoURL = &resolved
	}

	role := p.AuthorRole
	if p.AuthorIsOfficial {
		role = "admin"
	}

	return MedhaFeedPostResponse{
		ID:              p.ID,
		AuthorID:        p.AuthorID,
		AuthorFirstName: p.AuthorFirstName,
		AuthorLastName:  p.AuthorLastName,
		AuthorPhotoURL:  photoURL,
		AuthorRole:      role,
		AuthorUsername:  p.AuthorUsername,
		Content:         p.Content,
		ImageURLs:       imageURLs,
		Tags:            p.Tags,
		LikeCount:       p.LikeCount,
		CommentCount:    p.CommentCount,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// CreateMedhaFeedPost handles POST /api/v2/admin/feed.
// @Summary Create a Medha App official feed post
// @Description Creates a new post in the feed authored by the verified official Medha App system user.
// @Tags admin-feed
// @Security AdminToken
// @Accept json
// @Produce json
// @Param body body MedhaFeedPostRequest true "Post content & media"
// @Success 201 {object} response.DataResponse{data=MedhaFeedPostResponse} "Post created successfully"
// @Router /api/v2/admin/feed [post]
func (h *Handler) CreateMedhaFeedPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.postService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Social service is not initialized", r.URL.Path))
		return
	}

	systemUserID, err := h.medhaAppSystemUserID(ctx)
	if err != nil {
		h.logger.Error("failed to resolve Medha app system user", "error", err)
		h.internal(w, r, "resolve Medha app system user", err)
		return
	}

	var req MedhaFeedPostRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	post, err := h.postService.CreatePost(ctx, socialservice.CreatePostParams{
		AuthorID:  systemUserID,
		Content:   req.Content,
		ImageURLs: req.ImageURLs,
		Tags:      req.Tags,
	})
	if err != nil {
		if errors.Is(err, domain.ErrEmptyPost) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("A post must have either content text or at least one image/video URL.", r.URL.Path))
			return
		}
		h.logger.Error("failed to create Medha feed post", "error", err)
		h.internal(w, r, "create Medha feed post", err)
		return
	}

	fullPost, err := h.postService.GetPost(ctx, post.ID, systemUserID)
	if err != nil {
		h.logger.Error("failed to retrieve created Medha feed post", "error", err)
		h.internal(w, r, "get created Medha feed post", err)
		return
	}

	response.WriteData(w, http.StatusCreated, h.toMedhaFeedPostResponse(fullPost))
}

// ListMedhaFeedPosts handles GET /api/v2/admin/feed.
// @Summary List official Medha App feed posts
// @Description Returns official posts authored by Medha App with cursor pagination.
// @Tags admin-feed
// @Security AdminToken
// @Produce json
// @Param cursor query string false "Pagination cursor"
// @Param limit query int false "Paging limit (default 20)"
// @Success 200 {object} response.ListResponse{data=[]MedhaFeedPostResponse} "List of official posts"
// @Router /api/v2/admin/feed [get]
func (h *Handler) ListMedhaFeedPosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.postService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Social service is not initialized", r.URL.Path))
		return
	}

	systemUserID, err := h.medhaAppSystemUserID(ctx)
	if err != nil {
		h.logger.Error("failed to resolve Medha app system user", "error", err)
		h.internal(w, r, "resolve Medha app system user", err)
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if parsed, err := strconv.Atoi(lStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	posts, nextCursor, err := h.postService.ListByAuthor(ctx, systemUserID, systemUserID, cursor, limit)
	if err != nil {
		h.logger.Error("failed to list Medha feed posts", "error", err)
		h.internal(w, r, "list Medha feed posts", err)
		return
	}

	out := make([]MedhaFeedPostResponse, len(posts))
	for i, p := range posts {
		out[i] = h.toMedhaFeedPostResponse(p)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}

	response.WriteList(w, http.StatusOK, out, cursorPtr)
}

// GetMedhaFeedPost handles GET /api/v2/admin/feed/{id}.
// @Summary Get official Medha App feed post
// @Description Retrieves details of a specific official post by UUID.
// @Tags admin-feed
// @Security AdminToken
// @Produce json
// @Param id path string true "Post UUID"
// @Success 200 {object} response.DataResponse{data=MedhaFeedPostResponse} "Post details"
// @Router /api/v2/admin/feed/{id} [get]
func (h *Handler) GetMedhaFeedPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.postService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Social service is not initialized", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid post ID format.", r.URL.Path))
		return
	}

	systemUserID, err := h.medhaAppSystemUserID(ctx)
	if err != nil {
		h.logger.Error("failed to resolve Medha app system user", "error", err)
		h.internal(w, r, "resolve Medha app system user", err)
		return
	}

	post, err := h.postService.GetPost(ctx, postID, systemUserID)
	if err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Official feed post not found.", r.URL.Path))
			return
		}
		h.logger.Error("failed to get Medha feed post", "error", err, "post_id", postID)
		h.internal(w, r, "get Medha feed post", err)
		return
	}

	response.WriteData(w, http.StatusOK, h.toMedhaFeedPostResponse(post))
}

// UpdateMedhaFeedPost handles PUT /api/v2/admin/feed/{id}.
// @Summary Update official Medha App feed post
// @Description Modifies content, image, or tags of an existing official feed post.
// @Tags admin-feed
// @Security AdminToken
// @Accept json
// @Produce json
// @Param id path string true "Post UUID"
// @Param body body MedhaFeedPostRequest true "Updated post content"
// @Success 200 {object} response.DataResponse{data=MedhaFeedPostResponse} "Post updated successfully"
// @Router /api/v2/admin/feed/{id} [put]
func (h *Handler) UpdateMedhaFeedPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.postService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Social service is not initialized", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid post ID format.", r.URL.Path))
		return
	}

	var req MedhaFeedPostRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	systemUserID, err := h.medhaAppSystemUserID(ctx)
	if err != nil {
		h.logger.Error("failed to resolve Medha app system user", "error", err)
		h.internal(w, r, "resolve Medha app system user", err)
		return
	}

	post, err := h.postService.UpdatePost(ctx, postID, systemUserID, req.Content, req.ImageURLs, req.Tags)
	if err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Official feed post not found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrEmptyPost) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("A post must have either content text or at least one image/video URL.", r.URL.Path))
			return
		}
		h.logger.Error("failed to update Medha feed post", "error", err, "post_id", postID)
		h.internal(w, r, "update Medha feed post", err)
		return
	}

	fullPost, err := h.postService.GetPost(ctx, post.ID, systemUserID)
	if err != nil {
		h.logger.Error("failed to retrieve updated Medha feed post", "error", err)
		h.internal(w, r, "get updated Medha feed post", err)
		return
	}

	response.WriteData(w, http.StatusOK, h.toMedhaFeedPostResponse(fullPost))
}

// DeleteMedhaFeedPost handles DELETE /api/v2/admin/feed/{id}.
// @Summary Delete official Medha App feed post
// @Description Soft-deletes an official feed post.
// @Tags admin-feed
// @Security AdminToken
// @Produce json
// @Param id path string true "Post UUID"
// @Success 204 "Post deleted successfully"
// @Router /api/v2/admin/feed/{id} [delete]
func (h *Handler) DeleteMedhaFeedPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.postService == nil {
		apierrors.WriteProblemDetail(w, apierrors.ServiceUnavailable("Social service is not initialized", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid post ID format.", r.URL.Path))
		return
	}

	systemUserID, err := h.medhaAppSystemUserID(ctx)
	if err != nil {
		h.logger.Error("failed to resolve Medha app system user", "error", err)
		h.internal(w, r, "resolve Medha app system user", err)
		return
	}

	if err := h.postService.DeletePost(ctx, postID, systemUserID); err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("Official feed post not found.", r.URL.Path))
			return
		}
		h.logger.Error("failed to delete Medha feed post", "error", err, "post_id", postID)
		h.internal(w, r, "delete Medha feed post", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

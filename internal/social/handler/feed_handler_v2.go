package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/medha/backend/internal/infra/storage"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/server/middleware"
	"github.com/medha/backend/internal/social/domain"
	socialservice "github.com/medha/backend/internal/social/service"
	apierrors "github.com/medha/backend/pkg/errors"
	"github.com/medha/backend/pkg/response"
)

// FeedHandlerV2 handles V2 social feed HTTP endpoints.
// It reuses the existing PostService but is registered under /api/v2/feed.
type FeedHandlerV2 struct {
	postService *socialservice.PostService
	s3Client    *storage.S3Client // may be nil in dev
	logger      *slog.Logger
}

// NewFeedHandlerV2 creates a new FeedHandlerV2.
func NewFeedHandlerV2(postService *socialservice.PostService, s3Client *storage.S3Client, logger *slog.Logger) *FeedHandlerV2 {
	return &FeedHandlerV2{
		postService: postService,
		s3Client:    s3Client,
		logger:      logger,
	}
}

// --- Response DTOs ---

// FeedPostResponse represents a post in V2 feed API responses.
type FeedPostResponse struct {
	ID               uuid.UUID `json:"id"`
	AuthorID         uuid.UUID `json:"author_id"`
	AuthorFirstName  string    `json:"author_first_name"`
	AuthorLastName   string    `json:"author_last_name"`
	AuthorPhotoURL   *string   `json:"author_photo_url"`
	AuthorRole       string    `json:"author_role"`
	AuthorUsername   string    `json:"author_username"`
	Content          string    `json:"content"`
	ImageURLs        []string  `json:"image_urls"`
	Tags             []string  `json:"tags"`
	LikeCount        int       `json:"like_count"`
	LikedByUser      bool      `json:"liked_by_user"`
	AuthorIsOfficial bool      `json:"author_is_official"`
	CreatedAt        string    `json:"created_at"`
}

// --- Handlers ---

// ListFeed handles GET /api/v2/feed.
//
// @Summary      List community feed
// @Description  Returns paginated posts from the community feed, newest first. Supports cursor-based pagination for use with Paging 3. Pass the `next_cursor` from the previous response as `cursor` for the next page.
// @Tags         feed-v2
// @Produce      json
// @Security     BearerAuth
// @Param        cursor  query  string  false  "Pagination cursor from previous response"
// @Param        limit   query  int     false  "Number of posts per page (1-50, default 10)"
// @Success      200 {object} response.ListResponse{data=[]FeedPostResponse} "Paginated feed"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/feed [get]
func (h *FeedHandlerV2) ListFeed(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseFeedIntParam(r, "limit", 10)
	if limit > 50 {
		limit = 50
	}

	posts, nextCursor, err := h.postService.ListFeed(r.Context(), userID, cursor, limit)
	if err != nil {
		h.logger.Error("list feed v2 failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load the feed. Please try again later.", r.URL.Path))
		return
	}

	out := make([]FeedPostResponse, len(posts))
	for i, p := range posts {
		out[i] = h.toFeedPostResponse(p)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, out, cursorPtr)
}

// GetPost handles GET /api/v2/feed/{id}.
//
// @Summary      Get feed post
// @Description  Returns a single post by its UUID, including like state for the authenticated user.
// @Tags         feed-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Post UUID"
// @Success      200 {object} response.DataResponse{data=FeedPostResponse} "Post found"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid post ID"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Post not found"
// @Router       /api/v2/feed/{id} [get]
func (h *FeedHandlerV2) GetPost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The post ID provided is not valid.", r.URL.Path))
		return
	}

	post, err := h.postService.GetPost(r.Context(), postID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This post could not be found.", r.URL.Path))
			return
		}
		h.logger.Error("get post v2 failed", "error", err, "post_id", postID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load the post. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, h.toFeedPostResponse(post))
}

// LikePost handles POST /api/v2/feed/{id}/like.
//
// @Summary      Like a post
// @Description  Adds a like from the authenticated user to the specified post.
// @Tags         feed-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Post UUID"
// @Success      200 {object} response.DataResponse{data=map[string]string} "Post liked"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid post ID"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Failure      404 {object} apierrors.ProblemDetail "Post not found"
// @Failure      409 {object} apierrors.ProblemDetail "Already liked"
// @Router       /api/v2/feed/{id}/like [post]
func (h *FeedHandlerV2) LikePost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The post ID provided is not valid.", r.URL.Path))
		return
	}

	if err := h.postService.LikePost(r.Context(), postID, userID); err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This post could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrAlreadyLiked) {
			apierrors.WriteProblemDetail(w, apierrors.Conflict("You have already liked this post.", r.URL.Path))
			return
		}
		h.logger.Error("like post v2 failed", "error", err, "post_id", postID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to like the post. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "post liked"})
}

// UnlikePost handles DELETE /api/v2/feed/{id}/like.
//
// @Summary      Unlike a post
// @Description  Removes the authenticated user's like from the specified post.
// @Tags         feed-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Post UUID"
// @Success      200 {object} response.DataResponse{data=map[string]string} "Post unliked"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid post ID or not liked"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/feed/{id}/like [delete]
func (h *FeedHandlerV2) UnlikePost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The post ID provided is not valid.", r.URL.Path))
		return
	}

	if err := h.postService.UnlikePost(r.Context(), postID, userID); err != nil {
		if errors.Is(err, domain.ErrNotLiked) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("You haven't liked this post yet.", r.URL.Path))
			return
		}
		h.logger.Error("unlike post v2 failed", "error", err, "post_id", postID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to unlike the post. Please try again later.", r.URL.Path))
		return
	}

	response.WriteData(w, http.StatusOK, map[string]string{"message": "post unliked"})
}

// --- Helpers ---

func (h *FeedHandlerV2) toFeedPostResponse(p *domain.PostWithAuthor) FeedPostResponse {
	imageURLs := make([]string, len(p.ImageURLs))
	for i, u := range p.ImageURLs {
		if h.s3Client != nil {
			imageURLs[i] = h.s3Client.ResolveURLForBucket(storage.BucketFeeds, u)
		} else {
			imageURLs[i] = u
		}
	}

	resp := FeedPostResponse{
		ID:               p.ID,
		AuthorID:         p.AuthorID,
		AuthorFirstName:  p.AuthorFirstName,
		AuthorLastName:   p.AuthorLastName,
		AuthorRole:       p.AuthorRole,
		AuthorUsername:   p.AuthorUsername,
		Content:          p.Content,
		ImageURLs:        imageURLs,
		Tags:             p.Tags,
		LikeCount:        p.LikeCount,
		LikedByUser:      p.LikedByUser,
		AuthorIsOfficial: p.AuthorIsOfficial,
		CreatedAt:        epoch.ToTime(p.CreatedAt).Format(time.RFC3339),
	}

	if p.AuthorPhotoURL != nil && *p.AuthorPhotoURL != "" {
		var photoURL string
		if h.s3Client != nil {
			photoURL = h.s3Client.ResolveURLForBucket(storage.BucketProfiles, *p.AuthorPhotoURL)
		} else {
			photoURL = *p.AuthorPhotoURL
		}
		resp.AuthorPhotoURL = &photoURL
	}

	// Official posts are authored by the Medha App system account, which is
	// stored with role 'yajman' for matching purposes — clients should see it
	// as "admin", not that internal detail.
	if p.AuthorIsOfficial {
		resp.AuthorRole = "admin"
	}

	return resp
}

func parseFeedIntParam(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

// CreatePost handles POST /api/v2/feed.
//
// @Summary      Create feed post
// @Description  Creates a new post in the community feed. At least one of `content` or `image_urls` must be provided.
// @Tags         feed-v2
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      object{content=string,image_urls=[]string,tags=[]string}  true  "Post creation payload"
// @Success      201   {object}  response.DataResponse{data=FeedPostResponse} "Post created successfully"
// @Failure      400   {object}  apierrors.ProblemDetail "Empty post or invalid body"
// @Failure      401   {object}  apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/feed [post]
func (h *FeedHandlerV2) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	var req struct {
		Content   string   `json:"content"`
		ImageURLs []string `json:"image_urls"`
		Tags      []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("Invalid request body.", r.URL.Path))
		return
	}

	post, err := h.postService.CreatePost(r.Context(), socialservice.CreatePostParams{
		AuthorID:  userID,
		Content:   req.Content,
		ImageURLs: req.ImageURLs,
		Tags:      req.Tags,
	})
	if err != nil {
		if errors.Is(err, domain.ErrEmptyPost) {
			apierrors.WriteProblemDetail(w, apierrors.BadRequest("A post must have either content text or at least one image URL.", r.URL.Path))
			return
		}
		h.logger.Error("create post v2 failed", "error", err, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to create post. Please try again later.", r.URL.Path))
		return
	}

	// Fetch the full post with author details for response consistency
	fullPost, err := h.postService.GetPost(r.Context(), post.ID, userID)
	if err != nil {
		// Fallback: if GetPost fails for some reason, just write StatusCreated with ID
		response.WriteData(w, http.StatusCreated, map[string]string{"id": post.ID.String()})
		return
	}

	response.WriteData(w, http.StatusCreated, h.toFeedPostResponse(fullPost))
}

// DeletePost handles DELETE /api/v2/feed/{id}.
//
// @Summary      Delete feed post
// @Description  Soft-deletes a community feed post if the authenticated user is the author.
// @Tags         feed-v2
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Post UUID"
// @Success      204   "Post deleted successfully"
// @Failure      400   {object}  apierrors.ProblemDetail "Invalid post ID"
// @Failure      401   {object}  apierrors.ProblemDetail "Unauthorized"
// @Failure      403   {object}  apierrors.ProblemDetail "Not authorized to delete this post"
// @Failure      404   {object}  apierrors.ProblemDetail "Post not found"
// @Router       /api/v2/feed/{id} [delete]
func (h *FeedHandlerV2) DeletePost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	postID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The post ID provided is not valid.", r.URL.Path))
		return
	}

	if err := h.postService.DeletePost(r.Context(), postID, userID); err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			apierrors.WriteProblemDetail(w, apierrors.NotFound("This post could not be found.", r.URL.Path))
			return
		}
		if errors.Is(err, domain.ErrPostNotOwned) {
			apierrors.WriteProblemDetail(w, apierrors.Forbidden("You are not authorized to delete this post.", r.URL.Path))
			return
		}
		h.logger.Error("delete post v2 failed", "error", err, "post_id", postID, "user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to delete post. Please try again later.", r.URL.Path))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListUserPosts handles GET /api/v2/feed/user/{authorId}.
//
// @Summary      List posts by user
// @Description  Returns paginated posts authored by a specific user. Supports cursor-based pagination.
// @Tags         feed-v2
// @Produce      json
// @Security     BearerAuth
// @Param        authorId  path   string  true   "Author UUID"
// @Param        cursor    query  string  false  "Pagination cursor from previous response"
// @Param        limit     query  int     false  "Number of posts per page (default 10)"
// @Success      200 {object} response.ListResponse{data=[]FeedPostResponse} "Paginated author posts"
// @Failure      400 {object} apierrors.ProblemDetail "Invalid author ID"
// @Failure      401 {object} apierrors.ProblemDetail "Unauthorized"
// @Router       /api/v2/feed/user/{authorId} [get]
func (h *FeedHandlerV2) ListUserPosts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.UserIDFromContext(r.Context())
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.Unauthorized("authentication required", r.URL.Path))
		return
	}

	authorID, err := uuid.Parse(chi.URLParam(r, "authorId"))
	if err != nil {
		apierrors.WriteProblemDetail(w, apierrors.BadRequest("The author ID provided is not valid.", r.URL.Path))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseFeedIntParam(r, "limit", 10)
	if limit > 50 {
		limit = 50
	}

	posts, nextCursor, err := h.postService.ListByAuthor(r.Context(), authorID, userID, cursor, limit)
	if err != nil {
		h.logger.Error("list user posts v2 failed", "error", err, "author_id", authorID, "requesting_user_id", userID)
		apierrors.WriteProblemDetail(w, apierrors.InternalError("Unable to load posts. Please try again later.", r.URL.Path))
		return
	}

	out := make([]FeedPostResponse, len(posts))
	for i, p := range posts {
		out[i] = h.toFeedPostResponse(p)
	}

	var cursorPtr *string
	if nextCursor != "" {
		cursorPtr = &nextCursor
	}
	response.WriteList(w, http.StatusOK, out, cursorPtr)
}

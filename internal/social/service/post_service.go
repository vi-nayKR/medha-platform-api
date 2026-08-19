package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/social/domain"
)

// PostService handles social feed business logic.
type PostService struct {
	postRepo domain.PostRepository
	logger   *slog.Logger
}

// NewPostService creates a new PostService.
func NewPostService(postRepo domain.PostRepository, logger *slog.Logger) *PostService {
	return &PostService{
		postRepo: postRepo,
		logger:   logger,
	}
}

// CreatePostParams contains parameters for creating a post.
type CreatePostParams struct {
	AuthorID  uuid.UUID
	Content   string
	ImageURLs []string
	Tags      []string
}

// CreatePost creates a new social feed post.
func (s *PostService) CreatePost(ctx context.Context, params CreatePostParams) (*domain.Post, error) {
	if params.Content == "" && len(params.ImageURLs) == 0 {
		return nil, domain.ErrEmptyPost
	}

	if params.ImageURLs == nil {
		params.ImageURLs = []string{}
	}
	if params.Tags == nil {
		params.Tags = []string{}
	}

	post := &domain.Post{
		ID:           uuid.New(),
		AuthorID:     params.AuthorID,
		Content:      params.Content,
		ImageURLs:    params.ImageURLs,
		Tags:         params.Tags,
		LikeCount:    0,
		CommentCount: 0,
		IsPinned:     false,
		PostType:     "post",
		Visibility:   "public",
		FeedScore:    0,
		Specialty:    []string{},
		CreatedAt:    epoch.Now(),
		UpdatedAt:    epoch.Now(),
	}

	if err := s.postRepo.Create(ctx, post); err != nil {
		return nil, fmt.Errorf("create post: %w", err)
	}

	s.logger.Info("post created", "post_id", post.ID, "author_id", params.AuthorID)
	return post, nil
}

// GetPost returns a post with author details.
func (s *PostService) GetPost(ctx context.Context, postID, requestingUserID uuid.UUID) (*domain.PostWithAuthor, error) {
	post, err := s.postRepo.GetByIDWithAuthor(ctx, postID, requestingUserID)
	if err != nil {
		return nil, fmt.Errorf("get post: %w", err)
	}
	return post, nil
}

// ListFeed returns the community feed with cursor-based pagination.
func (s *PostService) ListFeed(ctx context.Context, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	posts, nextCursor, err := s.postRepo.ListFeed(ctx, requestingUserID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list feed: %w", err)
	}
	return posts, nextCursor, nil
}

// ListFeedV2 returns a ranked, personalised home-screen feed.
func (s *PostService) ListFeedV2(ctx context.Context, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	posts, nextCursor, err := s.postRepo.ListFeedV2(ctx, requestingUserID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list feed v2: %w", err)
	}
	return posts, nextCursor, nil
}

// ListByAuthor returns posts by a specific author (for portfolio grid).
func (s *PostService) ListByAuthor(ctx context.Context, authorID, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	posts, nextCursor, err := s.postRepo.ListByAuthor(ctx, authorID, requestingUserID, cursor, limit)
	if err != nil {
		return nil, "", fmt.Errorf("list by author: %w", err)
	}
	return posts, nextCursor, nil
}

// LikePost adds a like from a user to a post.
func (s *PostService) LikePost(ctx context.Context, postID, userID uuid.UUID) error {
	// Verify post exists
	if _, err := s.postRepo.GetByID(ctx, postID); err != nil {
		return fmt.Errorf("get post: %w", err)
	}

	if err := s.postRepo.Like(ctx, postID, userID); err != nil {
		return fmt.Errorf("like post: %w", err)
	}
	if err := s.postRepo.IncrementLikeCount(ctx, postID); err != nil {
		s.logger.Error("failed to increment like count", "error", err, "post_id", postID)
	}

	s.logger.Info("post liked", "post_id", postID, "user_id", userID)
	return nil
}

// UnlikePost removes a like from a user on a post.
func (s *PostService) UnlikePost(ctx context.Context, postID, userID uuid.UUID) error {
	if err := s.postRepo.Unlike(ctx, postID, userID); err != nil {
		return fmt.Errorf("unlike post: %w", err)
	}
	if err := s.postRepo.DecrementLikeCount(ctx, postID); err != nil {
		s.logger.Error("failed to decrement like count", "error", err, "post_id", postID)
	}

	s.logger.Info("post unliked", "post_id", postID, "user_id", userID)
	return nil
}

// DeletePost soft-deletes a post (owner only).
func (s *PostService) DeletePost(ctx context.Context, postID, userID uuid.UUID) error {
	post, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		return fmt.Errorf("get post: %w", err)
	}
	if post.AuthorID != userID {
		return domain.ErrPostNotOwned
	}
	if err := s.postRepo.SoftDelete(ctx, postID); err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	s.logger.Info("post deleted", "post_id", postID, "user_id", userID)
	return nil
}

// UpdatePost updates a post's content and media.
func (s *PostService) UpdatePost(ctx context.Context, postID, userID uuid.UUID, content string, imageURLs, tags []string) (*domain.Post, error) {
	if content == "" && len(imageURLs) == 0 {
		return nil, domain.ErrEmptyPost
	}
	post, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		return nil, fmt.Errorf("get post: %w", err)
	}
	if post.AuthorID != userID {
		return nil, domain.ErrPostNotOwned
	}

	post.Content = content
	if imageURLs == nil {
		post.ImageURLs = []string{}
	} else {
		post.ImageURLs = imageURLs
	}
	if tags == nil {
		post.Tags = []string{}
	} else {
		post.Tags = tags
	}
	post.UpdatedAt = epoch.Now()

	if err := s.postRepo.Update(ctx, post); err != nil {
		return nil, fmt.Errorf("update post: %w", err)
	}

	s.logger.Info("post updated", "post_id", postID, "user_id", userID)
	return post, nil
}

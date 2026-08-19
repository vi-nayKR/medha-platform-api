package domain

import (
	"context"

	"github.com/google/uuid"
)

// PostRepository defines the port for post and like data access.
type PostRepository interface {
	// Create inserts a new post.
	Create(ctx context.Context, post *Post) error

	// GetByID returns a non-deleted post by ID.
	GetByID(ctx context.Context, id uuid.UUID) (*Post, error)

	// Update updates post content, image_urls, and tags.
	Update(ctx context.Context, post *Post) error

	// GetByIDWithAuthor returns a post with author details and user's like state.
	GetByIDWithAuthor(ctx context.Context, id, requestingUserID uuid.UUID) (*PostWithAuthor, error)

	// SoftDelete marks a post as deleted.
	SoftDelete(ctx context.Context, id uuid.UUID) error

	// ListFeed returns posts ordered by recency, with author info and user's like state.
	ListFeed(ctx context.Context, requestingUserID uuid.UUID, cursor string, limit int) ([]*PostWithAuthor, string, error)

	// ListByAuthor returns posts by a specific author.
	ListByAuthor(ctx context.Context, authorID, requestingUserID uuid.UUID, cursor string, limit int) ([]*PostWithAuthor, string, error)

	// Like adds a like from a user to a post.
	// Returns ErrAlreadyLiked if already liked.
	Like(ctx context.Context, postID, userID uuid.UUID) error

	// Unlike removes a like from a user on a post.
	// Returns ErrNotLiked if not liked.
	Unlike(ctx context.Context, postID, userID uuid.UUID) error

	// IncrementLikeCount atomically increases like_count by 1.
	IncrementLikeCount(ctx context.Context, postID uuid.UUID) error

	// DecrementLikeCount atomically decreases like_count by 1 (floor 0).
	DecrementLikeCount(ctx context.Context, postID uuid.UUID) error

	// ListFeedV2 returns a ranked, personalised feed for the home screen.
	// It ranks posts by: followed authors first, then by feed_score DESC, then created_at DESC.
	ListFeedV2(ctx context.Context, requestingUserID uuid.UUID, cursor string, limit int) ([]*PostWithAuthor, string, error)
}

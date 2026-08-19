package domain

import (
	"errors"

	"github.com/google/uuid"
)

// Post represents a social feed post.
type Post struct {
	ID           uuid.UUID
	AuthorID     uuid.UUID
	Content      string
	ImageURLs    []string
	Tags         []string
	LikeCount    int
	CommentCount int
	IsPinned     bool
	PostType     string
	Visibility   string
	FeedScore    float64
	Specialty    []string
	CreatedAt    int64
	UpdatedAt    int64
	DeletedAt    *int64
}

// PostWithAuthor extends Post with joined author information.
type PostWithAuthor struct {
	Post
	AuthorFirstName   string
	AuthorLastName    string
	AuthorPhotoURL    *string
	AuthorRole        string
	AuthorSpecialty   string // new field
	LikedByUser       bool   // whether the requesting user has liked this post
	IsFollowingAuthor bool   // whether the requesting user follows the author
	AuthorUsername    string // username of the author
	AuthorIsOfficial  bool   // whether the author is a verified official account
}

// Like represents a user's like on a post.
type Like struct {
	ID        uuid.UUID
	PostID    uuid.UUID
	UserID    uuid.UUID
	CreatedAt int64
}

// Sentinel errors for social operations.
var (
	ErrPostNotFound     = errors.New("post not found")
	ErrAlreadyLiked     = errors.New("post already liked")
	ErrNotLiked         = errors.New("post not liked")
	ErrPostNotOwned     = errors.New("post does not belong to this user")
	ErrEmptyPost        = errors.New("post must have content or at least one image")
	ErrInvalidVisibility = errors.New("invalid post visibility value")
	ErrInvalidPostType   = errors.New("invalid post type")
)

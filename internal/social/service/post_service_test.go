package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/social/domain"
)

type fakePostRepo struct {
	domain.PostRepository
	posts        map[uuid.UUID]*domain.Post
	liked        map[uuid.UUID]map[uuid.UUID]bool
	incrementErr error
}

func newFakePostRepo() *fakePostRepo {
	return &fakePostRepo{
		posts: make(map[uuid.UUID]*domain.Post),
		liked: make(map[uuid.UUID]map[uuid.UUID]bool),
	}
}

func (r *fakePostRepo) Create(_ context.Context, post *domain.Post) error {
	cp := *post
	r.posts[post.ID] = &cp
	return nil
}

func (r *fakePostRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Post, error) {
	post, ok := r.posts[id]
	if !ok {
		return nil, domain.ErrPostNotFound
	}
	cp := *post
	return &cp, nil
}

func (r *fakePostRepo) Update(_ context.Context, post *domain.Post) error {
	if _, ok := r.posts[post.ID]; !ok {
		return domain.ErrPostNotFound
	}
	cp := *post
	r.posts[post.ID] = &cp
	return nil
}

func (r *fakePostRepo) Like(_ context.Context, postID, userID uuid.UUID) error {
	if r.liked[postID] == nil {
		r.liked[postID] = make(map[uuid.UUID]bool)
	}
	if r.liked[postID][userID] {
		return domain.ErrAlreadyLiked
	}
	r.liked[postID][userID] = true
	return nil
}

func (r *fakePostRepo) Unlike(_ context.Context, postID, userID uuid.UUID) error {
	if !r.liked[postID][userID] {
		return domain.ErrNotLiked
	}
	delete(r.liked[postID], userID)
	return nil
}

func (r *fakePostRepo) IncrementLikeCount(_ context.Context, postID uuid.UUID) error {
	if r.incrementErr != nil {
		return r.incrementErr
	}
	r.posts[postID].LikeCount++
	return nil
}

func (r *fakePostRepo) DecrementLikeCount(_ context.Context, postID uuid.UUID) error {
	if r.posts[postID].LikeCount > 0 {
		r.posts[postID].LikeCount--
	}
	return nil
}

func (r *fakePostRepo) SoftDelete(_ context.Context, id uuid.UUID) error {
	if _, ok := r.posts[id]; !ok {
		return domain.ErrPostNotFound
	}
	delete(r.posts, id)
	return nil
}

func TestPostService_CreatePostValidation(t *testing.T) {
	svc := NewPostService(newFakePostRepo(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := svc.CreatePost(context.Background(), CreatePostParams{AuthorID: uuid.New()})
	if !errors.Is(err, domain.ErrEmptyPost) {
		t.Fatalf("expected ErrEmptyPost, got %v", err)
	}
}

func TestPostService_CreatePostInitializesDefaults(t *testing.T) {
	repo := newFakePostRepo()
	svc := NewPostService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	authorID := uuid.New()

	post, err := svc.CreatePost(context.Background(), CreatePostParams{
		AuthorID: authorID,
		Content:  "A new ceremony story.",
	})
	if err != nil {
		t.Fatalf("CreatePost returned error: %v", err)
	}
	if post.AuthorID != authorID || post.PostType != "post" || post.Visibility != "public" {
		t.Fatalf("unexpected post defaults: %#v", post)
	}
	if post.ImageURLs == nil || post.Tags == nil || post.Specialty == nil {
		t.Fatal("expected slices to be initialized for JSON-safe responses")
	}
}

func TestPostService_LikeAndUnlikePost(t *testing.T) {
	repo := newFakePostRepo()
	postID := uuid.New()
	userID := uuid.New()
	repo.posts[postID] = &domain.Post{ID: postID, AuthorID: uuid.New()}
	svc := NewPostService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.LikePost(context.Background(), postID, userID); err != nil {
		t.Fatalf("LikePost returned error: %v", err)
	}
	if repo.posts[postID].LikeCount != 1 {
		t.Fatalf("expected like count 1, got %d", repo.posts[postID].LikeCount)
	}

	if err := svc.UnlikePost(context.Background(), postID, userID); err != nil {
		t.Fatalf("UnlikePost returned error: %v", err)
	}
	if repo.posts[postID].LikeCount != 0 {
		t.Fatalf("expected like count 0, got %d", repo.posts[postID].LikeCount)
	}
}

func TestPostService_DeletePostOwnership(t *testing.T) {
	repo := newFakePostRepo()
	postID := uuid.New()
	ownerID := uuid.New()
	repo.posts[postID] = &domain.Post{ID: postID, AuthorID: ownerID}
	svc := NewPostService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := svc.DeletePost(context.Background(), postID, uuid.New())
	if !errors.Is(err, domain.ErrPostNotOwned) {
		t.Fatalf("expected ErrPostNotOwned, got %v", err)
	}
	if err := svc.DeletePost(context.Background(), postID, ownerID); err != nil {
		t.Fatalf("DeletePost returned error for owner: %v", err)
	}
}

func TestPostService_UpdatePost(t *testing.T) {
	repo := newFakePostRepo()
	postID := uuid.New()
	ownerID := uuid.New()
	repo.posts[postID] = &domain.Post{ID: postID, AuthorID: ownerID, Content: "Old content"}
	svc := NewPostService(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Unowned update fails
	_, err := svc.UpdatePost(context.Background(), postID, uuid.New(), "New content", nil, nil)
	if !errors.Is(err, domain.ErrPostNotOwned) {
		t.Fatalf("expected ErrPostNotOwned, got %v", err)
	}

	// Empty content and images fails
	_, err = svc.UpdatePost(context.Background(), postID, ownerID, "", nil, nil)
	if !errors.Is(err, domain.ErrEmptyPost) {
		t.Fatalf("expected ErrEmptyPost, got %v", err)
	}

	// Owner update succeeds
	updated, err := svc.UpdatePost(context.Background(), postID, ownerID, "New content", []string{"http://image.png"}, []string{"tag1"})
	if err != nil {
		t.Fatalf("UpdatePost returned error: %v", err)
	}
	if updated.Content != "New content" || len(updated.ImageURLs) != 1 || updated.ImageURLs[0] != "http://image.png" || len(updated.Tags) != 1 || updated.Tags[0] != "tag1" {
		t.Fatalf("unexpected updated fields: %#v", updated)
	}
}

package service_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	authdomain "github.com/medha/backend/internal/auth/domain"
	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/story/domain"
	"github.com/medha/backend/internal/story/service"
	publisherdomain "github.com/medha/backend/internal/systempublisher/domain"
	publisherservice "github.com/medha/backend/internal/systempublisher/service"
	userdomain "github.com/medha/backend/internal/user/domain"
)

// --- Fakes ---

type fakeStoryRepo struct {
	domain.StoryRepository
	stories map[uuid.UUID]*domain.StoryWithPublisher
}

func newFakeStoryRepo() *fakeStoryRepo {
	return &fakeStoryRepo{stories: make(map[uuid.UUID]*domain.StoryWithPublisher)}
}

func (r *fakeStoryRepo) Create(_ context.Context, s *domain.Story) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	s.CreatedAt = epoch.Now()
	s.UpdatedAt = s.CreatedAt
	r.stories[s.ID] = &domain.StoryWithPublisher{Story: *s}
	return nil
}

func (r *fakeStoryRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.StoryWithPublisher, error) {
	sp, ok := r.stories[id]
	if !ok {
		return nil, domain.ErrStoryNotFound
	}
	return sp, nil
}

func (r *fakeStoryRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domain.Status) error {
	sp, ok := r.stories[id]
	if !ok {
		return domain.ErrStoryNotFound
	}
	sp.Status = status
	return nil
}

func (r *fakeStoryRepo) SoftDelete(_ context.Context, id uuid.UUID) error {
	if _, ok := r.stories[id]; !ok {
		return domain.ErrStoryNotFound
	}
	delete(r.stories, id)
	return nil
}

type fakeStoryViewRepo struct {
	views map[[2]uuid.UUID]int
}

func newFakeStoryViewRepo() *fakeStoryViewRepo {
	return &fakeStoryViewRepo{views: make(map[[2]uuid.UUID]int)}
}

func (r *fakeStoryViewRepo) RecordView(_ context.Context, storyID, viewerUserID uuid.UUID) error {
	r.views[[2]uuid.UUID{storyID, viewerUserID}]++
	return nil
}

type fakeUserRepo struct {
	userdomain.UserRepository
	users map[uuid.UUID]*userdomain.User
}

func (r *fakeUserRepo) GetByID(_ context.Context, id uuid.UUID) (*userdomain.User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, userdomain.ErrUserNotFound
	}
	return u, nil
}

type fakePublisherRepo struct {
	publisherdomain.SystemPublisherRepository
	publisher *publisherdomain.SystemPublisher
}

func (r *fakePublisherRepo) GetByKey(_ context.Context, key string) (*publisherdomain.SystemPublisher, error) {
	if r.publisher == nil || r.publisher.PublisherKey != key {
		return nil, publisherdomain.ErrPublisherNotFound
	}
	return r.publisher, nil
}

// --- Test setup helper ---

func newTestService(t *testing.T) (*service.StoryService, *fakeStoryRepo, *fakeUserRepo, uuid.UUID) {
	t.Helper()
	storyRepo := newFakeStoryRepo()
	viewRepo := newFakeStoryViewRepo()
	viewerID := uuid.New()
	userRepo := &fakeUserRepo{users: map[uuid.UUID]*userdomain.User{
		viewerID: {ID: viewerID, Role: authdomain.RolePandit, City: "Bengaluru", State: "Karnataka", CreatedAt: 1000},
	}}
	publisherRepo := &fakePublisherRepo{publisher: &publisherdomain.SystemPublisher{
		ID: uuid.New(), PublisherKey: "medha-app", DisplayName: "Medha", Username: "medhaapp", IsActive: true, IsVerified: true,
	}}
	publisherSvc := publisherservice.NewSystemPublisherService(publisherRepo)
	svc := service.NewStoryService(storyRepo, viewRepo, userRepo, publisherSvc, nil, slog.Default())
	return svc, storyRepo, userRepo, viewerID
}

func validParams() domain.CreateStoryParams {
	now := epoch.Now()
	return domain.CreateStoryParams{
		MediaType:     "image",
		MediaURL:      "https://s3-dev.medha.dev/admin-media/story.png",
		AudienceScope: domain.AudienceAllUsers,
		StartsAt:      now - 10,
		ExpiresAt:     now + 1000,
	}
}

// --- CreateStory validation tests ---

func TestCreateStory_RejectsInvertedWindow(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	params := validParams()
	params.StartsAt, params.ExpiresAt = params.ExpiresAt, params.StartsAt

	_, err := svc.CreateStory(context.Background(), "admin1", params)
	if !errors.Is(err, domain.ErrInvalidStoryWindow) {
		t.Fatalf("expected ErrInvalidStoryWindow, got %v", err)
	}
}

func TestCreateStory_RejectsInvalidAudienceScope(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	params := validParams()
	params.AudienceScope = domain.AudienceScope("everyone_on_earth")

	_, err := svc.CreateStory(context.Background(), "admin1", params)
	if !errors.Is(err, domain.ErrInvalidAudienceRule) {
		t.Fatalf("expected ErrInvalidAudienceRule, got %v", err)
	}
}

func TestCreateStory_RejectsUnsupportedMediaType(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	params := validParams()
	params.MediaType = "audio"

	_, err := svc.CreateStory(context.Background(), "admin1", params)
	if !errors.Is(err, domain.ErrInvalidMediaType) {
		t.Fatalf("expected ErrInvalidMediaType, got %v", err)
	}
}

func TestCreateStory_Success(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	story, err := svc.CreateStory(context.Background(), "admin1", validParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if story.Status != domain.StatusDraft {
		t.Errorf("expected new story to start as draft, got %s", story.Status)
	}
	if story.CreatedByAdmin != "admin1" {
		t.Errorf("expected created_by_admin=admin1, got %q", story.CreatedByAdmin)
	}
}

// --- RecordView protection tests ---

func TestRecordView_RejectsNonexistentStory(t *testing.T) {
	svc, _, _, viewerID := newTestService(t)
	err := svc.RecordView(context.Background(), viewerID, uuid.New())
	if !errors.Is(err, domain.ErrStoryNotFound) {
		t.Fatalf("expected ErrStoryNotFound, got %v", err)
	}
}

func TestRecordView_RejectsInactiveStory(t *testing.T) {
	svc, repo, _, viewerID := newTestService(t)
	story := &domain.Story{ID: uuid.New(), AudienceScope: domain.AudienceAllUsers, Status: domain.StatusDraft, StartsAt: epoch.Now() - 10, ExpiresAt: epoch.Now() + 1000}
	repo.stories[story.ID] = &domain.StoryWithPublisher{Story: *story}

	err := svc.RecordView(context.Background(), viewerID, story.ID)
	if !errors.Is(err, domain.ErrStoryNotFound) {
		t.Fatalf("expected ErrStoryNotFound for draft story, got %v", err)
	}
}

func TestRecordView_RejectsExpiredStory(t *testing.T) {
	svc, repo, _, viewerID := newTestService(t)
	story := &domain.Story{ID: uuid.New(), AudienceScope: domain.AudienceAllUsers, Status: domain.StatusActive, StartsAt: epoch.Now() - 1000, ExpiresAt: epoch.Now() - 10}
	repo.stories[story.ID] = &domain.StoryWithPublisher{Story: *story}

	err := svc.RecordView(context.Background(), viewerID, story.ID)
	if !errors.Is(err, domain.ErrStoryNotFound) {
		t.Fatalf("expected ErrStoryNotFound for expired story, got %v", err)
	}
}

func TestRecordView_RejectsIneligibleViewer(t *testing.T) {
	svc, repo, _, viewerID := newTestService(t)
	// Segment-targeted at a role the viewer doesn't have.
	story := &domain.Story{
		ID: uuid.New(), AudienceScope: domain.AudienceSegment,
		AudienceRules: domain.AudienceRules{Roles: []string{"yajman"}},
		Status:        domain.StatusActive, StartsAt: epoch.Now() - 10, ExpiresAt: epoch.Now() + 1000,
	}
	repo.stories[story.ID] = &domain.StoryWithPublisher{Story: *story}

	err := svc.RecordView(context.Background(), viewerID, story.ID)
	if !errors.Is(err, domain.ErrStoryNotFound) {
		t.Fatalf("expected ErrStoryNotFound for ineligible viewer, got %v", err)
	}
}

func TestRecordView_SuccessIsIdempotent(t *testing.T) {
	svc, repo, _, viewerID := newTestService(t)
	story := &domain.Story{ID: uuid.New(), AudienceScope: domain.AudienceAllUsers, Status: domain.StatusActive, StartsAt: epoch.Now() - 10, ExpiresAt: epoch.Now() + 1000}
	repo.stories[story.ID] = &domain.StoryWithPublisher{Story: *story}

	for i := 0; i < 3; i++ {
		if err := svc.RecordView(context.Background(), viewerID, story.ID); err != nil {
			t.Fatalf("unexpected error on view %d: %v", i, err)
		}
	}
}

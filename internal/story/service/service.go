package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/medha/backend/internal/platform/epoch"
	"github.com/medha/backend/internal/story/domain"
	publisherservice "github.com/medha/backend/internal/systempublisher/service"
	userdomain "github.com/medha/backend/internal/user/domain"
)

var allowedMediaTypes = map[string]bool{"image": true, "video": true}

// StoryService implements the official-story business workflows: publishing
// (admin-only, Phase 7), feed resolution (Phase 5), and view tracking
// (Phase 6).
type StoryService struct {
	storyRepo    domain.StoryRepository
	viewRepo     domain.StoryViewRepository
	userRepo     userdomain.UserRepository
	publisherSvc *publisherservice.SystemPublisherService
	// mediaURLPrefixes restricts media_url to our own managed storage —
	// arbitrary external URLs are never accepted (Phase 4).
	mediaURLPrefixes []string
	logger           *slog.Logger
}

// NewStoryService creates a new StoryService.
func NewStoryService(
	storyRepo domain.StoryRepository,
	viewRepo domain.StoryViewRepository,
	userRepo userdomain.UserRepository,
	publisherSvc *publisherservice.SystemPublisherService,
	mediaURLPrefixes []string,
	logger *slog.Logger,
) *StoryService {
	return &StoryService{
		storyRepo:        storyRepo,
		viewRepo:         viewRepo,
		userRepo:         userRepo,
		publisherSvc:     publisherSvc,
		mediaURLPrefixes: mediaURLPrefixes,
		logger:           logger,
	}
}

func (s *StoryService) validateMedia(mediaType, mediaURL string) error {
	if !allowedMediaTypes[mediaType] {
		return domain.ErrInvalidMediaType
	}
	if mediaURL == "" {
		return fmt.Errorf("%w: media_url is required", domain.ErrInvalidMediaType)
	}
	if len(s.mediaURLPrefixes) == 0 {
		return nil
	}
	for _, prefix := range s.mediaURLPrefixes {
		if strings.HasPrefix(mediaURL, prefix) {
			return nil
		}
	}
	return fmt.Errorf("%w: media_url must be an uploaded asset from managed storage", domain.ErrInvalidMediaType)
}

// CreateStory creates a draft story authored by the Medha App publisher.
// adminUsername is recorded as audit information only — it is never the
// displayed author.
func (s *StoryService) CreateStory(ctx context.Context, adminUsername string, params domain.CreateStoryParams) (*domain.Story, error) {
	if params.ExpiresAt <= params.StartsAt {
		return nil, domain.ErrInvalidStoryWindow
	}
	if !params.AudienceScope.IsValid() {
		return nil, domain.ErrInvalidAudienceRule
	}
	if err := s.validateMedia(params.MediaType, params.MediaURL); err != nil {
		return nil, err
	}

	publisher, err := s.publisherSvc.MedhaApp(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve medha app publisher: %w", err)
	}

	story := &domain.Story{
		ID:             uuid.New(),
		PublisherID:    publisher.ID,
		Title:          params.Title,
		Caption:        params.Caption,
		MediaType:      params.MediaType,
		MediaURL:       params.MediaURL,
		ThumbnailURL:   params.ThumbnailURL,
		ActionURL:      params.ActionURL,
		ActionLabel:    params.ActionLabel,
		AudienceScope:  params.AudienceScope,
		AudienceRules:  params.AudienceRules,
		Status:         domain.StatusDraft,
		Priority:       params.Priority,
		StartsAt:       params.StartsAt,
		ExpiresAt:      params.ExpiresAt,
		CreatedByAdmin: adminUsername,
	}
	if err := s.storyRepo.Create(ctx, story); err != nil {
		return nil, fmt.Errorf("create story: %w", err)
	}

	s.logger.Info("official story created", "story_id", story.ID, "admin_username", adminUsername)
	return story, nil
}

// UpdateStory edits a story's content/targeting/window (not its status —
// use the status transition methods below).
func (s *StoryService) UpdateStory(ctx context.Context, id uuid.UUID, params domain.CreateStoryParams) (*domain.StoryWithPublisher, error) {
	if params.ExpiresAt <= params.StartsAt {
		return nil, domain.ErrInvalidStoryWindow
	}
	if !params.AudienceScope.IsValid() {
		return nil, domain.ErrInvalidAudienceRule
	}
	if err := s.validateMedia(params.MediaType, params.MediaURL); err != nil {
		return nil, err
	}

	existing, err := s.storyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	story := existing.Story
	story.Title = params.Title
	story.Caption = params.Caption
	story.MediaType = params.MediaType
	story.MediaURL = params.MediaURL
	story.ThumbnailURL = params.ThumbnailURL
	story.ActionURL = params.ActionURL
	story.ActionLabel = params.ActionLabel
	story.AudienceScope = params.AudienceScope
	story.AudienceRules = params.AudienceRules
	story.Priority = params.Priority
	story.StartsAt = params.StartsAt
	story.ExpiresAt = params.ExpiresAt

	if err := s.storyRepo.Update(ctx, &story); err != nil {
		return nil, fmt.Errorf("update story: %w", err)
	}
	return s.storyRepo.GetByID(ctx, id)
}

// transition validates and applies a status change. adminUsername is logged
// for audit purposes.
func (s *StoryService) transition(ctx context.Context, adminUsername string, id uuid.UUID, to domain.Status) error {
	if err := s.storyRepo.UpdateStatus(ctx, id, to); err != nil {
		return err
	}
	s.logger.Info("official story status changed", "story_id", id, "to_status", to, "admin_username", adminUsername)
	return nil
}

// Publish transitions a story to active (or scheduled, if its window hasn't
// started yet — ListActiveForViewer only ever surfaces status=active within
// its time window, so scheduling is enforced by the window check, not a
// separate cron).
func (s *StoryService) Publish(ctx context.Context, adminUsername string, id uuid.UUID) error {
	return s.transition(ctx, adminUsername, id, domain.StatusActive)
}

// Pause hides a story from the feed without deleting it.
func (s *StoryService) Pause(ctx context.Context, adminUsername string, id uuid.UUID) error {
	return s.transition(ctx, adminUsername, id, domain.StatusPaused)
}

// Archive marks a story as permanently retired.
func (s *StoryService) Archive(ctx context.Context, adminUsername string, id uuid.UUID) error {
	return s.transition(ctx, adminUsername, id, domain.StatusArchived)
}

// Delete soft-deletes a story.
func (s *StoryService) Delete(ctx context.Context, adminUsername string, id uuid.UUID) error {
	if err := s.storyRepo.SoftDelete(ctx, id); err != nil {
		return err
	}
	s.logger.Info("official story deleted", "story_id", id, "admin_username", adminUsername)
	return nil
}

// GetByID returns a single story for admin editing.
func (s *StoryService) GetByID(ctx context.Context, id uuid.UUID) (*domain.StoryWithPublisher, error) {
	return s.storyRepo.GetByID(ctx, id)
}

// ListForAdmin returns all stories for the admin console.
func (s *StoryService) ListForAdmin(ctx context.Context, cursor string, limit int) ([]*domain.StoryWithPublisher, string, error) {
	return s.storyRepo.ListForAdmin(ctx, cursor, limit)
}

// GetFeed resolves the eligible, active official stories for a viewer. The
// viewer's own attributes are read server-side from their user record —
// never accepted from client input.
func (s *StoryService) GetFeed(ctx context.Context, viewerUserID uuid.UUID, cursor string, limit int) ([]*domain.StoryWithPublisher, string, error) {
	user, err := s.userRepo.GetByID(ctx, viewerUserID)
	if err != nil {
		return nil, "", fmt.Errorf("get viewer: %w", err)
	}

	viewer := domain.ViewerAttributes{
		UserID:    user.ID,
		Role:      user.Role.String(),
		City:      user.City,
		State:     user.State,
		CreatedAt: user.CreatedAt,
	}
	return s.storyRepo.ListActiveForViewer(ctx, viewer, cursor, limit)
}

// RecordView records that viewerUserID has seen storyID. Rejects views for
// stories that don't exist or the viewer isn't eligible for, so a caller
// can't inflate view counts on arbitrary/ineligible stories.
func (s *StoryService) RecordView(ctx context.Context, viewerUserID, storyID uuid.UUID) error {
	story, err := s.storyRepo.GetByID(ctx, storyID)
	if err != nil {
		return err
	}

	user, err := s.userRepo.GetByID(ctx, viewerUserID)
	if err != nil {
		return fmt.Errorf("get viewer: %w", err)
	}

	if story.Status != domain.StatusActive {
		return domain.ErrStoryNotFound
	}
	now := epoch.Now()
	if story.StartsAt > now || story.ExpiresAt <= now {
		return domain.ErrStoryNotFound
	}

	viewer := domain.ViewerAttributes{
		UserID:    user.ID,
		Role:      user.Role.String(),
		City:      user.City,
		State:     user.State,
		CreatedAt: user.CreatedAt,
	}
	eligible := story.AudienceScope == domain.AudienceAllUsers ||
		((story.AudienceScope == domain.AudienceSegment || story.AudienceScope == domain.AudienceExplicitUsers) && story.AudienceRules.Matches(viewer))
	if !eligible {
		return domain.ErrStoryNotFound
	}

	return s.viewRepo.RecordView(ctx, storyID, viewerUserID)
}

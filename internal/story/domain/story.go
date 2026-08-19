package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// AudienceScope describes who a story is targeted at.
type AudienceScope string

const (
	AudienceAllUsers      AudienceScope = "all_users"
	AudienceSegment       AudienceScope = "segment"
	AudienceExplicitUsers AudienceScope = "explicit_users"
)

func (s AudienceScope) IsValid() bool {
	switch s {
	case AudienceAllUsers, AudienceSegment, AudienceExplicitUsers:
		return true
	}
	return false
}

// Status is the lifecycle state of an official story campaign.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusExpired   Status = "expired"
	StatusArchived  Status = "archived"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusDraft, StatusScheduled, StatusActive, StatusPaused, StatusExpired, StatusArchived:
		return true
	}
	return false
}

// AudienceRules is a closed, validated set of segment-targeting dimensions —
// deliberately NOT arbitrary JSON: unmarshaling rejects unknown fields, and
// matching is plain field comparison (see MatchesUser), never expression
// evaluation. Only dimensions backed by real, existing user data are
// supported; extend this struct (and MatchesUser) to add more.
type AudienceRules struct {
	Roles           []string    `json:"roles,omitempty"`
	Cities          []string    `json:"cities,omitempty"`
	States          []string    `json:"states,omitempty"`
	CreatedAfter    *int64      `json:"created_after,omitempty"`
	CreatedBefore   *int64      `json:"created_before,omitempty"`
	ExplicitUserIDs []uuid.UUID `json:"explicit_user_ids,omitempty"`
}

// ParseAudienceRules validates and decodes raw JSON into AudienceRules,
// rejecting unknown fields so arbitrary/unexpected JSON can't sneak in.
func ParseAudienceRules(raw json.RawMessage) (AudienceRules, error) {
	var rules AudienceRules
	if len(raw) == 0 {
		return rules, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rules); err != nil {
		return AudienceRules{}, fmt.Errorf("invalid audience rules: %w", err)
	}
	return rules, nil
}

// ViewerAttributes is the subset of a user's own data needed to evaluate
// audience eligibility. Built server-side from the authenticated viewer's
// own record — never accepted from the client.
type ViewerAttributes struct {
	UserID    uuid.UUID
	Role      string
	City      string
	State     string
	CreatedAt int64
}

// Matches returns true if the viewer satisfies every populated rule
// dimension (rules left empty are not filters).
func (r AudienceRules) Matches(v ViewerAttributes) bool {
	if len(r.Roles) > 0 && !containsFold(r.Roles, v.Role) {
		return false
	}
	if len(r.Cities) > 0 && !containsFold(r.Cities, v.City) {
		return false
	}
	if len(r.States) > 0 && !containsFold(r.States, v.State) {
		return false
	}
	if r.CreatedAfter != nil && v.CreatedAt < *r.CreatedAfter {
		return false
	}
	if r.CreatedBefore != nil && v.CreatedAt > *r.CreatedBefore {
		return false
	}
	if len(r.ExplicitUserIDs) > 0 {
		found := false
		for _, id := range r.ExplicitUserIDs {
			if id == v.UserID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func containsFold(list []string, needle string) bool {
	for _, v := range list {
		if len(v) == len(needle) && (v == needle) {
			return true
		}
	}
	return false
}

// Story is an official announcement campaign — stored and shown once,
// regardless of how many users are eligible to see it (see ViewsRepository
// for per-viewer state).
type Story struct {
	ID            uuid.UUID
	PublisherID   uuid.UUID
	Title         string
	Caption       string
	MediaType     string
	MediaURL      string
	ThumbnailURL  string
	ActionURL     string
	ActionLabel   string
	AudienceScope AudienceScope
	AudienceRules AudienceRules
	Status        Status
	Priority      int
	StartsAt      int64
	ExpiresAt     int64
	// CreatedByAdmin is the admin username who created this campaign — audit
	// information only, never the displayed author. This project's admin
	// auth is a separate username/token system, not a row in users.
	CreatedByAdmin string
	CreatedAt      int64
	UpdatedAt      int64
	PublishedAt    *int64
	DeletedAt      *int64
}

// StoryWithPublisher joins a Story with its (already-resolved) publisher
// for API responses — the author representation is always the publisher,
// never a human user.
type StoryWithPublisher struct {
	Story
	PublisherKey         string
	PublisherDisplayName string
	PublisherUsername    string
	PublisherAvatarURL   string
	PublisherVerified    bool
}

// Sentinel errors for story operations.
var (
	ErrStoryNotFound       = errors.New("story not found")
	ErrInvalidAudienceRule = errors.New("invalid audience rules")
	ErrInvalidStoryWindow  = errors.New("expires_at must be after starts_at")
	ErrInvalidMediaType    = errors.New("unsupported media type")
)

// CreateStoryParams are the fields an admin may set when creating a story.
// Deliberately excludes publisher_id, status, and audit fields — those are
// assigned by the service, never taken from client input (no mass
// assignment of privileged fields).
type CreateStoryParams struct {
	Title         string
	Caption       string
	MediaType     string
	MediaURL      string
	ThumbnailURL  string
	ActionURL     string
	ActionLabel   string
	AudienceScope AudienceScope
	AudienceRules AudienceRules
	Priority      int
	StartsAt      int64
	ExpiresAt     int64
}

// StoryRepository defines the port for official story data access.
type StoryRepository interface {
	Create(ctx context.Context, s *Story) error
	GetByID(ctx context.Context, id uuid.UUID) (*StoryWithPublisher, error)
	Update(ctx context.Context, s *Story) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status) error
	SoftDelete(ctx context.Context, id uuid.UUID) error

	// ListActiveForViewer returns active, eligible stories for a viewer,
	// deterministically ordered (priority DESC, published_at DESC, id DESC),
	// with cursor pagination and no duplicates across pages.
	ListActiveForViewer(ctx context.Context, viewer ViewerAttributes, cursor string, limit int) ([]*StoryWithPublisher, string, error)

	// ListForAdmin returns all non-deleted stories for the admin console,
	// regardless of eligibility/status.
	ListForAdmin(ctx context.Context, cursor string, limit int) ([]*StoryWithPublisher, string, error)
}

// StoryViewRepository defines the port for per-viewer story view tracking.
type StoryViewRepository interface {
	// RecordView idempotently upserts a view: creates the row on first view,
	// bumps view_count/last_viewed_at on repeat views.
	RecordView(ctx context.Context, storyID, viewerUserID uuid.UUID) error
}

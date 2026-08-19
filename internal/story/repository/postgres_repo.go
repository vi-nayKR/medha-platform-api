package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/story/domain"
)

// PostgresStoryRepository implements domain.StoryRepository.
type PostgresStoryRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresStoryRepository creates a new PostgresStoryRepository.
func NewPostgresStoryRepository(pool *pgxpool.Pool) *PostgresStoryRepository {
	return &PostgresStoryRepository{pool: pool}
}

// Create inserts a new story.
func (r *PostgresStoryRepository) Create(ctx context.Context, s *domain.Story) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	rulesJSON, err := json.Marshal(s.AudienceRules)
	if err != nil {
		return fmt.Errorf("marshal audience rules: %w", err)
	}

	query := `
		INSERT INTO official_stories (
			id, publisher_id, title, caption, media_type, media_url, thumbnail_url,
			action_url, action_label, audience_scope, audience_rules, status, priority,
			starts_at, expires_at, created_by_admin
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING created_at, updated_at
	`
	err = database.GetExecutor(ctx, r.pool).QueryRow(ctx, query,
		s.ID, s.PublisherID, nullableString(s.Title), nullableString(s.Caption), s.MediaType, s.MediaURL,
		nullableString(s.ThumbnailURL), nullableString(s.ActionURL), nullableString(s.ActionLabel),
		s.AudienceScope, rulesJSON, s.Status, s.Priority, s.StartsAt, s.ExpiresAt, nullableString(s.CreatedByAdmin),
	).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert story: %w", err)
	}
	return nil
}

// Update modifies a story's editable fields.
func (r *PostgresStoryRepository) Update(ctx context.Context, s *domain.Story) error {
	rulesJSON, err := json.Marshal(s.AudienceRules)
	if err != nil {
		return fmt.Errorf("marshal audience rules: %w", err)
	}
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx, `
		UPDATE official_stories SET
			title = $2, caption = $3, media_type = $4, media_url = $5, thumbnail_url = $6,
			action_url = $7, action_label = $8, audience_scope = $9, audience_rules = $10,
			priority = $11, starts_at = $12, expires_at = $13
		WHERE id = $1 AND deleted_at IS NULL
	`, s.ID, nullableString(s.Title), nullableString(s.Caption), s.MediaType, s.MediaURL,
		nullableString(s.ThumbnailURL), nullableString(s.ActionURL), nullableString(s.ActionLabel),
		s.AudienceScope, rulesJSON, s.Priority, s.StartsAt, s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("update story: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrStoryNotFound
	}
	return nil
}

// UpdateStatus transitions a story's lifecycle status (draft/scheduled/active/paused/expired/archived).
func (r *PostgresStoryRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.Status) error {
	var publishedAtClause string
	if status == domain.StatusActive {
		publishedAtClause = `, published_at = COALESCE(published_at, (EXTRACT(EPOCH FROM NOW()))::BIGINT)`
	}
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE official_stories SET status = $2`+publishedAtClause+` WHERE id = $1 AND deleted_at IS NULL`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("update story status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrStoryNotFound
	}
	return nil
}

// SoftDelete marks a story as deleted.
func (r *PostgresStoryRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE official_stories SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return fmt.Errorf("soft delete story: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrStoryNotFound
	}
	return nil
}

const selectStoryColumns = `
	s.id, s.publisher_id, COALESCE(s.title, ''), COALESCE(s.caption, ''), s.media_type, s.media_url,
	COALESCE(s.thumbnail_url, ''), COALESCE(s.action_url, ''), COALESCE(s.action_label, ''),
	s.audience_scope, s.audience_rules, s.status, s.priority, s.starts_at, s.expires_at,
	COALESCE(s.created_by_admin, ''), s.created_at, s.updated_at, s.published_at, s.deleted_at,
	p.publisher_key, p.display_name, p.username, COALESCE(p.avatar_url, ''), p.is_verified
`

func scanStoryWithPublisher(row pgx.Row) (*domain.StoryWithPublisher, error) {
	var sp domain.StoryWithPublisher
	var rulesJSON []byte
	err := row.Scan(
		&sp.ID, &sp.PublisherID, &sp.Title, &sp.Caption, &sp.MediaType, &sp.MediaURL,
		&sp.ThumbnailURL, &sp.ActionURL, &sp.ActionLabel,
		&sp.AudienceScope, &rulesJSON, &sp.Status, &sp.Priority, &sp.StartsAt, &sp.ExpiresAt,
		&sp.CreatedByAdmin, &sp.CreatedAt, &sp.UpdatedAt, &sp.PublishedAt, &sp.DeletedAt,
		&sp.PublisherKey, &sp.PublisherDisplayName, &sp.PublisherUsername, &sp.PublisherAvatarURL, &sp.PublisherVerified,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStoryNotFound
		}
		return nil, fmt.Errorf("scan story: %w", err)
	}
	rules, err := domain.ParseAudienceRules(rulesJSON)
	if err != nil {
		return nil, err
	}
	sp.AudienceRules = rules
	return &sp, nil
}

// GetByID returns a story (any status) with its publisher joined.
func (r *PostgresStoryRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.StoryWithPublisher, error) {
	row := database.GetExecutor(ctx, r.pool).QueryRow(ctx, `
		SELECT `+selectStoryColumns+`
		FROM official_stories s JOIN system_publishers p ON p.id = s.publisher_id
		WHERE s.id = $1 AND s.deleted_at IS NULL
	`, id)
	return scanStoryWithPublisher(row)
}

// ListForAdmin returns all non-deleted stories regardless of status/eligibility.
func (r *PostgresStoryRepository) ListForAdmin(ctx context.Context, cursor string, limit int) ([]*domain.StoryWithPublisher, string, error) {
	var cursorTime int64
	var cursorID uuid.UUID
	hasCursor := false
	if cursor != "" {
		if parsed, err := parseCursor(cursor); err == nil {
			cursorTime, cursorID = parsed.time, parsed.id
			hasCursor = true
		}
	}

	query := `SELECT ` + selectStoryColumns + `
		FROM official_stories s JOIN system_publishers p ON p.id = s.publisher_id
		WHERE s.deleted_at IS NULL`
	args := []any{}
	if hasCursor {
		query += fmt.Sprintf(` AND (s.created_at, s.id) < ($%d, $%d)`, len(args)+1, len(args)+2)
		args = append(args, cursorTime, cursorID)
	}
	query += fmt.Sprintf(` ORDER BY s.created_at DESC, s.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list stories for admin: %w", err)
	}
	defer rows.Close()

	var stories []*domain.StoryWithPublisher
	for rows.Next() {
		sp, err := scanStoryWithPublisher(rows)
		if err != nil {
			return nil, "", err
		}
		stories = append(stories, sp)
	}

	var nextCursor string
	if len(stories) > limit {
		stories = stories[:limit]
		last := stories[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return stories, nextCursor, nil
}

// ListActiveForViewer returns eligible active stories for a viewer.
//
// Official stories are low-volume, admin-curated campaigns (not
// user-generated content at scale), so eligibility is resolved by fetching
// the bounded "currently in its active time window" set — cheap thanks to
// idx_official_stories_active_window — and matching audience rules in Go
// against the typed, schema-validated AudienceRules struct. This is
// deliberately not dynamic SQL/JSONB expression evaluation: rules are
// plain field comparisons, never arbitrary code.
func (r *PostgresStoryRepository) ListActiveForViewer(ctx context.Context, viewer domain.ViewerAttributes, cursor string, limit int) ([]*domain.StoryWithPublisher, string, error) {
	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, `
		SELECT `+selectStoryColumns+`
		FROM official_stories s JOIN system_publishers p ON p.id = s.publisher_id
		WHERE s.deleted_at IS NULL
		  AND s.status = 'active'
		  AND s.starts_at <= (EXTRACT(EPOCH FROM NOW()))::BIGINT
		  AND s.expires_at > (EXTRACT(EPOCH FROM NOW()))::BIGINT
		  AND p.is_active = TRUE
	`)
	if err != nil {
		return nil, "", fmt.Errorf("list active stories: %w", err)
	}
	defer rows.Close()

	var eligible []*domain.StoryWithPublisher
	for rows.Next() {
		sp, err := scanStoryWithPublisher(rows)
		if err != nil {
			return nil, "", err
		}
		if !storyMatchesViewer(sp, viewer) {
			continue
		}
		eligible = append(eligible, sp)
	}

	// Deterministic ordering: priority DESC, then published_at (falling back
	// to created_at) DESC, then id DESC as a final stable tiebreaker.
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		at, bt := a.CreatedAt, b.CreatedAt
		if a.PublishedAt != nil {
			at = *a.PublishedAt
		}
		if b.PublishedAt != nil {
			bt = *b.PublishedAt
		}
		if at != bt {
			return at > bt
		}
		return a.ID.String() > b.ID.String()
	})

	start := 0
	if cursor != "" {
		for i, sp := range eligible {
			if sp.ID.String() == cursor {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	var nextCursor string
	if end < len(eligible) {
		nextCursor = eligible[end-1].ID.String()
	} else {
		end = len(eligible)
	}
	if start > len(eligible) {
		start = len(eligible)
	}
	return eligible[start:end], nextCursor, nil
}

func storyMatchesViewer(sp *domain.StoryWithPublisher, viewer domain.ViewerAttributes) bool {
	switch sp.AudienceScope {
	case domain.AudienceAllUsers:
		return true
	case domain.AudienceSegment, domain.AudienceExplicitUsers:
		return sp.AudienceRules.Matches(viewer)
	default:
		return false
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type cursorData struct {
	time int64
	id   uuid.UUID
}

func parseCursor(cursor string) (cursorData, error) {
	timePart, idPart, ok := strings.Cut(cursor, "_")
	if !ok {
		return cursorData{}, fmt.Errorf("malformed cursor")
	}
	t, err := strconv.ParseInt(timePart, 10, 64)
	if err != nil {
		return cursorData{}, fmt.Errorf("malformed cursor time: %w", err)
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return cursorData{}, fmt.Errorf("malformed cursor id: %w", err)
	}
	return cursorData{time: t, id: id}, nil
}

func encodeCursor(t int64, id uuid.UUID) string {
	return fmt.Sprintf("%d_%s", t, id.String())
}

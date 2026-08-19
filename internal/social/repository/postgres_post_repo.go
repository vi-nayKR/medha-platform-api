package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/infra/database"
	"github.com/medha/backend/internal/social/domain"
)

// PostgresPostRepository implements domain.PostRepository using pgx.
type PostgresPostRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPostRepository creates a new PostgresPostRepository.
func NewPostgresPostRepository(pool *pgxpool.Pool) *PostgresPostRepository {
	return &PostgresPostRepository{pool: pool}
}

// Create inserts a new post.
func (r *PostgresPostRepository) Create(ctx context.Context, post *domain.Post) error {
	query := `
		INSERT INTO posts (id, author_id, content, image_urls, tags, like_count, comment_count, is_pinned, post_type, visibility, feed_score, specialty, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx, query,
		post.ID, post.AuthorID, post.Content, post.ImageURLs, post.Tags,
		post.LikeCount, post.CommentCount, post.IsPinned, post.PostType, post.Visibility, post.FeedScore, post.Specialty, post.CreatedAt, post.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert post: %w", err)
	}
	return nil
}

// GetByID returns a non-deleted post by ID.
func (r *PostgresPostRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Post, error) {
	query := `
		SELECT id, author_id, content, image_urls, tags, like_count, comment_count, is_pinned, post_type, visibility, feed_score, specialty, created_at, updated_at
		FROM posts WHERE id = $1 AND deleted_at IS NULL
	`
	var p domain.Post
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id).Scan(
		&p.ID, &p.AuthorID, &p.Content, &p.ImageURLs, &p.Tags,
		&p.LikeCount, &p.CommentCount, &p.IsPinned, &p.PostType, &p.Visibility, &p.FeedScore, &p.Specialty, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPostNotFound
		}
		return nil, fmt.Errorf("get post: %w", err)
	}
	return &p, nil
}

// GetByIDWithAuthor returns a post with author details.
func (r *PostgresPostRepository) GetByIDWithAuthor(ctx context.Context, id, requestingUserID uuid.UUID) (*domain.PostWithAuthor, error) {
	query := `
		SELECT
			p.id, p.author_id, p.content, p.image_urls, p.tags, p.like_count, p.comment_count, p.is_pinned, p.post_type, p.visibility, p.feed_score, p.specialty, p.created_at, p.updated_at,
			u.first_name, u.last_name, u.profile_photo_url, COALESCE(u.role::text, ''), COALESCE(u.username, ''),
			EXISTS(SELECT 1 FROM likes l WHERE l.post_id = p.id AND l.user_id = $2) AS liked,
			u.is_official AS author_is_official
		FROM posts p
		JOIN users u ON p.author_id = u.id
		WHERE p.id = $1 AND p.deleted_at IS NULL
	`
	var pa domain.PostWithAuthor
	var photoURL *string
	err := database.GetExecutor(ctx, r.pool).QueryRow(ctx, query, id, requestingUserID).Scan(
		&pa.ID, &pa.AuthorID, &pa.Content, &pa.ImageURLs, &pa.Tags,
		&pa.LikeCount, &pa.CommentCount, &pa.IsPinned, &pa.PostType, &pa.Visibility, &pa.FeedScore, &pa.Specialty, &pa.CreatedAt, &pa.UpdatedAt,
		&pa.AuthorFirstName, &pa.AuthorLastName, &photoURL, &pa.AuthorRole, &pa.AuthorUsername,
		&pa.LikedByUser, &pa.AuthorIsOfficial,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPostNotFound
		}
		return nil, fmt.Errorf("get post with author: %w", err)
	}
	pa.AuthorPhotoURL = photoURL
	return &pa, nil
}

// Update updates content, image_urls, tags and updated_at on a post.
func (r *PostgresPostRepository) Update(ctx context.Context, post *domain.Post) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE posts SET content = $2, image_urls = $3, tags = $4, updated_at = $5 WHERE id = $1 AND deleted_at IS NULL`,
		post.ID, post.Content, post.ImageURLs, post.Tags, post.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update post: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPostNotFound
	}
	return nil
}

// SoftDelete marks a post as deleted.
func (r *PostgresPostRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE posts SET deleted_at = (EXTRACT(EPOCH FROM NOW()))::BIGINT WHERE id = $1 AND deleted_at IS NULL`, id,
	)
	if err != nil {
		return fmt.Errorf("soft delete post: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPostNotFound
	}
	return nil
}

// ListFeed returns posts ordered by newest first with cursor-based pagination.
func (r *PostgresPostRepository) ListFeed(ctx context.Context, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	return r.listPosts(ctx, false, uuid.Nil, requestingUserID, cursor, limit)
}

// ListByAuthor returns posts by a specific author.
func (r *PostgresPostRepository) ListByAuthor(ctx context.Context, authorID, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	return r.listPosts(ctx, true, authorID, requestingUserID, cursor, limit)
}

func (r *PostgresPostRepository) listPosts(ctx context.Context, filterAuthor bool, authorID, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	var cursorTime int64
	var cursorID uuid.UUID
	hasCursor := false

	if cursor != "" {
		parsed, err := parseCursor(cursor)
		if err == nil {
			cursorTime = parsed.time
			cursorID = parsed.id
			hasCursor = true
		}
	}

	query := `
		SELECT
			p.id, p.author_id, p.content, p.image_urls, p.tags, p.like_count, p.comment_count, p.is_pinned, p.post_type, p.visibility, p.feed_score, p.specialty, p.created_at, p.updated_at,
			u.first_name, u.last_name, u.profile_photo_url, COALESCE(u.role::text, ''), COALESCE(u.username, ''),
			EXISTS(SELECT 1 FROM likes l WHERE l.post_id = p.id AND l.user_id = $1) AS liked,
			u.is_official AS author_is_official
		FROM posts p
		JOIN users u ON p.author_id = u.id
		WHERE p.deleted_at IS NULL
	`
	args := []any{requestingUserID}
	argIdx := 2

	if filterAuthor {
		// Filter by author: the Medha App system account's ID is the zero
		// UUID, so treating uuid.Nil as "no filter" would silently match every
		// post whenever that account is the one being queried.
		query += fmt.Sprintf(` AND p.author_id = $%d`, argIdx)
		args = append(args, authorID)
		argIdx++
	}

	if hasCursor {
		query += fmt.Sprintf(` AND (p.created_at, p.id) < ($%d, $%d)`, argIdx, argIdx+1)
		args = append(args, cursorTime, cursorID)
		argIdx += 2
	}

	query += fmt.Sprintf(` ORDER BY p.created_at DESC, p.id DESC LIMIT $%d`, argIdx)
	args = append(args, limit+1)

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	var posts []*domain.PostWithAuthor
	for rows.Next() {
		var pa domain.PostWithAuthor
		var photoURL *string
		if err := rows.Scan(
			&pa.ID, &pa.AuthorID, &pa.Content, &pa.ImageURLs, &pa.Tags,
			&pa.LikeCount, &pa.CommentCount, &pa.IsPinned, &pa.PostType, &pa.Visibility, &pa.FeedScore, &pa.Specialty, &pa.CreatedAt, &pa.UpdatedAt,
			&pa.AuthorFirstName, &pa.AuthorLastName, &photoURL, &pa.AuthorRole, &pa.AuthorUsername,
			&pa.LikedByUser, &pa.AuthorIsOfficial,
		); err != nil {
			return nil, "", fmt.Errorf("scan post: %w", err)
		}
		pa.AuthorPhotoURL = photoURL
		posts = append(posts, &pa)
	}

	var nextCursor string
	if len(posts) > limit {
		posts = posts[:limit]
		last := posts[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return posts, nextCursor, nil
}

// Like adds a like from a user to a post.
func (r *PostgresPostRepository) Like(ctx context.Context, postID, userID uuid.UUID) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`INSERT INTO likes (id, post_id, user_id) VALUES ($1, $2, $3)`,
		uuid.New(), postID, userID,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return domain.ErrAlreadyLiked
		}
		return fmt.Errorf("insert like: %w", err)
	}
	return nil
}

// Unlike removes a like and returns ErrNotLiked if not found.
func (r *PostgresPostRepository) Unlike(ctx context.Context, postID, userID uuid.UUID) error {
	tag, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`DELETE FROM likes WHERE post_id = $1 AND user_id = $2`,
		postID, userID,
	)
	if err != nil {
		return fmt.Errorf("delete like: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotLiked
	}
	return nil
}

// IncrementLikeCount atomically increases like_count by 1.
func (r *PostgresPostRepository) IncrementLikeCount(ctx context.Context, postID uuid.UUID) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE posts SET like_count = like_count + 1 WHERE id = $1 AND deleted_at IS NULL`,
		postID,
	)
	if err != nil {
		return fmt.Errorf("increment like count: %w", err)
	}
	return nil
}

// DecrementLikeCount atomically decreases like_count by 1 (floor 0).
func (r *PostgresPostRepository) DecrementLikeCount(ctx context.Context, postID uuid.UUID) error {
	_, err := database.GetExecutor(ctx, r.pool).Exec(ctx,
		`UPDATE posts SET like_count = GREATEST(like_count - 1, 0) WHERE id = $1 AND deleted_at IS NULL`,
		postID,
	)
	if err != nil {
		return fmt.Errorf("decrement like count: %w", err)
	}
	return nil
}

// --- Cursor helpers ---

type cursorData struct {
	time int64
	id   uuid.UUID
}

func parseCursor(cursor string) (cursorData, error) {
	idx := len(cursor) - 37
	if idx < 0 {
		return cursorData{}, fmt.Errorf("invalid cursor")
	}
	parts := []string{cursor[:idx], cursor[idx+1:]}
	t, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return cursorData{}, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return cursorData{}, err
	}
	return cursorData{time: t, id: id}, nil
}

func encodeCursor(t int64, id uuid.UUID) string {
	return strconv.FormatInt(t, 10) + "|" + id.String()
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	s := fmt.Sprintf("%v", err)
	for i := 0; i <= len(s)-5; i++ {
		if s[i:i+5] == "23505" {
			return true
		}
	}
	for i := 0; i <= len(s)-13; i++ {
		if s[i:i+13] == "duplicate key" {
			return true
		}
	}
	return false
}

// ListFeedV2 returns a ranked, personalised home-screen feed.
func (r *PostgresPostRepository) ListFeedV2(ctx context.Context, requestingUserID uuid.UUID, cursor string, limit int) ([]*domain.PostWithAuthor, string, error) {
	var cursorTime time.Time
	hasCursor := false

	if cursor != "" {
		parsed, err := time.Parse(time.RFC3339, cursor)
		if err == nil {
			cursorTime = parsed
			hasCursor = true
		}
	}

	query := `
		SELECT
			p.id,
			p.author_id,
			p.content,
			p.image_urls,
			p.tags,
			p.like_count,
			p.comment_count,
			p.post_type,
			p.visibility,
			p.feed_score,
			p.is_pinned,
			p.specialty,
			p.created_at,
			p.updated_at,
			u.first_name AS author_first_name,
			u.last_name AS author_last_name,
			u.profile_photo_url AS author_photo_url,
			COALESCE(u.role::text, '') AS author_role,
			COALESCE(u.username, '') AS author_username,
			FALSE AS is_following_author, /* TODO: replace with actual follows table check once exists */
			EXISTS (
				SELECT 1 FROM likes l
				WHERE l.post_id = p.id AND l.user_id = $1
			) AS liked_by_user,
			u.is_official AS author_is_official
		FROM posts p
		JOIN users u ON u.id = p.author_id
		WHERE
			p.deleted_at IS NULL
			AND p.visibility = 'public'
	`
	args := []any{requestingUserID}
	argIdx := 2

	if hasCursor {
		query += fmt.Sprintf(` AND p.created_at < $%d`, argIdx)
		args = append(args, cursorTime)
		argIdx++
	}

	query += fmt.Sprintf(`
		ORDER BY
			p.is_pinned DESC,
			is_following_author DESC,
			p.feed_score DESC,
			p.created_at DESC
		LIMIT $%d
	`, argIdx)
	args = append(args, limit+1)

	rows, err := database.GetExecutor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list feed v2: %w", err)
	}
	defer rows.Close()

	var posts []*domain.PostWithAuthor
	for rows.Next() {
		var pa domain.PostWithAuthor
		var photoURL *string
		if err := rows.Scan(
			&pa.ID,
			&pa.AuthorID,
			&pa.Content,
			&pa.ImageURLs,
			&pa.Tags,
			&pa.LikeCount,
			&pa.CommentCount,
			&pa.PostType,
			&pa.Visibility,
			&pa.FeedScore,
			&pa.IsPinned,
			&pa.Specialty,
			&pa.CreatedAt,
			&pa.UpdatedAt,
			&pa.AuthorFirstName,
			&pa.AuthorLastName,
			&photoURL,
			&pa.AuthorRole,
			&pa.AuthorUsername,
			&pa.IsFollowingAuthor,
			&pa.LikedByUser,
			&pa.AuthorIsOfficial,
		); err != nil {
			return nil, "", fmt.Errorf("scan post v2: %w", err)
		}
		pa.AuthorPhotoURL = photoURL
		posts = append(posts, &pa)
	}

	var nextCursor string
	if len(posts) > limit {
		posts = posts[:limit]
		last := posts[limit-1]
		nextCursor = strconv.FormatInt(last.CreatedAt, 10)
	}

	return posts, nextCursor, nil
}

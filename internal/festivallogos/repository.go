package festivallogos

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository handles all DB operations for festival_logos.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const selectCols = `
	SELECT id, name, slug, image_url_no_bg, is_active, display_order
	FROM festival_logos
`

func scan(row pgx.Row) (*FestivalLogo, error) {
	f := &FestivalLogo{}
	err := row.Scan(&f.ID, &f.Name, &f.Slug, &f.ImageURLNoBg, &f.IsActive, &f.DisplayOrder)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return f, err
}

// GetAll returns all festival logos ordered by display_order.
func (r *Repository) GetAll(ctx context.Context) ([]*FestivalLogo, error) {
	rows, err := r.pool.Query(ctx, selectCols+` ORDER BY display_order ASC`)
	if err != nil {
		return nil, fmt.Errorf("festival logos: get all: %w", err)
	}
	defer rows.Close()

	var results []*FestivalLogo
	for rows.Next() {
		f := &FestivalLogo{}
		if err := rows.Scan(&f.ID, &f.Name, &f.Slug, &f.ImageURLNoBg, &f.IsActive, &f.DisplayOrder); err != nil {
			return nil, fmt.Errorf("festival logos: scan row: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// GetByID returns a single festival logo by UUID.
func (r *Repository) GetByID(ctx context.Context, id string) (*FestivalLogo, error) {
	row := r.pool.QueryRow(ctx, selectCols+` WHERE id = $1`, id)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: get by id: %w", err)
	}
	return f, nil
}

// GetBySlug returns a single festival logo by slug.
func (r *Repository) GetBySlug(ctx context.Context, slug string) (*FestivalLogo, error) {
	row := r.pool.QueryRow(ctx, selectCols+` WHERE slug = $1`, slug)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: get by slug: %w", err)
	}
	return f, nil
}

// Create inserts a new festival logo row and returns the created entity.
func (r *Repository) Create(ctx context.Context, p CreateParams) (*FestivalLogo, error) {
	const q = `
		INSERT INTO festival_logos (name, slug, display_order)
		VALUES ($1, $2, $3)
		RETURNING id, name, slug, image_url_no_bg, is_active, display_order
	`
	row := r.pool.QueryRow(ctx, q, p.Name, p.Slug, p.DisplayOrder)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: create: %w", err)
	}
	return f, nil
}

// Upsert inserts or updates a festival logo row by slug (idempotent seed).
func (r *Repository) Upsert(ctx context.Context, p CreateParams) (*FestivalLogo, error) {
	const q = `
		INSERT INTO festival_logos (name, slug, display_order)
		VALUES ($1, $2, $3)
		ON CONFLICT (slug) DO UPDATE
			SET name = EXCLUDED.name, display_order = EXCLUDED.display_order, updated_at = now()
		RETURNING id, name, slug, image_url_no_bg, is_active, display_order
	`
	row := r.pool.QueryRow(ctx, q, p.Name, p.Slug, p.DisplayOrder)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: upsert: %w", err)
	}
	return f, nil
}

// UpdateImageURL sets image URL column for a festival logo.
func (r *Repository) UpdateImageURL(ctx context.Context, id string, imageURLNoBg *string) (*FestivalLogo, error) {
	const q = `
		UPDATE festival_logos
		SET image_url_no_bg = COALESCE($2, image_url_no_bg),
		    updated_at      = now()
		WHERE id = $1
		RETURNING id, name, slug, image_url_no_bg, is_active, display_order
	`
	row := r.pool.QueryRow(ctx, q, id, imageURLNoBg)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: update image url: %w", err)
	}
	return f, nil
}

// UpdateImageURLBySlug sets image URL column identified by slug (used during seed).
func (r *Repository) UpdateImageURLBySlug(ctx context.Context, slug string, imageURLNoBg *string) (*FestivalLogo, error) {
	const q = `
		UPDATE festival_logos
		SET image_url_no_bg = COALESCE($2, image_url_no_bg),
		    updated_at      = now()
		WHERE slug = $1
		RETURNING id, name, slug, image_url_no_bg, is_active, display_order
	`
	row := r.pool.QueryRow(ctx, q, slug, imageURLNoBg)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: update image url by slug: %w", err)
	}
	return f, nil
}

// Update modifies name, is_active, and display_order for a festival logo.
func (r *Repository) Update(ctx context.Context, id string, p UpdateParams) (*FestivalLogo, error) {
	const q = `
		UPDATE festival_logos
		SET name          = $2,
		    is_active     = $3,
		    display_order = $4,
		    updated_at    = now()
		WHERE id = $1
		RETURNING id, name, slug, image_url_no_bg, is_active, display_order
	`
	row := r.pool.QueryRow(ctx, q, id, p.Name, p.IsActive, p.DisplayOrder)
	f, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("festival logos: update: %w", err)
	}
	return f, nil
}

// Delete removes a festival logo by UUID.
func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM festival_logos WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("festival logos: delete: %w", err)
	}
	return nil
}

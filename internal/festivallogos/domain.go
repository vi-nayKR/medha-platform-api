package festivallogos

// FestivalLogo represents a festival image entry in the DB.
type FestivalLogo struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	ImageURLNoBg *string `json:"image_url_no_bg"`
	IsActive     bool    `json:"is_active"`
	DisplayOrder int     `json:"display_order"`
}

// CreateParams holds the fields for inserting a new festival logo entry.
type CreateParams struct {
	Name         string
	Slug         string
	DisplayOrder int
}

// UpdateParams holds updatable fields for a festival logo entry.
type UpdateParams struct {
	Name         string
	IsActive     bool
	DisplayOrder int
}

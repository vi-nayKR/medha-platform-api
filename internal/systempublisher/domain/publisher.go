package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// SystemPublisher is a platform-controlled content author with no
// authentication path of its own — distinct from a human user row in the
// users table. Looked up by its stable PublisherKey, never a hardcoded ID.
type SystemPublisher struct {
	ID           uuid.UUID
	PublisherKey string
	DisplayName  string
	Username     string
	AvatarURL    string
	Description  string
	IsVerified   bool
	IsActive     bool
	CreatedAt    int64
	UpdatedAt    int64
}

// MedhaAppPublisherKey is the stable key for the official Medha App
// publisher, seeded by migration 00047.
const MedhaAppPublisherKey = "medha-app"

// Sentinel errors for system publisher operations.
var (
	ErrPublisherNotFound = errors.New("system publisher not found")
)

// SystemPublisherRepository defines the port for system publisher data access.
type SystemPublisherRepository interface {
	GetByKey(ctx context.Context, publisherKey string) (*SystemPublisher, error)
	GetByID(ctx context.Context, id uuid.UUID) (*SystemPublisher, error)
}

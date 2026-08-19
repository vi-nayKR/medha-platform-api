package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/medha/backend/internal/systempublisher/domain"
)

// SystemPublisherService resolves system publishers by their stable key.
// The medha-app publisher is looked up once and cached: it's seeded once by
// migration and never changes identity while the process is running.
type SystemPublisherService struct {
	repo domain.SystemPublisherRepository

	mu    sync.RWMutex
	cache map[string]*domain.SystemPublisher
}

// NewSystemPublisherService creates a new SystemPublisherService.
func NewSystemPublisherService(repo domain.SystemPublisherRepository) *SystemPublisherService {
	return &SystemPublisherService{
		repo:  repo,
		cache: make(map[string]*domain.SystemPublisher),
	}
}

// GetByKey resolves a publisher by its stable key, e.g. "medha-app".
func (s *SystemPublisherService) GetByKey(ctx context.Context, publisherKey string) (*domain.SystemPublisher, error) {
	s.mu.RLock()
	if p, ok := s.cache[publisherKey]; ok {
		s.mu.RUnlock()
		return p, nil
	}
	s.mu.RUnlock()

	p, err := s.repo.GetByKey(ctx, publisherKey)
	if err != nil {
		return nil, fmt.Errorf("get publisher by key: %w", err)
	}

	s.mu.Lock()
	s.cache[publisherKey] = p
	s.mu.Unlock()

	return p, nil
}

// MedhaApp resolves the official Medha App publisher.
func (s *SystemPublisherService) MedhaApp(ctx context.Context) (*domain.SystemPublisher, error) {
	return s.GetByKey(ctx, domain.MedhaAppPublisherKey)
}

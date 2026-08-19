package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/medha/backend/internal/panchanga/domain"
)

// PanchangaService provides business logic for panchanga and festival operations.
type PanchangaService struct {
	repo  domain.PanchangaRepository
	cache sync.Map // cache for single-day panchanga lookups (key: int64 date epoch, value: *domain.Panchanga)
}

// NewPanchangaService creates a new PanchangaService.
func NewPanchangaService(repo domain.PanchangaRepository) *PanchangaService {
	return &PanchangaService{
		repo: repo,
	}
}

// GetTodayPanchanga returns today's panchanga (IST date).
func (s *PanchangaService) GetTodayPanchanga(ctx context.Context) (*domain.Panchanga, error) {
	ist := time.FixedZone("IST", 5*3600+1800)
	now := time.Now().In(ist)
	todayEpoch := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ist).Unix()

	if val, ok := s.cache.Load(todayEpoch); ok {
		if p, ok := val.(*domain.Panchanga); ok {
			return p, nil
		}
	}

	p, err := s.repo.GetByDate(ctx, todayEpoch)
	if err != nil {
		return nil, fmt.Errorf("get today panchanga: %w", err)
	}

	s.cache.Store(todayEpoch, p)
	return p, nil
}

// GetPanchangaByDate returns the panchanga for a date string "YYYY-MM-DD".
func (s *PanchangaService) GetPanchangaByDate(ctx context.Context, dateStr string) (*domain.Panchanga, error) {
	ist := time.FixedZone("IST", 5*3600+1800)
	t, err := time.ParseInLocation("2006-01-02", dateStr, ist)
	if err != nil {
		return nil, fmt.Errorf("invalid date format, use YYYY-MM-DD: %w", err)
	}

	epochVal := t.Unix()
	if val, ok := s.cache.Load(epochVal); ok {
		if p, ok := val.(*domain.Panchanga); ok {
			return p, nil
		}
	}

	p, err := s.repo.GetByDate(ctx, epochVal)
	if err != nil {
		return nil, fmt.Errorf("get panchanga by date: %w", err)
	}

	s.cache.Store(epochVal, p)
	return p, nil
}

// GetPanchangaByRange returns all panchanga records between two date strings (inclusive).
func (s *PanchangaService) GetPanchangaByRange(ctx context.Context, fromStr, toStr string) ([]*domain.Panchanga, error) {
	ist := time.FixedZone("IST", 5*3600+1800)
	from, err := time.ParseInLocation("2006-01-02", fromStr, ist)
	if err != nil {
		return nil, fmt.Errorf("invalid from date format, use YYYY-MM-DD: %w", err)
	}
	to, err := time.ParseInLocation("2006-01-02", toStr, ist)
	if err != nil {
		return nil, fmt.Errorf("invalid to date format, use YYYY-MM-DD: %w", err)
	}
	if to.Before(from) {
		return nil, fmt.Errorf("to date must be on or after from date")
	}
	if to.Sub(from) > 366*24*time.Hour {
		return nil, fmt.Errorf("date range cannot exceed 366 days")
	}
	return s.repo.GetByDateRange(ctx, from.Unix(), to.Unix())
}

// GetUpcomingFestivals returns upcoming festivals from today, up to limit entries.
func (s *PanchangaService) GetUpcomingFestivals(ctx context.Context, limit int) ([]*domain.Festival, error) {
	ist := time.FixedZone("IST", 5*3600+1800)
	now := time.Now().In(ist)
	todayEpoch := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ist).Unix()
	return s.repo.GetUpcomingFestivals(ctx, todayEpoch, limit)
}

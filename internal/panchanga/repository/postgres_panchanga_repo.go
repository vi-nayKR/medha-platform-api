package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/medha/backend/internal/panchanga/domain"
)

// PostgresPanchangaRepository implements domain.PanchangaRepository using pgxpool.
type PostgresPanchangaRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPanchangaRepository creates a new PostgresPanchangaRepository.
func NewPostgresPanchangaRepository(pool *pgxpool.Pool) *PostgresPanchangaRepository {
	return &PostgresPanchangaRepository{pool: pool}
}

const selectPanchanga = `
	SELECT id, date, samvatsara, ayana, rutu, masa, paksha, tithi, nakshatra,
	       yoga, karana, vasara, shraddha_tithi, masa_niyamaka, festivals_events,
	       sunrise, sunset, rahukala, gulikala, yamaganda
	FROM panchanga
`

func scanPanchangaRow(row pgx.Row) (*domain.Panchanga, error) {
	p := &domain.Panchanga{}
	err := row.Scan(
		&p.ID, &p.Date, &p.Samvatsara, &p.Ayana, &p.Rutu, &p.Masa, &p.Paksha,
		&p.Tithi, &p.Nakshatra, &p.Yoga, &p.Karana, &p.Vasara,
		&p.ShraddhaThithi, &p.MasaNiyamaka, &p.FestivalsEvents,
		&p.Sunrise, &p.Sunset, &p.Rahukala, &p.Gulikala, &p.Yamaganda,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func scanPanchangaRows(rows pgx.Rows) ([]*domain.Panchanga, error) {
	var results []*domain.Panchanga
	for rows.Next() {
		p := &domain.Panchanga{}
		if err := rows.Scan(
			&p.ID, &p.Date, &p.Samvatsara, &p.Ayana, &p.Rutu, &p.Masa, &p.Paksha,
			&p.Tithi, &p.Nakshatra, &p.Yoga, &p.Karana, &p.Vasara,
			&p.ShraddhaThithi, &p.MasaNiyamaka, &p.FestivalsEvents,
			&p.Sunrise, &p.Sunset, &p.Rahukala, &p.Gulikala, &p.Yamaganda,
		); err != nil {
			return nil, fmt.Errorf("scan panchanga row: %w", err)
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// GetByDate returns the Panchanga record for a specific date (epoch).
func (r *PostgresPanchangaRepository) GetByDate(ctx context.Context, dateEpoch int64) (*domain.Panchanga, error) {
	q := selectPanchanga + ` WHERE date = $1`
	row := r.pool.QueryRow(ctx, q, dateEpoch)
	return scanPanchangaRow(row)
}

// GetByDateRange returns Panchanga records for an inclusive date range.
func (r *PostgresPanchangaRepository) GetByDateRange(ctx context.Context, fromEpoch, toEpoch int64) ([]*domain.Panchanga, error) {
	q := selectPanchanga + ` WHERE date >= $1 AND date <= $2 ORDER BY date ASC`
	rows, err := r.pool.Query(ctx, q, fromEpoch, toEpoch)
	if err != nil {
		return nil, fmt.Errorf("panchanga range query: %w", err)
	}
	defer rows.Close()
	return scanPanchangaRows(rows)
}

// GetUpcomingFestivals returns all festivals (ignoring fromEpoch to allow clients to see past/future), ordered by date, up to limit.
func (r *PostgresPanchangaRepository) GetUpcomingFestivals(ctx context.Context, fromEpoch int64, limit int) ([]*domain.Festival, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, festival, date, description FROM festivals ORDER BY date ASC LIMIT $1`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("festivals query: %w", err)
	}
	defer rows.Close()

	var festivals []*domain.Festival
	for rows.Next() {
		f := &domain.Festival{}
		if err := rows.Scan(&f.ID, &f.Festival, &f.Date, &f.Description); err != nil {
			return nil, fmt.Errorf("scan festival row: %w", err)
		}
		festivals = append(festivals, f)
	}
	return festivals, rows.Err()
}

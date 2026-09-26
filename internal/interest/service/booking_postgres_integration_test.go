package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	eventdomain "github.com/medha/backend/internal/event/domain"
	eventrepo "github.com/medha/backend/internal/event/repository"
	interestdomain "github.com/medha/backend/internal/interest/domain"
	interestrepo "github.com/medha/backend/internal/interest/repository"
)

type bookingReadBarrier struct {
	interestdomain.InterestRepository
	mu      sync.Mutex
	reads   int
	release chan struct{}
}

func (r *bookingReadBarrier) GetByID(ctx context.Context, id uuid.UUID) (*interestdomain.Interest, error) {
	r.mu.Lock()
	r.reads++
	wait := r.reads <= 2
	if r.reads == 2 {
		close(r.release)
	}
	r.mu.Unlock()
	if wait {
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.InterestRepository.GetByID(ctx, id)
}

type bookingEventRepository struct {
	eventdomain.EventRepository
	pool *pgxpool.Pool
}

func (r bookingEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*eventdomain.Event, error) {
	var event eventdomain.Event
	err := r.pool.QueryRow(ctx, `
		SELECT id, yajman_id, status FROM events WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&event.ID, &event.YajmanID, &event.Status)
	if err == pgx.ErrNoRows {
		return nil, eventdomain.ErrEventNotFound
	}
	return &event, err
}

func TestConfirmBookingPostgresConcurrencyAndRollback(t *testing.T) {
	dsn := os.Getenv("MEDHA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MEDHA_TEST_DATABASE_URL to run PostgreSQL booking integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "booking_test_" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		admin.Close()
	})
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE events (
			id uuid PRIMARY KEY,
			yajman_id uuid NOT NULL,
			status text NOT NULL,
			deleted_at bigint
		);
		CREATE TABLE interests (
			id uuid PRIMARY KEY,
			pandit_id uuid NOT NULL,
			event_id uuid NOT NULL REFERENCES events(id),
			message text,
			status text NOT NULL,
			created_at bigint NOT NULL DEFAULT 1,
			updated_at bigint NOT NULL DEFAULT 1
		);
	`)
	if err != nil {
		t.Fatal(err)
	}

	baseInterestRepo := interestrepo.NewPostgresInterestRepository(pool)
	baseEventRepo := eventrepo.NewPostgresEventRepository(pool)
	eventRepo := bookingEventRepository{EventRepository: baseEventRepo, pool: pool}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Both callers pass the initial connected-state read before either begins its transaction.
	yajmanID, eventID := uuid.New(), uuid.New()
	interestA, interestB := uuid.New(), uuid.New()
	panditA, panditB := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO events (id, yajman_id, status) VALUES ($1,$2,'Active')`, eventID, yajmanID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, pandit uuid.UUID }{{interestA, panditA}, {interestB, panditB}} {
		if _, err := pool.Exec(ctx, `INSERT INTO interests (id, pandit_id, event_id, status) VALUES ($1,$2,$3,'connected')`, row.id, row.pandit, eventID); err != nil {
			t.Fatal(err)
		}
	}
	barrierRepo := &bookingReadBarrier{InterestRepository: baseInterestRepo, release: make(chan struct{})}
	svc := NewInterestService(pool, barrierRepo, eventRepo, nil, nil, logger)
	results := make(chan error, 2)
	for _, id := range []uuid.UUID{interestA, interestB} {
		go func(id uuid.UUID) {
			_, err := svc.ConfirmBooking(ctx, id, yajmanID)
			results <- err
		}(id)
	}
	first, second := <-results, <-results
	successes := 0
	for _, err := range []error{first, second} {
		if err == nil {
			successes++
		} else if err != eventdomain.ErrEventNotActive && err != interestdomain.ErrInterestNotConnected {
			t.Fatalf("unexpected competing booking error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one booking to succeed, got %d (%v, %v)", successes, first, second)
	}
	var accepted, rejected int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status='accepted'), COUNT(*) FILTER (WHERE status='rejected') FROM interests WHERE event_id=$1`, eventID).Scan(&accepted, &rejected); err != nil {
		t.Fatal(err)
	}
	var eventStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM events WHERE id=$1`, eventID).Scan(&eventStatus); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || rejected != 1 || eventStatus != string(eventdomain.EventStatusBooked) {
		t.Fatalf("booking invariant failed: accepted=%d rejected=%d event=%s", accepted, rejected, eventStatus)
	}

	// A real PostgreSQL trigger failure after interest writes must roll the whole transaction back.
	rollbackEvent, rollbackA, rollbackB := uuid.New(), uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO events (id, yajman_id, status) VALUES ($1,$2,'Active')`, rollbackEvent, yajmanID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, pandit uuid.UUID }{{rollbackA, uuid.New()}, {rollbackB, uuid.New()}} {
		if _, err := pool.Exec(ctx, `INSERT INTO interests (id, pandit_id, event_id, status) VALUES ($1,$2,$3,'connected')`, row.id, row.pandit, rollbackEvent); err != nil {
			t.Fatal(err)
		}
	}
	functionName := fmt.Sprintf("reject_booked_%s", schema)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.status = 'Booked' THEN RAISE EXCEPTION 'forced booking failure'; END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER reject_booking BEFORE UPDATE ON events FOR EACH ROW EXECUTE FUNCTION %s();
	`, functionName, functionName)); err != nil {
		t.Fatal(err)
	}
	svc = NewInterestService(pool, baseInterestRepo, eventRepo, nil, nil, logger)
	if _, err := svc.ConfirmBooking(ctx, rollbackA, yajmanID); err == nil {
		t.Fatal("expected forced PostgreSQL event update failure")
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM events WHERE id=$1`, rollbackEvent).Scan(&eventStatus); err != nil {
		t.Fatal(err)
	}
	if eventStatus != string(eventdomain.EventStatusActive) {
		t.Fatalf("event status changed despite rollback: %s", eventStatus)
	}
	var connected int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM interests WHERE event_id=$1 AND status='connected'`, rollbackEvent).Scan(&connected); err != nil {
		t.Fatal(err)
	}
	if connected != 2 {
		t.Fatalf("interest updates were not rolled back, connected=%d", connected)
	}
}

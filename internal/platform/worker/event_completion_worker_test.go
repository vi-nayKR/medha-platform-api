package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockEventRepo struct {
	mu           sync.Mutex
	calls        int
	retEvents    []uuid.UUID
	err          error
	calledSignal chan struct{}
}

func (m *mockEventRepo) MarkPastEventsCompleted(ctx context.Context) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calledSignal != nil {
		select {
		case m.calledSignal <- struct{}{}:
		default:
		}
	}
	return m.retEvents, m.err
}

type mockMatchRepo struct {
	mu        sync.Mutex
	calls     int
	retCount  int64
	err       error
	gotEventIDs []uuid.UUID
}

func (m *mockMatchRepo) MarkMatchesCompletedForEvents(ctx context.Context, eventIDs []uuid.UUID) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.gotEventIDs = eventIDs
	return m.retCount, m.err
}

func TestEventCompletionWorker_Run_Success(t *testing.T) {
	eventID1 := uuid.New()
	eventID2 := uuid.New()

	eventRepo := &mockEventRepo{
		retEvents: []uuid.UUID{eventID1, eventID2},
	}
	matchRepo := &mockMatchRepo{
		retCount: 2,
	}

	w := NewEventCompletionWorker(eventRepo, matchRepo, 1*time.Hour, slog.Default())
	w.run(context.Background())

	if eventRepo.calls != 1 {
		t.Errorf("expected 1 call to MarkPastEventsCompleted, got %d", eventRepo.calls)
	}
	if matchRepo.calls != 1 {
		t.Errorf("expected 1 call to MarkMatchesCompletedForEvents, got %d", matchRepo.calls)
	}
	if len(matchRepo.gotEventIDs) != 2 || matchRepo.gotEventIDs[0] != eventID1 || matchRepo.gotEventIDs[1] != eventID2 {
		t.Errorf("match repo received unexpected event IDs: %v", matchRepo.gotEventIDs)
	}
}

func TestEventCompletionWorker_Run_NoEvents(t *testing.T) {
	eventRepo := &mockEventRepo{
		retEvents: []uuid.UUID{},
	}
	matchRepo := &mockMatchRepo{}

	w := NewEventCompletionWorker(eventRepo, matchRepo, 1*time.Hour, slog.Default())
	w.run(context.Background())

	if eventRepo.calls != 1 {
		t.Errorf("expected 1 call to MarkPastEventsCompleted, got %d", eventRepo.calls)
	}
	if matchRepo.calls != 0 {
		t.Errorf("expected 0 calls to MarkMatchesCompletedForEvents when no events returned, got %d", matchRepo.calls)
	}
}

func TestEventCompletionWorker_Run_EventRepoError(t *testing.T) {
	eventRepo := &mockEventRepo{
		err: errors.New("db error"),
	}
	matchRepo := &mockMatchRepo{}

	w := NewEventCompletionWorker(eventRepo, matchRepo, 1*time.Hour, slog.Default())
	w.run(context.Background())

	if eventRepo.calls != 1 {
		t.Errorf("expected 1 call to MarkPastEventsCompleted, got %d", eventRepo.calls)
	}
	if matchRepo.calls != 0 {
		t.Errorf("expected 0 calls to MarkMatchesCompletedForEvents when event repo fails, got %d", matchRepo.calls)
	}
}

func TestEventCompletionWorker_Run_MatchRepoError(t *testing.T) {
	eventID1 := uuid.New()
	eventRepo := &mockEventRepo{
		retEvents: []uuid.UUID{eventID1},
	}
	matchRepo := &mockMatchRepo{
		err: errors.New("match db error"),
	}

	w := NewEventCompletionWorker(eventRepo, matchRepo, 1*time.Hour, slog.Default())
	w.run(context.Background())

	if eventRepo.calls != 1 {
		t.Errorf("expected 1 call to MarkPastEventsCompleted, got %d", eventRepo.calls)
	}
	if matchRepo.calls != 1 {
		t.Errorf("expected 1 call to MarkMatchesCompletedForEvents, got %d", matchRepo.calls)
	}
}

func TestEventCompletionWorker_StartGracefulShutdown(t *testing.T) {
	eventRepo := &mockEventRepo{
		calledSignal: make(chan struct{}, 10),
	}
	matchRepo := &mockMatchRepo{}

	// Run with a very short interval so it triggers, but we will cancel the context
	w := NewEventCompletionWorker(eventRepo, matchRepo, 10*time.Millisecond, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	
	// Start worker in a separate goroutine
	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	// Wait for the first run (immediate execution)
	select {
	case <-eventRepo.calledSignal:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for worker first execution")
	}

	// Now cancel the context to trigger stop
	cancel()

	// Worker should stop and exit Start
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for worker to stop after cancel")
	}
}

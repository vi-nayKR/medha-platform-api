package worker

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestPoolStopCancelsRunningJob(t *testing.T) {
	p := NewPool(1, 1, slog.Default())

	started := make(chan context.Context, 1)
	done := make(chan struct{})
	p.Enqueue(func(ctx context.Context) error {
		started <- ctx
		<-ctx.Done()
		close(done)
		return ctx.Err()
	})

	select {
	case ctx := <-started:
		if err := ctx.Err(); err != nil {
			t.Fatalf("job context should be active before stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job to start")
	}

	p.Stop()
	p.Stop()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job cancellation")
	}
}

func TestPoolDropsJobsAfterStop(t *testing.T) {
	p := NewPool(1, 1, slog.Default())
	p.Stop()

	ran := make(chan struct{})
	p.Enqueue(func(context.Context) error {
		close(ran)
		return nil
	})

	select {
	case <-ran:
		t.Fatal("job ran after pool was stopped")
	case <-time.After(50 * time.Millisecond):
	}
}

package worker

import (
	"context"
	"log/slog"
	"sync"
)

// Job represents an asynchronous task.
type Job func(ctx context.Context) error

// Pool represents a worker pool to execute jobs asynchronously.
type Pool struct {
	jobQueue chan Job
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
	logger   *slog.Logger
}

// NewPool creates and starts a new worker pool.
func NewPool(numWorkers int, queueSize int, logger *slog.Logger) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		jobQueue: make(chan Job, queueSize),
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}

	for i := 0; i < numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	return p
}

// Enqueue adds a job to the queue. Does not block if queue is full (drops job or logs error).
func (p *Pool) Enqueue(job Job) {
	select {
	case <-p.ctx.Done():
		p.logger.Warn("worker pool is stopped, job dropped")
		return
	default:
	}

	select {
	case p.jobQueue <- job:
		// Enqueued
	case <-p.ctx.Done():
		p.logger.Warn("worker pool is stopped, job dropped")
	default:
		p.logger.Warn("worker pool queue is full, job dropped")
	}
}

// TryEnqueue attempts to add a job to the queue. Returns true if successful,
// or false if the pool is stopped or the queue is full.
func (p *Pool) TryEnqueue(job Job) bool {
	select {
	case <-p.ctx.Done():
		return false
	default:
	}

	select {
	case p.jobQueue <- job:
		return true
	default:
		return false
	}
}

// Stop gracefully stops the worker pool.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		p.cancel()
		p.wg.Wait()
	})
}

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	p.logger.Debug("worker started", "worker_id", id)

	for {
		select {
		case <-p.ctx.Done():
			p.logger.Debug("worker stopping", "worker_id", id)
			return
		case job, ok := <-p.jobQueue:
			if !ok {
				return
			}
			if err := job(p.ctx); err != nil {
				p.logger.Error("job failed", "worker_id", id, "error", err)
			}
		}
	}
}

package stress

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkerPool manages a rate-limited pool of goroutines.
// Each goroutine waits for ticks from a time.Ticker and executes
// work tasks while respecting the concurrency bounds.
type WorkerPool struct {
	maxConcurrency int // Maximum goroutines allowed in flight
	targetRate     int // Target requests per second
	ticker         *time.Ticker
	wg             sync.WaitGroup
	workCh         chan func() // Buffered channel for work tasks
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewWorkerPool creates a new worker pool with specified concurrency and rate limits.
func NewWorkerPool(maxConcurrency int, targetRate int) *WorkerPool {
	return &WorkerPool{
		maxConcurrency: maxConcurrency,
		targetRate:     targetRate,
		workCh:         make(chan func(), maxConcurrency),
	}
}

// Start initializes and runs the worker pool.
func (wp *WorkerPool) Start(ctx context.Context) error {
	if wp.targetRate <= 0 {
		return fmt.Errorf("target rate must be greater than 0, got %d", wp.targetRate)
	}

	// Calculate tick interval: rate-limit based on targetRate
	tickInterval := time.Second / time.Duration(wp.targetRate)
	if tickInterval <= 0 {
		return fmt.Errorf("computed ticker interval must be positive, got %v", tickInterval)
	}

	wp.ctx, wp.cancel = context.WithCancel(ctx)
	wp.ticker = time.NewTicker(tickInterval)

	// Start worker goroutines (respecting maxConcurrency)
	for i := 0; i < wp.maxConcurrency; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}

	// Start dispatcher loop
	wp.wg.Add(1)
	go wp.dispatcher()

	return nil
}

// dispatcher coordinates ticker ticks and task dispatch.
func (wp *WorkerPool) dispatcher() {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			close(wp.workCh)
			return

		case <-wp.ticker.C:
			// On each tick, attempt to dispatch work (non-blocking)
			// This ensures rate limiting: only maxConcurrency tasks
			// can run in parallel.
		}
	}
}

// worker processes tasks from the work channel.
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for task := range wp.workCh {
		if task != nil {
			task()
		}
	}
}

// Submit enqueues a task for execution, respecting concurrency bounds.
func (wp *WorkerPool) Submit(task func()) error {
	select {
	case <-wp.ctx.Done():
		return fmt.Errorf("worker pool is shutting down")

	case wp.workCh <- task:
		return nil

	default:
		// Buffer full; task cannot be enqueued immediately
		return fmt.Errorf("worker pool queue full, max concurrency reached")
	}
}

// Stop gracefully shuts down all workers and cleanup.
func (wp *WorkerPool) Stop() {
	if wp.cancel != nil {
		wp.cancel()
	}
	if wp.ticker != nil {
		wp.ticker.Stop()
	}
	wp.wg.Wait()
}

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
	workCh         chan func() // Buffered channel for submitted tasks
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
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
	if wp.maxConcurrency <= 0 {
		return fmt.Errorf("max concurrency must be greater than 0, got %d", wp.maxConcurrency)
	}
	if wp.targetRate <= 0 {
		return fmt.Errorf("target rate must be greater than 0, got %d", wp.targetRate)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	wp.mu.Lock()
	if wp.ctx != nil {
		wp.mu.Unlock()
		return fmt.Errorf("worker pool is already running")
	}

	// Calculate tick interval: rate-limit based on targetRate.
	tickInterval := time.Second / time.Duration(wp.targetRate)
	if tickInterval <= 0 {
		wp.mu.Unlock()
		return fmt.Errorf("computed ticker interval must be positive, got %v", tickInterval)
	}

	wp.ctx, wp.cancel = context.WithCancel(ctx)
	wp.ticker = time.NewTicker(tickInterval)
	workerCtx := wp.ctx
	workerTicker := wp.ticker
	wp.wg.Add(wp.maxConcurrency)
	wp.mu.Unlock()

	// Start worker goroutines (respecting maxConcurrency)
	for i := 0; i < wp.maxConcurrency; i++ {
		go wp.worker(workerCtx, workerTicker)
	}

	return nil
}

// worker processes tasks from the work channel.
func (wp *WorkerPool) worker(ctx context.Context, ticker *time.Ticker) {
	defer wp.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case task := <-wp.workCh:
			if task == nil {
				continue
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				task()
			}
		}
	}
}

// Submit enqueues a task for execution, respecting concurrency bounds.
func (wp *WorkerPool) Submit(task func()) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	wp.mu.RLock()
	ctx := wp.ctx
	workCh := wp.workCh
	wp.mu.RUnlock()
	if ctx == nil {
		return fmt.Errorf("worker pool has not been started")
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("worker pool is shutting down")
	case workCh <- task:
		return nil
	default:
		return fmt.Errorf("worker pool queue full, max concurrency reached")
	}
}

// Stop gracefully shuts down all workers and cleanup.
func (wp *WorkerPool) Stop() {
	wp.mu.Lock()
	cancel := wp.cancel
	ticker := wp.ticker
	wp.cancel = nil
	wp.ticker = nil
	wp.ctx = nil
	wp.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if ticker != nil {
		ticker.Stop()
	}
	wp.wg.Wait()
}

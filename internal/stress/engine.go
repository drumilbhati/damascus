package stress

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StressEngine defines the interface contract for executing controlled load experiments.
type StressEngine interface {
	// Start begins generating traffic according to the provided LoadPlan.
	// It runs until the context is cancelled or the test is stopped.
	Start(ctx context.Context, plan LoadPlan) error

	// Stop terminates all running worker goroutines immediately.
	Stop()

	// GetStats retrieves real-time execution statistics (total requests, successes, errors).
	GetStats() Stats
}

// Stats captures live traffic generation metrics.
type Stats struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	ErrorCount    int64 `json:"error_count"`
	DroppedCount  int64 `json:"dropped_count"`
}

// HTTPStressEngine is a high-performance, rate-regulated HTTP load generation engine.
type HTTPStressEngine struct {
	client      *http.Client
	workerCount int
	mu          sync.RWMutex
	cancelFunc  context.CancelFunc
	running     bool

	// Atomic telemetry counters for lock-free, high-concurrency stats tracking
	totalRequests int64
	successCount  int64
	errorCount    int64
	droppedCount  int64
}

// NewHTTPStressEngine creates a new HTTP load generator.
// If client is nil, a high-throughput client with optimized connection pooling is used.
func NewHTTPStressEngine(client *http.Client, workerCount int) *HTTPStressEngine {
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        1000,
				MaxIdleConnsPerHost: 500,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		}
	}
	if workerCount <= 0 {
		workerCount = 100 // Default concurrency pool size
	}

	return &HTTPStressEngine{
		client:      client,
		workerCount: workerCount,
	}
}

// Start executes the stepped load plan. It continuously generates requests at the configured rate,
// increasing by StepRate every StepDurationSeconds until MaxRate is reached, or until ctx is cancelled.
func (e *HTTPStressEngine) Start(ctx context.Context, plan LoadPlan) error {
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("invalid load plan: %w", err)
	}

	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("stress engine is already running an active experiment")
	}
	e.running = true

	// Create child context for fast-path cancellation
	runCtx, cancel := context.WithCancel(ctx)
	e.cancelFunc = cancel

	// Reset counters for the new run
	atomic.StoreInt64(&e.totalRequests, 0)
	atomic.StoreInt64(&e.successCount, 0)
	atomic.StoreInt64(&e.errorCount, 0)
	atomic.StoreInt64(&e.droppedCount, 0)
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.running = false
		e.cancelFunc = nil
		e.mu.Unlock()
	}()

	// Buffer the work channel to handle short bursts without stalling rate generation
	workChan := make(chan struct{}, e.workerCount*2)
	var wg sync.WaitGroup

	// 1. Launch Worker Goroutines (Worker Pool)
	for i := 0; i < e.workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return // Immediate exit on context cancellation
				case _, ok := <-workChan:
					if !ok {
						return // Work channel closed
					}
					e.executeRequest(runCtx, plan)
				}
			}
		}()
	}

	// 2. Dispatch Loop with Step Rate Scheduling
	currentRate := plan.InitialRate
	stepTicker := time.NewTicker(time.Duration(plan.StepDurationSeconds) * time.Second)
	defer stepTicker.Stop()

	// 10ms resolution tick loop (100 ticks/sec) provides high precision across Windows & Linux
	const tickFrequency = 100
	tickDuration := time.Second / tickFrequency
	rateTicker := time.NewTicker(tickDuration)
	defer rateTicker.Stop()

	tokensPerTick := float64(currentRate) / float64(tickFrequency)
	var tokenAccumulator float64

	for {
		select {
		case <-runCtx.Done():
			// Fast-path safety stop: close channel and wait for workers to drain
			close(workChan)
			wg.Wait()
			return nil

		case <-stepTicker.C:
			// Step rate progression
			if currentRate < plan.MaxRate {
				if plan.StepRate > plan.MaxRate-currentRate {
					currentRate = plan.MaxRate
				} else {
					currentRate += plan.StepRate
				}
				tokensPerTick = float64(currentRate) / float64(tickFrequency)
			}

		case <-rateTicker.C:
			tokenAccumulator += tokensPerTick
			tokensToSend := int(tokenAccumulator)
			tokenAccumulator -= float64(tokensToSend)

			for i := 0; i < tokensToSend; i++ {
				select {
				case workChan <- struct{}{}:
				case <-runCtx.Done():
					close(workChan)
					wg.Wait()
					return nil
				default:
					// Worker pool saturated (all workers busy); count dropped dispatch
					atomic.AddInt64(&e.droppedCount, 1)
				}
			}
		}
	}
}

// Stop halts all active load generation immediately.
func (e *HTTPStressEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelFunc != nil {
		e.cancelFunc()
	}
}

// GetStats returns a point-in-time snapshot of generation metrics.
func (e *HTTPStressEngine) GetStats() Stats {
	return Stats{
		TotalRequests: atomic.LoadInt64(&e.totalRequests),
		SuccessCount:  atomic.LoadInt64(&e.successCount),
		ErrorCount:    atomic.LoadInt64(&e.errorCount),
		DroppedCount:  atomic.LoadInt64(&e.droppedCount),
	}
}

// executeRequest constructs and dispatches a single HTTP request according to the LoadPlan.
func (e *HTTPStressEngine) executeRequest(ctx context.Context, plan LoadPlan) {
	method := plan.Method
	if method == "" {
		method = http.MethodGet
	}

	var bodyReader io.Reader
	if plan.Payload != "" {
		bodyReader = strings.NewReader(plan.Payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, plan.TargetURL, bodyReader)
	if err != nil {
		atomic.AddInt64(&e.totalRequests, 1)
		atomic.AddInt64(&e.errorCount, 1)
		return
	}

	if plan.Payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "DAMASCUS-StressEngine/1.0")

	resp, err := e.client.Do(req)
	atomic.AddInt64(&e.totalRequests, 1)

	if err != nil {
		if ctx.Err() != nil {
			// Request was terminated due to context cancellation or shutdown
			return
		}
		atomic.AddInt64(&e.errorCount, 1)
		return
	}
	defer resp.Body.Close()

	// Drain response body to enable TCP connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		atomic.AddInt64(&e.errorCount, 1)
	} else {
		atomic.AddInt64(&e.successCount, 1)
	}
}

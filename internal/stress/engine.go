package stress

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Engine implements the interfaces.StressEngine contract.
type Engine struct {
	client  *http.Client
	cancel  context.CancelFunc
	running bool
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// NewEngine initializes a new stress engine instance.
func NewEngine(client *http.Client) *Engine {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Engine{
		client: client,
	}
}

// Start initiates the rate-controlled traffic generation.
func (e *Engine) Start(ctx context.Context, plan LoadPlan) error {
	if plan.InitialRate <= 0 {
		return fmt.Errorf("initial rate must be greater than 0, got %d", plan.InitialRate)
	}
	if plan.InitialRate > 1_000_000_000 {
		return fmt.Errorf("initial rate exceeds maximum supported limit of 1,000,000,000 req/s, got %d", plan.InitialRate)
	}

	interval := time.Second / time.Duration(plan.InitialRate)
	if interval <= 0 {
		return fmt.Errorf("computed ticker interval must be positive, got %v", interval)
	}

	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("stress engine is already running an active load plan")
	}
	e.running = true
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.running = false
		e.cancel = nil
		e.mu.Unlock()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.wg.Wait()
			return ctx.Err()

		case <-ticker.C:
			e.wg.Add(1)
			go func() {
				e.sendRequest(ctx, plan)
			}()
		}
	}
}

// sendRequest executes a single HTTP request with context propagation.
func (e *Engine) sendRequest(ctx context.Context, plan LoadPlan) {
	defer e.wg.Done()

	var body io.Reader
	if plan.Payload != "" {
		body = strings.NewReader(plan.Payload)
	}
	req, err := http.NewRequestWithContext(ctx, plan.Method, plan.TargetURL, body)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	resp, err := e.client.Do(req)
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		return
	}
	if resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	fmt.Printf("Request to %s returned status %s\n", plan.TargetURL, resp.Status)
}

// Stop cleanly aborts all in-flight workers and dispatch loops.
func (e *Engine) Stop() {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()

	if cancel != nil {
		cancel()
		e.wg.Wait()
	}
}

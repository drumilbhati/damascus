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
	client *http.Client
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
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
	e.mu.Lock()
	// TODO 1: Wrap ctx with context.WithCancel so e.Stop() can trigger cancellation
	// Save the cancel function in e.cancel
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

	// TODO 2: Compute ticker interval from plan.InitialRate (e.g. time.Second / rate)
	rate := plan.InitialRate
	if rate <= 0 {
		rate = 1
	}
	ticker := time.NewTicker(time.Second / time.Duration(rate)) // replace with computed interval
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// TODO 3: Context was cancelled (Emergency stop or timeout)
			// Wait for active workers in e.wg and return ctx.Err()
			e.wg.Wait()
			return ctx.Err()

		case <-ticker.C:
			// TODO 4: Dispatch an HTTP worker goroutine
			// Increment e.wg, launch goroutine, and call e.sendRequest(ctx, plan)
			e.wg.Add(1)
			go func() {
				e.sendRequest(ctx, plan)
			}()
		}
	}
}

// sendRequest executes a single HTTP request with context propagation.
func (e *Engine) sendRequest(ctx context.Context, plan LoadPlan) {
	// TODO 5: Remember to call defer e.wg.Done()
	defer e.wg.Done()

	// TODO 6: Create http.NewRequestWithContext using ctx, plan.Method, and plan.TargetURL
	// Send request via e.client.Do(req) and close response body
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

	// Optionally, log the response status code
	fmt.Printf("Request to %s returned status %s\n", plan.TargetURL, resp.Status)
}

// Stop cleanly aborts all in-flight workers and dispatch loops.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// TODO 7: If e.cancel is not nil, invoke it to signal all workers
	// Wait for e.wg to drain
	if e.cancel != nil {
		e.cancel()
		e.wg.Wait()
	}
}

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

// Engine executes one finite load plan at a time.
type Engine struct {
	client *http.Client
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	rate   atomic.Int64
}

func NewEngine(client *http.Client) *Engine {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Engine{client: client}
}

// CurrentRate is the offered load for the current step, independent of service traffic.
func (e *Engine) CurrentRate() int { return int(e.rate.Load()) }

func (e *Engine) Start(ctx context.Context, plan LoadPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if plan.MaxRate > 1_000_000_000 {
		return fmt.Errorf("maximum supported rate is 1,000,000,000 req/s")
	}
	e.mu.Lock()
	if e.done != nil {
		e.mu.Unlock()
		return fmt.Errorf("stress engine is already running an active load plan")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	e.cancel, e.done = cancel, done
	e.mu.Unlock()
	var workers sync.WaitGroup
	failures := make(chan error, 1)
	// Bound work in flight so slow endpoints cannot exhaust the generator.
	slots := make(chan struct{}, 256)
	defer func() {
		cancel()
		workers.Wait()
		e.rate.Store(0)
		e.mu.Lock()
		e.cancel, e.done = nil, nil
		close(done)
		e.mu.Unlock()
	}()
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	for rate := plan.InitialRate; ; {
		e.rate.Store(int64(rate))
		ticker := time.NewTicker(time.Second / time.Duration(rate))
		timer := time.NewTimer(time.Duration(plan.StepDurationSeconds) * time.Second)
		stepDone := false
		for !stepDone {
			select {
			case <-runCtx.Done():
				stepDone = true
			case <-timer.C:
				stepDone = true
			case <-ticker.C:
				select {
				case slots <- struct{}{}:
					workers.Add(1)
					go func() {
						defer workers.Done()
						defer func() { <-slots }()
						if err := e.sendRequest(runCtx, plan); err != nil && runCtx.Err() == nil {
							fail(err)
						}
					}()
				default:
					fail(fmt.Errorf("load generator reached its in-flight request limit"))
				}
			}
		}
		ticker.Stop()
		timer.Stop()
		if runCtx.Err() != nil {
			break
		}
		if rate == plan.MaxRate {
			break
		}
		if plan.MaxRate-rate < plan.StepRate {
			rate = plan.MaxRate
		} else {
			rate += plan.StepRate
		}
	}
	// Finish in-flight requests before reporting normal completion.
	workers.Wait()
	select {
	case err := <-failures:
		return err
	default:
	}
	return runCtx.Err()
}

func (e *Engine) sendRequest(ctx context.Context, plan LoadPlan) error {
	req, err := http.NewRequestWithContext(ctx, plan.Method, plan.TargetURL, strings.NewReader(plan.Payload))
	if err != nil {
		return fmt.Errorf("build load request: %w", err)
	}
	if plan.Payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.client.Do(req)
	if err != nil {
		// Network-level failure (connection refused, DNS, timeout): the target
		// is unreachable so no load can be generated. Abort the experiment.
		return fmt.Errorf("send load request: %w", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("read load response: %w", err)
	}
	// HTTP 4xx/5xx are valid responses from the target service under load.
	// They are counted as error rate by Prometheus/WatcherEngine and evaluated
	// by SafetyController. Aborting the experiment here would stop it the
	// moment the service starts degrading — the opposite of what we want.
	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	cancel, done := e.cancel, e.done
	e.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

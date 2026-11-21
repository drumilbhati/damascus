package stress

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrInFlightLimit means the generator cannot sustain the offered rate even
// with its worker pool at the configured maximum. The experiment can still
// analyze measurements collected before this point.
var ErrInFlightLimit = errors.New("load generator reached its in-flight request limit")

const (
	scaleInterval       = 200 * time.Millisecond
	workerHeadroom      = 1.5
	minWorkerFloor      = 4
	latencyEWMAWeight   = 0.2
	maxDispatchWaitTime = 100 * time.Millisecond
)

// Engine executes one finite load plan at a time with a dynamic worker pool.
// Workers are added when the dispatch queue backs up and retired when idle,
// using Little's law (workers ≈ rate × latency) as the steady-state target.
// The pool never exceeds maxInFlight, so slow endpoints cannot exhaust the host.
type Engine struct {
	client       *http.Client
	maxInFlight  int
	mu           sync.Mutex
	cancel       context.CancelFunc
	done         chan struct{}
	rateAdjustCh chan int
	initialRate  atomic.Int64

	rate      atomic.Int64
	workers   atomic.Int64
	busy      atomic.Int64
	latencyNs atomic.Int64 // EWMA of request latency
	peak      atomic.Int64
	sent      atomic.Int64
	missed    atomic.Int64
}

// EngineStats is a point-in-time view of the generator for dashboards and reports.
type EngineStats struct {
	CurrentRate  int     `json:"current_rate"`
	Workers      int     `json:"workers"`
	PeakWorkers  int     `json:"peak_workers"`
	InFlight     int     `json:"in_flight"`
	MaxInFlight  int     `json:"max_in_flight"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	Sent         int64   `json:"sent"`
	Missed       int64   `json:"missed"`
}

type EngineOption func(*Engine)

func WithMaxInFlight(limit int) EngineOption {
	return func(e *Engine) {
		if limit > 0 {
			e.maxInFlight = limit
		}
	}
}

func NewEngine(client *http.Client, opts ...EngineOption) *Engine {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	e := &Engine{client: client, maxInFlight: 256}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// CurrentRate is the offered load for the current step, independent of service traffic.
func (e *Engine) CurrentRate() int { return int(e.rate.Load()) }

// MultiplicativeDecrease reduces current rate multiplicatively by factor (e.g. 0.5 or 0.7),
// clamped to InitialRate, and notifies the active rate loop.
func (e *Engine) MultiplicativeDecrease(factor float64) int {
	if factor <= 0 || factor >= 1.0 {
		factor = 0.5
	}
	current := int(e.rate.Load())
	initial := int(e.initialRate.Load())
	if initial <= 0 {
		initial = 1
	}
	newRate := int(float64(current) * factor)
	if newRate < initial {
		newRate = initial
	}
	e.rate.Store(int64(newRate))
	select {
	case e.rateAdjustCh <- newRate:
	default:
	}
	return newRate
}

// Stats reports the worker pool's current size and throughput counters.
func (e *Engine) Stats() EngineStats {
	return EngineStats{
		CurrentRate:  e.CurrentRate(),
		Workers:      int(e.workers.Load()),
		PeakWorkers:  int(e.peak.Load()),
		InFlight:     int(e.busy.Load()),
		MaxInFlight:  e.maxInFlight,
		AvgLatencyMs: float64(e.latencyNs.Load()) / float64(time.Millisecond),
		Sent:         e.sent.Load(),
		Missed:       e.missed.Load(),
	}
}

// desiredWorkers applies Little's law with headroom, clamped to the pool bounds.
func (e *Engine) desiredWorkers(rate int, backlog int) int {
	latency := time.Duration(e.latencyNs.Load()).Seconds()
	desired := int(math.Ceil(float64(rate)*latency*workerHeadroom)) + minWorkerFloor
	if current := int(e.workers.Load()); backlog > 0 && current+backlog > desired {
		desired = current + backlog
	}
	return min(max(desired, min(minWorkerFloor, e.maxInFlight)), e.maxInFlight)
}

func (e *Engine) observeLatency(d time.Duration) {
	for {
		old := e.latencyNs.Load()
		next := int64(d)
		if old != 0 {
			next = int64(float64(old)*(1-latencyEWMAWeight) + float64(d)*latencyEWMAWeight)
		}
		if e.latencyNs.CompareAndSwap(old, next) {
			return
		}
	}
}

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
	e.rateAdjustCh = make(chan int, 8)
	e.initialRate.Store(int64(plan.InitialRate))
	e.mu.Unlock()

	e.latencyNs.Store(0)
	e.peak.Store(0)
	e.sent.Store(0)
	e.missed.Store(0)

	picker := newRequestPicker(specsForPlan(plan))
	queueCap := min(max(e.maxInFlight/8, 8), 256)
	jobs := make(chan int, queueCap) // each job carries the offered rate it was scheduled at
	retire := make(chan struct{})
	var workers sync.WaitGroup
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	var spawnMu sync.Mutex
	spawn := func(n int) {
		spawnMu.Lock()
		defer spawnMu.Unlock()
		n = min(n, e.maxInFlight-int(e.workers.Load()))
		for range n {
			workers.Add(1)
			if now := e.workers.Add(1); now > e.peak.Load() {
				e.peak.Store(now)
			}
			go func() {
				defer workers.Done()
				defer e.workers.Add(-1)
				for {
					select {
					case <-runCtx.Done():
						return
					case <-retire:
						return
					case rate := <-jobs:
						e.busy.Add(1)
						err := e.sendRequest(runCtx, picker, plan, rate)
						e.busy.Add(-1)
						if err != nil && runCtx.Err() == nil {
							fail(err)
						}
					}
				}
			}()
		}
	}
	defer func() {
		cancel()
		workers.Wait()
		e.rate.Store(0)
		e.mu.Lock()
		e.cancel, e.done = nil, nil
		close(done)
		e.mu.Unlock()
	}()

	spawn(e.desiredWorkers(plan.InitialRate, 0))
	go func() {
		ticker := time.NewTicker(scaleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				current := int(e.workers.Load())
				desired := e.desiredWorkers(int(e.rate.Load()), len(jobs))
				switch {
				case desired > current:
					spawn(desired - current)
				case desired < current && int(e.busy.Load()) < current/2:
					// Retire gradually; only idle workers can receive.
					for range (current - desired + 1) / 2 {
						select {
						case retire <- struct{}{}:
						default:
						}
					}
				}
			}
		}
	}()

	dispatch := func(rate int) {
		select {
		case jobs <- rate:
			return
		default:
		}
		if int(e.workers.Load()) >= e.maxInFlight {
			fail(fmt.Errorf("%w (%d) at %d RPS", ErrInFlightLimit, e.maxInFlight, rate))
			return
		}
		// Grow immediately instead of waiting for the next scaling tick.
		spawn(max(1, int(e.workers.Load())))
		wait := time.NewTimer(min(time.Second/time.Duration(rate), maxDispatchWaitTime))
		defer wait.Stop()
		select {
		case jobs <- rate:
		case <-wait.C:
			e.missed.Add(1)
		case <-runCtx.Done():
		}
	}

	isAIMD := strings.EqualFold(plan.Type, "aimd")
	stepSec := plan.StepDurationSeconds
	if stepSec <= 0 {
		if isAIMD {
			stepSec = 5
		} else {
			stepSec = 10
		}
	}
	stepDuration := time.Duration(stepSec) * time.Second

	rate := plan.InitialRate
	e.rate.Store(int64(rate))
	ticker := time.NewTicker(time.Second / time.Duration(rate))
	defer ticker.Stop()
	timer := time.NewTimer(stepDuration)
	defer timer.Stop()

	loopDone := false
	for !loopDone {
		select {
		case <-runCtx.Done():
			loopDone = true

		case <-timer.C:
			if isAIMD {
				if rate >= plan.MaxRate {
					loopDone = true
					break
				}
				if plan.MaxRate-rate < plan.StepRate {
					rate = plan.MaxRate
				} else {
					rate += plan.StepRate
				}
				e.rate.Store(int64(rate))
				ticker.Reset(time.Second / time.Duration(rate))
				timer.Reset(stepDuration)
			} else {
				if rate >= plan.MaxRate {
					loopDone = true
					break
				}
				if plan.MaxRate-rate < plan.StepRate {
					rate = plan.MaxRate
				} else {
					rate += plan.StepRate
				}
				e.rate.Store(int64(rate))
				ticker.Reset(time.Second / time.Duration(rate))
				timer.Reset(stepDuration)
			}

		case newRate := <-e.rateAdjustCh:
			if newRate > 0 && newRate != rate {
				rate = newRate
				e.rate.Store(int64(rate))
				ticker.Reset(time.Second / time.Duration(rate))
				timer.Reset(stepDuration)
			}

		case <-ticker.C:
			dispatch(rate)
			if isAIMD && e.missed.Load() > 0 {
				dropped := e.missed.Swap(0)
				if dropped > 0 && rate > plan.InitialRate {
					rate = max(plan.InitialRate, int(float64(rate)*0.7))
					e.rate.Store(int64(rate))
					ticker.Reset(time.Second / time.Duration(rate))
					timer.Reset(stepDuration)
				}
			}
		}
	}
	stopErr := runCtx.Err()
	if stopErr == nil {
		// Let queued and in-flight requests finish before reporting normal completion.
		for len(jobs) > 0 && runCtx.Err() == nil {
			time.Sleep(5 * time.Millisecond)
		}
		cancelWorkersWhenIdle(runCtx, &e.busy, cancel)
	}
	cancel()
	workers.Wait()
	select {
	case err := <-failures:
		return err
	default:
	}
	return stopErr
}

// cancelWorkersWhenIdle waits for in-flight requests to drain, then releases idle workers.
func cancelWorkersWhenIdle(ctx context.Context, busy *atomic.Int64, cancel context.CancelFunc) {
	for busy.Load() > 0 && ctx.Err() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
}

func newTraceparent() (traceID, header string) {
	var b [24]byte
	_, _ = rand.Read(b[:])
	traceID = hex.EncodeToString(b[:16])
	return traceID, "00-" + traceID + "-" + hex.EncodeToString(b[16:]) + "-01"
}

func (e *Engine) sendRequest(ctx context.Context, picker *requestPicker, plan LoadPlan, rate int) error {
	spec, seq := picker.next()
	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	payload := renderTemplate(spec.Body, seq)
	if payload != "" {
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, renderTemplate(spec.URL, seq), body)
	if err != nil {
		return fmt.Errorf("build load request %q: %w", spec.Name, err)
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	if payload != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	var traceID string
	if plan.TraceEvery > 0 && seq%uint64(plan.TraceEvery) == 0 && req.Header.Get("traceparent") == "" {
		var header string
		traceID, header = newTraceparent()
		req.Header.Set("traceparent", header)
	}
	start := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		// Network-level failure (connection refused, DNS, timeout): the target
		// is unreachable so no load can be generated. Abort the experiment.
		return fmt.Errorf("send load request %q: %w", spec.Name, err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("read load response %q: %w", spec.Name, err)
	}
	latency := time.Since(start)
	e.observeLatency(latency)
	e.sent.Add(1)
	if traceID != "" && plan.OnTrace != nil {
		plan.OnTrace(TraceRecord{TraceID: traceID, Request: spec.Name, Rate: rate, StatusCode: resp.StatusCode, Latency: latency, SentAt: start})
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

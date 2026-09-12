# DAMASCUS — Low-Level Design (LLD) Specification

**Detailed Technical Design, Interfaces, State Machine, Concurrency & Data Schemas**

---

## Table of Contents
1. [Target Environment & Observability Configuration](#1-target-environment--observability-configuration)
2. [Domain Models & Data Structures](#2-domain-models--data-structures)
3. [Go Interface Specifications](#3-go-interface-specifications)
4. [Experiment State Machine](#4-experiment-state-machine)
5. [Concurrency & Fast-Path Context Cancellation](#5-concurrency--fast-path-context-cancellation)
6. [Graph Analysis & Criticality Scoring Engine](#6-graph-analysis--criticality-scoring-engine)
7. [Safety Controller & Health Evaluation](#7-safety-controller--health-evaluation)
8. [Kafka Event Backbone & Messaging Schema](#8-kafka-event-backbone--messaging-schema)
9. [PostgreSQL Storage Schema (DDL)](#9-postgresql-storage-schema-ddl)
10. [REST API Specification](#10-rest-api-specification)

---

## 1. Target Environment & Observability Configuration

DAMASCUS targets external microservice environments (such as the **OpenTelemetry Astronomy Shop Demo** or enterprise meshes) by interacting through standard observability endpoints:

```go
package config

type TargetEnvironmentConfig struct {
	TargetBaseURL       string `json:"target_base_url"`       // e.g. http://frontend:8080 or http://localhost:8080
	JaegerTraceBaseURL  string `json:"jaeger_trace_base_url"`  // e.g. http://jaeger:16686
	PrometheusBaseURL   string `json:"prometheus_base_url"`   // e.g. http://prometheus:9090
	KafkaBrokers        string `json:"kafka_brokers"`         // e.g. localhost:9092
	PostgresDSN         string `json:"postgres_dsn"`          // e.g. postgres://user:pass@localhost:5432/damascus
}
```

---

## 2. Domain Models & Data Structures

### 2.1 Experiment Core Types

```go
package experiment

import "time"

type ExperimentState string

const (
	StateCreated   ExperimentState = "CREATED"
	StatePlanned   ExperimentState = "PLANNED"
	StateRunning   ExperimentState = "RUNNING"
	StateStopping  ExperimentState = "STOPPING"
	StateAnalyzing ExperimentState = "ANALYZING"
	StateReporting ExperimentState = "REPORTING"
	StateCompleted ExperimentState = "COMPLETED"
	StateAborted   ExperimentState = "ABORTED"
)

type ExperimentType string

const (
	ExperimentRamp ExperimentType = "ramp"
	ExperimentStep ExperimentType = "step"
)

type ExperimentConfig struct {
	InitialRate         int           `json:"initial_rate"`
	StepRate            int           `json:"step_rate"`
	MaxRate             int           `json:"max_rate"`
	StepDurationSeconds int           `json:"step_duration_seconds"`
	MaxP95LatencyMs     float64       `json:"max_p95_latency_ms"`
	MaxErrorRatePercent float64       `json:"max_error_rate_percent"`
	MinAvailabilityPct  float64       `json:"min_availability_pct"`
	RecoveryWindowSec   int           `json:"recovery_window_sec"`
}

type Experiment struct {
	ID            string           `json:"id"`
	TargetService string           `json:"target_service"`
	TargetURL     string           `json:"target_url"`
	Type          ExperimentType   `json:"type"`
	Config        ExperimentConfig `json:"config"`
	State         ExperimentState  `json:"state"`
	StopReason    string           `json:"stop_reason,omitempty"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	EndedAt       *time.Time       `json:"ended_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
}
```

### 2.2 Graph & Dependency Types

```go
package graph

type ServiceNode struct {
	Name      string            `json:"name"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type DependencyEdge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	CallCount int64   `json:"call_count"`
	Frequency float64 `json:"frequency"` // Calls per second
}

type DependencyGraph struct {
	Nodes map[string]*ServiceNode `json:"nodes"`
	Edges []DependencyEdge        `json:"edges"`
}

type ServiceScore struct {
	ServiceName string   `json:"service_name"`
	Score       float64  `json:"score"` // Normalized 0.0 to 1.0
	Reasons     []string `json:"reasons"`
}
```

### 2.3 Stress Engine & Rate Controller Types

```go
package stress

import (
	"context"
	"time"
)

type LoadPlan struct {
	TargetURL           string `json:"target_url"`
	Method              string `json:"method"` // GET, POST, etc.
	Payload             string `json:"payload,omitempty"`
	InitialRate         int    `json:"initial_rate"`
	StepRate            int    `json:"step_rate"`
	MaxRate             int    `json:"max_rate"`
	StepDurationSeconds int    `json:"step_duration_seconds"`
}

type RateStep struct {
	StepNumber int           `json:"step_number"`
	Rate       int           `json:"rate"`
	Duration   time.Duration `json:"duration"`
}

type StepHandler func(ctx context.Context, step int, rate int) error

type StepController struct {
	Plan         LoadPlan
	StepDuration time.Duration
}

// ClientConfig holds the tunable parameters for the http.Transport and
// per-request timeouts used by the stress engine's HTTP client wrapper.
// All fields are optional; zero values fall back to production defaults.
type ClientConfig struct {
	// MaxIdleConns – total idle keep-alive connections across all hosts.
	// Default: 1000.
	MaxIdleConns int

	// MaxIdleConnsPerHost – idle connections kept per target host.
	// Default: 100.
	MaxIdleConnsPerHost int

	// IdleConnTimeout – maximum time a keep-alive connection may sit idle
	// before being closed. Default: 90 s.
	IdleConnTimeout time.Duration

	// RequestTimeout – end-to-end timeout per HTTP request (dial + TLS +
	// send + read body). Applied via context.WithTimeout on each call.
	// Default: 30 s.
	RequestTimeout time.Duration
}

// Client wraps *http.Client with a tuned http.Transport and exposes a
// single reusable sendRequest method. It is safe for concurrent use.
//
// Construct with NewClient(cfg ClientConfig) *Client.
//
// sendRequest(ctx context.Context, plan LoadPlan) error
//   - Dispatches GET or POST based on plan.Method (case-insensitive).
//   - Applies plan.Payload as the JSON body for POST requests.
//   - Drains and closes the response body unconditionally to return the
//     TCP connection to the pool and prevent memory leaks.
//   - Returns a non-nil error for 4xx/5xx status codes or network failure.
```

### 2.4 Watcher & Metric Snapshot Types

```go
package watcher

import "time"

type MetricSnapshot struct {
	ExperimentID      string    `json:"experiment_id"`
	TargetService     string    `json:"target_service"`
	Timestamp         time.Time `json:"timestamp"`
	RequestRate       float64   `json:"request_rate"`
	P50LatencyMs      float64   `json:"p50_latency_ms"`
	P95LatencyMs      float64   `json:"p95_latency_ms"`
	P99LatencyMs      float64   `json:"p99_latency_ms"`
	ErrorRate         float64   `json:"error_rate"`
	Availability      float64   `json:"availability"`
	CPUUtilization    float64   `json:"cpu_utilization"`
	MemoryUtilization float64   `json:"memory_utilization"`
}

// PrometheusClient wraps the Prometheus v1 HTTP API to query real-time RED metrics
type PrometheusClient struct {
	api promv1.API
}

func NewPrometheusClient(address string) (*PrometheusClient, error)
func (c *PrometheusClient) QuerySnapshot(ctx context.Context, experimentID, targetService string) (MetricSnapshot, error)
func (c *PrometheusClient) QueryValue(ctx context.Context, query string) (float64, error)
func BuildP95LatencyQuery(service string) string
func BuildErrorRateQuery(service string) string
func BuildRequestRateQuery(service string) string
func ExtractFloatValue(val model.Value) (float64, error)
```

### 2.5 Safety Types

```go
package safety

type SafetyPolicy struct {
	MaxP95LatencyMs float64 `json:"max_p95_latency_ms"`
	MaxErrorRate    float64 `json:"max_error_rate"`    // e.g. 0.05 for 5%
	MinAvailability float64 `json:"min_availability"` // e.g. 0.99 for 99%
}

type SafetyDecision struct {
	ShouldStop bool   `json:"should_stop"`
	Reason     string `json:"reason"`
}

type SafetyAuditRecord struct {
	Timestamp      time.Time      `json:"timestamp"`
	ExperimentID   string         `json:"experiment_id"`
	TargetService  string         `json:"target_service"`
	BreachedMetric string         `json:"breached_metric,omitempty"`
	ObservedValue  float64        `json:"observed_value,omitempty"`
	ThresholdValue float64        `json:"threshold_value,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Decision       SafetyDecision `json:"decision"`
	Action         string         `json:"action"`
}
```

### 2.6 Capacity & Report Types

```go
package capacity

import "time"

type Observation struct {
	Timestamp         time.Time `json:"timestamp"`
	LoadRate          int       `json:"load_rate"`
	P95LatencyMs      float64   `json:"p95_latency_ms"`
	ErrorRate         float64   `json:"error_rate"`
	Availability      float64   `json:"availability"`
	CPUUtilization    float64   `json:"cpu_utilization"`
	MemoryUtilization float64   `json:"memory_utilization"`
}

type CapacityResult struct {
	MaximumTestedRate      int           `json:"maximum_tested_rate"`
	MaximumSustainableRate int           `json:"maximum_sustainable_rate"`
	DegradationRate        int           `json:"degradation_rate"`
	SafetyBoundaryRate     int           `json:"safety_boundary_rate"`
	RecoveryTime           time.Duration `json:"recovery_time"`
}

type ExperimentReport struct {
	ExperimentID           string         `json:"experiment_id"`
	TargetService          string         `json:"target_service"`
	CriticalityScore       float64        `json:"criticality_score"`
	MaximumTestedRate      int            `json:"maximum_tested_rate"`
	MaximumSustainableRate int            `json:"maximum_sustainable_rate"`
	DegradationRate        int            `json:"degradation_rate"`
	SafetyBoundaryRate     int            `json:"safety_boundary_rate"`
	RecoveryTime           time.Duration  `json:"recovery_time"`
	Observations           []Observation  `json:"observations"`
	Recommendations        []string       `json:"recommendations"`
	GeneratedAt            time.Time      `json:"generated_at"`
}
```

---

## 3. Go Interface Specifications

### 3.X Worker Pool Types

```go
type WorkerPool struct {
    maxConcurrency int           // Maximum concurrent goroutines in flight
    targetRate     int           // Target requests per second
    ticker         *time.Ticker  // Synchronized ticker used to dispatch work
}
```

Methods:

```go
func NewWorkerPool(maxConcurrency, targetRate int) *WorkerPool
func (wp *WorkerPool) Start(ctx context.Context) error
func (wp *WorkerPool) Submit(task func()) error
func (wp *WorkerPool) Stop()
```

- `NewWorkerPool(maxConcurrency, targetRate) *WorkerPool` creates a worker pool with the configured concurrency and rate limits.
- `Start(ctx context.Context) error` initializes the workers and starts ticker-gated task execution.
- `Submit(task func()) error` enqueues a work task for execution, returning an error if the pool is shutting down or queue capacity is exceeded.
- `Stop()` performs a graceful shutdown by canceling the context, stopping the ticker, and waiting for workers to finish.

```go
package interfaces

import (
	"context"
	"damascus/internal/capacity"
	"damascus/internal/events"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

// GraphAnalyzer extracts traces from Jaeger / OTel and ranks service criticality
type GraphAnalyzer interface {
	BuildGraph(ctx context.Context, lookbackDuration string) (*graph.DependencyGraph, error)
	ScoreCriticality(g *graph.DependencyGraph) []graph.ServiceScore
}

// StressEngine executes controlled HTTP/gRPC load plans.
// Internally it uses stress.Client (see internal/stress/client.go) which
// wraps http.Transport with configurable connection-pool parameters
// (MaxIdleConns=1000, MaxIdleConnsPerHost=100, IdleConnTimeout=90s) and
// dispatches requests via sendRequest with per-call context deadlines.
type StressEngine interface {
	Start(ctx context.Context, plan stress.LoadPlan) error
	Stop()
}

// Watcher polls Prometheus metrics in real-time
type Watcher interface {
	Start(ctx context.Context, experimentID string, targetService string) (<-chan watcher.MetricSnapshot, error)
	Stop()
}

// SafetyController checks metric snapshots against SLA boundaries
type SafetyController interface {
	Evaluate(snapshot watcher.MetricSnapshot) safety.SafetyDecision
}

// CapacityAnalyzer computes sustainable throughput and recovery metrics
type CapacityAnalyzer interface {
	Analyze(observations []capacity.Observation, policy safety.SafetyPolicy) capacity.CapacityResult
}

// ReportEngine builds structured JSON and rendered HTML reports
type ReportEngine interface {
	Generate(
		exp experiment.Experiment,
		capResult capacity.CapacityResult,
		scores []graph.ServiceScore,
		observations []capacity.Observation,
	) (capacity.ExperimentReport, error)
}

// ExperimentRepository persists lifecycle states, observations, and reports
type ExperimentRepository interface {
	Create(ctx context.Context, exp *experiment.Experiment) error
	GetByID(ctx context.Context, id string) (*experiment.Experiment, error)
	List(ctx context.Context) ([]experiment.Experiment, error)
	UpdateState(ctx context.Context, id string, state experiment.ExperimentState, stopReason string) error
	SaveObservation(ctx context.Context, expID string, obs capacity.Observation) error
	GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error)
	SaveReport(ctx context.Context, report capacity.ExperimentReport) error
	GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error)
}

// EventProducer streams asynchronous domain events to Kafka
type EventProducer interface {
	Publish(ctx context.Context, event events.DomainEvent) error
	Close() error
}
```

---

## 4. Experiment State Machine

```mermaid
stateDiagram-v2
    [*] --> CREATED
    CREATED --> PLANNED : Config Validated & Target URL Confirmed
    PLANNED --> RUNNING : Stress & Watcher Goroutines Spawned
    
    RUNNING --> STOPPING : Safety Threshold Breached OR User /stop
    RUNNING --> ABORTED : Fatal Crash / Target Network Drop
    
    STOPPING --> ANALYZING : Traffic Stopped (0 req/s) & Recovery Window Monitored
    ANALYZING --> REPORTING : Capacity & Degradation Metrics Computed
    REPORTING --> COMPLETED : Report Saved in DB & Published to Kafka
    
    ABORTED --> [*]
    COMPLETED --> [*]
```

### State Transitions & Trigger Matrix

| State Transition | Trigger / Condition | Actions Taken |
| :--- | :--- | :--- |
| `CREATED` $\rightarrow$ `PLANNED` | Valid request payload & reachable target service. | Construct `LoadPlan`, validate Prometheus metric queries. |
| `PLANNED` $\rightarrow$ `RUNNING` | Orchestrator starts execution context. | Launch worker goroutines (`StressEngine`) & Prometheus poller (`WatcherEngine`). |
| `RUNNING` $\rightarrow$ `STOPPING` | `SafetyController` triggers breach (`ShouldStop == true`) OR `/stop` called. | Invoke `context.CancelFunc` (instant stop), enter post-stress recovery monitoring. |
| `RUNNING` $\rightarrow$ `ABORTED` | Unrecoverable engine error or database drop. | Cancel workers, record failure reason in Postgres, publish alert to Kafka. |
| `STOPPING` $\rightarrow$ `ANALYZING` | Recovery window expires. | Pass collected observation array to `CapacityAnalyzer`. |
| `ANALYZING` $\rightarrow$ `REPORTING` | Capacity metrics computed. | Pass results to `ReportEngine` to assemble final deliverable. |
| `REPORTING` $\rightarrow$ `COMPLETED` | Artifacts stored. | Write report to PostgreSQL, publish completion event to Kafka topic `experiment-events`. |

---

## 5. Concurrency & Fast-Path Context Cancellation

### 5.1 EvaluatingController — WatcherEngine ↔ SafetyController Wiring

`EvaluatingController` (`internal/safety/controller.go`) is the concrete implementation
that connects the `WatcherEngine` metric stream directly to `SafetyController` evaluation
and executes an in-memory `context.CancelFunc` fast path on the first breach.

#### Wire-up (orchestrator responsibility)

```go
// 1. Create an experiment-scoped cancellable context.
expCtx, cancel := context.WithCancel(parentCtx)

// 2. Construct the EvaluatingController with the cancel function and SLA policy.
ec := safety.NewEvaluatingController(policy, cancel, logger)

// 3. Wire the handler into WatcherEngine — no additional plumbing required.
engine := watcher.NewWatcherEngine(promClient, ec.MakeSnapshotHandler())

// 4. Start both engines. The EvaluatingController will cancel expCtx
//    automatically on the first breach.
engine.Start(expCtx, experimentID, targetService)
stressEngine.Start(expCtx, loadPlan)
```

#### Cancellation contract

| Guarantee | Mechanism |
|---|---|
| Sub-second latency | `cancelFunc()` is called synchronously inside the `SnapshotHandler` callback, before the handler returns to WatcherEngine's poll goroutine |
| Exactly-once cancel | `sync.Once` wraps the `cancelFunc` call — safe even if two goroutines deliver breach snapshots simultaneously |
| Audit trail preserved | `Controller.Evaluate` is always called first; the audit log entry is written before cancellation |
| Nil-safe | `cancelFunc == nil` is checked; no panic if the caller passes a no-op context |

#### Concurrency sequence

```mermaid
sequenceDiagram
    autonumber
    participant EM as ExperimentManager
    participant Ctx as Go context.Context
    participant SE as StressEngine (Workers)
    participant WE as WatcherEngine
    participant EC as EvaluatingController
    participant KF as Kafka Producer

    EM->>Ctx: context.WithCancel(parentCtx)
    EM->>EC: NewEvaluatingController(policy, cancel, logger)
    EM->>WE: NewWatcherEngine(promClient, ec.MakeSnapshotHandler())
    EM->>SE: go SE.Start(ctx, loadPlan)
    EM->>WE: go WE.Start(ctx, expID, targetService)

    loop Every poll interval
        WE->>WE: QuerySnapshot(ctx, expID, svc)
        WE->>EC: SnapshotHandler(MetricSnapshot)
        EC->>EC: Controller.Evaluate(snapshot, policy)
        alt Metrics within bounds
            EC-->>WE: return (no-op)
        else Threshold Breached OR Observability Lost
            EC->>EC: cancelOnce.Do(cancelFunc)
            Note over EC,Ctx: FAST-PATH IN-MEMORY CANCELLATION
            EC->>Ctx: cancel()
            Ctx-->>SE: ctx.Done() signaled
            SE->>SE: All HTTP worker goroutines abort instantly
            Ctx-->>WE: ctx.Done() signaled
            WE->>WE: Poll loop exits
            Note over EM,KF: ASYNC NOTIFICATION (NON-BLOCKING)
            EM->>KF: Publish "SAFETY_STOP_TRIGGERED" event to Kafka
        end
    end
```

---

## 6. Graph Analysis & Criticality Scoring Engine

### 6.1 Criticality Score Formula

$$S(v) = w_1 \cdot \text{InDegree}(v) + w_2 \cdot \text{OutDegree}(v) + w_3 \cdot \text{CallFreq}(v) + w_4 \cdot \text{Depth}(v) + w_5 \cdot \text{SPOF}(v)$$

- **$\text{InDegree}(v)$**: Number of upstream services depending on $v$.
- **$\text{OutDegree}(v)$**: Number of downstream dependencies invoked by $v$.
- **$\text{CallFreq}(v)$**: Normalized calls per second routed to $v$.
- **$\text{Depth}(v)$**: Distance from the ingress API gateway.
- **$\text{SPOF}(v)$**: Binary indicator (1.0 or 0.0) indicating whether failure of $v$ partitions the service graph.
- **Weights**: $w_1 = 0.35, w_2 = 0.15, w_3 = 0.25, w_4 = 0.10, w_5 = 0.15$.

Normalized final score: $S(v) \in [0.0, 1.0]$.

---

## 7. Safety Controller & Health Evaluation

```go
func (c *Controller) Evaluate(ctx context.Context, snapshot watcher.MetricSnapshot, policy SafetyPolicy) SafetyDecision {
	if policy.MaxP95LatencyMs > 0 && snapshot.P95LatencyMs > policy.MaxP95LatencyMs {
		reason := FormatP95Breach(snapshot.P95LatencyMs, policy.MaxP95LatencyMs)
		c.logAndRecord(snapshot, MetricP95Latency, snapshot.P95LatencyMs, policy.MaxP95LatencyMs, reason, true)
		return SafetyDecision{
			ShouldStop: true,
			Reason:     reason,
		}
	}
	if policy.MaxErrorRate > 0 && snapshot.ErrorRate > policy.MaxErrorRate {
		reason := FormatErrorRateBreach(snapshot.ErrorRate, policy.MaxErrorRate)
		c.logAndRecord(snapshot, MetricErrorRate, snapshot.ErrorRate, policy.MaxErrorRate, reason, true)
		return SafetyDecision{
			ShouldStop: true,
			Reason:     reason,
		}
	}
	if policy.MinAvailability > 0 && snapshot.Availability < policy.MinAvailability {
		reason := FormatAvailabilityBreach(snapshot.Availability, policy.MinAvailability)
		c.logAndRecord(snapshot, MetricAvail, snapshot.Availability, policy.MinAvailability, reason, true)
		return SafetyDecision{
			ShouldStop: true,
			Reason:     reason,
		}
	}
	c.logAndRecord(snapshot, "", 0, 0, "", false)
	return SafetyDecision{ShouldStop: false}
}
```

When an SLA breach is evaluated (`ShouldStop: true`), the exact reason string is recorded in `Experiment.StopReason` and the experiment state transitions to `StateStopping`:

```go
func ApplySafetyDecision(exp *experiment.Experiment, decision SafetyDecision) bool
```

### 7.1 WatcherEngine — Loss-of-Observability Detection

`WatcherEngine` (`internal/watcher/evaluator.go`) continuously polls Prometheus for RED metrics and forwards `MetricSnapshot` values to an upstream `SnapshotHandler` callback.

#### Problem

If Prometheus becomes unreachable (network partition, container crash, DNS failure), the polling loop would silently receive errors and stop forwarding snapshots. This means a stress test could continue running **completely unmonitored**, defeating the entire safety mechanism.

#### Solution — Consecutive-Failure Fail-Safe

The engine tracks consecutive Prometheus query failures using an `atomic.Int64` counter. Once the counter reaches `failureThreshold` (default: **3**), it emits a *synthetic breach snapshot* — a `MetricSnapshot` with worst-case sentinel values — to the registered handler, which causes `SafetyController.Evaluate` to issue a `ShouldStop: true` decision and halt the experiment immediately.

#### Tuning Constants

| Constant | Default | Description |
|---|---|---|
| `DefaultConsecutiveFailureThreshold` | `3` | Back-to-back Prometheus errors before breach is emitted |
| `DefaultPollInterval` | `5s` | Cadence of each Prometheus poll cycle |
| `SyntheticBreachErrorRate` | `1.0` | ErrorRate injected into synthetic snapshots (100 %) |
| `SyntheticBreachAvailability` | `0.0` | Availability injected into synthetic snapshots (0 %) |

#### Sentinel Snapshot Format

```go
// Emitted when consecutiveFailures >= failureThreshold
MetricSnapshot{
    ExperimentID:  "<running experiment>",
    TargetService: "<target service>",
    Timestamp:     time.Now().UTC(),
    ErrorRate:     1.0,   // guaranteed to breach MaxErrorRate
    Availability:  0.0,   // guaranteed to breach MinAvailability
    P95LatencyMs:  0,
    RequestRate:   0,
}
```

The sentinel values guarantee that at least two of the three `SafetyPolicy` checks (`MaxErrorRate`, `MinAvailability`) will fire, leaving a clear audit trail that the stop was caused by observability loss rather than application degradation.

#### Public API

```go
package watcher

// WatcherEngine polls Prometheus and triggers fail-safe stops on connectivity loss.
type WatcherEngine struct { /* ... */ }

// NewWatcherEngine constructs a WatcherEngine with functional options.
func NewWatcherEngine(client *PrometheusClient, handler SnapshotHandler, opts ...WatcherEngineOption) *WatcherEngine

// Start launches the background polling loop (idempotent).
func (e *WatcherEngine) Start(ctx context.Context, experimentID, targetService string) error

// Stop terminates the polling loop (idempotent).
func (e *WatcherEngine) Stop()

// ConsecutiveFailures returns the current back-to-back error count (for health checks / tests).
func (e *WatcherEngine) ConsecutiveFailures() int

// BuildSyntheticBreachSnapshot is an exported helper for constructing sentinel snapshots.
func BuildSyntheticBreachSnapshot(experimentID, targetService string, consecutiveFailures int, cause error) MetricSnapshot

// Functional options
func WithPollInterval(d time.Duration) WatcherEngineOption
func WithConsecutiveFailureThreshold(n int) WatcherEngineOption
func WithLogger(l *slog.Logger) WatcherEngineOption
```

#### State Transitions Triggered by Observability Loss

| Condition | Action |
|---|---|
| `consecutiveFailures < failureThreshold` | Log warning, continue polling, do **not** forward snapshot |
| `consecutiveFailures >= failureThreshold` | Log error, emit synthetic breach snapshot to handler |
| Handler receives synthetic snapshot | `SafetyController.Evaluate` → `ShouldStop: true` → `StateStopping` |
| First successful query after failures | Reset counter to 0, log "connectivity restored", resume normal forwarding |

---

## 8. Kafka Event Backbone & Messaging Schema

**Topic**: `experiment-events`

```json
{
  "event_id": "evt_01J8R9XQ7A",
  "experiment_id": "exp_01J8R9W4N2",
  "event_type": "SAFETY_STOP_TRIGGERED",
  "timestamp": "2026-08-20T07:30:00Z",
  "service": "checkoutservice",
  "payload": {
    "breached_metric": "p95_latency_ms",
    "observed_value": 620.5,
    "threshold_value": 500.0,
    "reason": "P95 latency breached SLA: 620.50 ms > 500.00 ms",
    "action": "CONTEXT_CANCELLED"
  }
}
```

---

## 9. PostgreSQL Storage Schema (DDL)

```sql
CREATE TABLE IF NOT EXISTS services (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS dependencies (
    id SERIAL PRIMARY KEY,
    source_service VARCHAR(128) NOT NULL REFERENCES services(name),
    target_service VARCHAR(128) NOT NULL REFERENCES services(name),
    call_frequency DOUBLE PRECISION DEFAULT 0.0,
    observed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS experiments (
    id VARCHAR(64) PRIMARY KEY,
    target_service VARCHAR(128) NOT NULL,
    target_url VARCHAR(512) NOT NULL,
    test_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    initial_rate INT NOT NULL,
    step_rate INT NOT NULL,
    max_rate INT NOT NULL,
    step_duration_seconds INT NOT NULL,
    max_p95_latency_ms DOUBLE PRECISION NOT NULL,
    max_error_rate DOUBLE PRECISION NOT NULL,
    min_availability DOUBLE PRECISION NOT NULL,
    stop_reason TEXT,
    started_at TIMESTAMP WITH TIME ZONE,
    ended_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS service_scores (
    id SERIAL PRIMARY KEY,
    experiment_id VARCHAR(64) REFERENCES experiments(id) ON DELETE CASCADE,
    service_name VARCHAR(128) NOT NULL,
    criticality_score DOUBLE PRECISION NOT NULL,
    reasons JSONB,
    calculated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS observations (
    id SERIAL PRIMARY KEY,
    experiment_id VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    load_rate INT NOT NULL,
    p95_latency DOUBLE PRECISION NOT NULL,
    error_rate DOUBLE PRECISION NOT NULL,
    availability DOUBLE PRECISION NOT NULL,
    cpu_utilization DOUBLE PRECISION NOT NULL,
    memory_utilization DOUBLE PRECISION NOT NULL
);

CREATE TABLE IF NOT EXISTS reports (
    experiment_id VARCHAR(64) PRIMARY KEY REFERENCES experiments(id) ON DELETE CASCADE,
    target_service VARCHAR(128) NOT NULL,
    criticality_score DOUBLE PRECISION NOT NULL,
    maximum_tested_rate INT NOT NULL,
    maximum_sustainable_rate INT NOT NULL,
    degradation_rate INT NOT NULL,
    safety_boundary_rate INT NOT NULL,
    recovery_time_seconds DOUBLE PRECISION NOT NULL,
    report_data JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

---

## 10. REST API Specification

| Method | Endpoint | Description | Request Body | Response Body |
| :--- | :--- | :--- | :--- | :--- |
| `GET` | `/api/health` | Verify DAMASCUS & observability status | None | `{"status": "UP", "jaeger": "CONNECTED", "prometheus": "CONNECTED"}` |
| `GET` | `/api/dependencies` | Fetch trace dependency graph & criticality scores | None | `{"graph": {...}, "scores": [...]}` |
| `POST` | `/api/experiments` | Create a new experiment configuration | `CreateExperimentDTO` | `Experiment` object |
| `POST` | `/api/experiments/{id}/start` | Trigger execution of planned experiment | None | `{"status": "RUNNING"}` |
| `POST` | `/api/experiments/{id}/stop` | Manual emergency stop | `{"reason": "string"}` | `{"status": "STOPPING"}` |
| `GET` | `/api/experiments` | List all historical experiments | None | `[]Experiment` |
| `GET` | `/api/experiments/{id}` | Retrieve experiment metadata | None | `Experiment` object |
| `GET` | `/api/experiments/{id}/status` | Get live metrics stream snapshot | None | `MetricSnapshot` object |
| `GET` | `/api/experiments/{id}/report` | Retrieve completed experiment report | None | `ExperimentReport` JSON |

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

### 2.3 Stress Engine Types

```go
package stress

import "time"

type LoadPlan struct {
	TargetURL           string `json:"target_url"`
	Method              string `json:"method"` // GET, POST, etc.
	Payload             string `json:"payload,omitempty"`
	InitialRate         int    `json:"initial_rate"`
	StepRate            int    `json:"step_rate"`
	MaxRate             int    `json:"max_rate"`
	StepDurationSeconds int    `json:"step_duration_seconds"`
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
- `Start(ctx context.Context) error` initializes the workers and dispatcher loop, starting the ticker-driven scheduling loop.
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

```mermaid
sequenceDiagram
    autonumber
    participant EM as ExperimentManager
    participant Ctx as Go context.Context
    participant SE as StressEngine (Workers)
    participant WE as WatcherEngine
    participant SC as SafetyController
    participant KF as Kafka Producer

    EM->>Ctx: context.WithCancel(parentCtx)
    EM->>SE: go SE.Start(ctx, loadPlan)
    EM->>WE: go WE.Start(ctx, expID, targetService)
    
    loop Every 1 Second
        WE->>WE: Query Prometheus (/api/v1/query)
        WE->>SC: Evaluate(MetricSnapshot)
        alt Metrics within bounds
            SC-->>WE: SafetyDecision{ShouldStop: false}
        else Threshold Breached (e.g. P95 > 500ms)
            SC-->>WE: SafetyDecision{ShouldStop: true, Reason: "P95 > 500ms"}
            Note over WE,EM: FAST-PATH IN-MEMORY CANCELLATION
            WE->>EM: Trigger cancel callback
            EM->>Ctx: cancel()
            Ctx-->>SE: <-ctx.Done() signaled
            SE->>SE: All HTTP worker goroutines abort instantly
            Ctx-->>WE: <-ctx.Done() signaled
            WE->>WE: Poller stops load monitoring & switches to recovery mode
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
func (c *SafetyController) Evaluate(snapshot watcher.MetricSnapshot) safety.SafetyDecision {
	if snapshot.P95LatencyMs > c.policy.MaxP95LatencyMs {
		return safety.SafetyDecision{
			ShouldStop: true,
			Reason:     fmt.Sprintf("P95 latency breached SLA: %.2f ms > %.2f ms", snapshot.P95LatencyMs, c.policy.MaxP95LatencyMs),
		}
	}
	if snapshot.ErrorRate > c.policy.MaxErrorRate {
		return safety.SafetyDecision{
			ShouldStop: true,
			Reason:     fmt.Sprintf("Error rate breached SLA: %.2f%% > %.2f%%", snapshot.ErrorRate*100, c.policy.MaxErrorRate*100),
		}
	}
	if snapshot.Availability < c.policy.MinAvailability {
		return safety.SafetyDecision{
			ShouldStop: true,
			Reason:     fmt.Sprintf("Availability breached SLA: %.2f%% < %.2f%%", snapshot.Availability*100, c.policy.MinAvailability*100),
		}
	}
	return safety.SafetyDecision{ShouldStop: false}
}
```

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

# DAMASCUS — Engineering Progress & Decision Log

> **Chronological Development Log, Authorship, Logic Rationale, and Verification Status**

---

## 📅 Log: August 28, 2026

### 🔹 Core Execution Engines: StressEngine, WatcherEngine & SafetyController
- **Author / Contributor**: @Vidhan
- **PR / Branch**: `feat/stress-watcher-safety-engines`
- **Components Implemented**:
  - `internal/stress/engine.go` & `internal/stress/engine_test.go`
  - `internal/stress/load_plan.go` (`LoadPlan.Validate()`)
  - `internal/watcher/poller.go` & `internal/watcher/poller_test.go`
  - `internal/safety/controller.go` & `internal/safety/controller_test.go`
  - `cmd/demo/main.go`

---

### 🔍 Deep Dive: What Each Component Is, What It Does & How to Check It

#### 1. `StressEngine` (`internal/stress/engine.go`)
- **What it is**: The high-throughput HTTP/gRPC load generation engine responsible for driving controlled synthetic traffic against target microservices.
- **What it is doing**:
  - **Worker Pool Management**: Spawns a configurable pool of concurrent worker goroutines consuming tasks from a buffered channel (`workChan`).
  - **10ms High-Resolution Rate Regulation**: Uses a 100 ticks/sec accumulator to regulate request dispatch across operating systems without timer drift (essential for Windows).
  - **Stepped & Ramp Load Progression**: Starts at `InitialRate` and automatically steps up by `StepRate` every `StepDurationSeconds` until reaching `MaxRate`.
  - **Lock-Free Atomic Telemetry**: Updates `TotalRequests`, `SuccessCount` ($2\text{xx}$), `ErrorCount` ($4\text{xx}/5\text{xx}$), and `DroppedCount` using `sync/atomic` to avoid mutex lock contention under thousands of requests per second.
  - **Zero-Leak Fast Teardown**: Binds workers strictly to `runCtx`. Upon receiving `<-ctx.Done()`, worker loops break immediately, in-flight response bodies are drained, and `sync.WaitGroup` guarantees all goroutines exit cleanly.
- **How to Check / Test It**:
  ```powershell
  # Compile and run unit test suite
  go test -c -o runner.exe ./internal/stress; .\runner.exe "-test.v"; Remove-Item runner.exe
  ```
  *Tests include: `TestHTTPStressEngine_SteppedLoad`, `TestHTTPStressEngine_FastPathContextCancellation`, `TestHTTPStressEngine_StopMethod`, `TestHTTPStressEngine_InvalidLoadPlan`, `TestHTTPStressEngine_AlreadyRunningRejection`, `TestHTTPStressEngine_HTTPErrorTracking`.*

---

#### 2. `WatcherEngine` / Poller (`internal/watcher/poller.go`)
- **What it is**: The real-time telemetry ingestion poller that continuously extracts RED (Rate, Errors, Latency Duration) and resource metrics from Prometheus.
- **What it is doing**:
  - **Background Polling Loop**: Executes PromQL queries every `pollInterval` (default 1 second) and packages the results into structured `MetricSnapshot` records.
  - **PromQL Queries Executed**:
    - **Request Rate (RPS)**: `sum(rate(http_requests_total{service="<target>"}[1m]))`
    - **P50 Latency**: `histogram_quantile(0.50, sum(rate(http_request_duration_seconds_bucket{service="<target>"}[1m])) by (le)) * 1000`
    - **P95 Latency**: `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service="<target>"}[1m])) by (le)) * 1000`
    - **P99 Latency**: `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{service="<target>"}[1m])) by (le)) * 1000`
    - **Error Rate (5xx Ratio)**: `sum(rate(http_requests_total{service="<target>",status=~"5.."}[1m])) / sum(rate(http_requests_total{service="<target>"}[1m]))`
    - **Availability (Success Ratio)**: `1.0 - ErrorRate` (defined as the non-5xx success ratio required by the safety contract)
    - **Container CPU & Memory**: `container_cpu_usage_seconds_total` and `container_memory_working_set_bytes`.
  - **Channel Streaming**: Streams snapshots asynchronously over a Go channel (`<-chan MetricSnapshot`) and safely closes the channel upon test completion or `Stop()`.
- **How to Check / Test It**:
  ```powershell
  go test -v ./internal/watcher
  ```
  *Tests include: `TestPrometheusWatcher_PollingStream` (JSON decoding & channel streaming) and `TestPrometheusWatcher_StopMethod` (graceful channel closing).*

---

#### 3. `SafetyController` (`internal/safety/controller.go`)
- **What it is**: The closed-loop self-adaptive evaluator (MAPE-K feedback loop) that acts as an emergency circuit breaker.
- **What it is doing**:
  - **SLA Boundary Evaluation**: Ingests each incoming `MetricSnapshot` from `WatcherEngine` and evaluates it against strict non-functional constraints (`SafetyPolicy`):
    - Is $P_{95} \text{ Latency} > \text{MaxP95LatencyMs}$?
    - Is $\text{Error Rate} > \text{MaxErrorRate}$?
    - Is $\text{Availability} < \text{MinAvailability}$?
  - **Zero-Latency In-Memory Fast Path**: If any SLA is breached, returns a `SafetyDecision{ShouldStop: true, Reason: "..."}`. The orchestrator immediately calls in-memory `context.CancelFunc()`, stopping `StressEngine` workers in sub-milliseconds without waiting for Kafka or network queues.
- **How to Check / Test It**:
  ```powershell
  go test -v ./internal/safety
  ```
  *Tests include: `TestSafetyController_WithinBounds`, `TestSafetyController_P95LatencyBreached`, `TestSafetyController_ErrorRateBreached`, and `TestSafetyController_AvailabilityBreached`.*

---

#### 4. Interactive Live Demo (`cmd/demo/main.go`)
- **What it is**: A standalone terminal application that wires `StressEngine`, `WatcherEngine`, and `SafetyController` together in real time.
- **How to Run It**:
  ```powershell
  go run ./cmd/demo
  ```
- **What to Observe**:
  - **Scenario 1**: Computes in/out-degree graph metrics for caller/callee microservice relations.
  - **Scenario 2**: Probes Target, Jaeger, and Prometheus health status.
  - **Scenario 3**: Drives stepped load (20 $\rightarrow$ 50 $\rightarrow$ 80 RPS), printing HTTP response counters.
  - **Scenario 4**: Watches Prometheus metrics stream live, detects a latency SLA breach at $220.5\text{ms} > 150.0\text{ms}$, and triggers an immediate in-memory safety halt!

---

## 📅 Log: August 27, 2026

### 🔹 Domain Models for Stress, Watcher, Safety & Capacity (PR #71)
- **Author / Contributor**: @drumilbhati
- **PR / Branch**: `feat/phase-3.3` (Merged: `0bc8064`)
- **Components Implemented**:
  - `internal/stress/load_plan.go` (`LoadPlan`) & `load_plan_test.go`
  - `internal/watcher/metric_snapshot.go` (`MetricSnapshot`) & `metric_snapshot_test.go`
  - `internal/safety/safety.go` (`SafetyPolicy`, `SafetyDecision`) & `safety_test.go`
  - `internal/capacity/capacity.go` (`Observation`, `CapacityResult`, `ExperimentReport`) & `capacity_test.go`
- **Core Logic & Design Rationale**:
  - Defined explicit data contracts and JSON serialization schemas for traffic plans, metric snapshots, SLA policies, and capacity reports.
  - Added unit tests validating JSON marshaling/unmarshaling and boundary fields.
- **Verification & Test Status**:
  - Unit tests pass for all serialization contracts.

---

## 📅 Log: August 26, 2026

### 🔹 Dependency Graph Models & Degree Calculations (PR #70)
- **Author / Contributor**: @drumilbhati
- **PR / Branch**: `feat/phase-3.2` (Merged: `20b19c9`)
- **Components Implemented**:
  - `internal/graph/graph.go` (`DependencyGraph`, `ServiceNode`, `DependencyEdge`, `ServiceScore`) & `graph_test.go`
  - `internal/observability/checker_integration_test.go` (graceful offline handling)
- **Core Logic & Design Rationale**:
  - Reconstructed caller-callee directed graph topology (`Nodes` and `Edges`).
  - Added graph mathematical operations: `InDegree(service)` for caller count and `OutDegree(service)` for callee dependency count.
  - Implemented offline skipping for live integration tests when Docker is not running.
- **Verification & Test Status**:
  - Unit tests pass in `internal/graph/graph_test.go`.

---

## 📅 Log: August 25, 2026

### 🔹 Experiment Lifecycle & 8-State Machine Models (PR #68)
- **Author / Contributor**: @drumilbhati
- **PR / Branch**: `feat/phase3.1` (Merged: `56d94cc`)
- **Components Implemented**:
  - `internal/experiment/experiment.go` & `experiment_test.go`
- **Core Logic & Design Rationale**:
  - Implemented discrete 8-state lifecycle enum:
    $$\text{CREATED} \rightarrow \text{PLANNED} \rightarrow \text{RUNNING} \rightarrow \text{STOPPING} \rightarrow \text{ANALYZING} \rightarrow \text{REPORTING} \rightarrow \text{COMPLETED} \ (\text{or } \text{ABORTED})$$
  - Added `ExperimentConfig.Validate()` to enforce boundary constraints on rates, durations, latency, and error percentage.
  - Added `IsTerminal()` helper for lifecycle management.
- **Verification & Test Status**:
  - Unit tests pass in `internal/experiment/experiment_test.go`.

---

## 📅 Log: August 24, 2026

### 🔹 Observability Stack & Connectivity Prober (PR #67)
- **Author / Contributor**: @drumilbhati
- **PR / Branch**: `feat/phase-1.2-Implement-observability-connectivity-probing-and-Jaeger/Prometheus-API-verification` (Merged: `71db91f`)
- **Components Implemented**:
  - `deployments/docker-compose.otel-demo.yaml` (OTel Demo target mesh)
  - `deployments/otel-collector-config.yaml`
  - `deployments/prometheus.yaml` (1s sub-second scrape configuration)
  - `internal/config/config.go` (`EnvironmentConfig`, `HealthStatus`)
  - `internal/observability/checker.go` (`Checker`) & `checker_test.go`
- **Core Logic & Design Rationale**:
  - Deployed self-contained OpenTelemetry Astronomy Shop Demo and Jaeger/Prometheus observability backends.
  - Built pre-flight diagnostic prober evaluating Target HTTP response, Jaeger REST API (`/api/services`), and Prometheus PromQL API (`/api/v1/query?query=up`).
- **Verification & Test Status**:
  - Mock server unit tests pass in `internal/observability/checker_test.go`.

# DAMASCUS — Implementation Roadmap & Execution Checklist

**Telemetry-Driven Autonomous Resilience & Capacity Testing Platform**

---

## 🗺️ Roadmap Overview

DAMASCUS is divided into **12 structured development phases**. By targeting standard OpenTelemetry environments (such as the official **OpenTelemetry Astronomy Shop Demo**) or any pre-instrumented microservice mesh, development is focused 100% on the core **DAMASCUS Control Plane**, graph scoring algorithms, real-time safety enforcement, and capacity analysis.

```mermaid
gantt
    title DAMASCUS Implementation Roadmap
    dateFormat  YYYY-MM-DD
    section Phase 1 - Foundations & Ingestion
    Target Environment & Observability Setup :p1, 2026-08-20, 2d
    Go Project Domain & Interfaces           :p2, after p1, 1d
    section Phase 2 - Control & Analysis Engines
    StressEngine & HTTP Worker Pools         :p3, after p2, 2d
    WatcherEngine & Prometheus Poller        :p4, after p3, 2d
    SafetyController & Fast-Path Cancel      :p5, after p4, 1d
    GraphAnalyser & Criticality Scoring      :p6, after p5, 2d
    section Phase 3 - Orchestration & Storage
    ExperimentManager 8-State Machine        :p7, after p6, 2d
    Kafka Event Backbone                     :p8, after p7, 2d
    PostgreSQL Repository & DDL              :p9, after p8, 2d
    section Phase 4 - Reporting & Delivery
    CapacityAnalyzer & ReportEngine          :p10, after p9, 2d
    REST API & React Dashboard               :p11, after p10, 3d
    Docker Compose, K8s & E2E Verification   :p12, after p11, 2d
```

---

## ✅ Master Implementation Checklist

### Phase 1: Target Environment & Observability Ingestion
> [!NOTE]
> DAMASCUS connects to standard cloud observability backends (Jaeger and Prometheus), allowing it to target standard suites like the OpenTelemetry Astronomy Shop Demo without manual microservice boilerplate.

- [ ] **1.1 Observability Stack Deployment**
  - [ ] Deploy OpenTelemetry Astronomy Shop Demo (or target mesh) with OTel Collector, Jaeger, and Prometheus.
  - [ ] Verify Jaeger tracing web UI and API availability (`http://localhost:16686`).
  - [ ] Verify Prometheus metrics endpoint scraping target services (`http://localhost:9090`).
- [ ] **1.2 Ingestion & Connectivity Probing**
  - [ ] Test Jaeger traces retrieval (`GET /api/traces?service=frontend`).
  - [ ] Test Jaeger dependencies graph retrieval (`GET /api/dependencies`).
  - [ ] Test Prometheus PromQL RED metrics queries (Rate, Error, Duration P50/P95/P99).
  - [ ] Implement `CheckObservabilityStack(ctx)` connectivity probe in Go.

---

### Phase 2: Go Domain Models & Interface Contracts
- [ ] **2.1 Directory Structure Scaffolding**
  - [ ] `cmd/damascus/` (CLI & daemon entrypoint).
  - [ ] `internal/config/` (environment configuration & flags).
  - [ ] `internal/graph/` (dependency graph models & scoring logic).
  - [ ] `internal/stress/` (load engine & worker pool).
  - [ ] `internal/watcher/` (Prometheus metric poller).
  - [ ] `internal/safety/` (safety controller & SLA evaluations).
  - [ ] `internal/experiment/` (orchestrator & 8-state machine).
  - [ ] `internal/capacity/` (capacity & recovery calculations).
  - [ ] `internal/report/` (JSON & HTML report builder).
  - [ ] `internal/events/` (Kafka producer & event schemas).
  - [ ] `internal/storage/` (PostgreSQL repository & migrations).
  - [ ] `internal/api/` (REST API handlers & routing).
- [ ] **2.2 Core Domain Structs & Enums**
  - [ ] Define `ExperimentState`, `ExperimentType`, `ExperimentConfig`, `Experiment`.
  - [ ] Define `DependencyGraph`, `ServiceNode`, `DependencyEdge`, `ServiceScore`.
  - [ ] Define `LoadPlan`, `MetricSnapshot`, `SafetyPolicy`, `SafetyDecision`.
  - [ ] Define `Observation`, `CapacityResult`, `ExperimentReport`, `DomainEvent`.
- [ ] **2.3 Core Interface Contracts**
  - [ ] Define `GraphAnalyzer`, `StressEngine`, `Watcher`, `SafetyController`.
  - [ ] Define `CapacityAnalyzer`, `ReportEngine`, `ExperimentRepository`, `EventProducer`.

---

### Phase 3: StressEngine & HTTP Worker Pools
- [ ] **3.1 Load Generation Engine**
  - [ ] Implement rate-regulated worker pool using `time.Ticker`.
  - [ ] Support stepped load plans (`InitialRate` $\rightarrow$ `StepRate` per `StepDuration`).
  - [ ] Support smooth linear ramp-up load plans.
- [ ] **3.2 Concurrency & Fast Teardown**
  - [ ] Bind worker goroutines strictly to execution `context.Context`.
  - [ ] Implement immediate zero-leak goroutine termination on `<-ctx.Done()`.
  - [ ] Unit tests for rate precision, step transitions, and cancellation responsiveness.

---

### Phase 4: WatcherEngine & Prometheus Poller
- [ ] **4.1 PromQL Client Implementation**
  - [ ] Implement sub-second PromQL query client for request rate (RPS).
  - [ ] Query P50, P95, and P99 latency percentiles via histogram quantiles.
  - [ ] Calculate error rate percentage (`5xx` errors / total requests) and availability.
  - [ ] Scrape CPU and Memory container utilization metrics where available.
- [ ] **4.2 Streaming Metric Snapshots**
  - [ ] Stream snapshots through Go channel (`<-chan watcher.MetricSnapshot`).
  - [ ] Handle Prometheus query timeouts and network resilience.
  - [ ] Unit & integration tests against mock Prometheus API server.

---

### Phase 5: SafetyController & Fast-Path Context Cancellation
> [!IMPORTANT]
> Emergency halts execute directly in-memory via `context.CancelFunc()`, guaranteeing sub-millisecond reaction times independent of Kafka or network stability.

- [ ] **5.1 Threshold Evaluation Logic**
  - [ ] Implement SLA limit checks: P95 latency > `MaxP95LatencyMs`.
  - [ ] Implement SLA limit checks: Error rate > `MaxErrorRate`.
  - [ ] Implement SLA limit checks: Availability < `MinAvailability`.
- [ ] **5.2 Fast-Path Circuit Breaker Wiring**
  - [ ] Connect `WatcherEngine` $\rightarrow$ `SafetyController` $\rightarrow$ `context.CancelFunc`.
  - [ ] Benchmark cancellation latency to verify sub-millisecond execution.
  - [ ] Unit tests with boundary threshold fixtures (normal, threshold-edge, hard-breach).

---

### Phase 6: GraphAnalyser & Criticality Scoring Engine
- [ ] **6.1 Trace Ingestion & Graph Construction**
  - [ ] Query Jaeger trace API (`/api/traces` and `/api/dependencies`).
  - [ ] Construct directed `DependencyGraph` with nodes and weighted edges (call counts & frequencies).
- [ ] **6.2 Criticality Scoring Formula**
  - [ ] Calculate $\text{InDegree}(v)$, $\text{OutDegree}(v)$, $\text{CallFreq}(v)$, $\text{Depth}(v)$, $\text{SPOF}(v)$.
  - [ ] Compute weighted normalized score $S(v) \in [0.0, 1.0]$.
  - [ ] Generate human-readable rationale explanations for each score.
  - [ ] Unit tests verifying score accuracy against complex multi-tier topologies.

---

### Phase 7: ExperimentManager (8-State Orchestrator)
- [ ] **7.1 State Machine Implementation**
  - [ ] Implement states: `CREATED` $\rightarrow$ `PLANNED` $\rightarrow$ `RUNNING` $\rightarrow$ `STOPPING` $\rightarrow$ `ANALYZING` $\rightarrow$ `REPORTING` $\rightarrow$ `COMPLETED` (or `ABORTED`).
  - [ ] Enforce valid state transition matrix and reject invalid jumps.
- [ ] **7.2 Component Lifecycle Orchestration**
  - [ ] Initialize execution contexts and wire `StressEngine`, `WatcherEngine`, `SafetyController`.
  - [ ] Manage post-stress recovery cooldown observation window.
  - [ ] Unit tests for end-to-end state machine transitions and error recovery.

---

### Phase 8: Kafka Event Backbone
- [ ] **8.1 Kafka Producer Implementation**
  - [ ] Setup Kafka producer client using `franz-go` or `sarama`.
  - [ ] Implement JSON serialization for `DomainEvent` envelopes.
- [ ] **8.2 Asynchronous Domain Event Streaming**
  - [ ] Publish state transition events (`EXPERIMENT_STARTED`, `EXPERIMENT_STOPPED`, etc.).
  - [ ] Publish safety breach events (`SAFETY_STOP_TRIGGERED`) asynchronously.
  - [ ] Publish metric snapshot feeds to topic `experiment-events`.
  - [ ] Integration tests verifying event delivery to Kafka brokers.

---

### Phase 9: PostgreSQL Repository & Persistence
- [ ] **9.1 DDL Schema & Database Migrations**
  - [ ] Create migration script `schema.sql` for tables:
    - [ ] `services` & `dependencies`
    - [ ] `experiments` & `service_scores`
    - [ ] `observations` & `safety_events`
    - [ ] `reports`
- [ ] **9.2 SQL Repository Layer**
  - [ ] Implement `PostgresExperimentRepository` with `database/sql` & `pgx`.
  - [ ] Implement transactions for state updates and bulk observation inserts.
  - [ ] Integration tests using real or containerized PostgreSQL instance.

---

### Phase 10: CapacityAnalyzer & ReportEngine
- [ ] **10.1 Capacity Analytics Engine**
  - [ ] Calculate **Maximum Tested Rate** (highest RPS injected).
  - [ ] Calculate **Maximum Sustainable Rate** (highest RPS meeting all SLA bounds).
  - [ ] Detect **Degradation Point** (exact RPS where latency/error inflection occurs).
  - [ ] Determine **Safety Boundary Rate** (load triggering safety halt).
  - [ ] Compute **Recovery Time** (seconds taken for metrics to stabilize post-stress).
- [ ] **10.2 Report Generator**
  - [ ] Generate structured JSON reports.
  - [ ] Render self-contained HTML reports with embedded charts.
  - [ ] Formulate automated optimization recommendations based on bottleneck findings.

---

### Phase 11: REST API & React Web Dashboard
- [ ] **11.1 REST API Routes**
  - [ ] `GET /api/health` — Observability & backend connectivity status.
  - [ ] `GET /api/dependencies` — Dependency graph & criticality rankings.
  - [ ] `POST /api/experiments` — Create experiment configuration.
  - [ ] `POST /api/experiments/{id}/start` — Trigger planned experiment.
  - [ ] `POST /api/experiments/{id}/stop` — Manual emergency stop.
  - [ ] `GET /api/experiments` — List historical experiments.
  - [ ] `GET /api/experiments/{id}` — Get experiment details.
  - [ ] `GET /api/experiments/{id}/status` — Stream live telemetry snapshot.
  - [ ] `GET /api/experiments/{id}/report` — Retrieve final report.
- [ ] **11.2 React / Vite UI Dashboard**
  - [ ] Interactive dependency graph visualizer (e.g. React Flow or Cytoscape).
  - [ ] Criticality ranking leaderboard.
  - [ ] Real-time live metric telemetry graphs (RPS, Latency, Error Rate).
  - [ ] Capacity and resilience report viewer.

---

### Phase 12: Packaging, Kubernetes Deployment & E2E Verification
- [ ] **12.1 Containerization & Docker Compose**
  - [ ] Multi-stage `Dockerfile` for DAMASCUS control plane binary.
  - [ ] Unified `docker-compose.yml` bundling Target Mesh, OTel Collector, Jaeger, Prometheus, PostgreSQL, Kafka, and DAMASCUS.
- [ ] **12.2 Kubernetes Manifests**
  - [ ] Deployments, Services, and ConfigMaps for DAMASCUS components.
- [ ] **12.3 End-to-End Verification Scenarios**
  - [ ] Scenario A: Nominal stepped load test reaching maximum capacity.
  - [ ] Scenario B: High-intensity stress triggering sub-second safety stop.
  - [ ] Scenario C: Dependency degradation analysis and recovery timing verification.

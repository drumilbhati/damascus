# DAMASCUS — Implementation Roadmap & GitHub Issue Tracker

**Telemetry-Driven Autonomous Resilience & Capacity Testing Platform**

---

## 🗺️ System Overview & Workload Allocation

DAMASCUS is partitioned into **12 structured development phases** mapped across **36 GitHub Issues (Issues #25 – #60)**. Work is balanced equally across the 4 core team members (9 issues each) to ensure parallel progression without blocking cross-phase dependencies.

### Team Members
* **`drumilbhati`** — Drumil Bhati
* **`Dhairya0531`** — Dhairya Rupani
* **`DevKansara97`** — Dev Kansara
* **`VidhanNahar`** — Vidhan Nahar

---

## 📊 Team Issue Distribution Matrix

| Phase | Phase Name | `drumilbhati` | `Dhairya0531` | `DevKansara97` | `VidhanNahar` |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Phase 1** | Target Mesh & Ingestion | **#25** OTel Demo Stack | **#26** Stack Checker | **#27** Domain Models | — |
| **Phase 2** | Stress Engine | **#28** Rate Engine | **#29** HTTP Client | **#30** Ramp Planning | — |
| **Phase 3** | Watcher Engine | — | — | — | **#31** PromQL Client<br>**#32** Stream Channels<br>**#33** Resource Scraper |
| **Phase 4** | Safety Controller | — | — | **#34** SLA Thresholds | **#35** Circuit Breaker |
| **Phase 5** | Graph Analyser | **#36** Trace Ingestion | **#37** Graph Scoring | **#38** Criticality Explainer | — |
| **Phase 6** | Experiment Orchestrator | **#39** 8-State FSM | **#40** Wiring Manager | **#41** Cooldown Monitor | — |
| **Phase 7** | Kafka Event Backbone | **#42** Kafka Producer | **#43** State Publisher | **#44** Telemetry Stream | — |
| **Phase 8** | Persistence (PostgreSQL)| **#45** Schema & DDL | **#46** Postgres Repo | — | **#47** Bulk Ingest |
| **Phase 9** | Capacity & Reporting | **#48** Knee Detector | **#49** Recovery Calc | **#50** Report Engine | — |
| **Phase 10**| REST API & UI | **#51** REST API | **#52** React App | **#53** Graph Visualizer | **#54** Metric Charts |
| **Phase 11**| Container & K8s Deploy | — | — | — | **#55** Dockerfile<br>**#56** Compose Stack<br>**#57** K8s Manifests |
| **Phase 12**| E2E Verification | **#58** Stepped Capacity | **#59** Sub-Second Cutoff| **#60** Blast Radius | — |
| **Total** | **36 Issues** | **9 Issues** | **9 Issues** | **9 Issues** | **9 Issues** |

---

## 📋 Detailed Phase Breakdown & Issue Tracker

### Phase 1: Target Environment & Observability Foundations
* **[x] Issue #25: Deploy OpenTelemetry Astronomy Shop Demo Stack**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/deployments/docker-compose.otel-demo.yaml`, `internal/deployments/prometheus.yaml`, `internal/deployments/otel-collector-config.yaml`
  * **Scope**: Deploy 10+ Astronomy Shop microservices, Jaeger (`:16686`), Prometheus (`:9090`), OTel Collector (`:4317/:4318`), and Valkey cache.
  * **Status**: `COMPLETED` (All 15 containers running and healthy).

* **[x] Issue #26: Observability Connectivity & Health Prober**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/observability/checker.go`, `internal/observability/checker_test.go`, `internal/observability/checker_integration_test.go`
  * **Scope**: Implement `CheckObservabilityStack` testing reachability and Prometheus/Jaeger connectivity with structured status reporting.
  * **Status**: `COMPLETED` (100% tests passing, live integration verified).

* **[x] Issue #27: Core Domain Models & Interface Contracts**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/experiment/`, `internal/graph/`, `internal/capacity/`, `internal/watcher/`, `internal/safety/`, `internal/events/`, `internal/interfaces/`
  * **Scope**: Define canonical Go data models, enums, JSON tags, and subsystem interface contracts.
  * **Status**: `COMPLETED` (100% test coverage across all domain models).

---

### Phase 2: StressEngine (Traffic Generation)
* **[x] Issue #28: Rate-Regulated Traffic Engine & Cancellation Loop**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/stress/engine.go`, `internal/stress/engine_test.go`
  * **Scope**: Implement rate-controlled dispatch loop using `time.Ticker`, `sync.WaitGroup`, single-run mutex guards, and sub-millisecond `<-ctx.Done()` fast-path abort.
  * **Status**: `COMPLETED` (0 race conditions, 100% tests passing).

* **[x] Issue #29: High-Throughput HTTP Client & Connection Pooling**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/stress/client.go`, `internal/stress/client_test.go`
  * **Scope**: Reusable HTTP transport with 1000 max idle conns, TCP socket keep-alive, per-request deadlines, and response body draining.
  * **Status**: `COMPLETED` (100% tests passing).

* **[ ] Issue #30: Stepped and Linear Ramp Load Planning**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/stress/ramp.go`, `internal/stress/ramp_test.go`
  * **Scope**: Implement step-wise plateaus (`InitialRate` $\rightarrow$ `StepRate` per `StepDuration`) and smooth linear interpolation rate transitions.
  * **Status**: `PENDING`

---

### Phase 3: WatcherEngine (Prometheus Telemetry Poller)
* **[ ] Issue #31: PromQL RED Metrics Query Client**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `internal/watcher/poller.go`, `internal/watcher/poller_test.go`
  * **Scope**: Sub-second PromQL queries for Request Rate (RPS), Error Rate (5xx percentage), and Latency Percentiles (P50, P95, P99) via histogram quantiles.
  * **Status**: `PENDING`

* **[ ] Issue #32: Telemetry Snapshot Streaming Channels**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `internal/watcher/stream.go`, `internal/watcher/stream_test.go`
  * **Scope**: Stream periodic `MetricSnapshot` updates via Go channels (`<-chan MetricSnapshot`) with non-blocking buffer management and query timeout resilience.
  * **Status**: `PENDING`

* **[ ] Issue #33: Container Resource Utilization Scraper**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `internal/watcher/system.go`, `internal/watcher/system_test.go`
  * **Scope**: Query container CPU core utilization and resident memory usage (RSS) from Prometheus cAdvisor metrics.
  * **Status**: `PENDING`

---

### Phase 4: SafetyController (Real-Time Circuit Breaker)
* **[ ] Issue #34: Multi-Dimensional SLA Threshold Evaluator**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/safety/evaluator.go`, `internal/safety/evaluator_test.go`
  * **Scope**: Evaluate live metric snapshots against `SafetyPolicy` (`MaxP95LatencyMs`, `MaxErrorRatePercent`, `MinAvailabilityPercent`), returning `SafetyDecision`.
  * **Status**: `PENDING`

* **[ ] Issue #35: Fast-Path Context Cancellation Circuit Breaker**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `internal/safety/circuit_breaker.go`, `internal/safety/circuit_breaker_test.go`
  * **Scope**: Wire `WatcherEngine` $\rightarrow$ `SafetyController` $\rightarrow$ `context.CancelFunc()`. Ensure safety trip executes in $< 100\text{ms}$ in-memory without external broker dependencies.
  * **Status**: `PENDING`

---

### Phase 5: GraphAnalyser (Trace Ingest & Criticality Scoring)
* **[x] Issue #36: OpenTelemetry Trace Span Ingestion & Graph Builder**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/graph/analyser.go`, `internal/graph/analyser_test.go`
  * **Scope**: Ingest distributed spans from Jaeger REST API (`/api/services`, `/api/traces`), resolve cross-service RPC links, and populate `DependencyGraph` adjacency list.
  * **Status**: `COMPLETED` (89.2% test coverage, query escaping and status validation verified).

* **[ ] Issue #37: Graph Criticality Scoring Algorithm**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/graph/scoring.go`, `internal/graph/scoring_test.go`
  * **Scope**: Implement topological scoring formula $S(v) = w_1 \cdot \text{InDeg} + w_2 \cdot \text{OutDeg} + w_3 \cdot \text{Freq} + w_4 \cdot \text{Depth} + w_5 \cdot \text{SPOF}$.
  * **Status**: `PENDING`

* **[ ] Issue #38: Criticality Explanation & Bottleneck Rationale Generator**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/graph/explainer.go`, `internal/graph/explainer_test.go`
  * **Scope**: Generate human-readable explanations detailing why a service scored high (e.g. single point of failure, deep downstream blast radius).
  * **Status**: `PENDING`

---

### Phase 6: ExperimentManager (8-State Machine Orchestrator)
* **[ ] Issue #39: 8-State Finite State Machine**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/experiment/fsm.go`, `internal/experiment/fsm_test.go`
  * **Scope**: Implement state lifecycle `CREATED` $\rightarrow$ `PLANNED` $\rightarrow$ `RUNNING` $\rightarrow$ `STOPPING` $\rightarrow$ `ANALYZING` $\rightarrow$ `REPORTING` $\rightarrow$ `COMPLETED` / `ABORTED` with strict transition guards.
  * **Status**: `PENDING`

* **[ ] Issue #40: Subsystem Wiring & Lifecycle Manager**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/experiment/manager.go`, `internal/experiment/manager_test.go`
  * **Scope**: Orchestrate `StressEngine`, `WatcherEngine`, `SafetyController`, and repository persistence within unified execution context.
  * **Status**: `PENDING`

* **[ ] Issue #41: Post-Stress Cooldown & Recovery Monitor**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/experiment/cooldown.go`, `internal/experiment/cooldown_test.go`
  * **Scope**: Manage stabilization window post-traffic injection to observe metric recovery and calculate true time-to-recover ($T_{\text{recovery}}$).
  * **Status**: `PENDING`

---

### Phase 7: Kafka Event Backbone
* **[ ] Issue #42: Kafka Event Producer Client**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/events/producer.go`, `internal/events/producer_test.go`
  * **Scope**: Initialize thread-safe Kafka producer using `franz-go` or `sarama` with JSON serialization and retry backoff.
  * **Status**: `PENDING`

* **[ ] Issue #43: State Transition & Safety Breach Event Publisher**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/events/publisher.go`, `internal/events/publisher_test.go`
  * **Scope**: Publish asynchronous `DomainEvent` envelopes for state transitions (`EXPERIMENT_STARTED`, `EXPERIMENT_STOPPED`, `SAFETY_STOP_TRIGGERED`) to topic `experiment-events`.
  * **Status**: `PENDING`

* **[ ] Issue #44: Live Metric Telemetry Kafka Streamer**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/events/streamer.go`, `internal/events/streamer_test.go`
  * **Scope**: Stream live `MetricSnapshot` objects to Kafka topic `metric-telemetry` for consumption by external dashboards and alerting systems.
  * **Status**: `PENDING`

---

### Phase 8: PostgreSQL Persistence Layer
* **[ ] Issue #45: Database Migrations & DDL Schema**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/storage/schema.sql`, `internal/storage/migrations/`
  * **Scope**: Define relational tables: `services`, `dependencies`, `experiments`, `service_scores`, `observations`, `safety_events`, and `reports`.
  * **Status**: `PENDING`

* **[ ] Issue #46: PostgreSQL Experiment Repository**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/storage/postgres.go`, `internal/storage/postgres_test.go`
  * **Scope**: Implement `ExperimentRepository` interface with `database/sql` + `pgx`, connection pooling, and atomic transaction updates.
  * **Status**: `PENDING`

* **[ ] Issue #47: High-Throughput Bulk Observation Ingestion**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `internal/storage/bulk.go`, `internal/storage/bulk_test.go`
  * **Scope**: Implement batch insertion (`COPY` or multi-row `INSERT`) for time-series metric observations recorded during stress tests.
  * **Status**: `PENDING`

---

### Phase 9: CapacityAnalyzer & ReportEngine
* **[ ] Issue #48: Capacity Curve Knee & Saturation Detector**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/capacity/analyzer.go`, `internal/capacity/analyzer_test.go`
  * **Scope**: Analyze $(X, Y)$ throughput vs P95 latency curve to identify maximum sustainable rate, degradation knee point, and safety boundary.
  * **Status**: `PENDING`

* **[ ] Issue #49: Recovery Time & Blast Radius Calculator**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `internal/capacity/recovery.go`, `internal/capacity/recovery_test.go`
  * **Scope**: Measure time elapsed between stress cutoff and P95 latency returning to $\le 110\%$ of baseline; compute downstream blast radius.
  * **Status**: `PENDING`

* **[ ] Issue #50: Structured JSON & Self-Contained HTML Report Generator**
  * **Assignee**: `@DevKansara97`
  * **Files**: `internal/report/engine.go`, `internal/report/engine_test.go`, `internal/report/templates/`
  * **Scope**: Generate `ExperimentReport` JSON artifacts and render standalone HTML reports with embedded SVG/HTML5 charts.
  * **Status**: `PENDING`

---

### Phase 10: REST API & React Web Dashboard
* **[ ] Issue #51: Control Plane REST API Routing & Handlers**
  * **Assignee**: `@drumilbhati`
  * **Files**: `internal/api/router.go`, `internal/api/handlers.go`, `internal/api/api_test.go`
  * **Scope**: Implement HTTP endpoints (`GET /api/health`, `GET /api/dependencies`, `POST /api/experiments`, `POST /api/experiments/{id}/start`, `POST /api/experiments/{id}/stop`, `GET /api/experiments/{id}/report`).
  * **Status**: `PENDING`

* **[ ] Issue #52: React / Vite Web Dashboard Scaffolding**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `web/package.json`, `web/src/App.tsx`, `web/src/pages/Dashboard.tsx`
  * **Scope**: Modern React + TypeScript + Tailwind UI scaffold for managing experiments, trigger controls, and status monitoring.
  * **Status**: `PENDING`

* **[ ] Issue #53: Interactive Microservice Dependency Graph Visualizer**
  * **Assignee**: `@DevKansara97`
  * **Files**: `web/src/components/GraphView.tsx`, `web/src/components/CriticalityLeaderboard.tsx`
  * **Scope**: Render directed microservice topology with node criticality color highlights, edge weights, and failure impact animations.
  * **Status**: `PENDING`

* **[ ] Issue #54: Real-Time Telemetry Charts & Capacity Report Viewer**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `web/src/components/TelemetryView.tsx`, `web/src/components/ReportViewer.tsx`
  * **Scope**: Real-time live line charts (RPS, Latency P50/P95/P99, Error Rate) and interactive capacity degradation curve viewer.
  * **Status**: `PENDING`

---

### Phase 11: Packaging, Docker & Kubernetes Deployment
* **[ ] Issue #55: Multi-Stage Production Dockerfile**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `Dockerfile`, `.dockerignore`
  * **Scope**: Minimal scratch/alpine container image compiling DAMASCUS Go control plane binary.
  * **Status**: `PENDING`

* **[ ] Issue #56: Unified Production Docker Compose Stack**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `docker-compose.yml`
  * **Scope**: Single-command deployment bundling Target Mesh, OTel Collector, Jaeger, Prometheus, PostgreSQL, Kafka, and DAMASCUS.
  * **Status**: `PENDING`

* **[ ] Issue #57: Kubernetes Deployment Manifests & Helm Charts**
  * **Assignee**: `@VidhanNahar`
  * **Files**: `deployments/k8s/damascus-deployment.yaml`, `deployments/k8s/damascus-service.yaml`, `deployments/k8s/configmap.yaml`
  * **Scope**: K8s manifests with resource limits, liveness/readiness probes, and horizontal scaling.
  * **Status**: `PENDING`

---

### Phase 12: End-to-End Verification Scenarios
* **[ ] Issue #58: Nominal Stepped Load Capacity E2E Scenario**
  * **Assignee**: `@drumilbhati`
  * **Files**: `tests/e2e/nominal_capacity_test.go`
  * **Scope**: Automated E2E test verifying stepped traffic ramping until reaching maximum capacity with valid report generation.
  * **Status**: `PENDING`

* **[ ] Issue #59: Sub-Second Safety Cutoff & Circuit Breaker E2E Scenario**
  * **Assignee**: `@Dhairya0531`
  * **Files**: `tests/e2e/safety_circuit_breaker_test.go`
  * **Scope**: Automated E2E test injecting extreme traffic, asserting circuit breaker trips in $< 100\text{ms}$ upon threshold breach with 0 leaked goroutines.
  * **Status**: `PENDING`

* **[ ] Issue #60: Downstream Blast Radius & Recovery Timing E2E Scenario**
  * **Assignee**: `@DevKansara97`
  * **Files**: `tests/e2e/recovery_blast_radius_test.go`
  * **Scope**: Automated E2E test verifying downstream service impact correlation, graph edge highlighting, and recovery timing accuracy.
  * **Status**: `PENDING`

---

## 📋 Pre-PR Checklist Before Submitting Any Issue
- [ ] Code strictly follows package conventions (`internal/` for control plane, `services/` for target mesh).
- [ ] Code builds without errors or warnings (`go build ./...`).
- [ ] All unit and integration tests pass cleanly with 0 race conditions (`go test -v -race ./...`).
- [ ] No hardcoded secrets, database credentials, or absolute local paths.
- [ ] Documented any interface or model updates in [`docs/LLD.md`](./LLD.md).
- [ ] PR title follows conventional commits format (e.g. `feat(graph): implement trace span ingestion and dependency graph constructor`).

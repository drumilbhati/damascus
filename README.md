# DAMASCUS — Dependency-Aware Microservice Analysis, Capacity, and Stress-testing Utility System

> **Cloud Computing Course Project & Autonomous Resilience Engineering Platform**

**DAMASCUS is an autonomous chaos-testing and capacity-profiling control platform for distributed microservices. By connecting to standard cloud observability backends (Jaeger and Prometheus), DAMASCUS automatically discovers service dependencies, calculates structural criticality scores, executes controlled stress experiments, enforces real-time closed-loop safety stops, and profiles sustainable capacity boundaries.**
---

## 💡 System Mental Model

```text
 ┌────────────────────────────────────────────────────────────────────────┐
 │            Target Microservice Mesh (e.g. OpenTelemetry Demo)          │
 │                                                                        │
 │   [ Frontend ] ────> [ Checkout ] ────> [ Payment ] ───> [ Database ]  │
 │                                    └─> [ Inventory ]                   │
 └────────────────────────────────────┬───────────────────────────────────┘
                                      │ OpenTelemetry Spans & RED Metrics
                                      ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │                      Observability Infrastructure                      │
 │                 Jaeger (Traces)   +   Prometheus (Metrics)             │
 └────────────────────────────────────┬───────────────────────────────────┘
                                      │
                                      ▼
 ┌────────────────────────────────────────────────────────────────────────┐
 │                       DAMASCUS Control Platform                        │
 │                                                                        │
 │  Discover Telemetry ──> Build Dependency Graph ──> Rank Criticality    │
 │                                                           │            │
 │  Capacity & Recovery <── Fast-Path Safety Stop <─── Controlled Load    │
 └────────────────────────────────────┬───────────────────────────────────┘
                                      │
                 ┌────────────────────┴───────────────────┐
                 ▼                                        ▼
    PostgreSQL (Durable Data)                Kafka (`experiment-events`)
```

---

## 🚀 Quick Start: Launching the Target Playground

DAMASCUS targets standard OpenTelemetry-instrumented environments. You can launch the full **OpenTelemetry Astronomy Shop Demo** along with its complete observability stack using the self-contained compose bundle in [`deployments/`](deployments/):

```bash
# Start target microservices, OTel Collector, Jaeger, and Prometheus
docker compose -f deployments/docker-compose.otel-demo.yaml up -d
```

### 🌐 Live Service & Observability Endpoints

| Service | Local Endpoint | Description |
| :--- | :--- | :--- |
| 🔭 **Astronomy Web Shop** | [**`http://localhost:8080/`**](http://localhost:8080/) | Target e-commerce store with live product catalog and checkout flows. |
| 🔍 **Jaeger Traces UI & API** | [**`http://localhost:16686/`**](http://localhost:16686/) | Distributed trace trees and REST API (`/api/traces`, `/api/dependencies`). |
| 📈 **Prometheus Metrics** | [**`http://localhost:9090/`**](http://localhost:9090/) | Time-series metrics engine and PromQL API (`/api/v1/query`). |

To stop the target environment:

```bash
docker compose -f deployments/docker-compose.otel-demo.yaml down
```

---

## 📚 Project Documentation

The technical specifications and architectural designs are structured into modular guides:

- **[System Architecture (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md)**: Control plane vs. data plane decomposition, telemetry integration flow, and closed-loop MAPE-K architecture.
- **[Low-Level Design (`docs/LLD.md`)](docs/LLD.md)**: Go domain models, interface contracts, 8-state machine, criticality scoring formula, database DDLs, and REST API specifications.
- **[Cloud Computing Concepts (`docs/CLOUD_COMPUTING_CONCEPTS.md`)](docs/CLOUD_COMPUTING_CONCEPTS.md)**: Academic mapping to core cloud principles, telemetry-derived intelligence, Kafka event backbone vs. in-memory safety stop, and comparative analysis against traditional load tools.
- **[Implementation Roadmap & Checklist (`docs/IMPLEMENTATION_PLAN.md`)](docs/IMPLEMENTATION_PLAN.md)**: 12-phase execution roadmap and master deliverable checklist.

---

## 🏗️ Core System Components

| Component | Responsibility |
| :--- | :--- |
| **`GraphAnalyser`** | Ingests Jaeger distributed traces, constructs topology graphs, and scores service criticality ($S(v)$). |
| **`ExperimentManager`** | Orchestrates the 8-state experiment lifecycle (`CREATED` $\rightarrow$ `RUNNING` $\rightarrow$ `COMPLETED`). |
| **`StressEngine`** | Spawns goroutine worker pools to generate stepped and ramp HTTP/gRPC traffic. |
| **`WatcherEngine`** | Polls Prometheus at 1-second intervals for real-time RED metrics (Rate, Errors, Latency P50/P95/P99). |
| **`SafetyController`** | Evaluates observations against SLA limits and triggers fast-path in-memory `context.Context` aborts. |
| **`CapacityAnalyzer`** | Computes maximum sustainable rate, degradation knee, and post-stress recovery time. |
| **`ReportEngine`** | Compiles structured JSON deliverables and downloadable HTML dashboards. |
| **`Kafka Backbone`** | Asynchronously streams domain events (`experiment-events`) for multi-subscriber auditing and UI. |
| **`PostgreSQL`** | Persists experiments, dependencies, observations, and capacity reports. |

---

## 📁 Repository Structure

```text
damascus/
├── README.md                           # Project summary, quick start & architecture links
├── go.mod / go.sum                     # Go module definitions
├── deployments/                        # Target environment & infrastructure bundles
│   ├── docker-compose.otel-demo.yaml   # Standalone OpenTelemetry Demo stack
│   ├── otel-collector-config.yaml      # OTel Collector trace & metric pipelines
│   └── prometheus.yaml                 # 1s sub-second scraping configuration
└── docs/                               # System specifications & roadmap
    ├── ARCHITECTURE.md                 # System architecture & MAPE-K loop
    ├── LLD.md                          # Low-Level Design, Go models & DDL
    ├── CLOUD_COMPUTING_CONCEPTS.md     # Cloud resilience principles & academic mapping
    ├── IMPLEMENTATION_PLAN.md          # 12-phase execution plan & master checklist
    └── hld.png                         # High-level architecture diagram
```

---

## ⚡ Core Operational Pipeline

$$\text{Discover Telemetry} \longrightarrow \text{Build Graph} \longrightarrow \text{Rank Criticality} \longrightarrow \text{Controlled Load} \longrightarrow \text{Observe Metrics} \longrightarrow \text{Safety Stop} \longrightarrow \text{Capacity Analysis} \longrightarrow \text{Report}$$

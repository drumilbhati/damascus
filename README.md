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

DAMASCUS targets standard OpenTelemetry-instrumented environments. Launch the control plane, dashboard, **OpenTelemetry Astronomy Shop Demo**, and observability stack with the root Compose file:

```bash
cp .env.example .env
docker compose up -d --build
```

### 🌐 Live Service & Observability Endpoints

| Service | Local Endpoint | Description |
| :--- | :--- | :--- |
| 🖥️ **DAMASCUS Dashboard** | [**`http://localhost:3000/`**](http://localhost:3000/) | Web dashboard. |
| ⚙️ **DAMASCUS Control Plane** | [**`http://localhost:8080/api/health`**](http://localhost:8080/api/health) | REST API health endpoint. |
| 🔭 **Astronomy Web Shop** | [**`http://localhost:8085/`**](http://localhost:8085/) | Target e-commerce store with live product catalog and checkout flows. |
| 🔍 **Jaeger Traces UI & API** | [**`http://localhost:16686/`**](http://localhost:16686/) | Distributed trace trees and REST API (`/api/traces`, `/api/dependencies`). |
| 📈 **Prometheus Metrics** | [**`http://localhost:9090/`**](http://localhost:9090/) | Time-series metrics engine and PromQL API (`/api/v1/query`). |

To stop the target environment:

```bash
docker compose down
```

### Directly targeting a service

Jaeger provides service names, not callable URLs. To prefill direct targets in the experiment form, set `SERVICE_TARGET_URLS` in `.env` to a JSON map keyed by the exact telemetry service name, for example:

```dotenv
SERVICE_TARGET_URLS={"frontend":"http://frontend:8080/","my-http-service":"http://my-http-service:8080/test"}
```

The default Compose stack includes an internal `otel-demo-http-gateway` that translates JSON HTTP calls into the OTel Demo's gRPC methods and forwards its HTTP-only quote/email operations. Example direct targets include `http://otel-demo-http-gateway:8090/v1/CurrencyService/Convert` and `http://otel-demo-http-gateway:8090/v1/CheckoutService/PlaceOrder`. These URLs are reachable only inside the Compose network by default.

The dependency graph returns configured URLs and selecting a service pre-fills its target. On the experiment detail page, choose GET or POST and, for POST, provide the protobuf-JSON request body. GET adapter requests can carry a JSON body in a URL-encoded `request` query parameter; the sample `.env.example` provides repeatable read-only defaults. The gateway refuses GET for state-changing operations such as cart updates, shipping orders, charges, checkout, and email. POST bodies are replayed for every scheduled request and can mutate demo data or create simulated transactions, so use a disposable test environment. The UI asks for confirmation before starting any POST plan.

For example, to load-test currency conversion directly, select `currencyservice`; the default URL targets `CurrencyService.Convert`. To customize the operation, set the method to POST and use a body such as `{"from":{"currencyCode":"USD","units":1},"toCode":"EUR"}`. To load-test checkout, select `checkoutservice`, choose POST, and provide the demo's `PlaceOrder` request JSON (user, address, email, and test card). Each generated request repeats that body, so checkout can create multiple demo orders.

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
| **`StressEngine`** | Generates stepped or fixed-rate HTTP traffic; gRPC services can be exercised through an HTTP gateway. |
| **`WatcherEngine`** | Polls Prometheus at 1-second intervals for real-time RED metrics (Rate, Errors, Latency P50/P95/P99). |
| **`SafetyController`** | Evaluates observations against SLA limits and triggers fast-path in-memory `context.Context` aborts. |
| **`CapacityAnalyzer`** | Computes maximum sustainable rate, degradation knee, and post-stress recovery time. |
| **`ReportEngine`** | Compiles structured JSON deliverables and downloadable HTML dashboards. |
| **`Kafka Backbone`** | Asynchronously streams domain events (`experiment-events`) for multi-subscriber auditing and UI. |
| **`PostgreSQL`** | Persists experiments, dependencies, observations, and capacity reports. |

---

## Load strategies in the UI

As of November 13, 2026, the New Experiment form offers:

- **Step (default):** Hold the initial RPS for the step duration, increase by the configured increment, and repeat up to the maximum RPS.
- **Fixed:** Hold one target RPS for the run duration. The form shows only the fixed rate and duration; the API normalizes `max_rate` to `initial_rate` and `step_rate` to zero before validating the configuration.

**Ramp is temporarily hidden from the creation dropdown** because it currently uses the same stepped scheduler as Step. Existing ramp records remain visible, and the backend still accepts `ramp` for compatibility. Restore the UI option only once a distinct time-based ramp scheduler and its strategy-specific inputs are implemented.

Rates are offered targets, not measured throughput. Capacity estimates require at least 30 seconds of valid observations at a constant load level; use hold/run durations longer than 30 seconds to allow for sampling.

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

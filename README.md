# DAMASCUS — Dependency-Aware Microservice Analysis, Capacity, and Stress-testing Utility System

> **Cloud Computing Course Project**

**DAMASCUS is an internal chaos-testing and resilience-analysis platform for engineering teams operating microservice systems. Teams instrument their services with OpenTelemetry, provide DAMASCUS access to the environment telemetry, and DAMASCUS automatically builds a dependency-aware model, identifies structurally critical microservices, executes controlled load/fault experiments, and reports sustainable capacity limits and recovery bottlenecks.**

---

## 💡 Core Mental Model

```
 ┌────────────────────────────────────────────────────────────────────────┐
 │                      Instrumented Microservice Environment            │
 │                                                                        │
 │   [ Gateway ] ────> [ Order ] ────> [ Payment ] ───> [ Database ]      │
 │                                 └─> [ Inventory ]                      │
 └────────────────────────────────────┬───────────────────────────────────┘
                                      │
                         OpenTelemetry & Prometheus
                                      │
                                      v
 ┌────────────────────────────────────────────────────────────────────────┐
 │                      DAMASCUS Control Platform                         │
 │                                                                        │
 │ Discover Telemetry ──> Build Dependency Graph ──> Rank Criticality     │
 │                                                         │              │
 │ Report & Recovery <── Calculate Capacity <── Safety Stop <── Load Test  │
 └────────────────────────────────────────────────────────────────────────┘
```

---

## 📚 Project Documentation

The technical design and specifications are structured into detailed modules:

- **[System Architecture (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md)**: Control plane vs. data plane decomposition, telemetry integration flow, and core MAPE-K control loop.
- **[Low-Level Design (`docs/LLD.md`)](docs/LLD.md)**: Go domain models, interface contracts, 8-state machine, criticality scoring formula, database DDLs, and REST API specifications.
- **[Cloud Computing Concepts (`docs/CLOUD_COMPUTING_CONCEPTS.md`)](docs/CLOUD_COMPUTING_CONCEPTS.md)**: Academic mapping to core cloud principles (Observability $\rightarrow$ Traces $\rightarrow$ Graph Analysis $\rightarrow$ Intelligent Target Selection $\rightarrow$ Closed-Loop Safety Stop).
- **[Implementation Roadmap (`docs/IMPLEMENTATION_PLAN.md`)](docs/IMPLEMENTATION_PLAN.md)**: Step-by-step 14-phase roadmap from target microservice mesh to Kubernetes deployment.

---

## 🏗️ System Components

1. **`GraphAnalyser`**: Telemetry-driven dependency graph builder & criticality ranker.
2. **`ExperimentManager`**: Central orchestrator managing the 8-state experiment lifecycle.
3. **`StressEngine`**: Context-aware HTTP worker pool load generator.
4. **`WatcherEngine`**: Prometheus real-time metric poller.
5. **`SafetyController`**: Sub-second closed-loop emergency safety stop controller.
6. **`CapacityAnalyzer`**: Deterministic throughput limit and recovery duration analyzer.
7. **`ReportEngine`**: Multi-format report builder (JSON / HTML).
8. **`Kafka Backbone`**: Asynchronous event streaming pipeline (`experiment-events`).
9. **`PostgreSQL`**: Durable experiment state & report repository.

---

## ⚡ Core Operational Pipeline

```
Discover Telemetry -> Build Graph -> Rank Criticality -> Select Target -> Controlled Load -> Observe Metrics -> Safety Stop -> Capacity Analysis -> Report
```

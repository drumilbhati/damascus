# DAMASCUS — Dependency-Aware Microservice Analysis, Capacity, and Stress-testing Utility System

> **Cloud Computing Course Project**

DAMASCUS is a cloud-native distributed control plane that identifies critical microservices from dependency telemetry, safely stress-tests them with step load plans, observes performance via real-time telemetry, enforces closed-loop emergency stops upon SLA breach, and analyzes sustainable capacity bounds and recovery times.

---

## 📚 Project Documentation

The project design and technical specifications are structured into detailed documentation modules:

- **[System Architecture (`docs/ARCHITECTURE.md`)](docs/ARCHITECTURE.md)**: High-level architectural overview, control plane vs. data plane decomposition, component topology, and core workflows.
- **[Low-Level Design (`docs/LLD.md`)](docs/LLD.md)**: Complete Go domain models, interface contracts, state machine specifications, concurrency & context cancellation paths, graph scoring algorithms, database schemas, and REST API specifications.
- **[Cloud Computing Concepts (`docs/CLOUD_COMPUTING_CONCEPTS.md`)](docs/CLOUD_COMPUTING_CONCEPTS.md)**: Academic mapping to core cloud principles including autonomous MAPE-K control loops, resilience, blast radius control, and HPA evaluation.
- **[Implementation Roadmap (`docs/IMPLEMENTATION_PLAN.md`)](docs/IMPLEMENTATION_PLAN.md)**: Phase-by-phase execution plan from target microservice mesh to Kubernetes deployment.

---

## 🏗️ System Components

1. **`ExperimentManager`**: Central orchestrator managing the 8-state experiment lifecycle.
2. **`GraphAnalyser`**: Telemetry-driven dependency graph analyzer & criticality ranker.
3. **`StressEngine`**: Context-aware HTTP worker pool load generator.
4. **`WatcherEngine`**: Prometheus time-series metric poller.
5. **`SafetyController`**: Sub-second closed-loop emergency safety stop controller.
6. **`CapacityAnalyzer`**: Deterministic throughput limit and recovery duration analyzer.
7. **`ReportEngine`**: Multi-format report builder (JSON / HTML).
8. **`Kafka Backbone`**: Asynchronous event streaming pipeline (`experiment-events`).
9. **`PostgreSQL`**: Durable experiment state & report repository.

---

## ⚡ Quick Navigation & Core Flow

```
FIND (GraphAnalyser) -> TEST (StressEngine) -> MONITOR (WatcherEngine) -> PROTECT (SafetyController) -> ANALYZE (CapacityAnalyzer) -> REPORT (ReportEngine)
```

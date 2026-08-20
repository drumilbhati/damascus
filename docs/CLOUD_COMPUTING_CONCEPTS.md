# DAMASCUS — Cloud Computing Core Concepts & Academic Mapping

**Cloud Computing Principles, Architectural Patterns, and Resilience Engineering**

---

## 1. Overview & Cloud Resilience Value Proposition

Traditional load testing tools (such as JMeter, k6, or Locust) act as **black boxes**: they blindly hit a pre-configured URL with synthetic load without understanding the underlying microservice topology, internal single points of failure (SPOFs), or downstream service dependencies.

**DAMASCUS is an internal chaos-testing and resilience-analysis platform for teams operating distributed microservice systems.**

Instead of operating blindly, DAMASCUS connects to standard cloud observability backends (Jaeger for distributed traces and Prometheus for metrics). DAMASCUS then automatically:
1. **Discovers Telemetry**: Parses OpenTelemetry distributed traces (`Frontend -> Checkout -> Payment / Inventory -> Database`).
2. **Reconstructs Service Graph**: Maps caller-callee topologies and call frequencies across the target mesh.
3. **Ranks Criticality**: Computes mathematical criticality scores ($S(v)$) to identify structurally vulnerable services.
4. **Intelligently Recommends Targets**: Recommends testing high-criticality bottlenecks before peripheral leaf nodes.
5. **Executes Controlled Load**: Drives stepped/ramp traffic increments while continuously evaluating an autonomous safety loop.
6. **Enforces Sub-Second Safety**: Instantly terminates load via in-memory context cancellation upon SLA boundary breach.
7. **Profiles Capacity & Recovery**: Quantifies maximum sustainable throughput, degradation points, and post-stress recovery duration.

---

## 2. Telemetry Ingestion vs. Derived Intelligence

```text
               RAW OBSERVABILITY INPUTS
          (From Target Microservice Mesh)
                        │
            ┌───────────┴───────────┐
            ▼                       ▼
      Jaeger Traces        Prometheus Metrics
            │                       │
════════════╪═══════════════════════╪═════════════════════════════════════
            │                       │  DAMASCUS CONTROL PLANE
            ▼                       ▼
     GraphAnalyser             WatcherEngine
            │                       │
            ▼                       ▼
    Dependency Graph         MetricSnapshots
            │                       │
            ▼                       ▼
    Criticality Scores       SafetyController (Fast-Path Stop)
            │                       │
            ▼                       ▼
    Target Selection        Capacity & Recovery Analysis
```

### 2.1 Trace Ingestion $\rightarrow$ `GraphAnalyser`
From Jaeger distributed traces, DAMASCUS extracts span trees:

| Trace Field | Usage in DAMASCUS | Derived Topological Insight |
| :--- | :--- | :--- |
| `TraceID` / `SpanID` | Request tracking | Reconstructs end-to-end execution flow |
| `ParentSpanID` | Caller $\rightarrow$ Callee mapping | Builds directed dependency edges |
| `ServiceName` | Graph vertices | Identifies unique microservices |
| `Duration` | Span latency | Identifies upstream bottleneck latency |
| `Status` (Error / OK) | Fault observation | Pinpoints cascading failure origins |

### 2.2 Metrics Ingestion $\rightarrow$ `WatcherEngine`
From Prometheus time-series PromQL queries, DAMASCUS extracts real-time RED metrics:

| Metric | PromQL Source | Usage in DAMASCUS |
| :--- | :--- | :--- |
| **Request Rate (RPS)** | `sum(rate(http_requests_total[1m])) by (service)` | Measures load applied to target |
| **P50/P95/P99 Latency** | `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[1m])) by (le))` | SLA threshold evaluation & knee detection |
| **Error Rate (%)** | `sum(rate(http_requests_total{status=~"5.."}[1m])) / sum(rate(http_requests_total[1m]))` | Safety tripwire (breach when $> 5\%$) |
| **Availability (%)** | $1.0 - \text{ErrorRate}$ | Availability SLA enforcement |
| **CPU / Memory** | `container_cpu_usage_seconds_total`, `container_memory_working_set_bytes` | Correlates resource saturation with latency |

---

## 3. Key Cloud Computing Architectural Patterns

### 3.1 Autonomous Closed-Loop Control (MAPE-K)
DAMASCUS implements the canonical **MAPE-K** (Monitor, Analyze, Plan, Execute, Knowledge) self-adaptive cloud control loop:
- **Monitor**: `WatcherEngine` polls Prometheus every second for RED metrics.
- **Analyze**: `SafetyController` compares current latency/error measurements against SLA invariants.
- **Plan**: If an SLA boundary is breached, the controller triggers an immediate halt plan.
- **Execute**: `StressEngine` worker goroutines terminate instantly via Go `context.Context` cancellation.
- **Knowledge**: PostgreSQL and Kafka store historical dependency graphs, criticality scores, load degradation curves, and recovery timings.

### 3.2 Fast-Path Safety vs. Asynchronous Event Streaming
A critical cloud resilience design question is: *Why use Kafka if safety halts do not pass through Kafka?*

> [!IMPORTANT]
> **Safety Guarantee via In-Memory Fast Path**:
> Emergency stops are executed via in-memory Go `context.CancelFunc` callbacks. If Kafka experiences broker latency, network partitions, or disk full conditions, the safety stop is **guaranteed to execute in sub-millisecond time**.
>
> **Kafka for Scalable Fan-Out**:
> Kafka (`experiment-events`) handles asynchronous event distribution to external subscribers (UI dashboards, audit logging, SIEM systems, Slack/PagerDuty alerts) without putting secondary systems on the critical safety execution path.

```text
           [ Safety Triggered ]
                     │
         ┌───────────┴───────────┐
         │ (Sub-millisecond)     │ (Asynchronous Non-blocking)
         ▼                       ▼
  context.CancelFunc()      Kafka Producer
         │                       │
         ▼                       ▼
  Workers Cease Traffic    `experiment-events`
  (Target Protected)       (UI / Auditing / Webhooks)
```

---

## 4. Comparative Matrix: Traditional Load Testing vs. DAMASCUS

| Capability | Traditional Tools (k6, JMeter, Locust) | DAMASCUS Platform |
| :--- | :--- | :--- |
| **Topology Awareness** | Black-box / None (hits single target URLs) | **Full Dependency Graph** derived from Jaeger / OpenTelemetry distributed traces |
| **Target Selection** | Manual / Arbitrary guess | **Automated Criticality Ranking** ($S(v)$ formula based on In-Degree, Depth, SPOF) |
| **Safety Mechanisms** | Fixed timeout or manual kill | **Closed-Loop Safety Controller** with sub-second in-memory fast-path abort |
| **Degradation Detection**| Manual post-run chart inspection | **Automated Capacity & Recovery Analysis** (Sustainable RPS, Knee Point, Recovery Time) |
| **Event Backbone** | Standalone local log files | **Asynchronous Kafka Event Streaming** (`experiment-events`) |
| **Target Compatibility**| Any HTTP server | Any OpenTelemetry-instrumented environment (e.g. OpenTelemetry Astronomy Shop Demo) |

---

## 5. Course Defense & Academic Justification Summary

$$\text{Observability} \longrightarrow \text{Distributed Traces} \longrightarrow \text{Graph Topology} \longrightarrow \text{Criticality Scoring} \longrightarrow \text{Controlled Load} \longrightarrow \text{Real-Time Monitoring} \longrightarrow \text{Fast-Path Safety Stop} \longrightarrow \text{Capacity Profiling}$$

By decoupling DAMASCUS from proprietary application code and targeting standard OpenTelemetry/Jaeger/Prometheus APIs, the platform proves that **observability data can drive autonomous, intelligent resilience engineering** across any modern cloud-native system.

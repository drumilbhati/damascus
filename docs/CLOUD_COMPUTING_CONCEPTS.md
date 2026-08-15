# DAMASCUS — Cloud Computing Core Concepts & Academic Mapping

**Cloud Computing Principles, Architectural Patterns, and Course Justification**

---

## 1. Overview & Refined Value Proposition

Traditional load testing tools (like JMeter or k6) act as black boxes: they blindly hit a single pre-configured URL with traffic without any knowledge of the underlying microservice topology, internal single points of failure (SPOFs), or downstream database dependencies.

**DAMASCUS is an internal chaos-testing and resilience-analysis platform for teams operating microservice systems.**

Instead of operating blindly, teams instrument their microservices with OpenTelemetry and give DAMASCUS access to the observability data and target environment. DAMASCUS then automatically:
1. **Discovers Telemetry**: Parses OpenTelemetry distributed traces (`Gateway -> Order -> Payment / Inventory -> Database`).
2. **Builds Service Graph**: Reconstructs the full dependency tree with call frequencies.
3. **Ranks Criticality**: Calculates mathematical criticality scores ($S(v)$) to identify structurally critical microservices.
4. **Intelligently Selects Targets**: Recommends high-value resilience targets instead of random testing.
5. **Executes Controlled Load**: Increases traffic in stepped increments while enforcing a sub-second closed-loop safety stop.
6. **Analyzes Capacity & Recovery**: Reports sustainable capacity limits, degradation points, and post-stress recovery duration.

---

## 2. The Mental Model

```text
                                Application Environment
                    
                        ┌─────────┐      ┌─────────┐
                        │ Gateway │─────>│  Order  │
                        └─────────┘      └────┬────┘
                                              │
                                    ┌─────────┴─────────┐
                                    v                   v
                               ┌─────────┐        ┌─────────┐
                               │ Payment │        │Inventory│
                               └────┬────┘        └─────────┘
                                    │
                                    v
                               ┌─────────┐
                               │   DB    │
                               └─────────┘
                                    │
                       OpenTelemetry Spans & Metrics
                                    │
                                    v
                            DAMASCUS Control Plane
                                    │
                         ┌──────────┴──────────┐
                         v                     v
                 Trace Collector        WatcherEngine
                         │                     │
                         v                     v
                  GraphAnalyser         Prometheus
                         │                     │
                         v                     v
                Criticality Ranking     SafetyController
                         │                     │
                         v                     v
                  Target Selection ────> StressEngine
```

---

## 3. Why This Framing Makes the Project Stronger Academically

### 3.1 Access to Proprietary Internal Telemetry
Because DAMASCUS integrates directly with OpenTelemetry traces and Prometheus time-series metrics, it possesses **internal topological knowledge that a black-box load tester fundamentally cannot obtain**.

From traces, DAMASCUS sees the exact call paths:
```text
Service       Criticality Score   Primary Reason
--------------------------------------------------------------------------
Order             0.92            High in-degree + checkout orchestrator
Payment           0.88            Single point of failure for order completion
Database          0.81            Shared downstream storage bottleneck
Gateway           0.63            Entry pass-through
Inventory         0.41            Leaf read dependency
```

Instead of random load testing:
$$\text{random service} \longrightarrow \text{stress}$$

DAMASCUS makes an intelligent recommendation:
> **"Payment Service is structurally critical (Score: 0.88) and represents a high-value resilience target. Stress test Payment Service first."**

---

## 4. Key Cloud Computing Patterns Implemented

### 4.1 Autonomous Control Loops (MAPE-K Framework)
- **Monitor**: `WatcherEngine` scrapes real-time RED metrics from Prometheus.
- **Analyze**: `SafetyController` evaluates latency (P95/P99), error rates, and availability against non-functional SLA invariants.
- **Plan**: `ExperimentManager` updates state machine and prepares cancellation signals.
- **Execute**: `StressEngine` workers adapt or abort traffic generation in real time.
- **Knowledge**: `PostgreSQL` + `Kafka` preserve historical trace scores, degradation points, and telemetry history.

### 4.2 Control Plane vs. Data Plane Decomposition
- **Control Plane**: DAMASCUS Go control process runs orchestrators, scoring algorithms, and analysis engines without handling production end-user requests.
- **Data Plane**: The target microservices mesh handles user traffic while emitting traces/metrics to sidecars and collectors.

---

## 5. Comparative Matrix: Traditional Load Testing vs. DAMASCUS

| Capability | Traditional Tools (JMeter, Locust, k6) | DAMASCUS Platform |
| :--- | :--- | :--- |
| **Topology Awareness** | Black-box / None (hits pre-configured URLs) | **Full Dependency Graph** derived from OpenTelemetry traces |
| **Target Selection** | Manual / Arbitrary | **Automated Criticality Ranking** (In-degree, PageRank, SPOF) |
| **Safety Mechanisms** | Script timeout or manual cancellation | **Autonomous Closed-Loop Safety Controller** (Sub-second context stop) |
| **Degradation Detection** | Static post-test manual chart inspection | **Automated Capacity & Recovery Threshold Analysis** |
| **Event Backbone** | Standalone CSV / JSON logs | **Asynchronous Kafka Event Streaming** (`experiment-events`) |

---

## 6. End-to-End Story for Course Defense

**Observability $\longrightarrow$ Distributed Tracing $\longrightarrow$ Graph Analysis $\longrightarrow$ Intelligent Target Selection $\longrightarrow$ Distributed Load Generation $\longrightarrow$ Real-Time Monitoring $\longrightarrow$ Automatic Safety Stop $\longrightarrow$ Capacity Analysis.**

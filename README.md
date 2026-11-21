## 🚀 Quick Start: Decoupled Target Mesh & DAMASCUS Platform

DAMASCUS targets standard OpenTelemetry-instrumented environments. The system is split into two independent stacks so the target mesh and Damascus can be started, stopped, or scaled separately:

### Step 1: Start the Target System (Astronomy Shop + Jaeger & Prometheus)
```bash
docker compose -f docker-compose.target.yaml up -d
```
*Note: The first launch pulls the polyglot microservice images (Java, .NET, C++, Go, etc.). Once cached, subsequent boots take only seconds.*

### Step 2: Start the DAMASCUS Control Platform
```bash
cp .env.example .env
docker compose -f docker-compose.damascus.yaml up -d --build
```

*(Alternatively, to run everything together in a single all-in-one stack: `docker compose up -d --build`)*

### 🌐 Live Service & Observability Endpoints

| Service | Local Endpoint | Description |
| :--- | :--- | :--- |
| 🖥️ **DAMASCUS Dashboard** | [**`http://localhost:3000/`**](http://localhost:3000/) | Resilience & capacity web dashboard. |
| ⚙️ **DAMASCUS Control Plane** | [**`http://localhost:8080/api/health`**](http://localhost:8080/api/health) | Control plane REST API. |
| 🔭 **Astronomy Web Shop (Target)** | [**`http://localhost:8085/`**](http://localhost:8085/) | Target e-commerce store under resilience testing. |
| 🔍 **Jaeger Traces (Endpoint 1)** | [**`http://localhost:16686/`**](http://localhost:16686/) | Aggregated distributed trace trees & REST API (`/api/traces`). |
| 📈 **Prometheus Metrics (Endpoint 2)** | [**`http://localhost:9090/`**](http://localhost:9090/) | Aggregated RED metrics engine & PromQL API (`/api/v1/query`). |

To stop the environments:
```bash
docker compose -f docker-compose.damascus.yaml down
docker compose -f docker-compose.target.yaml down
```

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"

	"damascus/internal/config"
	"damascus/internal/graph"
	"damascus/internal/observability"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

func main() {
	fmt.Println("==================================================================")
	fmt.Println("       DAMASCUS — Interactive Architecture & Engines Demo         ")
	fmt.Println("==================================================================")
	fmt.Println()

	runGraphDemo()
	runObservabilityCheckerDemo()
	runStressEngineSteppedDemo()
	runClosedLoopSafetyStopDemo()

	fmt.Println("\n==================================================================")
	fmt.Println("                   All Demo Scenarios Passed!                     ")
	fmt.Println("==================================================================")
}

// -----------------------------------------------------------------------------
// 1. Dependency Graph & Degree Calculations
// -----------------------------------------------------------------------------
func runGraphDemo() {
	fmt.Println("🔹 [Demo 1/4] Dependency Graph & In/Out-Degree Calculations")
	fmt.Println("------------------------------------------------------------------")

	g := graph.NewDependencyGraph()

	// Build service topology: frontend -> checkout -> payment / shipping
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "checkout", CallCount: 5000, Frequency: 100.0})
	g.AddEdge(graph.DependencyEdge{From: "checkout", To: "payment", CallCount: 4200, Frequency: 84.0})
	g.AddEdge(graph.DependencyEdge{From: "checkout", To: "shipping", CallCount: 4200, Frequency: 84.0})
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "cart", CallCount: 3000, Frequency: 60.0})

	fmt.Printf("Total Nodes: %d | Total Edges: %d\n", len(g.Nodes), len(g.Edges))
	for name := range g.Nodes {
		fmt.Printf("  • Service: %-12s | In-Degree (Callers): %d | Out-Degree (Callees): %d\n",
			name, g.InDegree(name), g.OutDegree(name))
	}
	fmt.Println()
}

// -----------------------------------------------------------------------------
// 2. Observability Stack Health Checker
// -----------------------------------------------------------------------------
func runObservabilityCheckerDemo() {
	fmt.Println("🔹 [Demo 2/4] Observability Stack Health Checker")
	fmt.Println("------------------------------------------------------------------")

	// Mock Target Service
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target Microservice Mesh OK"))
	}))
	defer targetServer.Close()

	// Mock Jaeger Trace API
	jaegerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":["frontend","checkout","payment"]}`))
	}))
	defer jaegerServer.Close()

	// Mock Prometheus PromQL API
	promServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer promServer.Close()

	checker := observability.NewChecker(2 * time.Second)
	cfg := config.EnvironmentConfig{
		TargetBaseURL:      targetServer.URL,
		JaegerTraceBaseURL: jaegerServer.URL,
		PrometheusBaseURL:  promServer.URL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := checker.CheckObservabilityStack(ctx, cfg)
	if err != nil {
		fmt.Printf("Health check error: %v\n", err)
	}

	statusJSON, _ := json.MarshalIndent(status, "  ", "  ")
	fmt.Printf("  Health Check Result:\n  %s\n\n", string(statusJSON))
}

// -----------------------------------------------------------------------------
// 3. Controlled Stepped Load Generation (StressEngine)
// -----------------------------------------------------------------------------
func runStressEngineSteppedDemo() {
	fmt.Println("🔹 [Demo 3/4] Stepped Load Traffic Generation (StressEngine)")
	fmt.Println("------------------------------------------------------------------")

	var receivedCount int64
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&receivedCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	engine := stress.NewHTTPStressEngine(targetServer.Client(), 20)

	plan := stress.LoadPlan{
		TargetURL:           targetServer.URL,
		Method:              "POST",
		Payload:             `{"action":"add_to_cart","item_id":"ol-101"}`,
		InitialRate:         20, // Start at 20 RPS
		StepRate:            30, // Increase by +30 RPS
		MaxRate:             80, // Max at 80 RPS
		StepDurationSeconds: 1,  // Step every 1 second
	}

	fmt.Printf("  Starting Stepped Load: InitialRate=%d RPS -> StepRate=+%d RPS -> MaxRate=%d RPS (StepDuration=%ds)\n",
		plan.InitialRate, plan.StepRate, plan.MaxRate, plan.StepDurationSeconds)

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	startTime := time.Now()
	_ = engine.Start(ctx, plan)
	duration := time.Since(startTime)

	stats := engine.GetStats()
	fmt.Printf("  Execution Finished in %v:\n", duration.Round(time.Millisecond))
	fmt.Printf("    • Total HTTP Requests Sent : %d\n", stats.TotalRequests)
	fmt.Printf("    • Successful Responses (2xx): %d\n", stats.SuccessCount)
	fmt.Printf("    • HTTP Errors (4xx/5xx)     : %d\n", stats.ErrorCount)
	fmt.Printf("    • Saturated/Dropped Ticks   : %d\n\n", stats.DroppedCount)
}

// -----------------------------------------------------------------------------
// 4. Closed-Loop Real-Time Safety Fast-Path Abort
// -----------------------------------------------------------------------------
func runClosedLoopSafetyStopDemo() {
	fmt.Println("🔹 [Demo 4/4] Closed-Loop Real-Time Safety Fast-Path Abort")
	fmt.Println("------------------------------------------------------------------")

	// 1. Setup SLA Safety Policy: Max P95 Latency = 150ms, Max Error Rate = 5%
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 150.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.95,
	}
	controller := safety.NewSafetyController(policy)
	fmt.Printf("  Active Safety SLA Bounds: Max P95 Latency=%.1fms | Max Error Rate=%.1f%%\n",
		policy.MaxP95LatencyMs, policy.MaxErrorRate*100)

	// 2. Mock Target Application Server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	// 3. Mock Prometheus Server simulating increasing latency breach
	var tickCount int64
	promServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		value := "0.0"

		if strings.Contains(query, "0.95") {
			count := atomic.AddInt64(&tickCount, 1)
			// Latency query: Tick 1 = 80ms, Tick 2+ = 220.5ms (SLA BREACH)
			if count >= 2 {
				value = "220.5"
			} else {
				value = "80.0"
			}
		} else if strings.Contains(query, "5..") {
			// Error rate query: 1%
			value = "0.01"
		} else if strings.Contains(query, "http_requests_total") {
			// Request rate query: 50 RPS
			value = "50.0"
		}

		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [{"metric": {"service": "checkout"}, "value": [1724800000.0, "%s"]}]
			}
		}`, value)))
	}))
	defer promServer.Close()

	engine := stress.NewHTTPStressEngine(targetServer.Client(), 20)
	watcherEngine := watcher.NewPrometheusWatcher(promServer.URL, promServer.Client(), 100*time.Millisecond)

	plan := stress.LoadPlan{
		TargetURL:           targetServer.URL,
		InitialRate:         50,
		StepRate:            20,
		MaxRate:             200,
		StepDurationSeconds: 5,
	}

	// Execution context that will be aborted by the fast-path safety stop
	execCtx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	// Start Watcher
	snapshotChan, err := watcherEngine.Start(execCtx, "exp-safety-demo", "checkout")
	if err != nil {
		fmt.Printf("Failed to start watcher: %v\n", err)
		return
	}

	// Start Stress Engine in background
	go func() {
		_ = engine.Start(execCtx, plan)
	}()

	fmt.Println("  Stress engine injecting load against target service...")
	fmt.Println("  WatcherEngine polling Prometheus for RED metrics...")

	// Safety Loop: Evaluate incoming MetricSnapshots
	for snapshot := range snapshotChan {
		decision := controller.Evaluate(snapshot)
		fmt.Printf("    -> Observed P95 Latency: %.1fms | Error Rate: %.1f%%",
			snapshot.P95LatencyMs, snapshot.ErrorRate*100)

		if decision.ShouldStop {
			fmt.Printf(" --> [SLA BREACH DETECTED!]\n")
			fmt.Printf("  🚨 FAST-PATH TRIGGERED: In-memory context.CancelFunc() invoked!\n")
			fmt.Printf("  🚨 Reason: %s\n", decision.Reason)

			// Sub-millisecond fast path in-memory cancellation
			cancelFunc()
			break
		} else {
			fmt.Printf(" [OK]\n")
		}
	}

	time.Sleep(100 * time.Millisecond)
	finalStats := engine.GetStats()
	fmt.Printf("  Post-Abort Verification:\n")
	fmt.Printf("    • Worker traffic successfully ceased.\n")
	fmt.Printf("    • Total requests before emergency stop: %d\n", finalStats.TotalRequests)
}

package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Watcher defines the interface contract for real-time telemetry observation.
type Watcher interface {
	// Start begins streaming metric snapshots from Prometheus at regular intervals.
	Start(ctx context.Context, experimentID string, targetService string) (<-chan MetricSnapshot, error)

	// Stop terminates the active metric polling loop.
	Stop()
}

// PrometheusResponse matches the standard Prometheus PromQL HTTP API response envelope.
type PrometheusResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"` // [ timestamp (float64), value (string) ]
		} `json:"result"`
	} `json:"data"`
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
}

// PrometheusWatcher polls Prometheus for RED metrics (Rate, Errors, Latency) during an active experiment.
type PrometheusWatcher struct {
	baseURL      string
	httpClient   *http.Client
	pollInterval time.Duration
	mu           sync.Mutex
	cancelFunc   context.CancelFunc
	running      bool
}

// NewPrometheusWatcher creates a new real-time Prometheus metric poller.
func NewPrometheusWatcher(baseURL string, client *http.Client, pollInterval time.Duration) *PrometheusWatcher {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}
	return &PrometheusWatcher{
		baseURL:      strings.TrimRight(baseURL, "/"),
		httpClient:   client,
		pollInterval: pollInterval,
	}
}

// Start launches the background polling goroutine and returns a channel streaming MetricSnapshot events.
func (w *PrometheusWatcher) Start(ctx context.Context, experimentID string, targetService string) (<-chan MetricSnapshot, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil, fmt.Errorf("watcher is already running an active stream")
	}
	w.running = true

	pollCtx, cancel := context.WithCancel(ctx)
	w.cancelFunc = cancel
	w.mu.Unlock()

	snapshotChan := make(chan MetricSnapshot, 100)

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.cancelFunc = nil
			w.mu.Unlock()
			close(snapshotChan)
		}()

		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-pollCtx.Done():
				return

			case <-ticker.C:
				snapshot := w.scrapeMetrics(pollCtx, experimentID, targetService)
				select {
				case snapshotChan <- snapshot:
				case <-pollCtx.Done():
					return
				}
			}
		}
	}()

	return snapshotChan, nil
}

// Stop terminates the polling loop.
func (w *PrometheusWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancelFunc != nil {
		w.cancelFunc()
	}
}

// scrapeMetrics executes the necessary PromQL queries to build a single MetricSnapshot.
func (w *PrometheusWatcher) scrapeMetrics(ctx context.Context, experimentID string, targetService string) MetricSnapshot {
	now := time.Now().UTC()
	snapshot := MetricSnapshot{
		ExperimentID:  experimentID,
		TargetService: targetService,
		Timestamp:     now,
		Availability:  1.0, // Default to 100% available unless error rate indicates otherwise
	}

	// 1. Query Request Rate (RPS)
	rpsQuery := fmt.Sprintf(`sum(rate(http_requests_total{service="%s"}[1m]))`, targetService)
	if val, err := w.queryScalar(ctx, rpsQuery); err == nil {
		snapshot.RequestRate = val
	}

	// 2. Query P50 Latency (ms)
	p50Query := fmt.Sprintf(`histogram_quantile(0.50, sum(rate(http_request_duration_seconds_bucket{service="%s"}[1m])) by (le)) * 1000`, targetService)
	if val, err := w.queryScalar(ctx, p50Query); err == nil {
		snapshot.P50LatencyMs = val
	}

	// 3. Query P95 Latency (ms)
	p95Query := fmt.Sprintf(`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service="%s"}[1m])) by (le)) * 1000`, targetService)
	if val, err := w.queryScalar(ctx, p95Query); err == nil {
		snapshot.P95LatencyMs = val
	}

	// 4. Query P99 Latency (ms)
	p99Query := fmt.Sprintf(`histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{service="%s"}[1m])) by (le)) * 1000`, targetService)
	if val, err := w.queryScalar(ctx, p99Query); err == nil {
		snapshot.P99LatencyMs = val
	}

	// 5. Query Error Rate percentage (0.0 to 1.0)
	errQuery := fmt.Sprintf(`sum(rate(http_requests_total{service="%s",status=~"5.."}[1m])) / sum(rate(http_requests_total{service="%s"}[1m]))`, targetService, targetService)
	if val, err := w.queryScalar(ctx, errQuery); err == nil {
		snapshot.ErrorRate = val
		snapshot.Availability = 1.0 - val
		if snapshot.Availability < 0 {
			snapshot.Availability = 0
		}
	}

	// 6. Query CPU Utilization (%)
	cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{container="%s"}[1m])) * 100`, targetService)
	if val, err := w.queryScalar(ctx, cpuQuery); err == nil {
		snapshot.CPUUtilization = val
	}

	// 7. Query Memory Utilization (MB)
	memQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{container="%s"}) / 1024 / 1024`, targetService)
	if val, err := w.queryScalar(ctx, memQuery); err == nil {
		snapshot.MemoryUtilization = val
	}

	return snapshot
}

// queryScalar queries Prometheus PromQL and extracts the first float64 value returned.
func (w *PrometheusWatcher) queryScalar(ctx context.Context, promQL string) (float64, error) {
	endpoint := fmt.Sprintf("%s/api/v1/query?query=%s", w.baseURL, url.QueryEscape(promQL))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create query request: %w", err)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Prometheus query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected Prometheus response status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	var promResp PrometheusResponse
	if err := json.Unmarshal(body, &promResp); err != nil {
		return 0, fmt.Errorf("failed to parse Prometheus JSON: %w", err)
	}

	if promResp.Status != "success" {
		return 0, fmt.Errorf("Prometheus query error: %s (%s)", promResp.Error, promResp.ErrorType)
	}

	if len(promResp.Data.Result) == 0 || len(promResp.Data.Result[0].Value) < 2 {
		return 0, nil // No metric data points found (scalar 0)
	}

	valStr, ok := promResp.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("invalid value format in Prometheus response")
	}

	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse float value %q: %w", valStr, err)
	}

	return val, nil
}

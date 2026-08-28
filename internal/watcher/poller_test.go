package watcher_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"damascus/internal/watcher"
)

func TestPrometheusWatcher_PollingStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "vector",
				"result": [
					{
						"metric": {"service": "checkout"},
						"value": [1724800000.0, "150.5"]
					}
				]
			}
		}`))
	}))
	defer server.Close()

	watcherEngine := watcher.NewPrometheusWatcher(server.URL, server.Client(), 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	snapshotChan, err := watcherEngine.Start(ctx, "exp-123", "checkout")
	if err != nil {
		t.Fatalf("unexpected error starting watcher: %v", err)
	}

	var snapshotsReceived int
	for snap := range snapshotChan {
		snapshotsReceived++
		if snap.ExperimentID != "exp-123" {
			t.Errorf("expected ExperimentID exp-123, got: %s", snap.ExperimentID)
		}
		if snap.TargetService != "checkout" {
			t.Errorf("expected TargetService checkout, got: %s", snap.TargetService)
		}
		if snap.RequestRate != 150.5 {
			t.Errorf("expected RequestRate 150.5, got: %f", snap.RequestRate)
		}
	}

	if snapshotsReceived == 0 {
		t.Error("expected at least 1 snapshot from channel, got 0")
	}
}

func TestPrometheusWatcher_StopMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer server.Close()

	watcherEngine := watcher.NewPrometheusWatcher(server.URL, server.Client(), 50*time.Millisecond)

	snapshotChan, err := watcherEngine.Start(context.Background(), "exp-stop", "frontend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	watcherEngine.Stop()

	// Channel should close promptly after Stop()
	done := make(chan bool)
	go func() {
		for range snapshotChan {
		}
		done <- true
	}()

	select {
	case <-done:
		// Succeeded
	case <-time.After(1 * time.Second):
		t.Fatal("watcher channel did not close after Stop() called")
	}
}

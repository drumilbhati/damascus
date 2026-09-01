package stress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer spins up an in-process HTTP server and returns it together
// with a cleanup function. handler controls the response behaviour.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// ─── NewClient ────────────────────────────────────────────────────────────────

func TestNewClient_DefaultsApplied(t *testing.T) {
	c := NewClient(ClientConfig{})
	if c == nil {
		t.Fatal("NewClient returned nil for zero-value ClientConfig")
	}
}

func TestNewClient_CustomValues(t *testing.T) {
	cfg := ClientConfig{
		MaxIdleConns:        500,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     45 * time.Second,
		RequestTimeout:      10 * time.Second,
	}
	c := NewClient(cfg)
	if c == nil {
		t.Fatal("NewClient returned nil for custom ClientConfig")
	}
}

// ─── sendRequest – happy-path ─────────────────────────────────────────────────

func TestSendRequest_GET_200(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	if err := c.sendRequest(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error for 200 GET: %v", err)
	}
}

func TestSendRequest_POST_200(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{})
	plan := LoadPlan{
		TargetURL: srv.URL,
		Method:    "POST",
		Payload:   `{"item_id":"42","quantity":1}`,
	}

	if err := c.sendRequest(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error for 200 POST: %v", err)
	}
}

// ─── sendRequest – method normalisation ──────────────────────────────────────

// TestSendRequest_UnsupportedMethodReturnsError verifies that a method other
// than GET or POST is rejected immediately, without making any network call.
func TestSendRequest_UnsupportedMethodReturnsError(t *testing.T) {
	c := NewClient(ClientConfig{})
	plan := LoadPlan{TargetURL: "http://127.0.0.1:19999", Method: "DELETE"}

	err := c.sendRequest(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error for unsupported method, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported method") {
		t.Errorf("expected 'unsupported method' in error message, got: %v", err)
	}
}

// ─── sendRequest – error paths ────────────────────────────────────────────────

func TestSendRequest_4xxReturnsError(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	c := NewClient(ClientConfig{})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	if err := c.sendRequest(context.Background(), plan); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

func TestSendRequest_5xxReturnsError(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	c := NewClient(ClientConfig{})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	if err := c.sendRequest(context.Background(), plan); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestSendRequest_CancelledContext(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before the request is even made

	c := NewClient(ClientConfig{RequestTimeout: 5 * time.Second})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	if err := c.sendRequest(ctx, plan); err == nil {
		t.Fatal("expected error for pre-cancelled context, got nil")
	}
}

func TestSendRequest_RequestTimeout(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond) // longer than the client timeout below
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{RequestTimeout: 50 * time.Millisecond})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	if err := c.sendRequest(context.Background(), plan); err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestSendRequest_UnreachableHost(t *testing.T) {
	c := NewClient(ClientConfig{RequestTimeout: 500 * time.Millisecond})
	plan := LoadPlan{
		TargetURL: "http://127.0.0.1:19999", // nothing listening here
		Method:    "GET",
	}

	if err := c.sendRequest(context.Background(), plan); err == nil {
		t.Fatal("expected connection-refused error, got nil")
	}
}

// TestSendRequest_LargeBodyDrained verifies that a large response body is
// fully consumed (so the connection returns to the pool) without the caller
// having to handle it.
func TestSendRequest_LargeBodyDrained(t *testing.T) {
	largePayload := make([]byte, 1<<20) // 1 MiB of zeros
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(largePayload)
	})

	c := NewClient(ClientConfig{})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	if err := c.sendRequest(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error draining 1 MiB body: %v", err)
	}
}

// TestSendRequest_BodyReadTimeout verifies that a context deadline expiring
// during the response-body drain is surfaced as an error even when the HTTP
// status line was 200 OK. The server flushes headers immediately but then
// stalls before sending the body, triggering io.Copy to hit the deadline.
func TestSendRequest_BodyReadTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Flush status + headers immediately so http.Do returns.
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Stall the body long enough for the client timeout to fire.
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("late body"))
	}))
	t.Cleanup(srv.Close)

	// RequestTimeout is short enough to expire while the body is being drained.
	c := NewClient(ClientConfig{RequestTimeout: 80 * time.Millisecond})
	plan := LoadPlan{TargetURL: srv.URL, Method: "GET"}

	err := c.sendRequest(context.Background(), plan)
	if err == nil {
		t.Fatal("expected body-read timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "reading response body") {
		t.Errorf("expected 'reading response body' in error, got: %v", err)
	}
}

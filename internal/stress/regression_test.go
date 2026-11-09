package stress_test

import (
	"context"
	"damascus/internal/stress"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func goodResponse() *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}
}

func TestRegressionRampRatesAndCompletion(t *testing.T) {
	var mu sync.Mutex
	counts := map[int]int{}
	var engine *stress.Engine
	engine = stress.NewEngine(&http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing payload content type")
		}
		mu.Lock()
		counts[engine.CurrentRate()]++
		mu.Unlock()
		return goodResponse(), nil
	})})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := time.Now()
	err := engine.Start(ctx, stress.LoadPlan{TargetURL: "http://target.invalid", Method: "POST", Payload: `{}`, InitialRate: 10, StepRate: 10, MaxRate: 20, StepDurationSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second || elapsed > 3*time.Second {
		t.Fatalf("unexpected duration %v", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts[10] < 8 || counts[10] > 12 || counts[20] < 18 || counts[20] > 22 {
		t.Fatalf("incorrect step traffic: %v", counts)
	}
	if engine.CurrentRate() != 0 {
		t.Fatal("rate must be zero after completion")
	}
}

func TestRegressionRequestFailuresStopEngine(t *testing.T) {
	for _, kind := range []string{"connect", "http500", "http404", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			engine := stress.NewEngine(&http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
				switch kind {
				case "connect":
					return nil, errors.New("connection refused")
				case "timeout":
					return nil, context.DeadlineExceeded
				default:
					resp := goodResponse()
					resp.StatusCode = 500
					if kind == "http404" {
						resp.StatusCode = 404
					}
					return resp, nil
				}
			})})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := engine.Start(ctx, stress.LoadPlan{TargetURL: "http://target.invalid", Method: "GET", InitialRate: 100, MaxRate: 100, StepDurationSeconds: 5})
			if err == nil || ctx.Err() != nil {
				t.Fatalf("failure did not surface promptly: %v", err)
			}
		})
	}
}

func TestRegressionStopJoinsRequestsAndAllowsRestart(t *testing.T) {
	entered := make(chan struct{}, 1)
	engine := stress.NewEngine(&http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	plan := stress.LoadPlan{TargetURL: "http://target.invalid", Method: "GET", InitialRate: 20, MaxRate: 20, StepDurationSeconds: 1}
	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		go func() { done <- engine.Start(context.Background(), plan) }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("no request started")
		}
		engine.Stop()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("unexpected stop error %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("stop did not join engine")
		}
	}
}

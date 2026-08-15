package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthCheckEndpoint(t *testing.T) {
	handler, err := NewGatewayHandler("http://localhost:9999")
	if err != nil {
		t.Fatalf("failed to create gateway handler: %v", err)
	}

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}

	if body["status"] != "OK" || body["service"] != "api-gateway" {
		t.Errorf("unexpected health check response body: %v", body)
	}
}

func TestOrderProxying(t *testing.T) {
	// Mock downstream Order Service
	mockOrderService := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"order_id":"ord_1001","status":"created"}`))
	}))
	defer mockOrderService.Close()

	handler, err := NewGatewayHandler(mockOrderService.URL)
	if err != nil {
		t.Fatalf("failed to create gateway handler: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/orders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	expected := `{"order_id":"ord_1001","status":"created"}`
	if rec.Body.String() != expected {
		t.Errorf("expected response %s, got %s", expected, rec.Body.String())
	}
}

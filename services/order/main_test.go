package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrderServerURLValidation(t *testing.T) {
	invalidCfg := Config{
		PaymentServiceURL:        "invalid-url",
		InventoryServiceURL:      "http://localhost:8084",
		RecommendationServiceURL: "http://localhost:8085",
	}

	_, err := NewOrderServer(invalidCfg)
	if err == nil {
		t.Errorf("expected error for invalid PaymentServiceURL, got nil")
	}
}

func TestHealthCheckEndpoint(t *testing.T) {
	cfg := Config{
		PaymentServiceURL:        "http://localhost:8083",
		InventoryServiceURL:      "http://localhost:8084",
		RecommendationServiceURL: "http://localhost:8085",
	}

	serverImpl, err := NewOrderServer(cfg)
	if err != nil {
		t.Fatalf("failed to create OrderServer: %v", err)
	}

	handler := serverImpl.Routes()

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse body: %v", err)
	}

	if body["status"] != "OK" || body["service"] != "order-service" {
		t.Errorf("unexpected health check body: %v", body)
	}
}

func TestMethodNotAllowedForOrders(t *testing.T) {
	cfg := Config{
		PaymentServiceURL:        "http://localhost:8083",
		InventoryServiceURL:      "http://localhost:8084",
		RecommendationServiceURL: "http://localhost:8085",
	}

	serverImpl, err := NewOrderServer(cfg)
	if err != nil {
		t.Fatalf("failed to create OrderServer: %v", err)
	}

	handler := serverImpl.Routes()

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/orders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestCreateOrderOrchestration(t *testing.T) {
	mockPayment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pay" {
			t.Errorf("payment mock received invalid request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("traceparent") == "" {
			t.Errorf("expected traceparent header in payment request, got empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"payment_id":"pay_999","status":"SUCCESS"}`))
	}))
	defer mockPayment.Close()

	mockInventory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/stock" {
			t.Errorf("inventory mock received invalid request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("traceparent") == "" {
			t.Errorf("expected traceparent header in inventory request, got empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"item_id":"item_42","in_stock":true}`))
	}))
	defer mockInventory.Close()

	mockRecommendation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/recommend" {
			t.Errorf("recommendation mock received invalid request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("traceparent") == "" {
			t.Errorf("expected traceparent header in recommendation request, got empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":["item_43","item_44"]}`))
	}))
	defer mockRecommendation.Close()

	cfg := Config{
		PaymentServiceURL:        mockPayment.URL,
		InventoryServiceURL:      mockInventory.URL,
		RecommendationServiceURL: mockRecommendation.URL,
	}

	serverImpl, err := NewOrderServer(cfg)
	if err != nil {
		t.Fatalf("failed to create OrderServer: %v", err)
	}

	handler := serverImpl.Routes()

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/orders", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d", rec.Code)
	}

	var resp OrderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse order response: %v", err)
	}

	if resp.Status != "CONFIRMED" {
		t.Errorf("expected status CONFIRMED, got %s", resp.Status)
	}
	if resp.Payment["payment_id"] != "pay_999" {
		t.Errorf("expected payment_id pay_999, got %v", resp.Payment["payment_id"])
	}
}

func TestDegradedOrderResponse(t *testing.T) {
	mockPayment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockPayment.Close()

	mockInventory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"in_stock":true}`))
	}))
	defer mockInventory.Close()

	mockRecommendation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer mockRecommendation.Close()

	cfg := Config{
		PaymentServiceURL:        mockPayment.URL,
		InventoryServiceURL:      mockInventory.URL,
		RecommendationServiceURL: mockRecommendation.URL,
	}

	serverImpl, err := NewOrderServer(cfg)
	if err != nil {
		t.Fatalf("failed to create OrderServer: %v", err)
	}

	handler := serverImpl.Routes()

	req := httptest.NewRequestWithContext(context.Background(), "POST", "/orders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d", rec.Code)
	}

	var resp OrderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse order response: %v", err)
	}

	if resp.Status != "DEGRADED" {
		t.Errorf("expected status DEGRADED when payment fails, got %s", resp.Status)
	}
	if resp.Payment["error"] != "SERVICE_UNAVAILABLE" {
		t.Errorf("expected error SERVICE_UNAVAILABLE, got %v", resp.Payment["error"])
	}
}

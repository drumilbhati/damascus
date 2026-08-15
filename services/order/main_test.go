package main

import (
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

	req := httptest.NewRequest("GET", "/healthz", nil)
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

func TestCreateOrderOrchestration(t *testing.T) {
	// Mock Payment Service
	mockPayment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"payment_id":"pay_999","status":"SUCCESS"}`))
	}))
	defer mockPayment.Close()

	// Mock Inventory Service
	mockInventory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"item_id":"item_42","in_stock":true}`))
	}))
	defer mockInventory.Close()

	// Mock Recommendation Service
	mockRecommendation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	req := httptest.NewRequest("POST", "/orders", nil)
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Prometheus RED Metrics with bounded route label
var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "order_http_requests_total",
			Help: "Total HTTP requests processed by Order Service.",
		},
		[]string{"method", "handler", "code"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "order_http_request_duration_seconds",
			Help:    "HTTP request latency histogram in seconds for Order Service.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "handler"},
	)
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

type Config struct {
	Port                     string
	PaymentServiceURL        string
	InventoryServiceURL      string
	RecommendationServiceURL string
}

func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}
	paymentURL := os.Getenv("PAYMENT_SERVICE_URL")
	if paymentURL == "" {
		paymentURL = "http://localhost:8083"
	}
	inventoryURL := os.Getenv("INVENTORY_SERVICE_URL")
	if inventoryURL == "" {
		inventoryURL = "http://localhost:8084"
	}
	recommendationURL := os.Getenv("RECOMMENDATION_SERVICE_URL")
	if recommendationURL == "" {
		recommendationURL = "http://localhost:8085"
	}
	return Config{
		Port:                     port,
		PaymentServiceURL:        paymentURL,
		InventoryServiceURL:      inventoryURL,
		RecommendationServiceURL: recommendationURL,
	}
}

type OrderResponse struct {
	OrderID        string                 `json:"order_id"`
	Status         string                 `json:"status"`
	Payment        map[string]interface{} `json:"payment,omitempty"`
	Inventory      map[string]interface{} `json:"inventory,omitempty"`
	Recommendation map[string]interface{} `json:"recommendations,omitempty"`
}

type OrderServer struct {
	httpClient *http.Client
	cfg        Config
}

func NewOrderServer(cfg Config) (*OrderServer, error) {
	for _, rawURL := range []string{cfg.PaymentServiceURL, cfg.InventoryServiceURL, cfg.RecommendationServiceURL} {
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("invalid downstream service URL: %s", rawURL)
		}
	}

	// Register global W3C Trace Context Propagator
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tr := otelhttp.NewTransport(http.DefaultTransport)
	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	return &OrderServer{
		httpClient: client,
		cfg:        cfg,
	}, nil
}

func (s *OrderServer) callDownstream(ctx context.Context, method, targetURL string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("downstream service returned non-2xx status code: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		result = map[string]interface{}{"raw": string(bodyBytes)}
	}
	return result, nil
}

func (s *OrderServer) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	// Require POST method
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	orderID := fmt.Sprintf("ord_%d", time.Now().UnixNano())
	orderStatus := "CONFIRMED"

	// 1. Call Payment Service (POST /pay)
	paymentRes, err := s.callDownstream(ctx, http.MethodPost, s.cfg.PaymentServiceURL+"/pay")
	if err != nil {
		log.Printf("[WARN] Payment service call failed: %v", err)
		paymentRes = map[string]interface{}{"status": "degraded", "error": "SERVICE_UNAVAILABLE"}
		orderStatus = "DEGRADED"
	}

	// 2. Call Inventory Service (POST /stock)
	inventoryRes, err := s.callDownstream(ctx, http.MethodPost, s.cfg.InventoryServiceURL+"/stock")
	if err != nil {
		log.Printf("[WARN] Inventory service call failed: %v", err)
		inventoryRes = map[string]interface{}{"status": "degraded", "error": "SERVICE_UNAVAILABLE"}
		orderStatus = "DEGRADED"
	}

	// 3. Call Recommendation Service (GET /recommend)
	recRes, err := s.callDownstream(ctx, http.MethodGet, s.cfg.RecommendationServiceURL+"/recommend")
	if err != nil {
		log.Printf("[WARN] Recommendation service call failed: %v", err)
		recRes = map[string]interface{}{"status": "degraded", "error": "SERVICE_UNAVAILABLE"}
		orderStatus = "DEGRADED"
	}

	response := OrderResponse{
		OrderID:        orderID,
		Status:         orderStatus,
		Payment:        paymentRes,
		Inventory:      inventoryRes,
		Recommendation: recRes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(response)
}

func (s *OrderServer) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "OK",
			"service": "order-service",
		})
	})

	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/orders", s.handleCreateOrder)

	// Bounded route label metrics middleware
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		mux.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		code := strconv.Itoa(rw.statusCode)

		// Bound label cardinality: map recognized routes
		route := "not_found"
		switch r.URL.Path {
		case "/healthz":
			route = "/healthz"
		case "/metrics":
			route = "/metrics"
		case "/orders":
			route = "/orders"
		}

		httpRequestsTotal.WithLabelValues(r.Method, route, code).Inc()
		httpRequestDuration.WithLabelValues(r.Method, route).Observe(duration)
	})

	return otelhttp.NewHandler(metricsHandler, "order-service")
}

func main() {
	cfg := loadConfig()

	serverImpl, err := NewOrderServer(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize OrderServer: %v", err)
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      serverImpl.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[INFO] Order Service starting on port :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Order Service HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("[INFO] Order Service shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Order Service forced shutdown error: %v", err)
	}

	log.Println("[INFO] Order Service exited cleanly")
}

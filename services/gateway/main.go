package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
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
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_http_requests_total",
			Help: "Total number of HTTP requests processed by the API Gateway.",
		},
		[]string{"method", "handler", "code"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_http_request_duration_seconds",
			Help:    "HTTP request latency histogram in seconds for the API Gateway.",
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
	Port            string
	OrderServiceURL string
}

func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	orderURL := os.Getenv("ORDER_SERVICE_URL")
	if orderURL == "" {
		orderURL = "http://localhost:8082"
	}
	return Config{
		Port:            port,
		OrderServiceURL: orderURL,
	}
}

func NewGatewayHandler(orderServiceURL string) (http.Handler, error) {
	targetURL, err := url.Parse(orderServiceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid order service URL: %w", err)
	}

	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid order service URL scheme: %s (must be http or https)", targetURL.Scheme)
	}
	if targetURL.Host == "" {
		return nil, fmt.Errorf("invalid order service URL: host cannot be empty")
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Configure cloned Transport with ResponseHeaderTimeout and explicit timeouts
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	proxy.Transport = transport

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "OK",
			"service": "api-gateway",
		})
	})

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Reverse proxy forwarding /api/orders to Order Service
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		r.Host = targetURL.Host
		proxy.ServeHTTP(w, r)
	})

	// Wrap mux with metrics logging middleware
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		mux.ServeHTTP(rw, r)

		duration := time.Since(start).Seconds()
		handlerPath := r.URL.Path
		method := r.Method
		code := strconv.Itoa(rw.statusCode)

		httpRequestsTotal.WithLabelValues(method, handlerPath, code).Inc()
		httpRequestDuration.WithLabelValues(method, handlerPath).Observe(duration)
	})

	// Wrap with OpenTelemetry HTTP middleware
	otelHandler := otelhttp.NewHandler(metricsHandler, "api-gateway")

	return otelHandler, nil
}

func main() {
	cfg := loadConfig()

	handler, err := NewGatewayHandler(cfg.OrderServiceURL)
	if err != nil {
		log.Fatalf("Failed to initialize gateway handler: %v", err)
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("[INFO] API Gateway starting on port :%s (Forwarding /api/orders -> %s)", cfg.Port, cfg.OrderServiceURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API Gateway HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("[INFO] API Gateway shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("API Gateway forced shutdown error: %v", err)
	}

	log.Println("[INFO] API Gateway exited cleanly")
}

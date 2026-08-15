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
	"syscall"
	"time"
)

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

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

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

	// Reverse proxy forwarding /api/orders to Order Service
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		r.Host = targetURL.Host
		proxy.ServeHTTP(w, r)
	})

	return mux, nil
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

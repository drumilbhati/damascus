package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"damascus/internal/otelgateway"
)

func main() {
	gateway, err := otelgateway.New()
	if err != nil {
		slog.Error("initialize OTel Demo HTTP gateway", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              ":8090",
		Handler:           gateway.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	slog.Info("OTel Demo HTTP gateway listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve OTel Demo HTTP gateway", "error", err)
		os.Exit(1)
	}
}

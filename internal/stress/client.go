package stress

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClientConfig holds the tunable parameters for the HTTP transport and
// per-request timeouts. All fields are optional; zero values fall back to
// the defaults documented on each field.
type ClientConfig struct {
	// MaxIdleConns controls the maximum number of idle (keep-alive) connections
	// across all hosts. Defaults to 1000 when zero.
	MaxIdleConns int

	// MaxIdleConnsPerHost controls the maximum number of idle connections
	// that are maintained per target host. Defaults to 100 when zero.
	MaxIdleConnsPerHost int

	// IdleConnTimeout is the maximum amount of time a keep-alive connection
	// stays idle before being closed. Defaults to 90 s when zero.
	IdleConnTimeout time.Duration

	// RequestTimeout is the end-to-end timeout applied to each individual
	// HTTP request (dial + TLS + send + read body). Defaults to 30 s when zero.
	RequestTimeout time.Duration
}

// defaultClientConfig returns a ClientConfig populated with production-grade
// defaults tuned for high-throughput load generation.
func defaultClientConfig() ClientConfig {
	return ClientConfig{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		RequestTimeout:      30 * time.Second,
	}
}

// Client is a thin wrapper around *http.Client that exposes a single,
// reusable sendRequest method. It is safe for concurrent use by multiple
// goroutines.
type Client struct {
	http    *http.Client
	timeout time.Duration
}

// NewClient constructs a Client using the provided cfg. Any zero value in cfg
// is replaced by the corresponding default from defaultClientConfig.
func NewClient(cfg ClientConfig) *Client {
	defaults := defaultClientConfig()

	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = defaults.MaxIdleConns
	}
	if cfg.MaxIdleConnsPerHost == 0 {
		cfg.MaxIdleConnsPerHost = defaults.MaxIdleConnsPerHost
	}
	if cfg.IdleConnTimeout == 0 {
		cfg.IdleConnTimeout = defaults.IdleConnTimeout
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaults.RequestTimeout
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
	}

	return &Client{
		http: &http.Client{
			Transport: transport,
			// No global Timeout here; per-request deadlines are applied via ctx.
		},
		timeout: cfg.RequestTimeout,
	}
}

// sendRequest executes an HTTP GET or POST against the URL in plan, respecting
// both the caller-supplied ctx and the Client's configured RequestTimeout.
// The response body is always drained and closed to return the underlying
// TCP connection to the pool and prevent memory leaks.
//
// plan.Method must be "GET" or "POST" (case-insensitive). For POST requests,
// plan.Payload is sent as the request body with Content-Type: application/json.
//
// An HTTP-level error status (4xx, 5xx) is returned as a non-nil error so
// that the caller can record the attempt as a failed request.
func (c *Client) sendRequest(ctx context.Context, plan LoadPlan) error {
	// Apply per-request timeout on top of the parent context so the broader
	// experiment cancellation still propagates correctly.
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var req *http.Request
	var err error

	switch strings.ToUpper(plan.Method) {
	case http.MethodGet:
		req, err = http.NewRequestWithContext(reqCtx, http.MethodGet, plan.TargetURL, nil)
		if err != nil {
			return fmt.Errorf("stress client: build GET request: %w", err)
		}

	case http.MethodPost:
		body := strings.NewReader(plan.Payload)
		req, err = http.NewRequestWithContext(reqCtx, http.MethodPost, plan.TargetURL, body)
		if err != nil {
			return fmt.Errorf("stress client: build POST request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

	default:
		return fmt.Errorf("stress client: unsupported method %q (only GET and POST are allowed)", plan.Method)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stress client: execute request: %w", err)
	}

	// Drain and close the body so the connection returns to the pool.
	// Capture read errors (e.g. context deadline during body transfer) and
	// surface them before evaluating the status code so they are never lost.
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Printf("stress client: closing response body: %v\n", closeErr)
		}
	}()
	if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
		return fmt.Errorf("stress client: reading response body: %w", copyErr)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("stress client: upstream returned %d %s",
			resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return nil
}

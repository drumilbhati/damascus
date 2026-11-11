package stress_test

import (
	"net/http"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestNewLoadHTTPClientSizesConnectionPoolToConcurrency(t *testing.T) {
	const maxInFlight = 2048
	client := stress.NewLoadHTTPClient(maxInFlight, 10*time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.MaxIdleConns != maxInFlight {
		t.Errorf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, maxInFlight)
	}
	if transport.MaxIdleConnsPerHost != maxInFlight {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, maxInFlight)
	}
	if transport.MaxConnsPerHost != maxInFlight {
		t.Errorf("MaxConnsPerHost = %d, want %d", transport.MaxConnsPerHost, maxInFlight)
	}
	if client.Timeout != 10*time.Second {
		t.Errorf("Timeout = %s, want 10s", client.Timeout)
	}
}

func TestNewLoadHTTPClientUsesSafeDefaults(t *testing.T) {
	client := stress.NewLoadHTTPClient(0, 0)
	transport := client.Transport.(*http.Transport)
	if transport.MaxIdleConnsPerHost != 256 || transport.MaxConnsPerHost != 256 {
		t.Fatalf("default pool limits = idle:%d total:%d, want 256", transport.MaxIdleConnsPerHost, transport.MaxConnsPerHost)
	}
	if client.Timeout != 10*time.Second {
		t.Fatalf("default timeout = %s, want 10s", client.Timeout)
	}
}

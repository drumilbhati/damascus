package otelgateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewCompilesPinnedDemoSchema(t *testing.T) {
	gateway, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if gateway.file.Services().ByName("CurrencyService") == nil {
		t.Fatal("CurrencyService descriptor missing")
	}
	if gateway.file.Services().ByName("CheckoutService") == nil {
		t.Fatal("CheckoutService descriptor missing")
	}
}

func TestGatewayValidationAndMutationGuard(t *testing.T) {
	gateway, err := New()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "mutating gRPC operation rejects GET", method: http.MethodGet, path: "/v1/PaymentService/Charge", wantStatus: http.StatusMethodNotAllowed},
		{name: "mutating HTTP operation rejects GET", method: http.MethodGet, path: "/v1/emailservice/send_order_confirmation", wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown service", method: http.MethodGet, path: "/v1/UnknownService/Call", wantStatus: http.StatusNotFound},
		{name: "malformed JSON body", method: http.MethodPost, path: "/v1/CurrencyService/Convert", body: `{"from":`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			res := httptest.NewRecorder()
			gateway.Handler().ServeHTTP(res, req)
			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, tt.wantStatus, res.Body.String())
			}
		})
	}
}

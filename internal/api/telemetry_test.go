package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"damascus/internal/telemetry"
)

type fakeTelemetry struct{ err error }

func (f *fakeTelemetry) Get() telemetry.Settings {
	return telemetry.Settings{Sources: telemetry.Sources{JaegerURL: "http://jaeger:16686", PrometheusURL: "http://prometheus:9090"}, Origin: "env"}
}

func (f *fakeTelemetry) Update(_ context.Context, next telemetry.Sources) (telemetry.Settings, error) {
	if f.err != nil {
		return f.Get(), f.err
	}
	return telemetry.Settings{Sources: next, Origin: "ui"}, nil
}

func TestTelemetrySettingsRoutes(t *testing.T) {
	fake := &fakeTelemetry{}
	router := NewRouter(NewHandler(nil, nil, nil, WithTelemetrySettings(fake)))
	do := func(method, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/api/settings/telemetry", strings.NewReader(body)))
		return rec
	}
	if rec := do(http.MethodGet, ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"origin":"env"`) {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPut, `{"jaeger_url":"http://j:16686","prometheus_url":"http://p:9090"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"origin":"ui"`) {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPut, `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", rec.Code)
	}
	fake.err = &telemetry.FieldError{Fields: map[string]string{"jaeger_url": "unreachable"}}
	if rec := do(http.MethodPut, `{}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"fields":{"jaeger_url":"unreachable"}`) {
		t.Fatalf("field error: %d %s", rec.Code, rec.Body)
	}
	fake.err = telemetry.ErrBusy
	if rec := do(http.MethodPut, `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("busy: %d", rec.Code)
	}
}

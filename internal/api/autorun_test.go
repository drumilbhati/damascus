package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"damascus/internal/autorun"
	"damascus/internal/stress"
)

type fakeAutoRun struct {
	startErr error
	runs     map[string]autorun.Run
}

func (f *fakeAutoRun) Start(context.Context, time.Duration) (autorun.Run, error) {
	if f.startErr != nil {
		return autorun.Run{}, f.startErr
	}
	return autorun.Run{ID: "r1", State: autorun.RunPending}, nil
}
func (f *fakeAutoRun) Get(id string) (autorun.Run, bool) { r, ok := f.runs[id]; return r, ok }
func (f *fakeAutoRun) List() []autorun.Run               { return []autorun.Run{f.runs["latest"]} }
func (f *fakeAutoRun) Requests() []stress.RequestSpec {
	return []stress.RequestSpec{{Name: "a", Method: "GET", URL: "http://x/", Headers: map[string]string{"Authorization": "secret"}}}
}

func TestAutoRunRoutes(t *testing.T) {
	fake := &fakeAutoRun{runs: map[string]autorun.Run{"latest": {ID: "r0", State: autorun.RunCompleted}}}
	router := NewRouter(NewHandler(nil, nil, nil, WithAutoRun(fake)))
	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	if rec := do(http.MethodPost, "/api/autoruns"); rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	fake.startErr = autorun.ErrRunInProgress
	if rec := do(http.MethodPost, "/api/autoruns"); rec.Code != http.StatusConflict {
		t.Fatalf("conflict: %d", rec.Code)
	}
	fake.startErr = autorun.ErrNoRequests
	if rec := do(http.MethodPost, "/api/autoruns"); rec.Code != http.StatusBadRequest {
		t.Fatalf("no requests: %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/autoruns/latest"); rec.Code != http.StatusOK {
		t.Fatalf("latest: %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/autoruns/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing: %d", rec.Code)
	}
	rec := do(http.MethodGet, "/api/endpoints")
	var body EndpointsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Requests[0].Headers["Authorization"] != "[redacted]" {
		t.Fatalf("endpoints must redact headers: %s", rec.Body)
	}
	if rec := do(http.MethodGet, "/api/autoruns"); rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}

	unconfigured := NewRouter(NewHandler(nil, nil, nil))
	rec = httptest.NewRecorder()
	unconfigured.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/autoruns", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unconfigured: %d", rec.Code)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseYAMLOrJSONRequestSpecs_YAMLList(t *testing.T) {
	yamlData := `
- name: list-items
  method: GET
  url: http://my-svc:8080/items
  weight: 3

- name: create-order
  method: POST
  url: http://my-svc:8080/orders
  headers:
    Content-Type: application/json
    Authorization: Bearer test
  body: |
    {
      "orderId": "{{uuid}}",
      "qty": 1
    }
  weight: 2
  allow_mutation: true
`
	specs, err := ParseYAMLOrJSONRequestSpecs([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	if specs[0].Name != "list-items" || specs[0].Method != "GET" || specs[0].Weight != 3 {
		t.Errorf("spec 0 mismatch: %+v", specs[0])
	}
	if specs[1].Name != "create-order" || specs[1].Method != "POST" || specs[1].Weight != 2 || !specs[1].AllowMutation {
		t.Errorf("spec 1 mismatch: %+v", specs[1])
	}
	if specs[1].Body != `{"orderId":"{{uuid}}","qty":1}` {
		t.Errorf("spec 1 body not compacted: %s", specs[1].Body)
	}
	if specs[1].Headers["Authorization"] != "Bearer test" {
		t.Errorf("headers mismatch: %+v", specs[1].Headers)
	}
}

func TestParseYAMLOrJSONRequestSpecs_NativeYAMLMappingBody(t *testing.T) {
	yamlData := `
endpoints:
  - name: native-body
    method: POST
    url: http://my-svc:8080/submit
    body:
      id: "req-{{uuid}}"
      count: 42
    allow_mutation: true
`
	specs, err := ParseYAMLOrJSONRequestSpecs([]byte(yamlData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Body != `{"count":42,"id":"req-{{uuid}}"}` {
		t.Errorf("unexpected json body from yaml mapping: %s", specs[0].Body)
	}
}

func TestLoadConfigFile_FullConfig(t *testing.T) {
	yamlContent := `
version: "1.0"
telemetry:
  jaeger_url: "http://jaeger:16686"
  prometheus_url: "http://prometheus:9090"

autorun:
  enabled: true
  start_delay: 15s
  scope: bottleneck
  baseline_rps: 5
  baseline_seconds: 30
  probe_rps: 25
  probe_seconds: 40

endpoints:
  - name: get-cart
    method: GET
    url: http://frontend:8080/cart
    weight: 4

  - name: checkout
    method: POST
    url: http://frontend:8080/checkout
    body: |
      {"user": "u-{{uuid}}"}
    allow_mutation: true
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "damascus.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}

	if cfg.Telemetry.JaegerURL != "http://jaeger:16686" {
		t.Errorf("expected jaeger_url http://jaeger:16686, got %q", cfg.Telemetry.JaegerURL)
	}
	if cfg.Telemetry.PrometheusURL != "http://prometheus:9090" {
		t.Errorf("expected prometheus_url http://prometheus:9090, got %q", cfg.Telemetry.PrometheusURL)
	}
	if cfg.AutoRun.Enabled == nil || !*cfg.AutoRun.Enabled {
		t.Errorf("expected autorun.enabled = true")
	}
	if cfg.AutoRun.StartDelay != 15*time.Second {
		t.Errorf("expected start_delay 15s, got %v", cfg.AutoRun.StartDelay)
	}
	if cfg.AutoRun.BaselineRPS != 5 || cfg.AutoRun.ProbeRPS != 25 {
		t.Errorf("expected baseline 5 and probe 25, got %d and %d", cfg.AutoRun.BaselineRPS, cfg.AutoRun.ProbeRPS)
	}
	if len(cfg.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(cfg.Endpoints))
	}
	if cfg.Endpoints[0].Name != "get-cart" || cfg.Endpoints[0].Weight != 4 {
		t.Errorf("endpoint 0 mismatch: %+v", cfg.Endpoints[0])
	}
	if cfg.Endpoints[1].Name != "checkout" || !cfg.Endpoints[1].AllowMutation {
		t.Errorf("endpoint 1 mismatch: %+v", cfg.Endpoints[1])
	}
}

func TestFindConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "custom.yaml")
	if err := os.WriteFile(filePath, []byte("version: 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Explicit path
	if found := FindConfigFile(filePath); found != filePath {
		t.Errorf("expected %s, got %s", filePath, found)
	}

	// 2. Via DAMASCUS_CONFIG_FILE env
	t.Setenv("DAMASCUS_CONFIG_FILE", filePath)
	if found := FindConfigFile(""); found != filePath {
		t.Errorf("expected %s via env, got %s", filePath, found)
	}
}

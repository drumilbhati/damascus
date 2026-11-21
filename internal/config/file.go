package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"damascus/internal/stress"

	"gopkg.in/yaml.v3"
)

// FileTelemetryConfig holds Jaeger and Prometheus endpoint configurations.
type FileTelemetryConfig struct {
	JaegerURL     string `json:"jaeger_url" yaml:"jaeger_url"`
	PrometheusURL string `json:"prometheus_url" yaml:"prometheus_url"`
}

// FileAutoRunConfig holds pipeline execution and tuning parameters.
type FileAutoRunConfig struct {
	Enabled            *bool         `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	StartDelay         time.Duration `json:"start_delay,omitempty" yaml:"start_delay,omitempty"`
	Scope              string        `json:"scope,omitempty" yaml:"scope,omitempty"`
	BaselineRPS        int           `json:"baseline_rps,omitempty" yaml:"baseline_rps,omitempty"`
	BaselineSeconds    int           `json:"baseline_seconds,omitempty" yaml:"baseline_seconds,omitempty"`
	ProbeRPS           int           `json:"probe_rps,omitempty" yaml:"probe_rps,omitempty"`
	ProbeSeconds       int           `json:"probe_seconds,omitempty" yaml:"probe_seconds,omitempty"`
	InitialRPS         int           `json:"initial_rps,omitempty" yaml:"initial_rps,omitempty"`
	StepRPS            int           `json:"step_rps,omitempty" yaml:"step_rps,omitempty"`
	MaxRPS             int           `json:"max_rps,omitempty" yaml:"max_rps,omitempty"`
	StepSeconds        int           `json:"step_seconds,omitempty" yaml:"step_seconds,omitempty"`
	MaxP95Ms           float64       `json:"max_p95_ms,omitempty" yaml:"max_p95_ms,omitempty"`
	MaxErrorPct        float64       `json:"max_error_pct,omitempty" yaml:"max_error_pct,omitempty"`
	MinAvailabilityPct float64       `json:"min_availability_pct,omitempty" yaml:"min_availability_pct,omitempty"`
}

// FileConfig represents a structured universal Damascus configuration file (YAML or JSON).
type FileConfig struct {
	Version           string               `json:"version,omitempty" yaml:"version,omitempty"`
	Telemetry         FileTelemetryConfig  `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
	Endpoints         []stress.RequestSpec `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	ServiceTargetURLs map[string]string    `json:"service_target_urls,omitempty" yaml:"service_target_urls,omitempty"`
	AutoRun           FileAutoRunConfig    `json:"autorun,omitempty" yaml:"autorun,omitempty"`
}

// rawYAMLRequestSpec is the intermediate representation for parsing request specifications.
type rawYAMLRequestSpec struct {
	Name          string            `json:"name" yaml:"name"`
	Service       string            `json:"service" yaml:"service"`
	Method        string            `json:"method" yaml:"method"`
	URL           string            `json:"url" yaml:"url"`
	Headers       map[string]string `json:"headers" yaml:"headers"`
	Body          yaml.Node         `json:"body" yaml:"body"`
	Weight        int               `json:"weight" yaml:"weight"`
	AllowMutation bool              `json:"allow_mutation" yaml:"allow_mutation"`
}

// ParseSpecsFromYAMLNode converts a slice of rawYAMLRequestSpec into validated stress.RequestSpec.
func convertRawYAMLSpecs(items []rawYAMLRequestSpec) ([]stress.RequestSpec, error) {
	specs := make([]stress.RequestSpec, 0, len(items))
	names := make(map[string]bool, len(items))

	for i, item := range items {
		spec := stress.RequestSpec{
			Name:          strings.TrimSpace(item.Name),
			Service:       strings.TrimSpace(item.Service),
			Method:        strings.ToUpper(strings.TrimSpace(item.Method)),
			URL:           strings.TrimSpace(item.URL),
			Headers:       item.Headers,
			Weight:        item.Weight,
			AllowMutation: item.AllowMutation,
		}
		if spec.Method == "" {
			spec.Method = "GET"
		}
		if spec.Name == "" {
			spec.Name = fmt.Sprintf("%s-%d", strings.ToLower(spec.Method), i+1)
		}
		if names[spec.Name] {
			return nil, fmt.Errorf("duplicate request name %q", spec.Name)
		}
		names[spec.Name] = true

		// Extract body
		bodyStr, err := extractBodyFromYAMLNode(&item.Body)
		if err != nil {
			return nil, fmt.Errorf("request %q body: %w", spec.Name, err)
		}
		spec.Body = bodyStr

		if err := spec.Validate(); err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func extractBodyFromYAMLNode(node *yaml.Node) (string, error) {
	if node == nil || node.Kind == 0 || (node.Kind == yaml.ScalarNode && (node.Value == "" || node.Value == "null")) {
		return "", nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		val := strings.TrimSpace(node.Value)
		if val == "" || val == "null" {
			return "", nil
		}
		// If it's a JSON string object or array, compact it
		if (strings.HasPrefix(val, "{") && strings.HasSuffix(val, "}")) ||
			(strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]")) {
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(val)); err == nil {
				return compact.String(), nil
			}
		}
		return val, nil

	case yaml.MappingNode, yaml.SequenceNode:
		// Decode structured YAML node to generic object and serialize to compact JSON
		var raw any
		if err := node.Decode(&raw); err != nil {
			return "", err
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		return string(b), nil

	default:
		return "", fmt.Errorf("unsupported body node kind %v", node.Kind)
	}
}

// ParseYAMLOrJSONRequestSpecs parses either a YAML/JSON array of requests or a root config with an "endpoints" key.
func ParseYAMLOrJSONRequestSpecs(data []byte) ([]stress.RequestSpec, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal(trimmed, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to parse YAML/JSON: %w", err)
	}

	if len(rootNode.Content) == 0 {
		return nil, nil
	}

	docNode := rootNode.Content[0]

	// Case 1: Root is a sequence/array [ {...}, {...} ]
	if docNode.Kind == yaml.SequenceNode {
		var rawItems []rawYAMLRequestSpec
		if err := docNode.Decode(&rawItems); err != nil {
			return nil, fmt.Errorf("failed to decode endpoints list: %w", err)
		}
		return convertRawYAMLSpecs(rawItems)
	}

	// Case 2: Root is a mapping/dict with an "endpoints" key
	if docNode.Kind == yaml.MappingNode {
		for i := 0; i < len(docNode.Content); i += 2 {
			keyNode := docNode.Content[i]
			valNode := docNode.Content[i+1]
			if keyNode.Value == "endpoints" || keyNode.Value == "requests" {
				if valNode.Kind != yaml.SequenceNode {
					return nil, fmt.Errorf("%s must be a list/array of request specifications", keyNode.Value)
				}
				var rawItems []rawYAMLRequestSpec
				if err := valNode.Decode(&rawItems); err != nil {
					return nil, fmt.Errorf("failed to decode %s: %w", keyNode.Value, err)
				}
				return convertRawYAMLSpecs(rawItems)
			}
		}
		return nil, fmt.Errorf("mapping must contain an 'endpoints' or 'requests' list")
	}

	return nil, fmt.Errorf("expected a list of endpoints or a config mapping with 'endpoints'")
}

// LoadConfigFile loads and parses a full FileConfig from a YAML or JSON file.
func LoadConfigFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal(data, &rootNode); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}

	if len(rootNode.Content) == 0 {
		return &FileConfig{}, nil
	}

	docNode := rootNode.Content[0]

	// If the file is a bare sequence of endpoints, wrap it
	if docNode.Kind == yaml.SequenceNode {
		var rawItems []rawYAMLRequestSpec
		if err := docNode.Decode(&rawItems); err != nil {
			return nil, fmt.Errorf("decode endpoints from %q: %w", path, err)
		}
		specs, err := convertRawYAMLSpecs(rawItems)
		if err != nil {
			return nil, fmt.Errorf("endpoints in %q: %w", path, err)
		}
		return &FileConfig{Endpoints: specs}, nil
	}

	// Normal root mapping
	type fileConfigIntermediate struct {
		Version           string               `json:"version" yaml:"version"`
		Telemetry         FileTelemetryConfig  `json:"telemetry" yaml:"telemetry"`
		Endpoints         []rawYAMLRequestSpec `json:"endpoints" yaml:"endpoints"`
		Requests          []rawYAMLRequestSpec `json:"requests" yaml:"requests"`
		ServiceTargetURLs map[string]string    `json:"service_target_urls" yaml:"service_target_urls"`
		AutoRun           FileAutoRunConfig    `json:"autorun" yaml:"autorun"`
	}

	var intermediate fileConfigIntermediate
	if err := docNode.Decode(&intermediate); err != nil {
		return nil, fmt.Errorf("decode config structure from %q: %w", path, err)
	}

	cfg := &FileConfig{
		Version:           intermediate.Version,
		Telemetry:         intermediate.Telemetry,
		ServiceTargetURLs: intermediate.ServiceTargetURLs,
		AutoRun:           intermediate.AutoRun,
	}

	rawEndpoints := intermediate.Endpoints
	if len(rawEndpoints) == 0 && len(intermediate.Requests) > 0 {
		rawEndpoints = intermediate.Requests
	}

	if len(rawEndpoints) > 0 {
		specs, err := convertRawYAMLSpecs(rawEndpoints)
		if err != nil {
			return nil, fmt.Errorf("endpoints in %q: %w", path, err)
		}
		cfg.Endpoints = specs
	}

	return cfg, nil
}

// FindConfigFile determines the configuration file path to use.
// It checks explicitPath, DAMASCUS_CONFIG_FILE, CONFIG_FILE,
// and default file locations (damascus.yaml, damascus.yml, damascus.json).
func FindConfigFile(explicitPath string) string {
	if p := strings.TrimSpace(explicitPath); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return p
	}

	for _, envKey := range []string{"DAMASCUS_CONFIG_FILE", "CONFIG_FILE"} {
		if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
			if _, err := os.Stat(val); err == nil {
				return val
			}
			return val
		}
	}

	// Default candidates
	candidates := []string{
		"damascus.yaml",
		"damascus.yml",
		"damascus.json",
		filepath.Join("config", "damascus.yaml"),
		filepath.Join("config", "damascus.yml"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return ""
}

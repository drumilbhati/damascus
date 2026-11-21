package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"damascus/internal/stress"
)

// rawRequestSpec accepts "body" as either a JSON value (object/array) or a JSON
// string holding the literal payload, so developers can paste bodies naturally.
type rawRequestSpec struct {
	Name          string            `json:"name"`
	Service       string            `json:"service"`
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	Body          json.RawMessage   `json:"body"`
	Weight        int               `json:"weight"`
	AllowMutation bool              `json:"allow_mutation"`
}

// ParseRequestSpecs parses a JSON array of developer-supplied GET/POST operations.
func ParseRequestSpecs(raw string) ([]stress.RequestSpec, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var items []rawRequestSpec
	if err := dec.Decode(&items); err != nil {
		return nil, fmt.Errorf("DAMASCUS_ENDPOINTS must be a JSON array of {name, method, url, body?, headers?, weight?, service?, allow_mutation?}: %w", err)
	}
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
			return nil, fmt.Errorf("DAMASCUS_ENDPOINTS: duplicate request name %q", spec.Name)
		}
		names[spec.Name] = true
		body := bytes.TrimSpace(item.Body)
		switch {
		case len(body) == 0 || bytes.Equal(body, []byte("null")):
		case body[0] == '"':
			if err := json.Unmarshal(body, &spec.Body); err != nil {
				return nil, fmt.Errorf("DAMASCUS_ENDPOINTS: request %q has an invalid body string: %w", spec.Name, err)
			}
		default:
			var compact bytes.Buffer
			if err := json.Compact(&compact, body); err != nil {
				return nil, fmt.Errorf("DAMASCUS_ENDPOINTS: request %q body: %w", spec.Name, err)
			}
			spec.Body = compact.String()
		}
		if err := spec.Validate(); err != nil {
			return nil, fmt.Errorf("DAMASCUS_ENDPOINTS: %w", err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// LoadRequestSpecs reads specs from DAMASCUS_ENDPOINTS_FILE when set, otherwise
// from the inline DAMASCUS_ENDPOINTS value. Both YAML and JSON formats are supported.
func LoadRequestSpecs(inline, filePath string) ([]stress.RequestSpec, error) {
	if path := strings.TrimSpace(filePath); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read DAMASCUS_ENDPOINTS_FILE: %w", err)
		}
		// Try YAML/JSON parser (supports both full file config and bare list)
		specs, err := ParseYAMLOrJSONRequestSpecs(data)
		if err == nil {
			return specs, nil
		}
		// Fallback to strict JSON parser for backwards compatibility
		return ParseRequestSpecs(string(data))
	}

	if strings.TrimSpace(inline) == "" {
		return nil, nil
	}
	// Try parsing inline as YAML/JSON first
	if specs, err := ParseYAMLOrJSONRequestSpecs([]byte(inline)); err == nil {
		return specs, nil
	}
	return ParseRequestSpecs(inline)
}

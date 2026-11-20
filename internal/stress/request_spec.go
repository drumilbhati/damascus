package stress

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// RequestSpec is one developer-supplied HTTP operation the workers can replay.
// URL and Body may contain the placeholders {{uuid}}, {{seq}} and {{timestamp}},
// which are rendered freshly for every request so POSTs do not collide on keys.
type RequestSpec struct {
	Name    string            `json:"name"`
	Service string            `json:"service,omitempty"` // telemetry service.name; discovered from traces when empty
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Weight  int               `json:"weight,omitempty"`
	// AllowMutation must be true before a non-GET spec is replayed by the
	// unattended auto-run pipeline. Manual experiments are unaffected.
	AllowMutation bool `json:"allow_mutation,omitempty"`
}

// Validate checks that the spec can be turned into an HTTP request.
func (s RequestSpec) Validate() error {
	method := strings.ToUpper(strings.TrimSpace(s.Method))
	if method != http.MethodGet && method != http.MethodPost {
		return fmt.Errorf("request %q: method %q is not supported (use GET or POST)", s.Name, s.Method)
	}
	if method == http.MethodGet && s.Body != "" {
		return fmt.Errorf("request %q: GET requests cannot carry a body", s.Name)
	}
	parsed, err := url.ParseRequestURI(renderTemplate(s.URL, 0))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("request %q: url %q must be an absolute http(s) URL", s.Name, s.URL)
	}
	if s.Weight < 0 {
		return fmt.Errorf("request %q: weight cannot be negative", s.Name)
	}
	return nil
}

// SampleURL renders the URL template once, for display and experiment metadata.
func (s RequestSpec) SampleURL() string { return renderTemplate(s.URL, 0) }

// IsMutating reports whether replaying the spec may change target state.
func (s RequestSpec) IsMutating() bool {
	return !strings.EqualFold(strings.TrimSpace(s.Method), http.MethodGet)
}

func (s RequestSpec) weight() int {
	if s.Weight <= 0 {
		return 1
	}
	return s.Weight
}

var templateReplacer = func(seq uint64) *strings.Replacer {
	return strings.NewReplacer(
		"{{uuid}}", uuid.NewString(),
		"{{seq}}", strconv.FormatUint(seq, 10),
		"{{timestamp}}", strconv.FormatInt(time.Now().UnixMilli(), 10),
	)
}

func renderTemplate(s string, seq uint64) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	return templateReplacer(seq).Replace(s)
}

// requestPicker selects specs with smooth weighted round-robin, which is
// deterministic and keeps the observed mix close to the configured weights.
type requestPicker struct {
	mu      sync.Mutex
	specs   []RequestSpec
	current []int
	total   int
	seq     atomic.Uint64
}

func newRequestPicker(specs []RequestSpec) *requestPicker {
	p := &requestPicker{specs: specs, current: make([]int, len(specs))}
	for _, s := range specs {
		p.total += s.weight()
	}
	return p
}

// next returns the chosen spec and a monotonically increasing sequence number.
func (p *requestPicker) next() (RequestSpec, uint64) {
	seq := p.seq.Add(1)
	if len(p.specs) == 1 {
		return p.specs[0], seq
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	best := 0
	for i, s := range p.specs {
		p.current[i] += s.weight()
		if p.current[i] > p.current[best] {
			best = i
		}
	}
	p.current[best] -= p.total
	return p.specs[best], seq
}

// specsForPlan returns the request mix, falling back to the legacy single-target fields.
func specsForPlan(plan LoadPlan) []RequestSpec {
	if len(plan.Requests) > 0 {
		return plan.Requests
	}
	return []RequestSpec{{Name: "default", Method: plan.Method, URL: plan.TargetURL, Body: plan.Payload}}
}

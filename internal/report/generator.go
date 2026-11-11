package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/safety"
)

// Sentinel errors for report generation and rendering validation.
var (
	ErrEmptyExperimentID = errors.New("experiment ID cannot be empty")
	ErrNilReport         = errors.New("experiment report cannot be nil")
	ErrInvalidTemplate   = errors.New("invalid report template")
)

// ReportEngine defines the contract for compiling experiment telemetry, graph analysis,
// and capacity thresholds into structured JSON and standalone HTML deliverables.
type ReportEngine interface {
	// Generate synthesizes experiment metadata, capacity metrics, graph criticality scores,
	// and raw observations into a complete ExperimentReport domain model.
	Generate(
		exp experiment.Experiment,
		capResult capacity.CapacityResult,
		scores []graph.ServiceScore,
		observations []capacity.Observation,
	) (*capacity.ExperimentReport, error)

	// RenderJSON serializes the experiment report into formatted, indented JSON bytes.
	RenderJSON(report *capacity.ExperimentReport) ([]byte, error)

	// RenderHTML renders the experiment report into a standalone, self-contained HTML page
	// displaying criticality scores, sustainable load limits, degradation curves, and recommendations.
	RenderHTML(report *capacity.ExperimentReport) ([]byte, error)
}

// Generator implements ReportEngine producing multi-format JSON and standalone HTML reports.
type Generator struct {
	tmpl *template.Template
}

// HTMLReportData encapsulates the view model passed to the HTML template.
type HTMLReportData struct {
	*capacity.ExperimentReport
	Report                 *capacity.ExperimentReport
	CriticalityLevel       string
	CriticalityBadgeClass  string
	CriticalityDescription string
	CriticalityScorePct    int
	DegradationChartSVG    template.HTML
	FormattedGeneratedAt   string
	TotalObservations      int
	PeakP95LatencyMs       float64
	PeakErrorRate          float64
	PeakCPUUtilization     float64
	PeakMemoryUtilization  float64
	HasObservations        bool
	HasRecommendations     bool
}

// NewGenerator instantiates a Generator configured with the default standalone HTML template.
func NewGenerator() *Generator {
	tmpl, err := template.New("report").Funcs(templateFuncs()).Parse(defaultHTMLTemplate)
	if err != nil {
		// Default template is a known valid constant; panic if broken during compile/init
		panic(fmt.Sprintf("failed to parse default HTML report template: %v", err))
	}
	return &Generator{tmpl: tmpl}
}

// NewGeneratorWithTemplate instantiates a Generator using a custom HTML template.
func NewGeneratorWithTemplate(customHTML string) (*Generator, error) {
	if strings.TrimSpace(customHTML) == "" {
		return nil, ErrInvalidTemplate
	}
	tmpl, err := template.New("report").Funcs(templateFuncs()).Parse(customHTML)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTemplate, err)
	}
	return &Generator{tmpl: tmpl}, nil
}

// NewReportEngine provides a constructor returning the ReportEngine interface.
func NewReportEngine() ReportEngine {
	return NewGenerator()
}

// Generate compiles domain inputs into an ExperimentReport.
func (g *Generator) Generate(
	exp experiment.Experiment,
	capResult capacity.CapacityResult,
	scores []graph.ServiceScore,
	observations []capacity.Observation,
) (*capacity.ExperimentReport, error) {
	if strings.TrimSpace(exp.ID) == "" {
		return nil, ErrEmptyExperimentID
	}

	policy := safety.SafetyPolicy{MaxP95LatencyMs: exp.Config.MaxP95LatencyMs, MaxErrorRate: exp.Config.MaxErrorRatePercent / 100, MinAvailability: exp.Config.MinAvailabilityPct / 100}
	capResult = capacity.NewAnalyzer().Analyze(observations, policy)

	// Match target service criticality score
	var critScore float64
	var matchedScore *graph.ServiceScore
	for i := range scores {
		if scores[i].ServiceName == exp.TargetService {
			critScore = scores[i].Score
			matchedScore = &scores[i]
			break
		}
	}

	// Copy observations to avoid external mutation
	var obsCopy []capacity.Observation
	if len(observations) > 0 {
		obsCopy = make([]capacity.Observation, len(observations))
		copy(obsCopy, observations)
	} else {
		obsCopy = []capacity.Observation{}
	}

	sort.SliceStable(obsCopy, func(i, j int) bool { return obsCopy[i].Timestamp.Before(obsCopy[j].Timestamp) })
	for i := range obsCopy {
		obsCopy[i].Phase = capacity.Phase(obsCopy[i])
		obsCopy[i].SLAStatus = capacity.SLAStatus(obsCopy[i], policy)
	}

	// Generate actionable engineering recommendations
	recommendations := generateRecommendations(exp, capResult, matchedScore, obsCopy)

	report := &capacity.ExperimentReport{
		Measurements:           capResult.Measurements,
		StopReason:             exp.StopReason,
		ExperimentID:           exp.ID,
		TargetService:          exp.TargetService,
		CriticalityScore:       critScore,
		MaximumTestedRate:      capResult.MaximumTestedRate,
		PeakMeasuredRate:       capResult.PeakMeasuredRate,
		MaximumSustainableRate: capResult.MaximumSustainableRate,
		DegradationRate:        capResult.DegradationRate,
		SafetyBoundaryRate:     capResult.SafetyBoundaryRate,
		RecoveryTimeSec:        capResult.RecoveryTimeSec,
		ObservedBreach:         capResult.ObservedBreach,
		ObservedRecovery:       capResult.ObservedRecovery,
		Observations:           obsCopy,
		Recommendations:        recommendations,
		GeneratedAt:            time.Now().UTC(),
	}

	return report, nil
}

// RenderJSON serializes the ExperimentReport to formatted indented JSON bytes.
func (g *Generator) RenderJSON(report *capacity.ExperimentReport) ([]byte, error) {
	if report == nil {
		return nil, ErrNilReport
	}
	return json.MarshalIndent(report, "", "  ")
}

// RenderJSONString serializes the ExperimentReport to an indented JSON string.
func (g *Generator) RenderJSONString(report *capacity.ExperimentReport) (string, error) {
	b, err := g.RenderJSON(report)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RenderHTML renders the ExperimentReport into a standalone, self-contained HTML page.
func (g *Generator) RenderHTML(report *capacity.ExperimentReport) ([]byte, error) {
	if report == nil {
		return nil, ErrNilReport
	}

	data := g.buildHTMLReportData(report)

	var buf bytes.Buffer
	if err := g.tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to render HTML template: %w", err)
	}

	return buf.Bytes(), nil
}

// RenderHTMLString renders the ExperimentReport to a standalone HTML string.
func (g *Generator) RenderHTMLString(report *capacity.ExperimentReport) (string, error) {
	b, err := g.RenderHTML(report)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// GenerateJSON is a convenience method that combines Generate and RenderJSON.
func (g *Generator) GenerateJSON(
	exp experiment.Experiment,
	capResult capacity.CapacityResult,
	scores []graph.ServiceScore,
	observations []capacity.Observation,
) ([]byte, error) {
	report, err := g.Generate(exp, capResult, scores, observations)
	if err != nil {
		return nil, err
	}
	return g.RenderJSON(report)
}

// GenerateHTML is a convenience method that combines Generate and RenderHTML.
func (g *Generator) GenerateHTML(
	exp experiment.Experiment,
	capResult capacity.CapacityResult,
	scores []graph.ServiceScore,
	observations []capacity.Observation,
) ([]byte, error) {
	report, err := g.Generate(exp, capResult, scores, observations)
	if err != nil {
		return nil, err
	}
	return g.RenderHTML(report)
}

// buildHTMLReportData constructs the view model for template rendering.
func (g *Generator) buildHTMLReportData(report *capacity.ExperimentReport) HTMLReportData {
	level, badgeClass, desc := classifyCriticality(report.CriticalityScore)
	scorePct := int(math.Round(report.CriticalityScore * 100))
	if scorePct > 100 {
		scorePct = 100
	} else if scorePct < 0 {
		scorePct = 0
	}

	var peakP95, peakError, peakCPU, peakMemory float64
	for _, obs := range report.Observations {
		if obs.P95LatencyMs > peakP95 {
			peakP95 = obs.P95LatencyMs
		}
		if obs.ErrorRate > peakError {
			peakError = obs.ErrorRate
		}
		if obs.CPUUtilization > peakCPU {
			peakCPU = obs.CPUUtilization
		}
		if obs.MemoryUtilization > peakMemory {
			peakMemory = obs.MemoryUtilization
		}
	}

	svgChart := renderDegradationCurveSVG(report)

	genAtStr := report.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC")
	if report.GeneratedAt.IsZero() {
		genAtStr = time.Now().UTC().Format("2006-01-02 15:04:05 UTC")
	}

	return HTMLReportData{
		ExperimentReport:       report,
		Report:                 report,
		CriticalityLevel:       level,
		CriticalityBadgeClass:  badgeClass,
		CriticalityDescription: desc,
		CriticalityScorePct:    scorePct,
		DegradationChartSVG:    svgChart,
		FormattedGeneratedAt:   genAtStr,
		TotalObservations:      len(report.Observations),
		PeakP95LatencyMs:       peakP95,
		PeakErrorRate:          peakError,
		PeakCPUUtilization:     peakCPU,
		PeakMemoryUtilization:  peakMemory,
		HasObservations:        len(report.Observations) > 0,
		HasRecommendations:     len(report.Recommendations) > 0,
	}
}

// classifyCriticality maps numerical score (0.0 to 1.0) to risk tier and descriptions.
func classifyCriticality(score float64) (level, badgeClass, description string) {
	switch {
	case score >= 0.85:
		return "CRITICAL", "badge-rose", "Single Point of Failure or high-frequency hub with deep cascade failure potential."
	case score >= 0.65:
		return "HIGH", "badge-amber", "Significant mesh dependency with substantial fan-in and blast radius."
	case score >= 0.35:
		return "MEDIUM", "badge-indigo", "Standard intermediate dependency with balanced downstream fan-out."
	default:
		return "LOW", "badge-emerald", "Peripheral leaf node with minimal systemic impact on mesh availability."
	}
}

// generateRecommendations compiles deterministic, actionable engineering recommendations.
func generateRecommendations(
	exp experiment.Experiment,
	capResult capacity.CapacityResult,
	matchedScore *graph.ServiceScore,
	observations []capacity.Observation,
) []string {
	var recs []string
	seen := make(map[string]bool)
	addRec := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg != "" && !seen[msg] {
			seen[msg] = true
			recs = append(recs, msg)
		}
	}

	// 1. Criticality and graph structure recommendations
	if matchedScore != nil {
		if matchedScore.Score >= 0.85 {
			addRec(fmt.Sprintf("Critical component detected (%s, criticality score: %.2f). Enforce strict upstream admission control, rate limiting, and automated circuit breaking.", exp.TargetService, matchedScore.Score))
		} else if matchedScore.Score >= 0.65 {
			addRec(fmt.Sprintf("High criticality service (%s, score: %.2f). Ensure multi-instance redundancy and prioritized resource allocation.", exp.TargetService, matchedScore.Score))
		}

		for _, r := range matchedScore.Reasons {
			lr := strings.ToLower(r)
			if strings.Contains(lr, "single point of failure") || strings.Contains(lr, "spof") {
				addRec("Single Point of Failure (SPOF) identified in dependency graph. Deploy multi-zone replicas and configure automated failover.")
				break
			}
		}
	}

	m := capResult.Measurements
	addRec(m.CapacityReason)
	addRec("Measured service traffic includes background requests and uses a trailing 30-second window; offered load is the generator target, not a measured request count.")
	if m.BreachStatus == "observed" {
		addRec("An SLA breach was observed. Inspect the timestamped samples for the breached metric; a completed experiment does not imply every SLA passed.")
	}
	if m.RecoveryStatus == "not_recovered" {
		addRec("Recovery was not observed before monitoring ended. The observation-window length is not a recovery duration.")
	}
	if exp.StopReason != "" {
		addRec("Experiment stopped: " + exp.StopReason)
	}

	// 6. Observations metrics inspection (peak error rate, peak latency, peak CPU, peak memory)
	var peakP95, peakError, peakCPU, peakMemory float64
	for _, obs := range observations {
		if obs.P95LatencyMs > peakP95 {
			peakP95 = obs.P95LatencyMs
		}
		if obs.ErrorRate > peakError {
			peakError = obs.ErrorRate
		}
		if obs.CPUUtilization > peakCPU {
			peakCPU = obs.CPUUtilization
		}
		if obs.MemoryUtilization > peakMemory {
			peakMemory = obs.MemoryUtilization
		}
	}

	if exp.Config.MaxErrorRatePercent > 0 && (peakError*100) > exp.Config.MaxErrorRatePercent {
		addRec(fmt.Sprintf("Peak error rate reached %.2f%% (exceeding SLA threshold of %.2f%%). Inspect application error logs and downstream connection timeouts.", peakError*100, exp.Config.MaxErrorRatePercent))
	} else if peakError > 0.05 {
		addRec(fmt.Sprintf("Elevated error rate observed (peak %.2f%%). Verify error handling, retry policies, and circuit breaker trip thresholds.", peakError*100))
	}

	if exp.Config.MaxP95LatencyMs > 0 && peakP95 > exp.Config.MaxP95LatencyMs {
		addRec(fmt.Sprintf("Peak P95 latency reached %.1fms (exceeding SLA threshold of %.1fms). Optimize backend database queries and synchronous call chains.", peakP95, exp.Config.MaxP95LatencyMs))
	}

	if peakCPU > 0.80 {
		addRec(fmt.Sprintf("High CPU utilization observed (peak %.1f%%). Consider increasing container CPU limits or tuning multi-threading parameters.", peakCPU*100))
	}

	if peakMemory > 0.80 {
		addRec(fmt.Sprintf("High memory utilization observed (peak %.1f%%). Investigate heap allocation patterns to prevent Out-Of-Memory (OOM) termination.", peakMemory*100))
	}

	// Fallback if system operated smoothly within bounds
	if len(recs) == 0 {
		recs = append(recs, "System operated within normal operational thresholds. No auto-remediations required.")
	}

	return recs
}

// degradationPoint aggregates telemetry for an individual load rate.
type degradationPoint struct {
	loadRate int
	avgP95Ms float64
	avgError float64
}

// renderDegradationCurveSVG builds an inline SVG diagram displaying load rate vs P95 latency.
func renderDegradationCurveSVG(report *capacity.ExperimentReport) template.HTML {
	if len(report.Observations) == 0 {
		return template.HTML(`<p>No observations available.</p>`)
	}
	var b strings.Builder
	maxRate := 1.0
	for _, o := range report.Observations {
		if !capacity.ValidObservation(o) {
			continue
		}
		maxRate = math.Max(maxRate, o.LoadRate)
		if o.InjectedRate != nil {
			maxRate = math.Max(maxRate, *o.InjectedRate)
		}
	}
	b.WriteString(`<svg viewBox="0 0 900 280" role="img" aria-label="Measured service traffic and offered load over time" xmlns="http://www.w3.org/2000/svg"><path d="M60 20V230H880" stroke="#94a3b8" fill="none"/>`)
	start := report.Observations[0].Timestamp
	duration := report.Observations[len(report.Observations)-1].Timestamp.Sub(start).Seconds()
	if duration <= 0 {
		duration = 1
	}
	for series := 0; series < 2; series++ {
		color := "#16a34a"
		if series == 1 {
			color = "#2563eb"
		}
		var points []string
		flush := func() {
			if len(points) > 0 {
				fmt.Fprintf(&b, `<polyline points="%s" stroke="%s" fill="none" stroke-width="2"/>`, strings.Join(points, " "), color)
				points = nil
			}
		}
		for _, o := range report.Observations {
			if !capacity.ValidObservation(o) || (series == 1 && o.InjectedRate == nil) {
				flush()
				continue
			}
			rate := o.LoadRate
			if series == 1 {
				rate = *o.InjectedRate
			}
			x, y := 60+820*o.Timestamp.Sub(start).Seconds()/duration, 230-200*rate/maxRate
			points = append(points, fmt.Sprintf("%.2f,%.2f", x, y))
			fmt.Fprintf(&b, `<circle cx="%.2f" cy="%.2f" r="2" fill="%s"><title>%s: %.2f RPS</title></circle>`, x, y, color, o.Timestamp.UTC().Format("15:04:05"), rate)
		}
		flush()
	}
	fmt.Fprintf(&b, `<text x="60" y="14" font-size="12">RPS (max %.2f)</text><text x="60" y="250" font-size="12">0s</text><text x="820" y="250" font-size="12">%.1fs</text><text x="160" y="274" fill="#16a34a" font-size="12">Measured service traffic</text><text x="460" y="274" fill="#2563eb" font-size="12">Offered load target</text></svg>`, maxRate, duration)
	return template.HTML(b.String())
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatFloat": func(f float64, decimals int) string {
			return fmt.Sprintf("%.*f", decimals, f)
		},
		"formatPercent": func(v float64) string {
			if v <= 1.0 && v >= 0 {
				return fmt.Sprintf("%.2f%%", v*100)
			}
			return fmt.Sprintf("%.2f%%", v)
		},
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "N/A"
			}
			return t.UTC().Format("15:04:05 UTC")
		},
		"add": func(a, b int) int {
			return a + b
		},
		"nullable": func(v *float64) string {
			if v == nil {
				return "Unavailable"
			}
			return fmt.Sprintf("%.2f", *v)
		},
		"nullablePercent": func(v *float64) string {
			if v == nil {
				return "Unavailable"
			}
			return fmt.Sprintf("%.2f%%", *v*100)
		},
		"statusBadge": func(obs capacity.Observation, report *capacity.ExperimentReport) string {
			if obs.SLAStatus == "breach" {
				return "SLA breach"
			}
			if obs.SLAStatus == "within_limits" {
				return "Within SLA limits"
			}
			return "Unavailable"
		},
	}
}

// defaultHTMLTemplate is the complete, self-contained standalone HTML report template.
const defaultHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Damascus Capacity Analysis Report - {{.TargetService}}</title>
  <style>
    :root {
      --primary: #4f46e5;
      --primary-light: #eef2ff;
      --primary-dark: #3730a3;
      --success: #10b981;
      --success-light: #ecfdf5;
      --warning: #f59e0b;
      --warning-light: #fffbeb;
      --danger: #ef4444;
      --danger-light: #fef2f2;
      --slate-50: #f8fafc;
      --slate-100: #f1f5f9;
      --slate-200: #e2e8f0;
      --slate-300: #cbd5e1;
      --slate-400: #94a3b8;
      --slate-500: #64748b;
      --slate-600: #475569;
      --slate-700: #334155;
      --slate-800: #1e293b;
      --slate-900: #0f172a;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      background-color: var(--slate-50);
      color: var(--slate-900);
      line-height: 1.5;
      padding: 32px 20px;
    }
    .container {
      max-width: 1100px;
      margin: 0 auto;
    }
    .header {
      background: white;
      border: 1px solid var(--slate-200);
      border-radius: 16px;
      padding: 24px 28px;
      margin-bottom: 24px;
      box-shadow: 0 1px 3px rgba(0,0,0,0.04);
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      flex-wrap: wrap;
      gap: 16px;
    }
    .brand-title {
      display: flex;
      align-items: center;
      gap: 10px;
      margin-bottom: 6px;
    }
    .brand-badge {
      background: var(--primary);
      color: white;
      font-size: 11px;
      font-weight: 800;
      letter-spacing: 0.05em;
      padding: 4px 8px;
      border-radius: 6px;
    }
    .header h1 {
      font-size: 22px;
      font-weight: 800;
      color: var(--slate-900);
    }
    .header-meta {
      color: var(--slate-500);
      font-size: 13px;
      display: flex;
      gap: 16px;
      flex-wrap: wrap;
      margin-top: 6px;
    }
    .header-meta span strong {
      color: var(--slate-700);
    }
    .kpi-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }
    .kpi-card {
      background: white;
      border: 1px solid var(--slate-200);
      border-radius: 14px;
      padding: 20px;
      box-shadow: 0 1px 3px rgba(0,0,0,0.04);
    }
    .kpi-label {
      font-size: 11px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      color: var(--slate-500);
      margin-bottom: 8px;
    }
    .kpi-value {
      font-size: 26px;
      font-weight: 900;
      margin-bottom: 4px;
    }
    .kpi-desc {
      font-size: 12px;
      color: var(--slate-400);
    }
    .val-sustainable { color: var(--success); }
    .val-degradation { color: var(--warning); }
    .val-safety { color: var(--danger); }
    .val-recovery { color: var(--primary); }
    .val-tested { color: var(--slate-700); }

    .section-card {
      background: white;
      border: 1px solid var(--slate-200);
      border-radius: 16px;
      padding: 24px 28px;
      margin-bottom: 24px;
      box-shadow: 0 1px 3px rgba(0,0,0,0.04);
    }
    .section-header {
      margin-bottom: 18px;
    }
    .section-title {
      font-size: 17px;
      font-weight: 800;
      color: var(--slate-900);
      margin-bottom: 4px;
    }
    .section-subtitle {
      font-size: 13px;
      color: var(--slate-500);
    }
    .chart-container {
      width: 100%;
      overflow-x: auto;
      margin: 12px 0;
    }
    .criticality-box {
      display: flex;
      align-items: center;
      gap: 24px;
      flex-wrap: wrap;
      padding: 20px;
      background: var(--slate-50);
      border: 1px solid var(--slate-200);
      border-radius: 12px;
      margin-top: 12px;
    }
    .score-circle {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      width: 80px;
      height: 80px;
      border-radius: 50%;
      background: white;
      border: 3px solid var(--primary);
      box-shadow: 0 2px 4px rgba(79, 70, 229, 0.1);
    }
    .score-num {
      font-size: 22px;
      font-weight: 900;
      color: var(--primary);
      line-height: 1;
    }
    .score-max {
      font-size: 10px;
      color: var(--slate-400);
      font-weight: 600;
      margin-top: 2px;
    }
    .criticality-details {
      flex: 1;
      min-width: 240px;
    }
    .criticality-bar-wrap {
      margin-top: 8px;
      height: 8px;
      background: var(--slate-200);
      border-radius: 4px;
      overflow: hidden;
    }
    .criticality-bar-fill {
      height: 100%;
      background: linear-gradient(90deg, #10b981 0%, #f59e0b 60%, #ef4444 100%);
      border-radius: 4px;
    }
    .recommendations-list {
      list-style: none;
      display: flex;
      flex-direction: column;
      gap: 12px;
      margin-top: 12px;
    }
    .recommendation-item {
      display: flex;
      align-items: flex-start;
      gap: 12px;
      background: var(--slate-50);
      border: 1px solid var(--slate-200);
      border-radius: 10px;
      padding: 14px 16px;
      font-size: 13.5px;
      color: var(--slate-700);
    }
    .rec-icon {
      flex-shrink: 0;
      width: 20px;
      height: 20px;
      color: var(--success);
      margin-top: 1px;
    }
    .table-container {
      overflow-x: auto;
      max-height: 480px;
      overflow-y: auto;
      margin-top: 12px;
      border: 1px solid var(--slate-200);
      border-radius: 12px;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 13px;
      text-align: left;
    }
    th {
      background: var(--slate-100);
      color: var(--slate-600);
      font-weight: 700;
      padding: 12px 14px;
      border-bottom: 1px solid var(--slate-200);
      position: sticky;
      top: 0;
      z-index: 1;
      white-space: nowrap;
    }
    td {
      padding: 11px 14px;
      border-bottom: 1px solid var(--slate-100);
      color: var(--slate-700);
      white-space: nowrap;
    }
    tr:last-child td {
      border-bottom: none;
    }
    tr:hover td {
      background: var(--slate-50);
    }
    .badge {
      display: inline-block;
      padding: 3px 8px;
      border-radius: 6px;
      font-size: 11px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.04em;
    }
    .badge-emerald { background: var(--success-light); color: var(--success); }
    .badge-amber { background: var(--warning-light); color: var(--warning); }
    .badge-rose { background: var(--danger-light); color: var(--danger); }
    .badge-gray { background: var(--slate-100); color: var(--slate-500); }
    .badge-indigo { background: var(--primary-light); color: var(--primary); }

    .footer {
      text-align: center;
      color: var(--slate-400);
      font-size: 12px;
      margin-top: 32px;
      padding-bottom: 16px;
    }
  </style>
</head>
<body>
  <div class="container">
    <!-- Header -->
    <header class="header">
      <div>
        <div class="brand-title">
          <span class="brand-badge">DAMASCUS</span>
          <h1>Capacity Analysis Report</h1>
        </div>
        <div class="header-meta">
          <span>Target Service: <strong>{{.TargetService}}</strong></span>
          <span>Experiment ID: <strong>{{.ExperimentID}}</strong></span>
          <span>Generated: <strong>{{.FormattedGeneratedAt}}</strong></span>
        </div>
      </div>
    </header>

    <section class="kpi-grid">
      <div class="kpi-card"><div class="kpi-label">Peak measured service RPS</div><div class="kpi-value">{{nullable .Measurements.PeakMeasuredRate}}</div><div class="kpi-desc">Includes background traffic · {{.Measurements.SummaryScope}}</div></div>
      <div class="kpi-card"><div class="kpi-label">Peak offered load target</div><div class="kpi-value">{{nullable .Measurements.PeakOfferedRate}}</div><div class="kpi-desc">RPS target, not measured traffic</div></div>
      <div class="kpi-card"><div class="kpi-label">Peak P95 latency</div><div class="kpi-value">{{nullable .Measurements.PeakP95LatencyMs}} ms</div></div>
      <div class="kpi-card"><div class="kpi-label">Peak error rate</div><div class="kpi-value">{{nullablePercent .Measurements.PeakErrorRate}}</div></div>
      <div class="kpi-card"><div class="kpi-label">Minimum availability</div><div class="kpi-value">{{nullablePercent .Measurements.MinAvailability}}</div></div>
    </section>
    <section class="section-card"><h2>Sustainable capacity estimate</h2>
    {{if eq .Measurements.CapacityStatus "established"}}<p>{{nullable .Measurements.SustainableMeasuredRate}} measured service RPS at {{nullable .Measurements.SustainableOfferedRate}} offered RPS</p>{{else}}<p>Not established</p>{{end}}
    <p>{{.Measurements.CapacityReason}}</p>
    <h3>SLA breach</h3>{{if .ObservedBreach}}<p>Observed at {{nullable .Measurements.FirstBreachMeasuredRate}} measured RPS; offered target {{nullable .Measurements.FirstBreachOfferedRate}} RPS.</p>{{else}}<p>{{if eq .Measurements.BreachStatus "unavailable"}}Unavailable — no valid telemetry{{else}}Not observed{{end}}</p>{{end}}
    <h3>Recovery</h3>{{if .ObservedRecovery}}<p>{{nullable .Measurements.RecoverySeconds}}s to observed SLA compliance</p>{{else if eq .Measurements.RecoveryStatus "not_recovered"}}<p>Not observed within {{nullable .Measurements.RecoveryWindowSeconds}}s of monitoring</p>{{else if eq .Measurements.RecoveryStatus "not_needed"}}<p>Not needed — no SLA breach observed</p>{{else}}<p>Not observed</p>{{end}}
    </section>

    <!-- Criticality Score Section -->
    <section class="section-card">
      <div class="section-header">
        <h2 class="section-title">Microservice Criticality Analysis</h2>
        <p class="section-subtitle">Graph-based structural dependency evaluation and topological blast radius score</p>
      </div>
      <div class="criticality-box">
        <div class="score-circle">
          <span class="score-num">{{formatFloat .CriticalityScore 2}}</span>
          <span class="score-max">/ 1.00</span>
        </div>
        <div class="criticality-details">
          <div style="display: flex; align-items: center; gap: 10px;">
            <strong style="font-size: 15px; color: var(--slate-900);">{{.TargetService}}</strong>
            <span class="badge {{.CriticalityBadgeClass}}">{{.CriticalityLevel}} RISK</span>
          </div>
          <p style="font-size: 13px; color: var(--slate-600); margin-top: 4px;">{{.CriticalityDescription}}</p>
          <div class="criticality-bar-wrap">
            <div class="criticality-bar-fill" style="width: {{.CriticalityScorePct}}%;"></div>
          </div>
        </div>
      </div>
    </section>

    <!-- Degradation Curve Section -->
    <section class="section-card">
      <div class="section-header">
        <h2 class="section-title">Measured and offered traffic over time</h2>
        <p class="section-subtitle">Measured service traffic includes background requests. Offered load is the configured target. Rates use the trailing 30-second metric window.</p>
      </div>
      <div class="chart-container">
        {{.DegradationChartSVG}}
      </div>
    </section>

    <!-- Automated Engineering Recommendations -->
    <section class="section-card">
      <div class="section-header">
        <h2 class="section-title">Automated Engineering Recommendations</h2>
        <p class="section-subtitle">Synthesized remediation and tuning recommendations derived from capacity and safety telemetry</p>
      </div>
      <ul class="recommendations-list">
        {{range .Recommendations}}
        <li class="recommendation-item">
          <svg class="rec-icon" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
          </svg>
          <span>{{.}}</span>
        </li>
        {{end}}
      </ul>
    </section>

    <!-- Granular Telemetry Observations Table -->
    {{if .HasObservations}}
    <section class="section-card">
      <div class="section-header">
        <h2 class="section-title">Granular Observation Telemetry</h2>
        <p class="section-subtitle">Time-series data points recorded across all experimental stress tiers ({{.TotalObservations}} samples)</p>
      </div>
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>#</th>
              <th>Timestamp</th>
              <th>Measured service RPS</th><th>Offered target RPS</th><th>Phase</th>
              <th>P95 Latency</th>
              <th>Error Rate</th>
              <th>Availability</th>
              <th>CPU Util</th>
              <th>Memory Util</th>
              <th>Evaluation</th>
            </tr>
          </thead>
          <tbody>
            {{range $idx, $obs := .Observations}}
            <tr>
              <td>{{add $idx 1}}</td>
              <td>{{formatTime $obs.Timestamp}}</td>
              <td><strong>{{if eq $obs.SLAStatus "unavailable"}}Unavailable{{else}}{{formatFloat $obs.LoadRate 2}} RPS{{end}}</strong></td><td>{{nullable $obs.InjectedRate}}</td><td>{{$obs.Phase}}</td>
              <td>{{if eq $obs.SLAStatus "unavailable"}}Unavailable{{else}}{{formatFloat $obs.P95LatencyMs 1}} ms{{end}}</td>
              <td>{{if eq $obs.SLAStatus "unavailable"}}Unavailable{{else}}{{formatPercent $obs.ErrorRate}}{{end}}</td>
              <td>{{if eq $obs.SLAStatus "unavailable"}}Unavailable{{else}}{{formatPercent $obs.Availability}}{{end}}</td>
              <td>{{formatPercent $obs.CPUUtilization}}</td>
              <td>{{formatPercent $obs.MemoryUtilization}}</td>
              <td>{{statusBadge $obs $.Report}}</td>
            </tr>
            {{end}}
          </tbody>
        </table>
      </div>
    </section>
    {{end}}

    <footer class="footer">
      DAMASCUS Chaos &amp; Resilience Engineering Control Plane &bull; Standalone Capacity Report
    </footer>
  </div>
</body>
</html>`

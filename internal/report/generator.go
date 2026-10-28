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

	// Generate actionable engineering recommendations
	recommendations := generateRecommendations(exp, capResult, matchedScore, obsCopy)

	report := &capacity.ExperimentReport{
		ExperimentID:           exp.ID,
		TargetService:          exp.TargetService,
		CriticalityScore:       critScore,
		MaximumTestedRate:      capResult.MaximumTestedRate,
		MaximumSustainableRate: capResult.MaximumSustainableRate,
		DegradationRate:        capResult.DegradationRate,
		SafetyBoundaryRate:     capResult.SafetyBoundaryRate,
		RecoveryTimeSec:        capResult.RecoveryTimeSec,
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

	// 2. Sustainable load limits & throughput capacity
	if capResult.MaximumSustainableRate == 0 && capResult.MaximumTestedRate > 0 {
		addRec(fmt.Sprintf("Service failed to sustain initial tested traffic (%d RPS) without breaching SLA limits. Immediate performance bottleneck profiling and vertical/horizontal scaling required.", capResult.MaximumTestedRate))
	} else if capResult.MaximumTestedRate > 0 && capResult.MaximumSustainableRate < capResult.MaximumTestedRate {
		addRec(fmt.Sprintf("Maximum sustainable throughput (%d RPS) is below peak tested load (%d RPS). Configure ingress load shedding or Horizontal Pod Autoscaling (HPA) above %d RPS.", capResult.MaximumSustainableRate, capResult.MaximumTestedRate, capResult.MaximumSustainableRate))
	} else if exp.Config.MaxRate > 0 && capResult.MaximumSustainableRate < exp.Config.MaxRate {
		addRec(fmt.Sprintf("Maximum sustainable throughput (%d RPS) fell short of planned ceiling (%d RPS). Scale service replicas to meet target capacity.", capResult.MaximumSustainableRate, exp.Config.MaxRate))
	}

	// 3. Degradation knee curve analysis
	if capResult.DegradationRate > 0 {
		warningThreshold := int(float64(capResult.DegradationRate) * 0.85)
		if warningThreshold < 1 {
			warningThreshold = capResult.DegradationRate
		}
		addRec(fmt.Sprintf("Performance degradation knee detected at %d RPS (latency slope surged >50%%). Set proactive early-warning alerts and autoscaling triggers at %d RPS.", capResult.DegradationRate, warningThreshold))
	}

	// 4. Hard safety boundary breaches
	if capResult.SafetyBoundaryRate > 0 && capResult.SafetyBoundaryRate < capResult.MaximumTestedRate {
		addRec(fmt.Sprintf("Hard SLA safety boundary breached at %d RPS. Ensure ingress admission control drops non-essential traffic prior to reaching this threshold.", capResult.SafetyBoundaryRate))
	}

	// 5. Recovery Time Objective (RTO)
	if capResult.RecoveryTimeSec > 0 {
		if exp.Config.RecoveryWindowSec > 0 && capResult.RecoveryTimeSec > exp.Config.RecoveryWindowSec {
			addRec(fmt.Sprintf("Post-stress recovery time (%ds) exceeded configured recovery window (%ds). Optimize connection pool draining and GC reclamation to accelerate recovery.", capResult.RecoveryTimeSec, exp.Config.RecoveryWindowSec))
		} else {
			addRec(fmt.Sprintf("Post-stress recovery stabilized in %ds. Telemetry returned to normal baseline SLA.", capResult.RecoveryTimeSec))
		}
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
	loadRate   int
	avgP95Ms   float64
	avgError   float64
}

// renderDegradationCurveSVG builds an inline SVG diagram displaying load rate vs P95 latency.
func renderDegradationCurveSVG(report *capacity.ExperimentReport) template.HTML {
	// Aggregate active observations by integer load rate
	type tierAgg struct {
		totalP95   float64
		totalError float64
		count      int
	}
	tiersMap := make(map[int]*tierAgg)

	for _, obs := range report.Observations {
		rate := int(math.Round(obs.LoadRate))
		if rate <= 0 {
			continue
		}
		agg, exists := tiersMap[rate]
		if !exists {
			agg = &tierAgg{}
			tiersMap[rate] = agg
		}
		agg.totalP95 += obs.P95LatencyMs
		agg.totalError += obs.ErrorRate
		agg.count++
	}

	// If no active load tiers exist, render clean fallback SVG
	if len(tiersMap) == 0 {
		fallback := `<svg viewBox="0 0 800 160" width="100%" height="160" xmlns="http://www.w3.org/2000/svg">
  <rect width="800" height="160" fill="#f8fafc" rx="8" stroke="#e2e8f0"/>
  <text x="400" y="85" text-anchor="middle" fill="#94a3b8" font-size="14" font-family="-apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif">
    No active stress observation telemetry recorded to plot degradation curve
  </text>
</svg>`
		return template.HTML(fallback)
	}

	var rates []int
	for r := range tiersMap {
		rates = append(rates, r)
	}
	sort.Ints(rates)

	var points []degradationPoint
	maxRate := report.MaximumTestedRate
	var maxP95 float64

	for _, r := range rates {
		agg := tiersMap[r]
		avgP95 := agg.totalP95 / float64(agg.count)
		avgErr := agg.totalError / float64(agg.count)
		points = append(points, degradationPoint{
			loadRate: r,
			avgP95Ms: avgP95,
			avgError: avgErr,
		})
		if r > maxRate {
			maxRate = r
		}
		if avgP95 > maxP95 {
			maxP95 = avgP95
		}
	}

	if maxRate <= 0 {
		maxRate = 100
	}
	if maxP95 <= 0 {
		maxP95 = 50.0
	}
	// Headroom for Y-axis (20% higher than max latency)
	yMax := maxP95 * 1.20

	// SVG layout geometry
	svgW := 800.0
	svgH := 320.0
	padL := 75.0
	padR := 50.0
	padT := 45.0
	padB := 50.0
	plotW := svgW - padL - padR
	plotH := svgH - padT - padB

	mapX := func(rate int) float64 {
		return padL + (float64(rate)/float64(maxRate))*plotW
	}
	mapY := func(p95 float64) float64 {
		val := padT + plotH - (p95/yMax)*plotH
		if val < padT {
			return padT
		}
		if val > padT+plotH {
			return padT + plotH
		}
		return val
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "<svg viewBox=\"0 0 %.0f %.0f\" width=\"100%%\" height=\"auto\" xmlns=\"http://www.w3.org/2000/svg\" style=\"max-height: 360px;\">\n", svgW, svgH)

	// Defs: Gradients and shadows
	buf.WriteString(`  <defs>
    <linearGradient id="curveGradient" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#4f46e5" stop-opacity="0.25"/>
      <stop offset="100%" stop-color="#4f46e5" stop-opacity="0.0"/>
    </linearGradient>
  </defs>` + "\n")

	// Background
	fmt.Fprintf(&buf, "  <rect width=\"%.0f\" height=\"%.0f\" fill=\"#ffffff\" rx=\"10\"/>\n", svgW, svgH)

	// Y-axis horizontal grid lines (4 levels)
	gridLevels := 4
	for i := 0; i <= gridLevels; i++ {
		ratio := float64(i) / float64(gridLevels)
		yVal := padT + plotH - (ratio * plotH)
		latVal := ratio * yMax
		fmt.Fprintf(&buf, "  <line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#e2e8f0\" stroke-width=\"1\" stroke-dasharray=\"3,3\"/>\n", padL, yVal, padL+plotW, yVal)
		fmt.Fprintf(&buf, "  <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" fill=\"#64748b\" font-size=\"11\" font-family=\"system-ui, sans-serif\">%.0fms</text>\n", padL-10, yVal+4, latVal)
	}

	// X-axis baseline and tick labels
	fmt.Fprintf(&buf, "  <line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#cbd5e1\" stroke-width=\"1.5\"/>\n", padL, padT+plotH, padL+plotW, padT+plotH)
	xTicks := 5
	for i := 0; i <= xTicks; i++ {
		ratio := float64(i) / float64(xTicks)
		rateVal := int(math.Round(ratio * float64(maxRate)))
		xVal := padL + ratio*plotW
		fmt.Fprintf(&buf, "  <line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#cbd5e1\" stroke-width=\"1\"/>\n", xVal, padT+plotH, xVal, padT+plotH+4)
		fmt.Fprintf(&buf, "  <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" fill=\"#64748b\" font-size=\"11\" font-family=\"system-ui, sans-serif\">%d RPS</text>\n", xVal, padT+plotH+18, rateVal)
	}

	// Axis Titles
	fmt.Fprintf(&buf, "  <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" fill=\"#475569\" font-size=\"12\" font-weight=\"600\" font-family=\"system-ui, sans-serif\">Offered Load Rate (Requests / Sec)</text>\n", padL+plotW/2, svgH-10)
	fmt.Fprintf(&buf, "  <text transform=\"rotate(-90)\" x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" fill=\"#475569\" font-size=\"12\" font-weight=\"600\" font-family=\"system-ui, sans-serif\">P95 Latency (ms)</text>\n", -(padT + plotH/2), 20.0)

	// Shaded area under the curve
	var areaPoints []string
	firstX := mapX(points[0].loadRate)
	baseY := padT + plotH
	areaPoints = append(areaPoints, fmt.Sprintf("%.1f,%.1f", firstX, baseY))
	for _, pt := range points {
		areaPoints = append(areaPoints, fmt.Sprintf("%.1f,%.1f", mapX(pt.loadRate), mapY(pt.avgP95Ms)))
	}
	lastX := mapX(points[len(points)-1].loadRate)
	areaPoints = append(areaPoints, fmt.Sprintf("%.1f,%.1f", lastX, baseY))
	fmt.Fprintf(&buf, "  <polygon points=\"%s\" fill=\"url(#curveGradient)\"/>\n", strings.Join(areaPoints, " "))

	// Degradation curve polyline
	var linePoints []string
	for _, pt := range points {
		linePoints = append(linePoints, fmt.Sprintf("%.1f,%.1f", mapX(pt.loadRate), mapY(pt.avgP95Ms)))
	}
	fmt.Fprintf(&buf, "  <polyline points=\"%s\" fill=\"none\" stroke=\"#4f46e5\" stroke-width=\"3\" stroke-linecap=\"round\" stroke-linejoin=\"round\"/>\n", strings.Join(linePoints, " "))

	// Threshold vertical markers
	// 1. Sustainable Rate
	if report.MaximumSustainableRate > 0 && report.MaximumSustainableRate <= maxRate {
		sx := mapX(report.MaximumSustainableRate)
		fmt.Fprintf(&buf, "  <line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#10b981\" stroke-width=\"2\" stroke-dasharray=\"4,3\"/>\n", sx, padT, sx, baseY)
		fmt.Fprintf(&buf, "  <rect x=\"%.1f\" y=\"%.1f\" width=\"94\" height=\"20\" rx=\"4\" fill=\"#10b981\"/>\n", sx-47, padT-22)
		fmt.Fprintf(&buf, "  <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" fill=\"#ffffff\" font-size=\"10\" font-weight=\"700\" font-family=\"system-ui, sans-serif\">Sustainable: %d</text>\n", sx, padT-8, report.MaximumSustainableRate)
	}

	// 2. Degradation Knee Rate
	if report.DegradationRate > 0 && report.DegradationRate <= maxRate {
		kx := mapX(report.DegradationRate)
		fmt.Fprintf(&buf, "  <line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#f59e0b\" stroke-width=\"2\" stroke-dasharray=\"4,3\"/>\n", kx, padT, kx, baseY)
		fmt.Fprintf(&buf, "  <rect x=\"%.1f\" y=\"%.1f\" width=\"80\" height=\"20\" rx=\"4\" fill=\"#f59e0b\"/>\n", kx-40, padT-22)
		fmt.Fprintf(&buf, "  <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" fill=\"#ffffff\" font-size=\"10\" font-weight=\"700\" font-family=\"system-ui, sans-serif\">Knee: %d RPS</text>\n", kx, padT-8, report.DegradationRate)
	}

	// 3. Safety Boundary Rate (render if distinct from sustainable rate)
	if report.SafetyBoundaryRate > 0 && report.SafetyBoundaryRate <= maxRate && report.SafetyBoundaryRate != report.MaximumSustainableRate {
		bx := mapX(report.SafetyBoundaryRate)
		fmt.Fprintf(&buf, "  <line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#ef4444\" stroke-width=\"2\" stroke-dasharray=\"4,3\"/>\n", bx, padT, bx, baseY)
		fmt.Fprintf(&buf, "  <rect x=\"%.1f\" y=\"%.1f\" width=\"86\" height=\"20\" rx=\"4\" fill=\"#ef4444\"/>\n", bx-43, padT-22)
		fmt.Fprintf(&buf, "  <text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" fill=\"#ffffff\" font-size=\"10\" font-weight=\"700\" font-family=\"system-ui, sans-serif\">Breach: %d RPS</text>\n", bx, padT-8, report.SafetyBoundaryRate)
	}

	// Data Point Circles
	for _, pt := range points {
		cx := mapX(pt.loadRate)
		cy := mapY(pt.avgP95Ms)
		tooltip := fmt.Sprintf("Load: %d RPS | P95: %.1fms | Error: %.2f%%", pt.loadRate, pt.avgP95Ms, pt.avgError*100)
		fmt.Fprintf(&buf, "  <circle cx=\"%.1f\" cy=\"%.1f\" r=\"5\" fill=\"#4f46e5\" stroke=\"#ffffff\" stroke-width=\"2\"><title>%s</title></circle>\n", cx, cy, template.HTMLEscapeString(tooltip))
	}

	buf.WriteString("</svg>")
	return template.HTML(buf.String())
}

// templateFuncs defines helper functions available inside HTML templates.
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
		"statusBadge": func(obs capacity.Observation, report *capacity.ExperimentReport) template.HTML {
			rate := int(math.Round(obs.LoadRate))
			if rate <= 0 {
				return template.HTML(`<span class="badge badge-gray">Idle / Recovery</span>`)
			}
			if report.SafetyBoundaryRate > 0 && rate >= report.SafetyBoundaryRate {
				return template.HTML(`<span class="badge badge-rose">SLA Breach</span>`)
			}
			if report.DegradationRate > 0 && rate >= report.DegradationRate {
				return template.HTML(`<span class="badge badge-amber">Degradation Knee</span>`)
			}
			return template.HTML(`<span class="badge badge-emerald">Sustainable</span>`)
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

    <!-- Executive KPI Grid -->
    <section class="kpi-grid">
      <div class="kpi-card">
        <div class="kpi-label">Sustainable Ceiling</div>
        <div class="kpi-value val-sustainable">{{.MaximumSustainableRate}} <span style="font-size: 16px; font-weight: 600;">RPS</span></div>
        <div class="kpi-desc">Zero SLA violations</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">Degradation Knee</div>
        {{if gt .DegradationRate 0}}
        <div class="kpi-value val-degradation">{{.DegradationRate}} <span style="font-size: 16px; font-weight: 600;">RPS</span></div>
        <div class="kpi-desc">Latency slope surge &gt;50%</div>
        {{else}}
        <div class="kpi-value" style="color: var(--slate-500); font-size: 20px;">Not detected</div>
        <div class="kpi-desc">No knee transition observed</div>
        {{end}}
      </div>
      <div class="kpi-card">
        <div class="kpi-label">Safety Breach Rate</div>
        <div class="kpi-value val-safety">{{.SafetyBoundaryRate}} <span style="font-size: 16px; font-weight: 600;">RPS</span></div>
        <div class="kpi-desc">Hard SLA boundary breach</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">Recovery Time (RTO)</div>
        <div class="kpi-value val-recovery">{{.RecoveryTimeSec}}<span style="font-size: 16px; font-weight: 600;">s</span></div>
        <div class="kpi-desc">Baseline SLA restored</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-label">Peak Tested Load</div>
        <div class="kpi-value val-tested">{{.MaximumTestedRate}} <span style="font-size: 16px; font-weight: 600;">RPS</span></div>
        <div class="kpi-desc">Max load rate tested</div>
      </div>
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
        <h2 class="section-title">Throughput Degradation Curve</h2>
        <p class="section-subtitle">Latency scaling curve plotting P95 latency against progressive request load rates</p>
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
              <th>Load Rate</th>
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
              <td><strong>{{formatFloat $obs.LoadRate 0}} RPS</strong></td>
              <td>{{formatFloat $obs.P95LatencyMs 1}} ms</td>
              <td>{{formatPercent $obs.ErrorRate}}</td>
              <td>{{formatPercent $obs.Availability}}</td>
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

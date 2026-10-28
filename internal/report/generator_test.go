package report_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/report"
)

func sampleExperiment() experiment.Experiment {
	now := time.Now().UTC()
	return experiment.Experiment{
		ID:            "exp-test-123",
		TargetService: "paymentservice",
		TargetURL:     "http://paymentservice:8080/charge",
		Type:          experiment.ExperimentStep,
		State:         experiment.StateCompleted,
		Config: experiment.ExperimentConfig{
			InitialRate:         10,
			StepRate:            10,
			MaxRate:             100,
			StepDurationSeconds: 10,
			MaxP95LatencyMs:     150.0,
			MaxErrorRatePercent: 2.0,
			MinAvailabilityPct:  99.0,
			RecoveryWindowSec:   15,
		},
		CreatedAt: now.Add(-10 * time.Minute),
	}
}

func sampleCapacityResult() capacity.CapacityResult {
	return capacity.CapacityResult{
		MaximumTestedRate:      100,
		MaximumSustainableRate: 70,
		DegradationRate:        60,
		SafetyBoundaryRate:     80,
		RecoveryTimeSec:        12,
	}
}

func sampleScores() []graph.ServiceScore {
	return []graph.ServiceScore{
		{
			ServiceName: "frontend",
			Score:       0.45,
			Reasons:     []string{"Edge gateway with high fan-out"},
		},
		{
			ServiceName: "paymentservice",
			Score:       0.88,
			Reasons:     []string{"Single Point of Failure (SPOF)", "Deep downstream dependency"},
		},
		{
			ServiceName: "emailservice",
			Score:       0.20,
			Reasons:     []string{"Asynchronous leaf node"},
		},
	}
}

func sampleObservations() []capacity.Observation {
	now := time.Now().UTC()
	var obs []capacity.Observation

	// Pre-stress / Initial step (10 RPS)
	obs = append(obs, capacity.Observation{
		Timestamp:         now.Add(1 * time.Second),
		LoadRate:          10,
		P95LatencyMs:      15.0,
		ErrorRate:         0.0,
		Availability:      1.0,
		CPUUtilization:    0.20,
		MemoryUtilization: 0.35,
	})

	// Step 2 (30 RPS)
	obs = append(obs, capacity.Observation{
		Timestamp:         now.Add(2 * time.Second),
		LoadRate:          30,
		P95LatencyMs:      22.0,
		ErrorRate:         0.0,
		Availability:      1.0,
		CPUUtilization:    0.35,
		MemoryUtilization: 0.40,
	})

	// Step 3 (60 RPS - Degradation Knee)
	obs = append(obs, capacity.Observation{
		Timestamp:         now.Add(3 * time.Second),
		LoadRate:          60,
		P95LatencyMs:      55.0,
		ErrorRate:         0.005,
		Availability:      0.998,
		CPUUtilization:    0.65,
		MemoryUtilization: 0.55,
	})

	// Step 4 (80 RPS - SLA Breach)
	obs = append(obs, capacity.Observation{
		Timestamp:         now.Add(4 * time.Second),
		LoadRate:          80,
		P95LatencyMs:      180.0,
		ErrorRate:         0.035,
		Availability:      0.985,
		CPUUtilization:    0.85,
		MemoryUtilization: 0.75,
	})

	// Step 5 (100 RPS - High Stress)
	obs = append(obs, capacity.Observation{
		Timestamp:         now.Add(5 * time.Second),
		LoadRate:          100,
		P95LatencyMs:      320.0,
		ErrorRate:         0.08,
		Availability:      0.960,
		CPUUtilization:    0.92,
		MemoryUtilization: 0.88,
	})

	// Step 6 (0 RPS - Recovery)
	obs = append(obs, capacity.Observation{
		Timestamp:         now.Add(6 * time.Second),
		LoadRate:          0,
		P95LatencyMs:      18.0,
		ErrorRate:         0.0,
		Availability:      1.0,
		CPUUtilization:    0.15,
		MemoryUtilization: 0.40,
	})

	return obs
}

func TestNewGenerator(t *testing.T) {
	gen := report.NewGenerator()
	if gen == nil {
		t.Fatal("expected non-nil Generator from NewGenerator")
	}

	engine := report.NewReportEngine()
	if engine == nil {
		t.Fatal("expected non-nil ReportEngine from NewReportEngine")
	}
}

func TestNewGeneratorWithTemplate(t *testing.T) {
	t.Run("Valid custom template", func(t *testing.T) {
		customTmpl := `<html><body><h1>Report: {{.TargetService}}</h1></body></html>`
		gen, err := report.NewGeneratorWithTemplate(customTmpl)
		if err != nil {
			t.Fatalf("unexpected error creating generator with custom template: %v", err)
		}
		if gen == nil {
			t.Fatal("expected non-nil generator")
		}

		rep := &capacity.ExperimentReport{TargetService: "cartservice"}
		html, err := gen.RenderHTMLString(rep)
		if err != nil {
			t.Fatalf("unexpected render error: %v", err)
		}
		if !strings.Contains(html, "Report: cartservice") {
			t.Errorf("expected rendered HTML to contain 'Report: cartservice', got: %s", html)
		}
	})

	t.Run("Empty template string", func(t *testing.T) {
		_, err := report.NewGeneratorWithTemplate("")
		if err == nil {
			t.Error("expected error for empty template string, got nil")
		}
	})

	t.Run("Invalid syntax in template", func(t *testing.T) {
		_, err := report.NewGeneratorWithTemplate("<html>{{.UnclosedTag")
		if err == nil {
			t.Error("expected error for malformed template syntax, got nil")
		}
	})
}

func TestGenerator_Generate(t *testing.T) {
	gen := report.NewGenerator()
	exp := sampleExperiment()
	capResult := sampleCapacityResult()
	scores := sampleScores()
	obs := sampleObservations()

	t.Run("Successful report compilation", func(t *testing.T) {
		rep, err := gen.Generate(exp, capResult, scores, obs)
		if err != nil {
			t.Fatalf("unexpected error generating report: %v", err)
		}

		if rep.ExperimentID != exp.ID {
			t.Errorf("expected ExperimentID %q, got %q", exp.ID, rep.ExperimentID)
		}
		if rep.TargetService != exp.TargetService {
			t.Errorf("expected TargetService %q, got %q", exp.TargetService, rep.TargetService)
		}
		if rep.CriticalityScore != 0.88 {
			t.Errorf("expected CriticalityScore 0.88, got %f", rep.CriticalityScore)
		}
		if rep.MaximumTestedRate != 100 {
			t.Errorf("expected MaximumTestedRate 100, got %d", rep.MaximumTestedRate)
		}
		if rep.MaximumSustainableRate != 70 {
			t.Errorf("expected MaximumSustainableRate 70, got %d", rep.MaximumSustainableRate)
		}
		if rep.DegradationRate != 60 {
			t.Errorf("expected DegradationRate 60, got %d", rep.DegradationRate)
		}
		if rep.SafetyBoundaryRate != 80 {
			t.Errorf("expected SafetyBoundaryRate 80, got %d", rep.SafetyBoundaryRate)
		}
		if rep.RecoveryTimeSec != 12 {
			t.Errorf("expected RecoveryTimeSec 12, got %d", rep.RecoveryTimeSec)
		}
		if len(rep.Observations) != len(obs) {
			t.Errorf("expected %d observations, got %d", len(obs), len(rep.Observations))
		}
		if len(rep.Recommendations) == 0 {
			t.Errorf("expected recommendations to be populated, got empty slice")
		}
		if rep.GeneratedAt.IsZero() {
			t.Errorf("expected non-zero GeneratedAt timestamp")
		}
	})

	t.Run("Empty experiment ID fails", func(t *testing.T) {
		invalidExp := exp
		invalidExp.ID = ""
		_, err := gen.Generate(invalidExp, capResult, scores, obs)
		if err == nil {
			t.Error("expected error for empty experiment ID, got nil")
		}
		if !errors.Is(err, report.ErrEmptyExperimentID) {
			t.Errorf("expected ErrEmptyExperimentID, got: %v", err)
		}
	})

	t.Run("Target service missing from scores defaults to zero criticality", func(t *testing.T) {
		unknownExp := exp
		unknownExp.TargetService = "untracked-service"
		rep, err := gen.Generate(unknownExp, capResult, scores, obs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rep.CriticalityScore != 0.0 {
			t.Errorf("expected CriticalityScore 0.0 for unknown service, got %f", rep.CriticalityScore)
		}
	})

	t.Run("Empty observations handled gracefully", func(t *testing.T) {
		rep, err := gen.Generate(exp, capResult, scores, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rep.Observations == nil || len(rep.Observations) != 0 {
			t.Errorf("expected empty non-nil observations slice, got: %+v", rep.Observations)
		}
	})
}

func TestGenerator_Recommendations(t *testing.T) {
	gen := report.NewGenerator()

	t.Run("SPOF and critical score recommendation", func(t *testing.T) {
		exp := sampleExperiment()
		scores := []graph.ServiceScore{
			{
				ServiceName: exp.TargetService,
				Score:       0.95,
				Reasons:     []string{"Identified as Single Point of Failure (SPOF)"},
			},
		}
		rep, err := gen.Generate(exp, sampleCapacityResult(), scores, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		joined := strings.Join(rep.Recommendations, " ")
		if !strings.Contains(joined, "Critical component detected") {
			t.Errorf("expected critical component recommendation, got: %v", rep.Recommendations)
		}
		if !strings.Contains(joined, "Single Point of Failure (SPOF)") {
			t.Errorf("expected SPOF recommendation, got: %v", rep.Recommendations)
		}
	})

	t.Run("Degradation knee and throughput shortage recommendations", func(t *testing.T) {
		exp := sampleExperiment()
		capRes := capacity.CapacityResult{
			MaximumTestedRate:      150,
			MaximumSustainableRate: 80,
			DegradationRate:        75,
			SafetyBoundaryRate:     100,
			RecoveryTimeSec:        25,
		}
		rep, err := gen.Generate(exp, capRes, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		joined := strings.Join(rep.Recommendations, " ")
		if !strings.Contains(joined, "degradation knee detected at 75 RPS") && !strings.Contains(joined, "Degradation knee detected at 75 RPS") {
			t.Errorf("expected degradation knee recommendation, got: %v", rep.Recommendations)
		}
		if !strings.Contains(joined, "Maximum sustainable throughput (80 RPS) is below peak tested load (150 RPS)") {
			t.Errorf("expected throughput shortage recommendation, got: %v", rep.Recommendations)
		}
		// RecoveryWindowSec in exp is 15s; recovery took 25s
		if !strings.Contains(joined, "exceeded configured recovery window (15s)") {
			t.Errorf("expected recovery window exceeded recommendation, got: %v", rep.Recommendations)
		}
	})

	t.Run("Initial rate failure recommendation", func(t *testing.T) {
		exp := sampleExperiment()
		capRes := capacity.CapacityResult{
			MaximumTestedRate:      50,
			MaximumSustainableRate: 0,
		}
		rep, err := gen.Generate(exp, capRes, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		joined := strings.Join(rep.Recommendations, " ")
		if !strings.Contains(joined, "Service failed to sustain initial tested traffic") {
			t.Errorf("expected initial traffic failure recommendation, got: %v", rep.Recommendations)
		}
	})

	t.Run("High CPU, memory, and error telemetry recommendations", func(t *testing.T) {
		exp := sampleExperiment()
		exp.Config.MaxErrorRatePercent = 2.0
		exp.Config.MaxP95LatencyMs = 100.0

		highStressObs := []capacity.Observation{
			{
				LoadRate:          50,
				P95LatencyMs:      180.0, // exceeds 100.0
				ErrorRate:         0.06,  // 6% exceeds 2%
				CPUUtilization:    0.95,  // 95% exceeds 80%
				MemoryUtilization: 0.89,  // 89% exceeds 80%
			},
		}

		rep, err := gen.Generate(exp, sampleCapacityResult(), nil, highStressObs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		joined := strings.Join(rep.Recommendations, " ")
		if !strings.Contains(joined, "Peak error rate reached") {
			t.Errorf("expected peak error recommendation, got: %v", rep.Recommendations)
		}
		if !strings.Contains(joined, "Peak P95 latency reached") {
			t.Errorf("expected peak latency recommendation, got: %v", rep.Recommendations)
		}
		if !strings.Contains(joined, "High CPU utilization observed") {
			t.Errorf("expected CPU recommendation, got: %v", rep.Recommendations)
		}
		if !strings.Contains(joined, "High memory utilization observed") {
			t.Errorf("expected Memory recommendation, got: %v", rep.Recommendations)
		}
	})

	t.Run("Normal operation fallback recommendation", func(t *testing.T) {
		exp := sampleExperiment()
		exp.Config.MaxRate = 50
		capRes := capacity.CapacityResult{
			MaximumTestedRate:      50,
			MaximumSustainableRate: 50,
			DegradationRate:        0,
			SafetyBoundaryRate:     50,
			RecoveryTimeSec:        0,
		}
		normalObs := []capacity.Observation{
			{
				LoadRate:          50,
				P95LatencyMs:      20.0,
				ErrorRate:         0.0,
				CPUUtilization:    0.30,
				MemoryUtilization: 0.30,
			},
		}

		rep, err := gen.Generate(exp, capRes, nil, normalObs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(rep.Recommendations) != 1 || !strings.Contains(rep.Recommendations[0], "System operated within normal operational thresholds") {
			t.Errorf("expected normal operation fallback recommendation, got: %v", rep.Recommendations)
		}
	})
}

func TestGenerator_RenderJSON(t *testing.T) {
	gen := report.NewGenerator()

	t.Run("Nil report returns ErrNilReport", func(t *testing.T) {
		_, err := gen.RenderJSON(nil)
		if !errors.Is(err, report.ErrNilReport) {
			t.Errorf("expected ErrNilReport, got: %v", err)
		}
		_, errStr := gen.RenderJSONString(nil)
		if !errors.Is(errStr, report.ErrNilReport) {
			t.Errorf("expected ErrNilReport, got: %v", errStr)
		}
	})

	t.Run("Valid JSON serialization and round-trip unmarshaling", func(t *testing.T) {
		rep, err := gen.Generate(sampleExperiment(), sampleCapacityResult(), sampleScores(), sampleObservations())
		if err != nil {
			t.Fatalf("unexpected error generating report: %v", err)
		}

		jsonBytes, err := gen.RenderJSON(rep)
		if err != nil {
			t.Fatalf("unexpected error rendering JSON: %v", err)
		}

		jsonStr, err := gen.RenderJSONString(rep)
		if err != nil {
			t.Fatalf("unexpected error rendering JSON string: %v", err)
		}
		if string(jsonBytes) != jsonStr {
			t.Error("expected RenderJSON and RenderJSONString output to match")
		}

		var decoded capacity.ExperimentReport
		if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
			t.Fatalf("failed to unmarshal JSON report: %v", err)
		}

		if decoded.ExperimentID != rep.ExperimentID {
			t.Errorf("expected ExperimentID %q, got %q", rep.ExperimentID, decoded.ExperimentID)
		}
		if decoded.TargetService != rep.TargetService {
			t.Errorf("expected TargetService %q, got %q", rep.TargetService, decoded.TargetService)
		}
		if decoded.CriticalityScore != rep.CriticalityScore {
			t.Errorf("expected CriticalityScore %f, got %f", rep.CriticalityScore, decoded.CriticalityScore)
		}
		if decoded.MaximumSustainableRate != rep.MaximumSustainableRate {
			t.Errorf("expected MaximumSustainableRate %d, got %d", rep.MaximumSustainableRate, decoded.MaximumSustainableRate)
		}
		if len(decoded.Observations) != len(rep.Observations) {
			t.Errorf("expected %d observations, got %d", len(rep.Observations), len(decoded.Observations))
		}
	})
}

func TestGenerator_RenderHTML(t *testing.T) {
	gen := report.NewGenerator()

	t.Run("Nil report returns ErrNilReport", func(t *testing.T) {
		_, err := gen.RenderHTML(nil)
		if !errors.Is(err, report.ErrNilReport) {
			t.Errorf("expected ErrNilReport, got: %v", err)
		}
		_, errStr := gen.RenderHTMLString(nil)
		if !errors.Is(errStr, report.ErrNilReport) {
			t.Errorf("expected ErrNilReport, got: %v", errStr)
		}
	})

	t.Run("Valid standalone HTML generation and structural validation", func(t *testing.T) {
		rep, err := gen.Generate(sampleExperiment(), sampleCapacityResult(), sampleScores(), sampleObservations())
		if err != nil {
			t.Fatalf("unexpected error generating report: %v", err)
		}

		htmlBytes, err := gen.RenderHTML(rep)
		if err != nil {
			t.Fatalf("unexpected error rendering HTML: %v", err)
		}

		htmlStr, err := gen.RenderHTMLString(rep)
		if err != nil {
			t.Fatalf("unexpected error rendering HTML string: %v", err)
		}
		if string(htmlBytes) != htmlStr {
			t.Error("expected RenderHTML and RenderHTMLString to produce identical output")
		}

		// Verify key structural components of the standalone HTML report
		checks := []struct {
			name     string
			contains string
		}{
			{"HTML5 doctype", "<!DOCTYPE html>"},
			{"Standalone head and style", "<style>"},
			{"Target service name", "paymentservice"},
			{"Experiment ID", "exp-test-123"},
			{"Criticality section", "Microservice Criticality Analysis"},
			{"Criticality badge", "CRITICAL RISK"},
			{"Criticality score value", "0.88"},
			{"Sustainable Ceiling KPI", "70"},
			{"Degradation Knee KPI", "60"},
			{"Safety Breach KPI", "80"},
			{"Recovery Time KPI", "12"},
			{"Peak Tested Load KPI", "100"},
			{"Degradation Curve Section", "Throughput Degradation Curve"},
			{"Inline SVG curve", "<svg"},
			{"SVG polyline", "<polyline"},
			{"SVG threshold markers", "Sustainable: 70"},
			{"SVG knee marker", "Knee: 60 RPS"},
			{"SVG breach marker", "Breach: 80 RPS"},
			{"Recommendations Section", "Automated Engineering Recommendations"},
			{"Observations Table", "Granular Observation Telemetry"},
			{"Table row data", "RPS"},
			{"Footer", "DAMASCUS Chaos &amp; Resilience Engineering Control Plane"},
		}

		for _, tc := range checks {
			if !strings.Contains(htmlStr, tc.contains) {
				t.Errorf("HTML missing expected element %s (substring %q)", tc.name, tc.contains)
			}
		}
	})

	t.Run("HTML fallback when no active observations are present", func(t *testing.T) {
		rep := &capacity.ExperimentReport{
			ExperimentID:           "exp-empty-obs",
			TargetService:          "cartservice",
			CriticalityScore:       0.25,
			MaximumTestedRate:      0,
			MaximumSustainableRate: 0,
			Observations:           []capacity.Observation{},
			Recommendations:        []string{"No recommendations."},
			GeneratedAt:            time.Now().UTC(),
		}

		html, err := gen.RenderHTMLString(rep)
		if err != nil {
			t.Fatalf("unexpected render error: %v", err)
		}

		if !strings.Contains(html, "No active stress observation telemetry recorded to plot degradation curve") {
			t.Errorf("expected fallback SVG message when observations are empty, got HTML:\n%s", html)
		}
		if !strings.Contains(html, "LOW RISK") {
			t.Errorf("expected LOW RISK badge for score 0.25")
		}
	})
}

func TestGenerator_ConvenienceMethods(t *testing.T) {
	gen := report.NewGenerator()
	exp := sampleExperiment()
	capRes := sampleCapacityResult()
	scores := sampleScores()
	obs := sampleObservations()

	t.Run("GenerateJSON", func(t *testing.T) {
		jsonBytes, err := gen.GenerateJSON(exp, capRes, scores, obs)
		if err != nil {
			t.Fatalf("unexpected error in GenerateJSON: %v", err)
		}
		if len(jsonBytes) == 0 {
			t.Fatal("expected non-empty JSON bytes")
		}

		var decoded capacity.ExperimentReport
		if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
			t.Fatalf("failed to unmarshal generated JSON: %v", err)
		}
		if decoded.ExperimentID != exp.ID {
			t.Errorf("expected experiment ID %s, got %s", exp.ID, decoded.ExperimentID)
		}
	})

	t.Run("GenerateHTML", func(t *testing.T) {
		htmlBytes, err := gen.GenerateHTML(exp, capRes, scores, obs)
		if err != nil {
			t.Fatalf("unexpected error in GenerateHTML: %v", err)
		}
		if !strings.Contains(string(htmlBytes), "<!DOCTYPE html>") {
			t.Error("expected valid HTML document from GenerateHTML")
		}
	})

	t.Run("Generate invalid experiment returns error", func(t *testing.T) {
		invalidExp := exp
		invalidExp.ID = ""
		_, err := gen.GenerateJSON(invalidExp, capRes, scores, obs)
		if err == nil {
			t.Error("expected error from GenerateJSON on invalid experiment, got nil")
		}
		_, errHTML := gen.GenerateHTML(invalidExp, capRes, scores, obs)
		if errHTML == nil {
			t.Error("expected error from GenerateHTML on invalid experiment, got nil")
		}
	})
}

func TestGenerator_Concurrency(t *testing.T) {
	gen := report.NewGenerator()
	exp := sampleExperiment()
	capRes := sampleCapacityResult()
	scores := sampleScores()
	obs := sampleObservations()

	const workers = 25
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			rep, err := gen.Generate(exp, capRes, scores, obs)
			if err != nil {
				t.Errorf("concurrent Generate error: %v", err)
				return
			}
			jsonBytes, err := gen.RenderJSON(rep)
			if err != nil || len(jsonBytes) == 0 {
				t.Errorf("concurrent RenderJSON error: %v", err)
				return
			}
			htmlBytes, err := gen.RenderHTML(rep)
			if err != nil || len(htmlBytes) == 0 {
				t.Errorf("concurrent RenderHTML error: %v", err)
				return
			}
		}()
	}

	wg.Wait()
}

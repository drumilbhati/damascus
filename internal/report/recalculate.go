package report

import (
	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"strings"
)

// Recalculate returns a new report using saved observations and current analysis
// semantics. It neither mutates the stored object nor writes historical records.
func (g *Generator) Recalculate(exp experiment.Experiment, saved *capacity.ExperimentReport) (*capacity.ExperimentReport, error) {
	score := graph.ServiceScore{ServiceName: exp.TargetService, Score: saved.CriticalityScore}
	for _, recommendation := range saved.Recommendations {
		if strings.Contains(recommendation, "Single Point of Failure") {
			score.Reasons = append(score.Reasons, recommendation)
		}
	}
	updated, err := g.Generate(exp, capacity.CapacityResult{}, []graph.ServiceScore{score}, saved.Observations)
	if err != nil {
		return nil, err
	}
	updated.GeneratedAt = saved.GeneratedAt
	return updated, nil
}

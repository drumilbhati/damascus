package interfaces

import (
	"context"

	"damascus/internal/capacity"
	"damascus/internal/events"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

// GraphAnalyzer extracts traces from Jaeger / OTel and ranks service criticality.
type GraphAnalyzer interface {
	BuildGraph(ctx context.Context, lookbackDuration string) (*graph.DependencyGraph, error)
	ScoreCriticality(g *graph.DependencyGraph) []graph.ServiceScore
}

// StressEngine generates and regulates synthetic load against the target service.
type StressEngine interface {
	Start(ctx context.Context, plan stress.LoadPlan) error
	Stop()
}

// Watcher monitors real-time RED metrics from Prometheus.
type Watcher interface {
	Start(ctx context.Context, experimentID string, targetService string) error
	Stop()
}

// SafetyController evaluates real-time metric snapshots against SLA thresholds.
type SafetyController interface {
	Evaluate(ctx context.Context, snapshot watcher.MetricSnapshot, policy safety.SafetyPolicy) safety.SafetyDecision
}

// CapacityAnalyzer models the throughput knee and maximum sustainable capacity.
type CapacityAnalyzer interface {
	Analyze(observations []capacity.Observation, policy safety.SafetyPolicy) capacity.CapacityResult
}

// ReportEngine compiles and exports the final experiment report.
type ReportEngine interface {
	Generate(exp experiment.Experiment,
		capResult capacity.CapacityResult,
		scores []graph.ServiceScore,
		observations []capacity.Observation,
	) (capacity.ExperimentReport, error)
}

// ExperimentRepository persists experiment lifecycle data to PostgreSQL.
type ExperimentRepository interface {
	Create(ctx context.Context, exp *experiment.Experiment) error
	GetByID(ctx context.Context, id string) (*experiment.Experiment, error)
	List(ctx context.Context) ([]experiment.Experiment, error)
	UpdateState(ctx context.Context, id string, state experiment.ExperimentState, stopReason string) error
	SaveObservation(ctx context.Context, expID string, obs capacity.Observation) error
	GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error)
	SaveReport(ctx context.Context, report capacity.ExperimentReport) error
	GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error)
}

// EventProducer publishes domain events to the Kafka message broker.
type EventProducer interface {
	Publish(ctx context.Context, event events.DomainEvent) error
	Close() error
}

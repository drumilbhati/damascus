export type ExperimentState =
  | 'CREATED'
  | 'PLANNED'
  | 'RUNNING'
  | 'STOPPING'
  | 'ANALYZING'
  | 'REPORTING'
  | 'COMPLETED'
  | 'ABORTED';

export type ExperimentType = 'ramp' | 'step';

export interface ExperimentConfig {
  initial_rate: number;
  step_rate: number;
  max_rate: number;
  step_duration_seconds: number;
  max_p95_latency_ms: number;
  max_error_rate_percent: number;
  min_availability_pct: number;
  recovery_window_sec: number;
}

export interface Experiment {
  id: string;
  target_service: string;
  target_url: string;
  type: ExperimentType;
  state: ExperimentState;
  config: ExperimentConfig;
  stop_reason?: string;
  started_at?: string;
  ended_at?: string;
  created_at: string;
}

export interface MetricSnapshot {
  experiment_id: string;
  target_service: string;
  timestamp: string;
  request_rate: number;
  p50_latency_ms: number;
  p95_latency_ms: number;
  p99_latency_ms: number;
  error_rate: number;
  availability: number;
  cpu_utilization: number;
  memory_utilization: number;
}

export interface DependencyNode {
  name: string;
  metadata?: Record<string, string>;
}

export interface DependencyEdge {
  from: string;
  to: string;
  call_count: number;
  frequency: number;
}

export interface DependencyGraph {
  nodes: Record<string, DependencyNode>;
  edges: DependencyEdge[];
}

export interface ServiceScore {
  service_name: string;
  score: number;
  reasons: string[];
}

export interface DependenciesResponse {
  graph: DependencyGraph;
  scores: ServiceScore[];
}

export interface HealthStatus {
  status: 'UP' | 'DOWN';
  target?: string;
  jaeger?: string;
  prometheus?: string;
  errors?: string;
  [key: string]: string | undefined;
}

export interface CapacityObservation {
  timestamp: string;
  load_rate: number;
  p95_latency_ms: number;
  error_rate: number;
  availability: number;
  cpu_utilization?: number;
  memory_utilization?: number;
}

export interface ExperimentReport {
  experiment_id: string;
  target_service: string;
  criticality_score: number;
  maximum_tested_rate: number;
  maximum_sustainable_rate: number;
  degradation_rate: number;
  safety_boundary_rate: number;
  recovery_time_sec: number;
  observations: CapacityObservation[];
  recommendations: string[];
  generated_at: string;
}

export interface CreateExperimentPayload {
  target_service: string;
  target_url: string;
  type: ExperimentType;
  config: ExperimentConfig;
}

export interface StartExperimentPayload {
  plan?: {
    target_url?: string;
    method?: string;
    initial_rate?: number;
    step_rate?: number;
    max_rate?: number;
    step_duration_seconds?: number;
    payload?: string;
  };
}

export interface StopExperimentPayload {
  reason?: string;
}

export interface ServicesResponse {
  services: string[];
}

import { apiClient } from './client';
import type {
  Experiment,
  MetricSnapshot,
  DependenciesResponse,
  HealthStatus,
  ExperimentReport,
  CreateExperimentPayload,
  StartExperimentPayload,
  StopExperimentPayload,
} from '../types';

export const fetchHealth = async (): Promise<HealthStatus> => {
  // A degraded stack still returns a useful health document with HTTP 503.
  const res = await apiClient.get<HealthStatus>('/health', {
    // The backend probes three components sequentially, up to five seconds each.
    timeout: 20000,
    validateStatus: (status) => status === 200 || status === 503,
  });
  return res.data;
};

export const fetchDependencies = async (lookbackSeconds = 3600): Promise<DependenciesResponse> => {
  const res = await apiClient.get<DependenciesResponse>(`/dependencies?lookback=${lookbackSeconds}`, { timeout: 60000 });
  return res.data;
};

export const fetchExperiments = async (): Promise<Experiment[]> => {
  const res = await apiClient.get<Experiment[]>('/experiments');
  return res.data;
};

export const fetchExperiment = async (id: string): Promise<Experiment> => {
  const res = await apiClient.get<Experiment>(`/experiments/${id}`);
  return res.data;
};

export const fetchExperimentStatus = async (id: string): Promise<MetricSnapshot> => {
  const res = await apiClient.get<MetricSnapshot>(`/experiments/${id}/status`);
  return res.data;
};

export const fetchExperimentReport = async (id: string): Promise<ExperimentReport> => {
  const res = await apiClient.get<ExperimentReport>(`/experiments/${id}/report`);
  return res.data;
};

export const createExperiment = async (payload: CreateExperimentPayload): Promise<Experiment> => {
  const res = await apiClient.post<Experiment>('/experiments', payload);
  return res.data;
};

export const startExperiment = async (id: string, payload?: StartExperimentPayload): Promise<{ status: string }> => {
  const res = await apiClient.post<{ status: string }>(`/experiments/${id}/start`, payload || {}, { timeout: 60000 });
  return res.data;
};

export const stopExperiment = async (id: string, payload?: StopExperimentPayload): Promise<{ status: string }> => {
  const res = await apiClient.post<{ status: string }>(`/experiments/${id}/stop`, payload || {});
  return res.data;
};

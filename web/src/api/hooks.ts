import useSWR from 'swr';
import { defaultFetcher } from './client';
import type {
  Experiment,
  MetricSnapshot,
  DependenciesResponse,
  HealthStatus,
  ExperimentReport,
} from '../types';

export const useHealth = (refreshInterval = 15000) => {
  return useSWR<HealthStatus>('/health', defaultFetcher, {
    refreshInterval,
    revalidateOnFocus: true,
  });
};

export const useDependencies = (lookbackSeconds = 3600) => {
  return useSWR<DependenciesResponse>(
    `/dependencies?lookback=${lookbackSeconds}`,
    defaultFetcher,
    {
      revalidateOnFocus: false,
    }
  );
};

export const useExperiments = (refreshInterval = 5000) => {
  return useSWR<Experiment[]>('/experiments', defaultFetcher, {
    refreshInterval,
  });
};

export const useExperiment = (id: string | null | undefined) => {
  return useSWR<Experiment>(
    id ? `/experiments/${id}` : null,
    defaultFetcher
  );
};

export const useExperimentStatus = (
  id: string | null | undefined,
  isActive = false
) => {
  return useSWR<MetricSnapshot>(
    id ? `/experiments/${id}/status` : null,
    defaultFetcher,
    {
      refreshInterval: isActive ? 1000 : 0,
      dedupingInterval: 500,
    }
  );
};

export const useExperimentReport = (id: string | null | undefined) => {
  return useSWR<ExperimentReport>(
    id ? `/experiments/${id}/report` : null,
    defaultFetcher,
    {
      revalidateOnFocus: false,
    }
  );
};

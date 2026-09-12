import { http } from './http';
import type { MetricsList, NodeMetrics, PodMetrics } from './types';

export function getNodeMetrics(cluster: string): Promise<MetricsList<NodeMetrics>> {
  return http.get<MetricsList<NodeMetrics>>(`/clusters/${cluster}/metrics/nodes`);
}

export function getPodMetrics(
  cluster: string,
  namespace?: string,
): Promise<MetricsList<PodMetrics>> {
  return http.get<MetricsList<PodMetrics>>(`/clusters/${cluster}/metrics/pods`, { namespace });
}

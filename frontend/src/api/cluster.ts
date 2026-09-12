import { http } from './http';
import type {
  ClusterInfo,
  ClusterStatusData,
  RegisterClusterRequest,
  RotateKubeconfigRequest,
} from './types';

/** 后端返回 {items,total} */
export interface ClusterListData {
  items: ClusterInfo[];
  total: number;
}

export function listClusters(): Promise<ClusterListData> {
  return http.get<ClusterListData>('/clusters');
}

export function getClusterStatus(cluster: string): Promise<ClusterStatusData> {
  return http.get<ClusterStatusData>(`/clusters/${cluster}/status`);
}

export function registerCluster(req: RegisterClusterRequest): Promise<ClusterInfo> {
  return http.post<ClusterInfo>('/clusters', req);
}

export function deleteCluster(cluster: string): Promise<void> {
  return http.delete<void>(`/clusters/${cluster}`);
}

export function rotateKubeconfig(
  cluster: string,
  req: RotateKubeconfigRequest,
): Promise<ClusterInfo> {
  return http.put<ClusterInfo>(`/clusters/${cluster}/kubeconfig`, req);
}

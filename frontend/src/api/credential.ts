import { http } from './http';
import type { IssuedKubeconfig, IssuedKubeconfigCreated } from './types';

export interface IssueKubeconfigRequest {
  /** Go duration 字符串,如 "24h";缺省由后端取 24h,上限 7d */
  ttl?: string;
  description?: string;
}

/** 签发客户端 kubeconfig;token/downloadUrl 只返回一次 */
export function issueKubeconfig(
  cluster: string,
  req: IssueKubeconfigRequest = {},
): Promise<IssuedKubeconfigCreated> {
  return http.post<IssuedKubeconfigCreated>(`/clusters/${cluster}/kubeconfigs`, req);
}

/** 当前用户凭证列表(脱敏) */
export function listMyKubeconfigs(): Promise<{ items: IssuedKubeconfig[]; total: number }> {
  return http.get<{ items: IssuedKubeconfig[]; total: number }>('/kubeconfigs');
}

/** 撤销凭证(属主或 admin) */
export function revokeKubeconfig(id: number): Promise<void> {
  return http.delete<void>(`/kubeconfigs/${id}`);
}

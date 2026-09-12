import { http } from './http';
import type {
  EventItem,
  K8sObject,
  PageResult,
  ResourceItem,
  ResourceListQuery,
  ScaleRequest,
  SecretPlainData,
  YamlData,
  LogsData,
} from './types';
import type { CrdNames } from './types';

export type ResourceKind =
  | 'deployments'
  | 'statefulsets'
  | 'daemonsets'
  | 'pods'
  | 'services'
  | 'ingresses'
  | 'configmaps'
  | 'secrets'
  | 'pvcs'
  | 'pvs'
  | 'nodes'
  | 'namespaces'
  | 'crds';

/** CRD 动态资源:resource 为 crd plural 名,经 discovery 解析 GVR,同构走通用端点 */
export type DynamicKind = string & {};

/** 集群级资源(不接 namespace 参数) */
export const CLUSTER_SCOPED: readonly ResourceKind[] = ['nodes', 'namespaces', 'crds', 'pvs'];

export function isClusterScoped(kind: ResourceKind): boolean {
  return CLUSTER_SCOPED.includes(kind);
}

/** 通用资源列表:内存分页 PageResult,items 为 model.ResourceItem */
export function listResources(
  cluster: string,
  kind: ResourceKind | DynamicKind,
  query: ResourceListQuery,
): Promise<PageResult<ResourceItem>> {
  return http.get<PageResult<ResourceItem>>(`/clusters/${cluster}/${kind}`, {
    namespace: query.namespace,
    labelSelector: query.labelSelector,
    fieldSelector: query.fieldSelector,
    keyword: query.keyword,
    sortBy: query.sortBy,
    order: query.order,
    page: query.page,
    size: query.size,
  });
}

/** 详情返回 K8s 原生 unstructured 对象;secrets 默认脱敏,reveal=true 需 operator+ */
export function getResource(
  cluster: string,
  kind: ResourceKind | DynamicKind,
  name: string,
  namespace?: string,
  reveal = false,
): Promise<K8sObject> {
  return http.get<K8sObject>(`/clusters/${cluster}/${kind}/${name}`, {
    namespace,
    reveal: reveal || undefined,
  });
}

/** Secret 明文:?reveal=true&decode=true(operator+,写审计) */
export function getSecretData(
  cluster: string,
  name: string,
  namespace?: string,
): Promise<SecretPlainData> {
  return http.get<SecretPlainData>(`/clusters/${cluster}/secrets/${name}`, {
    namespace,
    reveal: 'true',
    decode: 'true',
  });
}

/** 创建资源:body 为解析后的 K8s unstructured JSON 对象(前端完成 YAML→JSON) */
export function createResource(
  cluster: string,
  kind: ResourceKind | DynamicKind,
  body: K8sObject,
  namespace?: string,
): Promise<K8sObject> {
  return http.post<K8sObject>(`/clusters/${cluster}/${kind}`, body, { namespace });
}

export function deleteResource(
  cluster: string,
  kind: ResourceKind | DynamicKind,
  name: string,
  namespace?: string,
): Promise<void> {
  return http.delete<void>(`/clusters/${cluster}/${kind}/${name}`, { namespace });
}

export function getResourceYaml(
  cluster: string,
  kind: ResourceKind | DynamicKind,
  name: string,
  namespace?: string,
): Promise<YamlData> {
  return http.get<YamlData>(`/clusters/${cluster}/${kind}/${name}/yaml`, { namespace });
}

/** 保存 YAML;resourceVersion 冲突时后端返回 code 40406(HTTP 409) */
export function updateResourceYaml(
  cluster: string,
  kind: ResourceKind | DynamicKind,
  name: string,
  yaml: string,
  namespace?: string,
): Promise<K8sObject> {
  return http.put<K8sObject>(`/clusters/${cluster}/${kind}/${name}/yaml`, { yaml }, { namespace });
}

export function restartPod(cluster: string, name: string, namespace?: string): Promise<void> {
  return http.post<void>(`/clusters/${cluster}/pods/${name}/restart`, {}, { namespace });
}

export function restartDeployment(
  cluster: string,
  name: string,
  namespace?: string,
): Promise<K8sObject> {
  return http.post<K8sObject>(
    `/clusters/${cluster}/deployments/${name}/restart`,
    {},
    { namespace },
  );
}

export function scaleDeployment(
  cluster: string,
  name: string,
  replicas: number,
  namespace?: string,
): Promise<K8sObject> {
  const req: ScaleRequest = { replicas };
  return http.put<K8sObject>(
    `/clusters/${cluster}/deployments/${name}/scale`,
    req,
    { namespace },
  );
}

export interface PodLogsQuery {
  namespace?: string;
  container?: string;
  tailLines?: number;
  sinceSeconds?: number;
  previous?: boolean;
}

export function getPodLogs(
  cluster: string,
  name: string,
  opts: PodLogsQuery,
): Promise<LogsData> {
  return http.get<LogsData>(`/clusters/${cluster}/pods/${name}/logs`, {
    namespace: opts.namespace,
    container: opts.container,
    tailLines: opts.tailLines,
    sinceSeconds: opts.sinceSeconds,
    previous: opts.previous,
  });
}

/** 从 CRD 详情对象解析 plural/kind/作用域(供 CRD 动态列表跳转) */
export function crdNamesOf(obj: K8sObject): CrdNames {
  const spec = (obj['spec'] ?? {}) as {
    names?: { plural?: string; kind?: string };
    scope?: string;
  };
  return {
    plural: spec.names?.plural ?? '',
    kind: spec.names?.kind ?? '',
    namespaced: spec.scope !== 'Cluster',
  };
}

/** Deployment/StatefulSet/DaemonSet 的 ready/want 副本摘要(spec/status 数字字段) */
function replicaStatus(o: K8sObject, readyPath: string[], wantPath: string[]): string {
  const ready = numAt(o, readyPath);
  const want = numAt(o, wantPath);
  return `${ready}/${want}`;
}

function numAt(o: K8sObject, path: string[]): number {
  let cur: unknown = o;
  for (const k of path) {
    if (cur === null || typeof cur !== 'object') return 0;
    cur = (cur as Record<string, unknown>)[k];
  }
  return typeof cur === 'number' ? cur : 0;
}

function statusFromObject(o: K8sObject): string {
  const kind = typeof o['kind'] === 'string' ? o['kind'] : '';
  switch (kind) {
    case 'Pod':
      return strAt(o, ['status', 'phase']);
    case 'Deployment':
    case 'StatefulSet':
      return replicaStatus(o, ['status', 'readyReplicas'], ['spec', 'replicas']);
    case 'DaemonSet':
      return replicaStatus(o, ['status', 'numberReady'], ['status', 'desiredNumberScheduled']);
    case 'PersistentVolumeClaim':
    case 'PersistentVolume':
      return strAt(o, ['status', 'phase']);
    case 'Namespace':
    case 'Service':
    case 'ConfigMap':
    case 'Secret':
    case 'Ingress':
      return '—';
    default:
      return '';
  }
}

function strAt(o: K8sObject, path: string[]): string {
  let cur: unknown = o;
  for (const k of path) {
    if (cur === null || typeof cur !== 'object') return '';
    cur = (cur as Record<string, unknown>)[k];
  }
  return typeof cur === 'string' ? cur : '';
}

/**
 * WS event.object 为 K8s 原生 unstructured 对象(与详情端点同构),
 * 转换为与 REST 列表一致的 ResourceItem 摘要(与后端 toResourceItem 对齐)。
 */
export function resourceItemOf(obj: K8sObject): ResourceItem {
  const meta = (obj['metadata'] ?? {}) as {
    name?: string;
    namespace?: string;
    creationTimestamp?: string;
    labels?: Record<string, string>;
  };
  return {
    kind: typeof obj['kind'] === 'string' ? obj['kind'] : undefined,
    name: meta.name ?? '',
    namespace: meta.namespace ?? '',
    status: statusFromObject(obj),
    createdAt: meta.creationTimestamp ?? '',
    labels: meta.labels,
  };
}

/** WS event.object(corev1.Event unstructured)→ 事件列表 EventItem 摘要 */
export function eventItemOf(obj: K8sObject): EventItem {
  const meta = (obj['metadata'] ?? {}) as {
    name?: string;
    namespace?: string;
  };
  const involved = (obj['involvedObject'] ?? {}) as { kind?: string; name?: string };
  const source = (obj['source'] ?? {}) as { component?: string };
  return {
    name: meta.name ?? '',
    namespace: meta.namespace ?? '',
    type: typeof obj['type'] === 'string' ? obj['type'] : undefined,
    reason: typeof obj['reason'] === 'string' ? obj['reason'] : undefined,
    message: typeof obj['message'] === 'string' ? obj['message'] : undefined,
    source: typeof source.component === 'string' ? source.component : undefined,
    count: typeof obj['count'] === 'number' ? obj['count'] : undefined,
    involvedKind: involved.kind,
    involvedName: involved.name,
    firstSeenAt:
      typeof obj['firstTimestamp'] === 'string' ? obj['firstTimestamp'] : undefined,
    lastTimestamp:
      typeof obj['lastTimestamp'] === 'string'
        ? obj['lastTimestamp']
        : typeof obj['eventTime'] === 'string'
          ? obj['eventTime']
          : undefined,
  };
}

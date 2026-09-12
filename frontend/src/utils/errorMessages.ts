import { ApiError } from '@/api/http';

/**
 * ApiError.code → 中文文案集中映射(与 backend/internal/pkg/errcode/codes.go 对齐);
 * 未收录的 code 透传后端 message。
 */
const CODE_MESSAGES: Record<number, string> = {
  40001: '请求参数无效',
  40100: '用户名或密码错误',
  40101: '接入令牌无效、已过期或已吊销',
  40102: '凭证无效或已过期',
  40300: '没有执行该操作的权限',
  40400: '资源不存在',
  40401: '集群不存在或未注册',
  40402: 'kubeconfig 格式非法',
  40403: 'kubeconfig 含多个 context,请显式指定',
  40404: '集群不可达,请检查网络与 API Server',
  40405: '集群名称已存在',
  40406: '资源已被修改或已存在(resourceVersion 冲突),请刷新后重试',
  40407: '下载链接无效或已过期',
  40410: '用户不存在',
  40411: '用户已被禁用',
  40412: '用户名已存在',
  40413: '角色名已存在',
  40414: '角色不存在',
  40415: '用户组名已存在',
  40416: '用户组不存在',
  40417: '授权不存在或重复提交',
  50000: '服务内部错误,请稍后重试',
  50100: 'Kubernetes 操作失败',
  50200: 'Agent 隧道不可用',
};

/**
 * 权限点中英对照表(与 src/api/rbac.ts PERMISSION_POINTS、后端 rbac 定义对齐)。
 * 403 文案据此生成,新增权限点时同步维护。
 */
const PERM_LABELS: Record<string, string> = {
  'clusters:read': '集群查看',
  'clusters:write': '集群管理',
  'resources:read': '资源查看',
  'resources:write': '资源写入',
  'secrets:read': 'Secret 明文读取',
  'secrets:read-masked': 'Secret 脱敏读取',
  'logs:read': '日志查看',
  'terminal:use': '终端使用',
  'events:read': '事件查看',
  'metrics:read': '监控指标查看',
  'credentials:issue': '凭证签发',
  'k8s-proxy:use': 'K8s 代理使用',
  'users:manage': '用户管理',
  'enroll-tokens:manage': '接入令牌管理',
  'audit:read': '审计日志查看',
};

const CONTACT_ADMIN = '请联系管理员在「权限管理」中授权';

/** "insufficient privilege: terminal:use" → 「权限不足:需要 终端使用(terminal:use)权限,…」 */
function humanizeForbidden(message: string): string {
  const perm = message.replace(/^insufficient privilege:\s*/, '').trim();
  if (perm !== '' && perm !== message) {
    const label = PERM_LABELS[perm];
    if (label !== undefined) {
      return `权限不足:需要 ${label}(${perm})权限,${CONTACT_ADMIN}`;
    }
    return `权限不足:需要 ${perm} 权限,${CONTACT_ADMIN}`;
  }
  if (message.includes('admin privilege')) {
    return `权限不足:该操作需要管理员(admin)权限,${CONTACT_ADMIN}`;
  }
  return `权限不足:${message},${CONTACT_ADMIN}`;
}

/** 统一把未知错误转换为用户可读文案(403 带具体权限点) */
export function humanizeError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 40300) return humanizeForbidden(e.message);
    return CODE_MESSAGES[e.code] ?? e.message;
  }
  if (e instanceof Error) {
    return e.message;
  }
  return String(e);
}

/** @deprecated 兼容旧名,等价 humanizeError */
export const errorMessage = humanizeError;

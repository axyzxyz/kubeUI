// 与 backend/internal/model、internal/pkg/pagination 一一对应的对外 DTO 类型
// (契约基准:backend/internal/api/handler/router.go 实际路由 + model json tag)

/** 统一响应体,见 04-coding-standards.md §4.2 */
export interface ApiResponse<T> {
  code: number;
  message: string;
  data: T;
}

/** 标准分页结果(内存分页,page/size,无 continueToken) */
export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  size: number;
}

export interface PageQuery {
  page?: number;
  size?: number;
  sortBy?: string;
  order?: 'asc' | 'desc';
}

export interface ResourceListQuery extends PageQuery {
  namespace?: string;
  labelSelector?: string;
  fieldSelector?: string;
  /** 后端 listResourceQuery 暂不支持 keyword,预留(见契约缺口清单) */
  keyword?: string;
}

// ---------- 认证与用户 ----------

export interface LoginRequest {
  username: string;
  password: string;
}

export interface UserInfo {
  name: string;
  roles: string[];
}

/** backend/internal/service/auth.go TokenPair */
export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
}

export interface RefreshRequest {
  refreshToken: string;
}

/** backend/internal/model.User */
export interface UserItem {
  id: number;
  username: string;
  role: string;
  disabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface CreateUserRequest {
  username: string;
  password: string;
  role: string;
}

export interface SetUserStatusRequest {
  disabled: boolean;
}

export interface ChangeMyPasswordRequest {
  oldPassword: string;
  newPassword: string;
}

// ---------- 集群 ----------

export type ClusterAccessMode = 'direct' | 'agent';

export type ClusterStatus = 'ready' | 'degraded' | 'reconnecting' | 'offline';

/** backend/internal/model.ClusterInfo */
export interface ClusterInfo {
  name: string;
  status: ClusterStatus;
  version?: string;
  accessMode: ClusterAccessMode;
  nodeCount?: number;
  description?: string;
  createdAt: string;
  updatedAt: string;
}

/** backend/internal/model.ClusterStatus */
export interface ClusterStatusData {
  name: string;
  status: ClusterStatus;
  version?: string;
  lastTransitionTime: string;
  message?: string;
}

export interface RegisterClusterRequest {
  name: string;
  kubeconfig: string;
  contextName?: string;
  description?: string;
  /** 接入方式:direct(默认,平台直连) | agent(反连,注册后需部署 Agent) */
  accessMode?: 'direct' | 'agent';
}

export interface RotateKubeconfigRequest {
  kubeconfig: string;
  contextName?: string;
}

// ---------- 资源通用 DTO ----------

/** backend/internal/model.ResourceItem:全部资源列表/WS event object 的通用摘要 */
export interface ResourceItem {
  kind?: string;
  name: string;
  namespace?: string;
  status?: string;
  createdAt?: string;
  labels?: Record<string, string>;
}

/** 详情端点返回 K8s 原生 unstructured 对象(JSON) */
export type K8sObject = Record<string, unknown>;

/** Secret 明文(decode=true)响应 */
export interface SecretPlainData {
  name: string;
  namespace: string;
  data: Record<string, string>;
}

// ---------- 事件 / 审计 ----------

/** backend/internal/model.EventItem */
export interface EventItem {
  name: string;
  namespace?: string;
  /** Normal | Warning */
  type?: string;
  reason?: string;
  message?: string;
  source?: string;
  count?: number;
  involvedKind?: string;
  involvedName?: string;
  firstSeenAt?: string;
  lastTimestamp?: string;
}

export interface EventListQuery extends PageQuery {
  namespace?: string;
  fieldSelector?: string;
}

/** backend/internal/model.AuditLog */
export interface AuditLog {
  id: number;
  requestId: string;
  userId: number;
  username: string;
  action: string;
  resource: string;
  cluster?: string;
  namespace?: string;
  name?: string;
  sourceIp: string;
  userAgent: string;
  result: 'allow' | 'deny';
  createdAt: string;
}

export interface AuditLogQuery extends PageQuery {
  username?: string;
  cluster?: string;
  action?: string;
}

// ---------- 指标(backend/internal/model) ----------

export interface NodeMetrics {
  name: string;
  /** 数量字符串,如 "750m"/"16Gi" */
  cpu: string;
  memory: string;
  /** 百分比字符串,如 "12.5%" */
  cpuPct?: string;
  memPct?: string;
}

export interface ContainerStat {
  name: string;
  cpu: string;
  memory: string;
}

export interface PodMetrics {
  namespace: string;
  name: string;
  containers: ContainerStat[];
}

export interface MetricsList<T> {
  items: T[];
}

// ---------- 资源操作 ----------

export interface YamlData {
  yaml: string;
}

/** backend/internal/model.PodLogs */
export interface LogsData {
  logs: string;
  container?: string;
}

export interface ScaleRequest {
  replicas: number;
}

// ---------- 集成补齐的 DTO(契约见 docs/api/rest.md) ----------

/** backend/internal/model.MePermissions */
export interface UserPermissions {
  role: string;
  permissions: string[];
}

/** backend/internal/model.ResetPasswordResult(admin 重置密码,一次性) */
export interface ResetPasswordResult {
  username: string;
  password: string;
}

/** backend/internal/model.IssuedKubeconfigView(脱敏) */
export interface IssuedKubeconfig {
  id: number;
  userId: number;
  cluster: string;
  description?: string;
  expiresAt: string;
  revoked: boolean;
  createdAt: string;
}

/** backend/internal/model.IssuedKubeconfigCreated(token/downloadUrl 只返回一次) */
export interface IssuedKubeconfigCreated extends IssuedKubeconfig {
  token: string;
  downloadUrl: string;
  linkExpiresAt: string;
}

/** backend/internal/model.EnrollTokenView(脱敏) */
export interface EnrollToken {
  id: number;
  cluster: string;
  expiresAt: string;
  /** 长期 token(永不过期),展示优先于 expiresAt */
  noExpiry: boolean;
  revoked: boolean;
  createdAt: string;
}

/** backend/internal/model.EnrollTokenCreated(token 只返回一次) */
export interface EnrollTokenCreated extends EnrollToken {
  token: string;
}

/** backend/internal/model.AgentManifest */
export interface AgentManifest {
  yaml: string;
  /** 平台对外基础地址,供二进制/Docker 启动命令拼接 */
  serverUrl: string;
}

/** CRD 列表项(经通用 ResourceItem,附带 spec.names 解析结果) */
export interface CrdNames {
  plural: string;
  kind: string;
  namespaced: boolean;
}

// ---------- 平台 RBAC(角色 / 用户组 / 角色组 / 授权)----------
// 契约:中心授权表模型(grants 为唯一关联事实源,后端并行开发中,字段以后端实际实现为准)

/** 权限点标识,如 "clusters:read";全量枚举见 src/api/rbac.ts PERMISSION_POINTS */
export type PermissionPoint = string;

/** backend/internal/model.Role(TODO(backend):结构以后端为准) */
export interface RoleItem {
  id: number;
  name: string;
  description: string;
  /** 内置角色(admin/operator/viewer)只读,DELETE/PUT 返回 403 */
  builtin: boolean;
  permissions: PermissionPoint[];
}

export interface SaveRoleRequest {
  name: string;
  description: string;
  permissions: PermissionPoint[];
}

/** backend/internal/model.UserGroup(TODO(backend):结构以后端为准) */
export interface UserGroupItem {
  id: number;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
}

export interface SaveUserGroupRequest {
  name: string;
  description: string;
}

export interface AddGroupMemberRequest {
  userId: number;
}

/** 绑定范围:cluster/namespace 为 "*" 表示全部 */
export interface Scope {
  cluster: string;
  namespace: string;
}

/** 授权主体类型:平台用户 / 用户组 */
export type GrantSubjectType = 'user' | 'group';

/** 授权对象类型:单个角色 / 角色组 */
export type GrantObjectType = 'role' | 'role_group';

/** backend/internal/model.RoleGroup(TODO(backend):结构以后端为准) */
export interface RoleGroupItem {
  id: number;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
}

export interface SaveRoleGroupRequest {
  name: string;
  description: string;
}

export interface AddRoleGroupRoleRequest {
  roleId: number;
}

/** backend/internal/model.Grant(中心授权表,TODO(backend):结构以后端为准) */
export interface GrantItem {
  id: number;
  subjectType: GrantSubjectType;
  subjectId: number;
  subjectName: string;
  objectType: GrantObjectType;
  objectId: number;
  objectName: string;
  scopes: Scope[];
  createdAt: string;
}

export interface CreateGrantRequest {
  subjectType: GrantSubjectType;
  subjectId: number;
  objectType: GrantObjectType;
  objectId: number;
  scopes: Scope[];
}

/** GET /grants 过滤参数:全部可选、可组合(subjectId 需与 subjectType 搭配) */
export interface GrantListQuery {
  subjectType?: GrantSubjectType;
  subjectId?: number;
  objectType?: GrantObjectType;
  objectId?: number;
}

/** GET /users/:username/effective-permissions 响应(TODO(backend):结构以后端为准) */
export interface EffectivePermissions {
  permissions: PermissionPoint[];
}

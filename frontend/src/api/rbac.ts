// 平台 RBAC:角色 / 用户组 / 角色组 / 授权(grants,唯一关联事实源)/ 有效权限
// 契约:中心授权表模型(后端并行开发中,字段如有出入以后端为准;role-bindings 端点已删除)
import { http } from './http';
import type {
  AddGroupMemberRequest,
  AddRoleGroupRoleRequest,
  CreateGrantRequest,
  EffectivePermissions,
  GrantItem,
  GrantListQuery,
  PermissionPoint,
  RoleGroupItem,
  RoleItem,
  SaveRoleGroupRequest,
  SaveRoleRequest,
  SaveUserGroupRequest,
  UserGroupItem,
  UserItem,
} from './types';

/** 权限点全量枚举(与后端 rbac 定义一致,TODO(backend):以后端枚举为准) */
export const PERMISSION_POINTS: readonly PermissionPoint[] = [
  'clusters:read',
  'clusters:write',
  'resources:read',
  'resources:write',
  'secrets:read',
  'secrets:read-masked',
  'logs:read',
  'terminal:use',
  'events:read',
  'metrics:read',
  'credentials:issue',
  'k8s-proxy:use',
  'users:manage',
  'enroll-tokens:manage',
  'audit:read',
];

// ---------- 角色 ----------

/** TODO(backend):契约约定返回裸数组;若后端实现为 PageResult 需同步调整 */
export function listRoles(): Promise<RoleItem[]> {
  return http.get<RoleItem[]>('/roles');
}

export function createRole(req: SaveRoleRequest): Promise<RoleItem> {
  return http.post<RoleItem>('/roles', req);
}

export function updateRole(id: number, req: SaveRoleRequest): Promise<RoleItem> {
  return http.put<RoleItem>(`/roles/${id}`, req);
}

/** 内置角色删除返回 403 */
export function deleteRole(id: number): Promise<void> {
  return http.delete<void>(`/roles/${id}`);
}

// ---------- 用户组 ----------

/** TODO(backend):契约约定返回裸数组;若后端实现为 PageResult 需同步调整 */
export function listUserGroups(): Promise<UserGroupItem[]> {
  return http.get<UserGroupItem[]>('/user-groups');
}

export function createUserGroup(req: SaveUserGroupRequest): Promise<UserGroupItem> {
  return http.post<UserGroupItem>('/user-groups', req);
}

export function updateUserGroup(id: number, req: SaveUserGroupRequest): Promise<UserGroupItem> {
  return http.put<UserGroupItem>(`/user-groups/${id}`, req);
}

export function deleteUserGroup(id: number): Promise<void> {
  return http.delete<void>(`/user-groups/${id}`);
}

/** 成员管理抽屉需要读取当前成员(契约补充端点,TODO(backend):后端暂未定义) */
export function listGroupMembers(groupId: number): Promise<UserItem[]> {
  return http.get<UserItem[]>(`/user-groups/${groupId}/members`);
}

export function addGroupMember(groupId: number, req: AddGroupMemberRequest): Promise<void> {
  return http.post<void>(`/user-groups/${groupId}/members`, req);
}

export function removeGroupMember(groupId: number, userId: number): Promise<void> {
  return http.delete<void>(`/user-groups/${groupId}/members/${userId}`);
}

// ---------- 角色组 ----------

/** TODO(backend):契约约定返回裸数组;若后端实现为 PageResult 需同步调整 */
export function listRoleGroups(): Promise<RoleGroupItem[]> {
  return http.get<RoleGroupItem[]>('/role-groups');
}

export function createRoleGroup(req: SaveRoleGroupRequest): Promise<RoleGroupItem> {
  return http.post<RoleGroupItem>('/role-groups', req);
}

export function updateRoleGroup(id: number, req: SaveRoleGroupRequest): Promise<RoleGroupItem> {
  return http.put<RoleGroupItem>(`/role-groups/${id}`, req);
}

export function deleteRoleGroup(id: number): Promise<void> {
  return http.delete<void>(`/role-groups/${id}`);
}

/** 角色组内角色列表 */
export function listRoleGroupRoles(groupId: number): Promise<RoleItem[]> {
  return http.get<RoleItem[]>(`/role-groups/${groupId}/roles`);
}

export function addRoleGroupRole(groupId: number, req: AddRoleGroupRoleRequest): Promise<void> {
  return http.post<void>(`/role-groups/${groupId}/roles`, req);
}

export function removeRoleGroupRole(groupId: number, roleId: number): Promise<void> {
  return http.delete<void>(`/role-groups/${groupId}/roles/${roleId}`);
}

// ---------- 授权(中心授权表) ----------

/** 过滤参数全部可选、可组合;subjectId 需与 subjectType 搭配,objectId 需与 objectType 搭配 */
export function listGrants(query: GrantListQuery): Promise<GrantItem[]> {
  return http.get<GrantItem[]>('/grants', {
    subjectType: query.subjectType,
    subjectId: query.subjectId,
    objectType: query.objectType,
    objectId: query.objectId,
  });
}

export function createGrant(req: CreateGrantRequest): Promise<GrantItem> {
  return http.post<GrantItem>('/grants', req);
}

export function deleteGrant(id: number): Promise<void> {
  return http.delete<void>(`/grants/${id}`);
}

// ---------- 有效权限 ----------

/** TODO(backend):user-group 成员的有效权限需聚合其所属组授权,结构以后端为准 */
export function getEffectivePermissions(
  username: string,
  query: { cluster?: string; namespace?: string },
): Promise<EffectivePermissions> {
  return http.get<EffectivePermissions>(`/users/${username}/effective-permissions`, {
    cluster: query.cluster,
    namespace: query.namespace,
  });
}

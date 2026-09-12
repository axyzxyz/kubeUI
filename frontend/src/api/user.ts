import { http } from './http';
import { loadAuth } from '@/utils/storage';
import type {
  ChangeMyPasswordRequest,
  EffectivePermissions,
  CreateUserRequest,
  PageResult,
  ResetPasswordResult,
  SetUserStatusRequest,
  UserItem,
  UserPermissions,
} from './types';

export function listUsers(page = 1, size = 20): Promise<PageResult<UserItem>> {
  return http.get<PageResult<UserItem>>('/users', { page, size });
}

export function createUser(req: CreateUserRequest): Promise<UserItem> {
  return http.post<UserItem>('/users', req);
}

export function setUserStatus(name: string, req: SetUserStatusRequest): Promise<void> {
  return http.put<void>(`/users/${name}/status`, req);
}

export function changeMyPassword(req: ChangeMyPasswordRequest): Promise<void> {
  return http.put<void>('/users/me/password', req);
}

/** 当前登录用户信息 */
export function getMe(): Promise<UserItem> {
  return http.get<UserItem>('/users/me');
}

/** 当前用户角色与权限 */
export function getMyPermissions(): Promise<UserPermissions> {
  return http.get<UserPermissions>('/users/me/permissions');
}

/**
 * 当前登录用户在指定集群(+可选命名空间)维度下的有效权限(含 scope 授权)。
 * ns 传空/不传表示集群级;username 取当前登录名,无需调用方传递。
 */
export function getEffectivePermissions(
  cluster: string,
  ns?: string,
): Promise<EffectivePermissions> {
  const username = loadAuth()?.user.name ?? '';
  return http.get<EffectivePermissions>(`/users/${username}/effective-permissions`, {
    cluster,
    namespace: ns,
  });
}

/** admin 重置任意用户密码,返回一次性新密码 */
export function resetUserPassword(username: string): Promise<ResetPasswordResult> {
  return http.post<ResetPasswordResult>(`/users/${username}/reset-password`, {});
}

/** admin 删除用户(禁删自己与内置 admin) */
export function deleteUser(username: string): Promise<void> {
  return http.delete<void>(`/users/${username}`);
}

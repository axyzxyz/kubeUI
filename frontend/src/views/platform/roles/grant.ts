// 授权弹窗的预选参数与生效范围编辑行模型(四处入口复用)
import type { GrantObjectType, GrantSubjectType } from '@/api/types';

/** 打开 GrantDialog 时的预选:从角色行 / 角色组行 / 用户组行反向打开时使用 */
export interface GrantPreset {
  subjectType?: GrantSubjectType;
  subjectId?: number;
  subjectName?: string;
  objectType?: GrantObjectType;
  objectId?: number;
  objectName?: string;
}

/** 生效范围编辑行:'*' 表示全部 */
export interface ScopeRow {
  cluster: string;
  namespace: string;
}

export const ALL_CLUSTERS = '*';
export const ALL_NAMESPACES = '*';

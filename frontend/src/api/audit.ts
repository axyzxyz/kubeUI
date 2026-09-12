import { http } from './http';
import type { AuditLog, AuditLogQuery, PageResult } from './types';

/** 后端仅支持 username/cluster/action 过滤 + 分页(无时间范围) */
export function listAuditLogs(query: AuditLogQuery): Promise<PageResult<AuditLog>> {
  return http.get<PageResult<AuditLog>>('/audit-logs', {
    username: query.username,
    cluster: query.cluster,
    action: query.action,
    page: query.page,
    size: query.size,
    sortBy: query.sortBy,
    order: query.order,
  });
}

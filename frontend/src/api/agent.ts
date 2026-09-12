import { http } from './http';
import type { AgentManifest, EnrollToken, EnrollTokenCreated } from './types';

export interface CreateEnrollTokenRequest {
  cluster: string;
  /** 有效期:"24h"/"30d"/"6mo"/"1y";"permanent" 长期;缺省 24h */
  ttl?: string;
}

/** 创建一次性 Enrollment Token(admin);token 原文只返回一次 */
export function createEnrollToken(req: CreateEnrollTokenRequest): Promise<EnrollTokenCreated> {
  return http.post<EnrollTokenCreated>('/enroll-tokens', req);
}

/** 列出 Enrollment Token(脱敏) */
export function listEnrollTokens(): Promise<{ items: EnrollToken[]; total: number }> {
  return http.get<{ items: EnrollToken[]; total: number }>('/enroll-tokens');
}

/** 吊销 Enrollment Token */
export function revokeEnrollToken(id: number): Promise<void> {
  return http.delete<void>(`/enroll-tokens/${id}`);
}

/** 渲染 Agent 部署 YAML(匿名端点,token 校验但不消费) */
export function getAgentManifest(token: string): Promise<AgentManifest> {
  return http.get<AgentManifest>('/agent/manifest', { token });
}

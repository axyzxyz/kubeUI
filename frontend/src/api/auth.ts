import { http } from './http';
import type { LoginRequest, RefreshRequest, TokenPair } from './types';

export function login(req: LoginRequest): Promise<TokenPair> {
  return http.post<TokenPair>('/auth/login', req);
}

export function refresh(req: RefreshRequest): Promise<TokenPair> {
  return http.post<TokenPair>('/auth/refresh', req);
}

export function logout(req: RefreshRequest): Promise<void> {
  return http.post<void>('/auth/logout', req);
}

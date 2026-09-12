import { clearAuth, loadAuth, saveAuth } from '@/utils/storage';
import type { ApiResponse, TokenPair } from './types';

export const API_BASE = '/api/v1';

export class ApiError extends Error {
  constructor(
    public readonly code: number,
    message: string,
    public readonly httpStatus: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

/** 静默刷新单飞:并发 401 只触发一次 refresh */
let refreshing: Promise<boolean> | null = null;

/** 用 refreshToken 换新 token 对;失败返回 false(调用方跳登录) */
export async function tryRefresh(): Promise<boolean> {
  if (refreshing !== null) return refreshing;
  refreshing = (async () => {
    const auth = loadAuth();
    if (auth === null || auth.refreshToken === '') return false;
    try {
      const resp = await fetch(`${API_BASE}/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refreshToken: auth.refreshToken }),
      });
      if (resp.status !== 200) return false;
      const body = (await resp.json()) as ApiResponse<TokenPair>;
      if (body.code !== 0) return false;
      saveAuth({ ...auth, ...body.data });
      return true;
    } catch {
      return false;
    }
  })();
  try {
    return await refreshing;
  } finally {
    refreshing = null;
  }
}

/** 统一 401 兜底:清凭证并跳登录页 */
function handleUnauthorized(): void {
  clearAuth();
  if (window.location.pathname !== '/login') {
    window.location.href = '/login';
  }
}

function buildUrl(
  path: string,
  query?: Record<string, string | number | boolean | undefined>,
): string {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  if (query !== undefined) {
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== '') {
        url.searchParams.set(key, String(value));
      }
    }
  }
  return url.pathname + url.search;
}

function bearerToken(): string | undefined {
  const auth = loadAuth();
  if (auth === null || auth.accessToken === '') return undefined;
  return auth.accessToken;
}

async function requestOnce<T>(
  path: string,
  init: RequestInit,
  query: Record<string, string | number | boolean | undefined> | undefined,
): Promise<{ ok: true; data: T } | { ok: false; unauthorized: boolean }> {
  const token = bearerToken();
  const resp = await fetch(buildUrl(path, query), {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token !== undefined ? { Authorization: `Bearer ${token}` } : {}),
      ...init.headers,
    },
  });
  if (resp.status === 401) {
    return { ok: false, unauthorized: true };
  }
  const body = (await resp.json()) as ApiResponse<T>;
  if (body.code !== 0) {
    throw new ApiError(body.code, body.message, resp.status);
  }
  return { ok: true, data: body.data };
}

async function request<T>(
  path: string,
  init: RequestInit = {},
  query?: Record<string, string | number | boolean | undefined>,
): Promise<T> {
  let res = await requestOnce<T>(path, init, query);
  // 401 先静默 refresh 一次并重放;再失败才跳登录(认证端点自身不刷新)。
  if (!res.ok && res.unauthorized && !path.startsWith('/auth/')) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      res = await requestOnce<T>(path, init, query);
    }
  }
  if (!res.ok) {
    handleUnauthorized();
    throw new ApiError(40100, 'unauthorized', 401);
  }
  return res.data;
}

function jsonBody(body: unknown): string {
  return JSON.stringify(body);
}

export const http = {
  get: <T>(path: string, query?: Record<string, string | number | boolean | undefined>) =>
    request<T>(path, { method: 'GET' }, query),
  post: <T>(
    path: string,
    body: unknown,
    query?: Record<string, string | number | boolean | undefined>,
  ) => request<T>(path, { method: 'POST', body: jsonBody(body) }, query),
  put: <T>(
    path: string,
    body: unknown,
    query?: Record<string, string | number | boolean | undefined>,
  ) => request<T>(path, { method: 'PUT', body: jsonBody(body) }, query),
  delete: <T>(path: string, query?: Record<string, string | number | boolean | undefined>) =>
    request<T>(path, { method: 'DELETE' }, query),
};

import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import { login as apiLogin, logout as apiLogout } from '@/api/auth';
import { tryRefresh } from '@/api/http';
import type { LoginRequest, UserInfo } from '@/api/types';
import { clearAuth, loadAuth, saveAuth } from '@/utils/storage';

interface StoredAuth {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  user: UserInfo;
}

/** 后端登录响应只有 TokenPair(无 user 信息),从 JWT payload 解出 username/role */
function userFromJwt(token: string): UserInfo {
  try {
    const part = token.split('.')[1] ?? '';
    const b64 = part.replace(/-/g, '+').replace(/_/g, '/');
    const payload = JSON.parse(atob(b64)) as { username?: string; role?: string };
    const role = payload.role ?? 'viewer';
    return { name: payload.username ?? '', roles: [role] };
  } catch {
    return { name: '', roles: [] };
  }
}

/** 提前刷新窗口:access token 过期前 2 分钟(或剩余时间的 20%,取小) */
function refreshDelayMs(expiresAt: string): number {
  const exp = Date.parse(expiresAt);
  if (Number.isNaN(exp)) return 10 * 60_000;
  const remain = exp - Date.now();
  return Math.max(5_000, Math.min(remain - 2 * 60_000, remain * 0.2));
}

export const useAuthStore = defineStore('auth', () => {
  const stored = loadAuth();
  const accessToken = ref<string>(stored?.accessToken ?? '');
  const refreshToken = ref<string>(stored?.refreshToken ?? '');
  const expiresAt = ref<string>(stored?.expiresAt ?? '');
  const user = ref<UserInfo | null>(stored?.user ?? null);

  let refreshTimer: number | null = null;

  const isLoggedIn = computed(() => accessToken.value !== '');
  const isAdmin = computed(() => user.value?.roles.includes('admin') ?? false);

  function scheduleRefresh(): void {
    if (refreshTimer !== null) {
      window.clearTimeout(refreshTimer);
      refreshTimer = null;
    }
    if (refreshToken.value === '' || expiresAt.value === '') return;
    const delay = refreshDelayMs(expiresAt.value);
    refreshTimer = window.setTimeout(() => {
      void extend();
    }, delay);
  }

  function applyPair(pair: {
    accessToken: string;
    refreshToken: string;
    expiresAt: string;
  }): void {
    accessToken.value = pair.accessToken;
    refreshToken.value = pair.refreshToken;
    expiresAt.value = pair.expiresAt;
    // 刷新后用户名/角色可能变化,重新从 JWT 解出
    user.value = userFromJwt(pair.accessToken);
    saveAuth({
      accessToken: pair.accessToken,
      refreshToken: pair.refreshToken,
      expiresAt: pair.expiresAt,
      user: user.value,
    });
    scheduleRefresh();
  }

  /** 静默续期:供定时器与 401 前主动调用 */
  async function extend(): Promise<boolean> {
    const ok = await tryRefresh();
    if (ok) {
      const next = loadAuth();
      if (next !== null) {
        accessToken.value = next.accessToken;
        refreshToken.value = next.refreshToken;
        expiresAt.value = next.expiresAt;
        scheduleRefresh();
      }
    }
    return ok;
  }

  async function login(req: LoginRequest): Promise<void> {
    const pair = await apiLogin(req);
    applyPair(pair);
  }

  function logout(): void {
    const rt = refreshToken.value;
    if (rt !== '') {
      // 尽力吊销服务端 refresh token,失败不阻断本地登出
      void apiLogout({ refreshToken: rt }).catch(() => undefined);
    }
    if (refreshTimer !== null) {
      window.clearTimeout(refreshTimer);
      refreshTimer = null;
    }
    accessToken.value = '';
    refreshToken.value = '';
    expiresAt.value = '';
    user.value = null;
    clearAuth();
  }

  // 存量会话恢复:启动时按 expiresAt 排定时刷新
  if (isLoggedIn.value) scheduleRefresh();

  return {
    accessToken,
    refreshToken,
    expiresAt,
    user,
    isLoggedIn,
    isAdmin,
    login,
    logout,
    extend,
  };
});

export type { StoredAuth };

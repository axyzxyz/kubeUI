/** localStorage 读写封装(禁止未类型化的直接读写) */
interface StoredToken {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  user: { name: string; roles: string[] };
}

const TOKEN_KEY = 'kubeui.auth';
const THEME_KEY = 'kubeui.theme';
const NS_KEY = 'kubeui.namespace';

export type { StoredToken };

export function loadAuth(): StoredToken | null {
  const raw = localStorage.getItem(TOKEN_KEY);
  if (raw === null) {
    return null;
  }
  try {
    return JSON.parse(raw) as StoredToken;
  } catch {
    return null;
  }
}

export function saveAuth(value: StoredToken): void {
  localStorage.setItem(TOKEN_KEY, JSON.stringify(value));
}

export function clearAuth(): void {
  localStorage.removeItem(TOKEN_KEY);
}

export function loadTheme(): 'light' | 'dark' {
  const raw = localStorage.getItem(THEME_KEY);
  return raw === 'light' ? 'light' : 'dark';
}

export function saveTheme(value: 'light' | 'dark'): void {
  localStorage.setItem(THEME_KEY, value);
}

export function loadNamespace(): string {
  return localStorage.getItem(NS_KEY) ?? '';
}

export function saveNamespace(value: string): void {
  localStorage.setItem(NS_KEY, value);
}

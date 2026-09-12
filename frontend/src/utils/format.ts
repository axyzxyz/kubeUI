/** 相对时间与格式化工具(K8s AGE 惯例) */
export function relativeTime(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) {
    return '—';
  }
  const diff = Math.max(0, now - t);
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.floor(h / 24);
  return `${d}d`;
}

export function absoluteTime(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) {
    return '—';
  }
  const d = new Date(t);
  const pad = (n: number): string => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0';
  const units = ['B', 'Ki', 'Mi', 'Gi', 'Ti'];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i += 1;
  }
  const unit = units[i] ?? 'B';
  return `${value.toFixed(value >= 10 ? 0 : 1)}${unit}`;
}

export function formatCpu(millis: number): string {
  return millis >= 1000 ? `${(millis / 1000).toFixed(1)}核` : `${Math.round(millis)}m`;
}

/** 解析后端百分比字符串(如 "12.5%")为数字;非法返回 0 */
export function parsePct(s: string | undefined): number {
  if (s === undefined) return 0;
  const n = Number.parseFloat(s.replace('%', ''));
  return Number.isNaN(n) ? 0 : n;
}

/**
 * 解析 K8s CPU quantity(如 "97804391n"/"750u"/"250m"/"2")为毫核数;
 * 非法返回 0。单位: n=10^-9 核,u=10^-6,m=10^-3,无后缀 = 核。
 */
export function parseCpu(q: string | undefined): number {
  if (q === undefined || q === '') return 0;
  const m = /^(-?\d+(?:\.\d+)?)(n|u|m)?$/.exec(q.trim());
  if (m === null) return 0;
  const v = Number.parseFloat(m[1] ?? '0');
  if (Number.isNaN(v)) return 0;
  switch (m[2]) {
    case 'n':
      return v / 1e6;
    case 'u':
      return v / 1e3;
    case 'm':
      return v;
    default:
      return v * 1000;
  }
}

/** 解析 K8s 内存 quantity(如 "2730344Ki"/"16Gi"/"128974848")为字节数;非法返回 0 */
export function parseMemBytes(q: string | undefined): number {
  if (q === undefined || q === '') return 0;
  const m = /^(-?\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$/.exec(q.trim());
  if (m === null) return 0;
  const v = Number.parseFloat(m[1] ?? '0');
  if (Number.isNaN(v)) return 0;
  const unit = m[2] ?? '';
  const bin: Record<string, number> = { Ki: 2 ** 10, Mi: 2 ** 20, Gi: 2 ** 30, Ti: 2 ** 40, Pi: 2 ** 50, Ei: 2 ** 60 };
  const dec: Record<string, number> = { k: 1e3, M: 1e6, G: 1e9, T: 1e12, P: 1e15, E: 1e18 };
  const mult = bin[unit] ?? dec[unit] ?? 1;
  return v * mult;
}

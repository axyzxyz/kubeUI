import { ref, watch, type ComputedRef, type Ref } from 'vue';
import { ApiError } from '@/api/http';
import { getEffectivePermissions, getMyPermissions } from '@/api/user';

type StringRef = Ref<string> | ComputedRef<string>;

/**
 * 全局权限(/users/me/permissions)会话级缓存:
 * admin 含 "*" 时各处直接放行;非 admin 在 effective-permissions 不可用时兜底。
 */
let globalPermsPromise: Promise<Set<string>> | null = null;

function loadGlobalPerms(): Promise<Set<string>> {
  globalPermsPromise ??= getMyPermissions()
    .then((p) => new Set(p.permissions))
    .catch((e: unknown) => {
      // 失败(含 401)不缓存,下次重试
      globalPermsPromise = null;
      throw e;
    });
  return globalPermsPromise;
}

export interface PermsState {
  /** 当前 cluster(+ns)维度的有效权限集合;admin 时为 {"*"} */
  perms: Ref<Set<string>>;
  /** 权限判断:admin 恒 true;未加载完成时按已加载集合判断(默认收敛为无权限) */
  can: (perm: string) => boolean;
}

/**
 * 按集群/命名空间维度的有效权限。
 * - admin(me/permissions 含 "*")直接全 true,不请求 effective-permissions;
 * - 非 admin 请求 effective-permissions;注意后端该路由挂了 AdminOnly 中间件,
 *   非 admin 会得到 40300,此时回退到全局权限(/users/me/permissions,内置角色全局生效);
 * - 同一 cluster|ns key 结果缓存,不重复请求;请求失败(含 401)不缓存;
 * - watch cluster/ns 变化自动重新加载,过期响应丢弃。
 */
export function usePerms(clusterRef: StringRef, nsRef: StringRef): PermsState {
  const perms = ref(new Set<string>());
  const admin = ref(false);
  const cache = new Map<string, Set<string>>();

  async function load(): Promise<void> {
    const globalPerms = await loadGlobalPerms();
    if (globalPerms.has('*')) {
      admin.value = true;
      perms.value = new Set(['*']);
      return;
    }
    admin.value = false;
    const cluster = clusterRef.value;
    const ns = nsRef.value;
    const key = `${cluster}|${ns}`;
    const cached = cache.get(key);
    if (cached !== undefined) {
      perms.value = cached;
      return;
    }
    try {
      const res = await getEffectivePermissions(cluster, ns === '' ? undefined : ns);
      const set = new Set(res.permissions);
      cache.set(key, set);
      // 响应期间 cluster/ns 已变化则丢弃过期结果
      if (clusterRef.value === cluster && nsRef.value === ns) {
        perms.value = set;
      }
    } catch (e) {
      // 后端 effective-permissions 仅 admin 可调(AdminOnly):非 admin 回退全局权限;
      // 401 由 http 层统一跳登录,均不缓存。
      if (e instanceof ApiError && e.code === 40300) {
        cache.set(key, globalPerms);
        if (clusterRef.value === cluster && nsRef.value === ns) {
          perms.value = globalPerms;
        }
      }
    }
  }

  watch(
    [clusterRef, nsRef],
    () => {
      load().catch(() => undefined);
    },
    { immediate: true },
  );

  function can(perm: string): boolean {
    if (admin.value) return true;
    return perms.value.has(perm);
  }

  return { perms, can };
}

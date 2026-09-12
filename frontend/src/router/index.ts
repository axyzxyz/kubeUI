import { createRouter, createWebHistory } from 'vue-router';
import { useAuthStore } from '@/stores/authStore';
import { isClusterScoped } from '@/api/workload';

const VALID_KINDS = new Set<string>([
  'deployments',
  'statefulsets',
  'daemonsets',
  'pods',
  'services',
  'ingresses',
  'configmaps',
  'secrets',
  'pvcs',
  'pvs',
  'nodes',
  'namespaces',
  'crds',
]);

/** CRD 动态资源 plural 名:小写字母开头,仅小写字母/数字/中划线 */
const DYNAMIC_KIND_RE = /^[a-z][a-z0-9-]*$/;

function validKind(kind: string): boolean {
  // 内置枚举之外视为 CRD plural,由后端 discovery 解析,解析失败返回 40001
  return VALID_KINDS.has(kind) || DYNAMIC_KIND_RE.test(kind);
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/layouts/BlankLayout.vue'),
      children: [
        { path: '', name: 'LoginView', component: () => import('@/views/login/LoginView.vue') },
      ],
    },
    {
      path: '/',
      component: () => import('@/layouts/DefaultLayout.vue'),
      children: [
        { path: '', redirect: '/clusters' },
        {
          path: 'clusters',
          name: 'ClusterList',
          component: () => import('@/views/platform/ClusterList.vue'),
        },
        {
          path: 'clusters/register',
          name: 'ClusterRegister',
          component: () => import('@/views/platform/ClusterRegister.vue'),
        },
        {
          path: 'clusters/:cluster/overview',
          name: 'ClusterOverview',
          component: () => import('@/views/cluster/ClusterOverviewView.vue'),
        },
        {
          path: 'clusters/:cluster/resources/:kind',
          name: 'ResourceList',
          component: () => import('@/views/cluster/resources/ResourceList.vue'),
          beforeEnter: (to) => {
            const kind = String(to.params.kind);
            return validKind(kind) ? true : { name: 'ClusterList' };
          },
        },
        {
          path: 'clusters/:cluster/crds',
          name: 'CrdList',
          component: () => import('@/views/cluster/CrdListView.vue'),
        },
        {
          path: 'clusters/:cluster/events',
          name: 'ClusterEvents',
          component: () => import('@/views/cluster/EventsView.vue'),
        },
        {
          path: 'clusters/:cluster/monitoring',
          name: 'ClusterMonitoring',
          component: () => import('@/views/cluster/MonitoringView.vue'),
        },
        {
          path: 'users',
          name: 'Users',
          component: () => import('@/views/platform/UsersView.vue'),
          // 系统级页面:非 admin 直接 URL 访问由守卫拦截(页面内部另有后端 403 兜底)
          beforeEnter: () => {
            const auth = useAuthStore();
            return auth.isAdmin ? true : { name: 'ClusterList' };
          },
        },
        {
          path: 'roles',
          name: 'Roles',
          component: () => import('@/views/platform/RoleManageView.vue'),
          // 前端仅做入口控制,后端 RBAC 同样校验(403)
          beforeEnter: () => {
            const auth = useAuthStore();
            return auth.isAdmin ? true : { name: 'ClusterList' };
          },
        },
        {
          path: 'audit',
          name: 'AuditLogs',
          component: () => import('@/views/platform/AuditLogsView.vue'),
          beforeEnter: () => {
            const auth = useAuthStore();
            return auth.isAdmin ? true : { name: 'ClusterList' };
          },
        },
        {
          path: 'credentials',
          name: 'Credentials',
          component: () => import('@/views/platform/CredentialView.vue'),
        },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/clusters' },
  ],
});

router.beforeEach((to) => {
  const auth = useAuthStore();
  if (to.name === 'LoginView' || to.name === 'Login') {
    return auth.isLoggedIn ? { name: 'ClusterList' } : true;
  }
  return auth.isLoggedIn ? true : { name: 'LoginView' };
});

// 懒加载 chunk 加载失败(如发版后旧 index.html 引用已删除的 hash chunk)会
// 让路由导航中断、页面渲染出无事件绑定的残缺 DOM(表现为整页按钮/导航无响应)。
// 这里按会话一次性整页刷新,重新拉取最新的 index.html 与 chunk。
let chunkReloaded = false;
router.onError((error) => {
  const msg = String((error as { message?: string }).message ?? '');
  const isChunkError =
    msg.includes('Failed to fetch dynamically imported module') ||
    msg.includes('Importing a module script failed') ||
    /Loading( CSS)? chunk .* failed/i.test(msg);
  if (isChunkError && !chunkReloaded) {
    chunkReloaded = true;
    window.sessionStorage.setItem('kubeui.chunkReload', '1');
    window.location.reload();
  }
});

export { isClusterScoped };

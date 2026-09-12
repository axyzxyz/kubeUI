<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import type { ResourceKind } from '@/api/workload';
import { useAuthStore } from '@/stores/authStore';
import { useClusterStore } from '@/stores/clusterStore';
import StatusBadge from '@/components/StatusBadge.vue';

const route = useRoute();
const router = useRouter();
const auth = useAuthStore();
const clusterStore = useClusterStore();

const clusterName = computed(() => String(route.params.cluster ?? clusterStore.currentName));

interface NavItem {
  label: string;
  to: string;
  display: boolean;
}

/** 分区:侧边栏内带小标题与缩进的子模块,模块间以分隔线区隔 */
interface NavSection {
  title: string;
  items: NavItem[];
  display: boolean;
}

interface MenuGroup {
  title: string;
  /** 无分区的平铺组(平台管理) */
  items?: NavItem[];
  /** 分区组(集群) */
  sections?: NavSection[];
}

const visible = computed(() => clusterName.value !== '');

function resItem(kind: ResourceKind, label: string): NavItem {
  return { label, to: `/clusters/${clusterName.value}/resources/${kind}`, display: visible.value };
}

const RESOURCE_SECTIONS: NavSection[] = [
  {
    title: '工作负载',
    display: visible.value,
    items: [
      resItem('deployments', 'Deployments'),
      resItem('statefulsets', 'StatefulSets'),
      resItem('daemonsets', 'DaemonSets'),
      resItem('pods', 'Pods'),
    ],
  },
  {
    title: '网络',
    display: visible.value,
    items: [resItem('services', 'Services'), resItem('ingresses', 'Ingresses')],
  },
  {
    title: '配置',
    display: visible.value,
    items: [resItem('configmaps', 'ConfigMaps')],
  },
  {
    title: '存储',
    display: visible.value,
    items: [resItem('pvcs', 'PVC'), resItem('pvs', 'PV')],
  },
  {
    title: '节点',
    display: visible.value,
    items: [resItem('nodes', 'Nodes')],
  },
];

const groups = computed<MenuGroup[]>(() => [
  {
    title: '平台管理',
    items: [
      // 系统级入口仅 admin 可见(users/audit/roles 页面另由后端 403 与路由守卫兜底)
      { label: '集群列表', to: '/clusters', display: true },
      { label: '用户管理', to: '/users', display: auth.isAdmin },
      { label: '权限管理', to: '/roles', display: auth.isAdmin },
      { label: '审计日志', to: '/audit', display: auth.isAdmin },
    ],
  },
  {
    title: `集群: ${clusterName.value}`,
    sections: [
      {
        title: '概览',
        display: visible.value,
        items: [{ label: '集群总览', to: `/clusters/${clusterName.value}/overview`, display: visible.value }],
      },
      ...RESOURCE_SECTIONS,
      {
        title: '可观测',
        display: visible.value,
        items: [
          { label: 'CRD / 自定义资源', to: `/clusters/${clusterName.value}/crds`, display: visible.value },
          { label: '事件', to: `/clusters/${clusterName.value}/events`, display: visible.value },
          { label: '监控', to: `/clusters/${clusterName.value}/monitoring`, display: visible.value },
        ],
      },
    ],
  },
]);

const current = computed(() => String(route.fullPath));

function isActive(to: string): boolean {
  return current.value === to || current.value.startsWith(to);
}
</script>

<template>
  <aside class="sidenav">
    <div v-for="group in groups" :key="group.title" class="group">
      <div class="group-title">{{ group.title }}</div>
      <!-- 平铺组 -->
      <template v-if="group.items">
        <div
          v-for="item in group.items"
          v-show="item.display"
          :key="item.to"
          class="item"
          :class="{ active: isActive(item.to) }"
          @click="router.push(item.to)"
        >
          {{ item.label }}
        </div>
      </template>
      <!-- 分区组:小标题 + 缩进子项 + 左侧引导线,模块间分隔线 -->
      <template v-for="(section, si) in group.sections" :key="group.title + section.title">
        <div v-if="section.display" class="section" :class="{ separated: si > 0 }">
          <div class="section-title">{{ section.title }}</div>
          <div class="section-items">
            <div
              v-for="item in section.items"
              v-show="item.display"
              :key="item.to"
              class="item sub"
              :class="{ active: isActive(item.to) }"
              @click="router.push(item.to)"
            >
              {{ item.label }}
            </div>
          </div>
        </div>
      </template>
    </div>
    <div v-if="auth.isAdmin" class="footer">
      <el-button type="primary" size="small" @click="router.push('/clusters/register')">
        + 注册集群
      </el-button>
    </div>
    <StatusBadge v-if="clusterStore.current" class="hidden" :status="clusterStore.current.status" />
  </aside>
</template>

<style scoped>
.sidenav {
  width: 232px;
  flex: none;
  background: var(--bg-surface);
  border-right: 1px solid var(--border-1);
  display: flex;
  flex-direction: column;
  padding: 12px 8px;
  overflow: auto;
}
.group {
  margin-bottom: 16px;
}
.group-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-1);
  padding: 4px 8px 8px;
  letter-spacing: 0.5px;
}
/* 模块(分区):之间用分隔线 + 间距制造模块感 */
.section {
  padding: 2px 4px;
}
.section.separated {
  border-top: 1px dashed var(--border-1);
  margin-top: 10px;
  padding-top: 10px;
}
.section-title {
  font-size: 11px;
  color: var(--text-3);
  padding: 2px 8px 4px;
  letter-spacing: 1px;
}
/* 子项缩进 + 左侧引导线,形成从属层级 */
.section-items {
  margin-left: 14px;
  padding-left: 6px;
  border-left: 1px solid var(--border-1);
}
.item {
  padding: 7px 12px;
  border-radius: 4px;
  cursor: pointer;
  color: var(--text-2);
  font-size: 13px;
}
.item.sub {
  padding: 5px 10px;
  font-size: 12.5px;
}
.item:hover {
  background: var(--bg-surface-2);
  color: var(--text-1);
}
.item.active {
  background: var(--brand-primary-bg);
  color: var(--brand-primary);
  font-weight: 500;
  box-shadow: inset 2px 0 0 var(--brand-primary);
}
.footer {
  margin-top: auto;
  padding: 8px;
}
.hidden {
  display: none;
}
</style>

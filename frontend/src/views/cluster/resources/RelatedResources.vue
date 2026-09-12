<script setup lang="ts">
/**
 * 详情抽屉「关联资源」区块(仅 deployments/statefulsets/daemonsets/services/ingresses):
 * - 工作负载:spec.selector.matchLabels → labelSelector 列出匹配 Pods(点击打开 Pod 详情);
 * - services:spec.selector → labelSelector 列出匹配 Pods;
 * - ingresses:遍历 spec.rules→http.paths→backend.service 只读列出关联 Service。
 * 数据在抽屉打开时拉取;列表接口复用 listResources(labelSelector)。
 */
import { computed, ref, watch } from 'vue';
import { listResources } from '@/api/workload';
import type { K8sObject, ResourceItem } from '@/api/types';
import RelativeTime from '@/components/RelativeTime.vue';
import StatusBadge from '@/components/StatusBadge.vue';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  cluster: string;
  kind: string;
  namespace: string;
  /** 父资源的 K8s 原生 unstructured 详情对象(抽屉已加载,无需重复请求) */
  obj: K8sObject | null;
  /** 抽屉是否打开:打开时才拉取关联数据 */
  open: boolean;
}>();

const emit = defineEmits<{ openPod: [pod: ResourceItem] }>();

type Mode = 'pods' | 'ingress-services' | 'none';

const mode = computed<Mode>(() => {
  if (['deployments', 'statefulsets', 'daemonsets', 'services'].includes(props.kind)) {
    return 'pods';
  }
  return props.kind === 'ingresses' ? 'ingress-services' : 'none';
});

/** 从 unstructured 对象按路径读取 Record<string,string>(缺路径/类型不符返回 null) */
function stringMapAt(obj: K8sObject | null, path: string[]): Record<string, string> | null {
  if (obj === null) return null;
  let cur: unknown = obj;
  for (const k of path) {
    if (cur === null || typeof cur !== 'object') return null;
    cur = (cur as Record<string, unknown>)[k];
  }
  if (cur === null || typeof cur !== 'object' || Array.isArray(cur)) return null;
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(cur as Record<string, unknown>)) {
    if (typeof v === 'string') out[k] = v;
  }
  return out;
}

/** 工作负载取 spec.selector.matchLabels;Service 取 spec.selector */
const selector = computed<Record<string, string> | null>(() => {
  if (mode.value !== 'pods') return null;
  const path = props.kind === 'services' ? ['spec', 'selector'] : ['spec', 'selector', 'matchLabels'];
  return stringMapAt(props.obj, path);
});

const labelSelector = computed<string>(() => {
  const sel = selector.value;
  if (sel === null) return '';
  return Object.entries(sel)
    .map(([k, v]) => `${k}=${v}`)
    .join(',');
});

const pods = ref<ResourceItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');

async function load(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await listResources(props.cluster, 'pods', {
      namespace: props.namespace === '' ? undefined : props.namespace,
      labelSelector: labelSelector.value === '' ? undefined : labelSelector.value,
      page: 1,
      size: 200,
    });
    pods.value = res.items;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

watch(
  () => [props.open, mode.value, labelSelector.value, props.cluster, props.namespace] as const,
  ([open]) => {
    if (open && mode.value === 'pods' && labelSelector.value !== '') {
      void load();
    }
  },
  { immediate: true },
);

/** ingress 关联 Service:rules→http.paths→backend.service(只读展示,数据来自详情对象) */
interface IngressServiceRef {
  host: string;
  path: string;
  service: string;
}

const ingressServices = computed<IngressServiceRef[]>(() => {
  if (props.obj === null) return [];
  const rules = (props.obj['spec'] ?? {}) as {
    rules?: {
      host?: string;
      http?: { paths?: { path?: string; backend?: { service?: { name?: string } } }[] };
    }[];
  };
  const out: IngressServiceRef[] = [];
  for (const rule of rules.rules ?? []) {
    for (const p of rule.http?.paths ?? []) {
      const svc = p.backend?.service?.name;
      if (svc !== undefined && svc !== '') {
        out.push({ host: rule.host ?? '—', path: p.path ?? '—', service: svc });
      }
    }
  }
  return out;
});
</script>

<template>
  <div>
    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      show-icon
      style="margin-bottom: 12px"
    />

    <template v-if="mode === 'pods'">
      <el-alert
        v-if="selector === null || Object.keys(selector).length === 0"
        type="info"
        :closable="false"
        title="该资源未配置 selector,无法关联 Pod"
        style="margin-bottom: 12px"
      />
      <template v-else>
        <div class="selector-line">
          labelSelector:<code class="mono">{{ labelSelector }}</code>
        </div>
        <el-table
          v-loading="loading"
          :data="pods"
          size="small"
          class="table-compact"
          row-key="name"
          @row-click="(row: ResourceItem) => emit('openPod', row)"
        >
          <el-table-column label="名称" min-width="180">
            <template #default="{ row }">
              <el-link type="primary">{{ row.name }}</el-link>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <StatusBadge :status="row.status ?? '—'" />
            </template>
          </el-table-column>
          <!-- listResources 摘要不含重启计数(ResourceItem 无该字段),暂以 — 占位 -->
          <el-table-column label="重启" width="70">
            <template #default>—</template>
          </el-table-column>
          <el-table-column label="AGE" width="90">
            <template #default="{ row }">
              <RelativeTime v-if="row.createdAt" :value="row.createdAt" />
              <span v-else>—</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="140">
            <template #default="{ row }">
              <el-link type="primary" @click.stop="emit('openPod', row)">查看详情</el-link>
            </template>
          </el-table-column>
        </el-table>
      </template>
    </template>

    <template v-else-if="mode === 'ingress-services'">
      <el-table :data="ingressServices" size="small" class="table-compact" row-key="service">
        <el-table-column prop="host" label="Host" min-width="180" show-overflow-tooltip />
        <el-table-column prop="path" label="Path" min-width="140" />
        <el-table-column prop="service" label="关联 Service" min-width="180" />
      </el-table>
    </template>
  </div>
</template>

<style scoped>
.selector-line {
  font-size: 13px;
  color: var(--text-2);
  margin-bottom: 8px;
}
.mono {
  background: var(--bg-inset);
  padding: 1px 6px;
  border-radius: 4px;
}
:deep(.el-table__row) {
  cursor: pointer;
}
</style>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { getResource, restartPod, type ResourceKind } from '@/api/workload';
import type { K8sObject, ResourceItem } from '@/api/types';
import LogViewer from '@/components/LogViewer.vue';
import StatusBadge from '@/components/StatusBadge.vue';
import TerminalPanel from '@/components/TerminalPanel.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import { humanizeError } from '@/utils/errorMessages';
import RelatedResources from './RelatedResources.vue';

const props = defineProps<{
  modelValue: boolean;
  cluster: string;
  kind: ResourceKind;
  name: string;
  namespace: string;
}>();

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const pod = ref<K8sObject | null>(null);
const errorMsg = ref('');
const activeTab = ref('overview');

const isPod = computed(() => props.kind === 'pods');

interface ContainerInfo {
  name: string;
  image: string;
  ready: boolean;
  restartCount: number;
  state: string;
}

interface PodView {
  phase: string;
  podIP: string;
  node: string;
  createdAt: string;
  containers: ContainerInfo[];
}

/** 详情为 K8s 原生 unstructured JSON;容器列表取 status.containerStatuses,缺失时回退 spec.containers */
function toView(obj: K8sObject): PodView {
  const meta = (obj['metadata'] ?? {}) as { creationTimestamp?: string };
  const spec = (obj['spec'] ?? {}) as {
    nodeName?: string;
    containers?: { name: string; image?: string }[];
  };
  const status = (obj['status'] ?? {}) as {
    phase?: string;
    podIP?: string;
    containerStatuses?: {
      name: string;
      image?: string;
      ready?: boolean;
      restartCount?: number;
      state?: Record<string, unknown>;
    }[];
  };
  const stateOf = (st?: Record<string, unknown>): string => {
    if (st === undefined) return 'Unknown';
    const keys = Object.keys(st);
    return keys[0] !== undefined ? keys[0] : 'Unknown';
  };
  const containers: ContainerInfo[] = (status.containerStatuses ?? []).map((cs) => ({
    name: cs.name,
    image: cs.image ?? '',
    ready: cs.ready ?? false,
    restartCount: cs.restartCount ?? 0,
    state: stateOf(cs.state),
  }));
  if (containers.length === 0) {
    for (const c of spec.containers ?? []) {
      containers.push({
        name: c.name,
        image: c.image ?? '',
        ready: false,
        restartCount: 0,
        state: 'Unknown',
      });
    }
  }
  return {
    phase: status.phase ?? 'Unknown',
    podIP: status.podIP ?? '',
    node: spec.nodeName ?? '',
    createdAt: meta.creationTimestamp ?? '',
    containers,
  };
}

const view = computed<PodView | null>(() => (pod.value === null ? null : toView(pod.value)));

/** 容器下拉数据源:详情 containerStatuses,不再是 ['app'] 占位 */
const containers = computed(() => view.value?.containers.map((c) => c.name) ?? []);

/** 「关联资源」支持的工作负载/网络类型(SVG 同构端点已覆盖) */
const RELATED_KINDS: readonly string[] = [
  'deployments',
  'statefulsets',
  'daemonsets',
  'services',
  'ingresses',
];
const hasRelated = computed(() => RELATED_KINDS.includes(props.kind));

/** 关联 Pod 行点击 → 嵌套打开该 Pod 的详情(复用本抽屉,含日志/终端) */
const nestedPod = ref<ResourceItem | null>(null);

watch(
  () => props.modelValue,
  (v) => {
    if (v) void load();
  },
);

async function load(): Promise<void> {
  errorMsg.value = '';
  pod.value = null;
  try {
    pod.value = await getResource(props.cluster, props.kind, props.name, props.namespace);
  } catch (e) {
    errorMsg.value = humanizeError(e);
  }
}

async function onRestart(): Promise<void> {
  try {
    await restartPod(props.cluster, props.name, props.namespace);
    ElMessage.success(`已触发重启 ${props.name}`);
  } catch (e) {
    ElMessage.error(humanizeError(e));
  }
}
</script>

<template>
  <el-drawer
    :model-value="props.modelValue"
    :title="`${props.kind} · ${props.name}`"
    size="72%"
    append-to-body
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />

    <el-tabs v-model="activeTab">
      <el-tab-pane label="概览" name="overview">
        <el-descriptions v-if="view" :column="2" border size="small">
          <el-descriptions-item label="名称">{{ props.name }}</el-descriptions-item>
          <el-descriptions-item label="Namespace">{{ props.namespace }}</el-descriptions-item>
          <el-descriptions-item label="状态">
            <StatusBadge :status="view.phase" />
          </el-descriptions-item>
          <el-descriptions-item label="节点">{{ view.node || '—' }}</el-descriptions-item>
          <el-descriptions-item label="IP">{{ view.podIP || '—' }}</el-descriptions-item>
          <el-descriptions-item label="AGE">
            <RelativeTime v-if="view.createdAt !== ''" :value="view.createdAt" />
            <span v-else>—</span>
          </el-descriptions-item>
        </el-descriptions>

        <div v-if="isPod && view" class="section-title">容器</div>
        <el-table
          v-if="isPod && view"
          :data="view.containers"
          size="small"
          class="table-compact"
          row-key="name"
        >
          <el-table-column prop="name" label="名称" min-width="140" />
          <el-table-column prop="image" label="镜像" min-width="220" show-overflow-tooltip />
          <el-table-column label="Ready" width="80">
            <template #default="{ row }">{{ row.ready ? '是' : '否' }}</template>
          </el-table-column>
          <el-table-column prop="restartCount" label="重启" width="70" />
          <el-table-column prop="state" label="状态" width="110" />
        </el-table>

        <el-button
          v-if="isPod"
          type="warning"
          size="small"
          style="margin-top: 12px"
          @click="onRestart"
        >
          重启 Pod(删除重建)
        </el-button>
      </el-tab-pane>

      <el-tab-pane v-if="isPod" label="日志" name="logs" lazy>
        <LogViewer
          :cluster="props.cluster"
          :pod="props.name"
          :namespace="props.namespace"
          :containers="containers"
        />
      </el-tab-pane>

      <el-tab-pane v-if="isPod" label="终端" name="terminal" lazy>
        <TerminalPanel
          :cluster="props.cluster"
          :pod="props.name"
          :namespace="props.namespace"
          :containers="containers"
        />
      </el-tab-pane>

      <el-tab-pane v-if="hasRelated" label="关联资源" name="related" lazy>
        <RelatedResources
          :cluster="props.cluster"
          :kind="props.kind"
          :namespace="props.namespace"
          :obj="pod"
          :open="props.modelValue"
          @open-pod="nestedPod = $event"
        />
      </el-tab-pane>
    </el-tabs>

    <!-- 关联 Pod 详情(嵌套抽屉,复用本组件,自带日志/终端) -->
    <PodDetailDrawer
      v-if="nestedPod !== null"
      :model-value="true"
      :cluster="props.cluster"
      kind="pods"
      :name="nestedPod.name"
      :namespace="nestedPod.namespace ?? props.namespace"
      @update:model-value="(v: boolean) => !v && (nestedPod = null)"
    />
  </el-drawer>
</template>

<style scoped>
.section-title {
  font-size: 14px;
  font-weight: 600;
  margin: 12px 0 6px;
}
</style>

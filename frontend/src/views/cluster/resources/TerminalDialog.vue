<script setup lang="ts">
import { ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { getResource, listResources } from '@/api/workload';
import type { K8sObject, ResourceItem } from '@/api/types';
import TerminalPanel from '@/components/TerminalPanel.vue';
import { humanizeError } from '@/utils/errorMessages';

/**
 * 行级 exec 终端 Dialog:pods 直接连;deployments/statefulsets/daemonsets
 * 先按 spec.selector.matchLabels 找第一个 Running Pod 再连。
 * 用法:<TerminalDialog v-model="open" cluster="c1" kind="deployments" name="app" namespace="default" />
 */
const props = defineProps<{
  modelValue: boolean;
  cluster: string;
  kind: string;
  name: string;
  namespace?: string;
}>();

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const WORKLOADS = ['deployments', 'statefulsets', 'daemonsets'];

const pod = ref('');
const containers = ref<string[]>([]);
const loading = ref(false);
const errorMsg = ref('');

function containersOf(obj: K8sObject): string[] {
  const spec = (obj['spec'] ?? {}) as {
    containers?: { name?: string }[];
  };
  return (spec.containers ?? [])
    .map((c) => c.name ?? '')
    .filter((n) => n !== '');
}

async function resolvePod(): Promise<string> {
  if (!WORKLOADS.includes(props.kind)) return props.name;
  const detail = await getResource(props.cluster, props.kind, props.name, props.namespace);
  const spec = (detail['spec'] ?? {}) as {
    selector?: { matchLabels?: Record<string, string> };
  };
  const labels = spec.selector?.matchLabels ?? { app: props.name };
  const labelSelector = Object.entries(labels)
    .map(([k, v]) => `${k}=${v}`)
    .join(',');
  let res = await listResources(props.cluster, 'pods', {
    namespace: props.namespace,
    labelSelector,
    page: 1,
    size: 100,
  });
  if (res.items.length === 0) {
    // matchLabels 查不到时回退按名称模糊匹配
    res = await listResources(props.cluster, 'pods', {
      namespace: props.namespace,
      keyword: props.name,
      page: 1,
      size: 100,
    });
  }
  const running = res.items.find(
    (it: ResourceItem) => it.status === 'Running' || it.status === '',
  );
  if (running === undefined) return '';
  return running.name;
}

async function load(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  pod.value = '';
  containers.value = [];
  try {
    const podName = await resolvePod();
    if (podName === '') {
      ElMessage.warning('该工作负载当前没有运行中的 Pod');
      emit('update:modelValue', false);
      return;
    }
    pod.value = podName;
    const detail = await getResource(props.cluster, 'pods', podName, props.namespace);
    containers.value = containersOf(detail);
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.modelValue,
  (v) => {
    if (v) void load();
  },
);
</script>

<template>
  <el-dialog
    :model-value="props.modelValue"
    :title="`终端 · ${pod || props.name}${props.namespace ? ` (${props.namespace})` : ''}`"
    width="85vw"
    top="4vh"
    class="terminal-dialog"
    append-to-body
    destroy-on-close
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div v-if="errorMsg" class="error-tip">{{ errorMsg }}</div>
    <div v-loading="loading" class="term-wrap">
      <TerminalPanel
        v-if="!loading && errorMsg === '' && pod !== ''"
        :cluster="props.cluster"
        :pod="pod"
        :namespace="props.namespace ?? 'default'"
        :containers="containers"
      />
    </div>
  </el-dialog>
</template>

<style scoped>
/* 大尺寸自适应:dialog 高约 86vh(4vh top + header),终端区域占满剩余空间 */
.term-wrap {
  height: calc(86vh - 120px);
  min-height: 400px;
  display: flex;
  flex-direction: column;
}
.term-wrap > :deep(*) {
  flex: 1;
}
.error-tip {
  color: var(--danger);
  margin-bottom: 8px;
}
</style>

<style>
/* append-to-body 下 dialog 根节点在组件作用域外,需全局样式控制宽度 */
.terminal-dialog {
  max-width: 1600px;
}
</style>

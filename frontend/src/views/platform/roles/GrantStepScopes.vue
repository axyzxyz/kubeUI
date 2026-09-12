<script setup lang="ts">
import { reactive } from 'vue';
import { listResources } from '@/api/workload';
import type { ClusterInfo } from '@/api/types';
import { ALL_CLUSTERS, ALL_NAMESPACES } from './grant';
import type { ScopeRow } from './grant';

const props = defineProps<{
  scopes: ScopeRow[];
  clusters: ClusterInfo[];
}>();

const emit = defineEmits<{
  add: [];
  remove: [index: number];
  'cluster-change': [index: number, cluster: string];
  'namespace-change': [index: number, namespace: string];
}>();

// 命名空间按集群懒加载缓存(仅本组件局部,避免污染 clusterStore 当前集群)
const nsCache = reactive(new Map<string, string[]>());
const nsLoadingMap = reactive<Record<string, boolean>>({});

async function fetchNamespaces(cluster: string): Promise<void> {
  if (cluster === '' || cluster === ALL_CLUSTERS || nsCache.has(cluster)) return;
  nsLoadingMap[cluster] = true;
  try {
    const res = await listResources(cluster, 'namespaces', { page: 1, size: 500 });
    nsCache.set(
      cluster,
      res.items.map((it) => it.name),
    );
  } catch {
    nsCache.set(cluster, []);
  } finally {
    nsLoadingMap[cluster] = false;
  }
}

function nsOptions(cluster: string): string[] {
  return nsCache.get(cluster) ?? [];
}

function nsLoading(cluster: string): boolean {
  return nsLoadingMap[cluster] === true;
}

function onClusterChange(index: number, cluster: string): void {
  emit('cluster-change', index, cluster);
  if (cluster !== '' && cluster !== ALL_CLUSTERS) {
    void fetchNamespaces(cluster);
  }
}
</script>

<template>
  <div class="step-block">
    <div class="scope-head">
      <div class="step-title">③ 生效范围(cluster / namespace,* 表示全部)</div>
      <el-button size="small" @click="emit('add')">+ 添加范围</el-button>
    </div>
    <div v-for="(row, idx) in props.scopes" :key="idx" class="scope-row">
      <el-select
        :model-value="row.cluster"
        placeholder="选择集群"
        style="width: 220px"
        @update:model-value="(v: string) => onClusterChange(idx, v)"
      >
        <el-option label="全部集群" :value="ALL_CLUSTERS" />
        <el-option v-for="c in props.clusters" :key="c.name" :label="c.name" :value="c.name" />
      </el-select>
      <el-select
        :model-value="row.namespace"
        :placeholder="row.cluster === ALL_CLUSTERS ? '所有命名空间' : '选择命名空间'"
        :loading="nsLoading(row.cluster)"
        :disabled="row.cluster === '' || row.cluster === ALL_CLUSTERS"
        style="width: 220px"
        @update:model-value="(v: string) => emit('namespace-change', idx, v)"
      >
        <el-option label="所有命名空间" :value="ALL_NAMESPACES" />
        <el-option v-for="n in nsOptions(row.cluster)" :key="n" :label="n" :value="n" />
      </el-select>
      <el-button link type="danger" @click="emit('remove', idx)">移除</el-button>
    </div>
  </div>
</template>

<style scoped>
.step-block {
  margin-bottom: 8px;
}
.scope-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 10px;
}
.step-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-1);
}
.scope-row {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 8px;
}
</style>

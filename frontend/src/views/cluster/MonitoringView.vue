<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { getNodeMetrics, getPodMetrics } from '@/api/metrics';
import type { ContainerStat, NodeMetrics, PodMetrics } from '@/api/types';
import MetricBar from '@/components/MetricBar.vue';
import { formatBytes, formatCpu, parseCpu, parseMemBytes, parsePct } from '@/utils/format';
import { humanizeError } from '@/utils/errorMessages';

const route = useRoute();
const clusterName = computed(() => String(route.params.cluster));

const nodeMetrics = ref<NodeMetrics[]>([]);
const podMetrics = ref<PodMetrics[]>([]);
const loading = ref(false);
const errorMsg = ref('');
let timer: number | null = null;

/** Pod 指标按容器展平展示(后端返回 {namespace,name,containers:[]}) */
interface PodMetricRow extends ContainerStat {
  namespace: string;
  pod: string;
}

const podRows = computed<PodMetricRow[]>(() =>
  podMetrics.value.flatMap((p) =>
    (p.containers ?? []).map((c) => ({ ...c, namespace: p.namespace, pod: p.name })),
  ),
);

const sortedPodRows = computed(() =>
  [...podRows.value].sort((a, b) => parsePct(b.memory) - parsePct(a.memory)).slice(0, 200),
);

// 容器 Top 表前端分页(metrics 接口不分页,快照全量返回)
const page = ref(1);
const size = ref(20);
const pagedPodRows = computed(() =>
  sortedPodRows.value.slice((page.value - 1) * size.value, page.value * size.value),
);

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const [nodes, pods] = await Promise.all([
      getNodeMetrics(clusterName.value),
      getPodMetrics(clusterName.value),
    ]);
    nodeMetrics.value = nodes.items;
    podMetrics.value = pods.items;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void refresh();
  timer = window.setInterval(() => {
    void refresh();
  }, 30_000);
});

onUnmounted(() => {
  if (timer !== null) window.clearInterval(timer);
});
</script>

<template>
  <div class="page">
    <div class="head">
      <h1 class="page-title">监控 · metrics-server 快照</h1>
      <el-button size="small" @click="refresh">刷新</el-button>
    </div>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />

    <div class="section-title">Node 使用率</div>
    <el-table v-loading="loading" :data="nodeMetrics" class="table-compact" row-key="name">
      <el-table-column prop="name" label="节点" min-width="160" />
      <el-table-column label="CPU" min-width="220">
        <template #default="{ row }">
          <MetricBar :percent="parsePct(row.cpuPct)" :label="formatCpu(parseCpu(row.cpu))" />
        </template>
      </el-table-column>
      <el-table-column label="内存" min-width="220">
        <template #default="{ row }">
          <MetricBar :percent="parsePct(row.memPct)" :label="formatBytes(parseMemBytes(row.memory))" />
        </template>
      </el-table-column>
    </el-table>

    <div class="section-title">容器用量 Top(按内存排序)</div>
    <el-table
      v-loading="loading"
      :data="pagedPodRows"
      class="table-compact"
      row-key="pod"
    >
      <el-table-column prop="namespace" label="NS" width="130" />
      <el-table-column prop="pod" label="Pod" min-width="200" />
      <el-table-column prop="name" label="容器" min-width="140" />
      <el-table-column label="CPU" width="110">
        <template #default="{ row }">{{ formatCpu(parseCpu(row.cpu)) }}</template>
      </el-table-column>
      <el-table-column label="内存" width="110">
        <template #default="{ row }">{{ formatBytes(parseMemBytes(row.memory)) }}</template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="sortedPodRows.length"
      :page-size="size"
      style="margin-top: 12px"
    />
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.section-title {
  font-size: 16px;
  font-weight: 600;
  margin: 16px 0 8px;
}
</style>

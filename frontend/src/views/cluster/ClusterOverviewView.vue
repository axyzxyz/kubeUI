<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { getClusterStatus } from '@/api/cluster';
import { listEvents } from '@/api/event';
import type { ClusterStatusData, EventItem } from '@/api/types';
import RelativeTime from '@/components/RelativeTime.vue';
import StatusBadge from '@/components/StatusBadge.vue';
import { useClusterStore } from '@/stores/clusterStore';
import { humanizeError } from '@/utils/errorMessages';

const route = useRoute();
const router = useRouter();
const clusterStore = useClusterStore();

const clusterName = computed(() => String(route.params.cluster));

const ACCESS_MODE_LABELS: Record<string, string> = { direct: '直连', agent: 'Agent 接入' };

const accessModeText = computed(() => {
  const mode = clusterStore.current?.accessMode;
  return mode === undefined ? '—' : (ACCESS_MODE_LABELS[mode] ?? mode);
});
const status = ref<ClusterStatusData | null>(null);
const warningEvents = ref<EventItem[]>([]);
const errorMsg = ref('');
const loading = ref(false);

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    status.value = await getClusterStatus(clusterName.value);
    const ev = await listEvents(clusterName.value, { page: 1, size: 8 });
    warningEvents.value = ev.items.filter((e) => e.type === 'Warning');
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function goDeployments(): void {
  void router.push(`/clusters/${clusterName.value}/resources/deployments`);
}

onMounted(() => {
  if (clusterStore.currentName !== clusterName.value) {
    clusterStore.setCurrent(clusterName.value);
  }
  void refresh();
});
</script>

<template>
  <div class="page">
    <h1 class="page-title">集群总览 · {{ clusterName }}</h1>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      show-icon
      style="margin-bottom: 12px"
    />

    <div v-loading="loading" class="cards">
      <div class="card">
        <div class="card-label">状态</div>
        <StatusBadge v-if="status" :status="status.status" />
        <span v-else>—</span>
      </div>
      <div class="card">
        <div class="card-label">K8s 版本</div>
        <span class="num">{{ status?.version ?? '—' }}</span>
      </div>
      <div class="card">
        <div class="card-label">接入方式</div>
        <span>{{ accessModeText }}</span>
      </div>
      <div class="card">
        <div class="card-label">状态变更</div>
        <RelativeTime v-if="status" :value="status.lastTransitionTime" />
        <span v-else>—</span>
      </div>
      <div class="card clickable" @click="router.push(`/clusters/${clusterName}/events`)">
        <div class="card-label">Warning 事件</div>
        <span class="num warn">{{ warningEvents.length }}</span>
      </div>
      <div class="card clickable" @click="goDeployments">
        <div class="card-label">工作负载</div>
        <span>前往 →</span>
      </div>
    </div>

    <div class="section">
      <div class="section-title">最近 Warning 事件</div>
      <el-table
        v-loading="loading"
        :data="warningEvents"
        class="table-compact"
        row-key="name"
        size="small"
      >
        <el-table-column label="最近时间" width="100">
          <template #default="{ row }"><RelativeTime :value="row.lastTimestamp" /></template>
        </el-table-column>
        <el-table-column prop="reason" label="原因" width="140" />
        <el-table-column label="对象" width="200">
          <template #default="{ row }">{{ row.involvedKind ?? "—" }}/{{ row.involvedName ?? "—" }}</template>
        </el-table-column>
        <el-table-column prop="message" label="消息" min-width="300" show-overflow-tooltip />
        <el-table-column prop="count" label="次数" width="70" />
      </el-table>
    </div>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 12px;
  margin-bottom: 24px;
}
.card {
  background: var(--bg-surface);
  border: 1px solid var(--border-1);
  border-radius: 8px;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 16px;
}
.card-label {
  font-size: 12px;
  color: var(--text-3);
}
.clickable {
  cursor: pointer;
}
.warn {
  color: var(--warning);
}
.section-title {
  font-size: 16px;
  font-weight: 600;
  margin: 12px 0;
}
</style>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { useResourceWatch } from '@/composables/useResourceWatch';
import { useEventStore } from '@/stores/eventStore';
import { useClusterStore } from '@/stores/clusterStore';
import { eventItemOf } from '@/api/workload';
import RelativeTime from '@/components/RelativeTime.vue';
import StatusBadge from '@/components/StatusBadge.vue';
import type { WsEvent } from '@/api/ws';

const route = useRoute();
const clusterStore = useClusterStore();
const eventStore = useEventStore();

const clusterName = computed(() => String(route.params.cluster));
const page = ref(1);
const size = ref(20);
const onlyWarning = ref(false);

const filtered = computed(() =>
  onlyWarning.value ? eventStore.events.filter((e) => e.type === 'Warning') : eventStore.events,
);

function onWatchEvent(ev: WsEvent): void {
  if (ev.action === 'deleted') {
    eventStore.applyDelete(ev.name);
  } else {
    eventStore.applyUpsert(eventItemOf(ev.object));
  }
}

useResourceWatch(() => ({
  cluster: clusterName.value,
  resource: 'events',
  namespace: clusterStore.namespace,
  onEvent: onWatchEvent,
}));

async function refresh(): Promise<void> {
  await eventStore.fetchEvents(clusterName.value, clusterStore.namespace, page.value, size.value);
}

onMounted(refresh);
</script>

<template>
  <div class="page">
    <h1 class="page-title">事件</h1>

    <div class="filters">
      <el-checkbox v-model="onlyWarning" @change="refresh">仅 Warning</el-checkbox>
      <el-button size="small" @click="refresh">刷新</el-button>
      <el-tag size="small" type="info">WS 实时流: 已订阅</el-tag>
    </div>

    <el-alert
      v-if="eventStore.error"
      type="error"
      :closable="false"
      :title="eventStore.error"
      style="margin-bottom: 12px"
    />

    <el-table v-loading="eventStore.loading" :data="filtered" class="table-compact" row-key="name">
      <el-table-column label="级别" width="110">
        <template #default="{ row }"><StatusBadge :status="row.type" /></template>
      </el-table-column>
      <el-table-column label="最近时间" width="100">
        <template #default="{ row }"><RelativeTime :value="row.lastTimestamp" /></template>
      </el-table-column>
      <el-table-column prop="reason" label="原因" width="140" />
      <el-table-column label="对象" width="220">
        <template #default="{ row }">{{ row.involvedKind ?? "—" }}/{{ row.involvedName ?? "—" }}</template>
      </el-table-column>
      <el-table-column prop="message" label="消息" min-width="320" show-overflow-tooltip />
      <el-table-column prop="count" label="次数" width="70" />
      <el-table-column prop="namespace" label="NS" width="110" />
    </el-table>
    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="eventStore.total"
      :page-size="size"
      style="margin-top: 12px"
      @current-change="refresh"
    />
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 12px;
}
</style>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { listAuditLogs } from '@/api/audit';
import type { AuditLog } from '@/api/types';
import RelativeTime from '@/components/RelativeTime.vue';
import { useClusterStore } from '@/stores/clusterStore';
import { usePagination } from '@/composables/usePagination';
import { humanizeError } from '@/utils/errorMessages';

const clusterStore = useClusterStore();
const { page, size, total } = usePagination(20);

const items = ref<AuditLog[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const filters = ref({ username: '', cluster: '', action: '' });

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await listAuditLogs({ ...filters.value, page: page.value, size: size.value });
    items.value = res.items;
    total.value = res.total;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function onSearch(): void {
  page.value = 1;
  void refresh();
}

onMounted(refresh);
</script>

<template>
  <div class="page">
    <h1 class="page-title">审计日志</h1>

    <div class="filters">
      <el-input
        v-model="filters.username"
        placeholder="用户"
        size="small"
        style="width: 140px"
        clearable
        @change="onSearch"
      />
      <el-input
        v-model="filters.cluster"
        placeholder="集群"
        size="small"
        style="width: 140px"
        clearable
        @change="onSearch"
      />
      <el-input
        v-model="filters.action"
        placeholder="操作(如 delete)"
        size="small"
        style="width: 160px"
        clearable
        @change="onSearch"
      />
      <el-button size="small" type="primary" @click="onSearch">查询</el-button>
      <el-tag size="small" type="info">当前集群: {{ clusterStore.currentName || '—' }}</el-tag>
    </div>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin: 12px 0"
    />

    <el-table v-loading="loading" :data="items" class="table-compact" row-key="requestId">
      <el-table-column label="时间" width="170">
        <template #default="{ row }">
          <RelativeTime :value="row.createdAt" />
        </template>
      </el-table-column>
      <el-table-column prop="username" label="用户" width="120" />
      <el-table-column prop="action" label="操作" width="120" />
      <el-table-column prop="resource" label="资源类型" width="120" />
      <el-table-column prop="cluster" label="集群" width="120" />
      <el-table-column prop="namespace" label="NS" width="110" />
      <el-table-column prop="name" label="名称" min-width="140" />
      <el-table-column prop="sourceIp" label="来源 IP" width="130" />
      <el-table-column label="结果" width="80">
        <template #default="{ row }">
          <el-tag :type="row.result === 'allow' ? 'success' : 'danger'" size="small">{{
            row.result
          }}</el-tag>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="total"
      :page-size="size"
      style="margin-top: 12px"
      @current-change="refresh"
    />
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
</style>

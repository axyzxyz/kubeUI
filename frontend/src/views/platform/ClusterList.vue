<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { deleteCluster } from '@/api/cluster';
import { listResources } from '@/api/workload';
import type { ClusterInfo } from '@/api/types';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import EmptyState from '@/components/EmptyState.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import StatusBadge from '@/components/StatusBadge.vue';
import { useAuthStore } from '@/stores/authStore';
import { useClusterStore } from '@/stores/clusterStore';

const router = useRouter();
const clusterStore = useClusterStore();
const auth = useAuthStore();

const pendingDelete = ref<ClusterInfo | null>(null);
const deleting = ref(false);

// 前端分页(集群列表接口不分页,集群数量通常有限)
const page = ref(1);
const size = ref(10);

const pagedClusters = computed(() =>
  clusterStore.clusters.slice((page.value - 1) * size.value, page.value * size.value),
);

/** accessMode 中文映射 */
const ACCESS_MODE_LABELS: Record<string, string> = { direct: '直连', agent: 'Agent 接入' };

function accessModeLabel(mode: string): string {
  return ACCESS_MODE_LABELS[mode] ?? mode;
}

async function refresh(): Promise<void> {
  await clusterStore.fetchClusters();
  await fillNodeCounts();
}

/** 后端列表 DTO 的 nodeCount 为 omitempty 可能缺省,用 nodes 列表 total 兜底填充 */
async function fillNodeCounts(): Promise<void> {
  for (const c of clusterStore.clusters) {
    if (c.nodeCount !== undefined) continue;
    try {
      const res = await listResources(c.name, 'nodes', { page: 1, size: 1 });
      c.nodeCount = res.total;
    } catch {
      // 节点数获取失败保持 "—"
    }
  }
}

async function onConfirmDelete(): Promise<void> {
  if (pendingDelete.value === null) return;
  deleting.value = true;
  try {
    await deleteCluster(pendingDelete.value.name);
    pendingDelete.value = null;
    await refresh();
  } finally {
    deleting.value = false;
  }
}

onMounted(refresh);
</script>

<template>
  <div class="page">
    <div class="head">
      <h1 class="page-title">集群列表</h1>
      <el-button
        v-if="auth.isAdmin"
        type="primary"
        @click="router.push('/clusters/register')"
      >
        + 注册集群
      </el-button>
    </div>

    <el-alert
      v-if="clusterStore.error"
      type="error"
      :closable="false"
      :title="clusterStore.error"
      show-icon
      style="margin-bottom: 12px"
    />

    <EmptyState
      v-if="!clusterStore.loading && clusterStore.clusters.length === 0"
      title="还没有注册任何集群"
      description="注册你的第一个集群,开始统一管理"
    >
      <el-button
        v-if="auth.isAdmin"
        type="primary"
        @click="router.push('/clusters/register')"
      >
        注册集群
      </el-button>
    </EmptyState>

    <el-table
      v-else
      v-loading="clusterStore.loading"
      :data="pagedClusters"
      class="table-compact"
      row-key="name"
    >
      <el-table-column label="名称" min-width="160">
        <template #default="{ row }">
          <el-link type="primary" @click="router.push(`/clusters/${row.name}/overview`)">
            {{ row.name }}
          </el-link>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="140">
        <template #default="{ row }">
          <StatusBadge :status="row.status" />
        </template>
      </el-table-column>
      <el-table-column prop="version" label="版本" width="110" />
      <el-table-column label="节点数" width="90">
        <template #default="{ row }">
          <span class="num">{{ row.nodeCount ?? '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="接入方式" width="110">
        <template #default="{ row }">{{ accessModeLabel(row.accessMode) }}</template>
      </el-table-column>
      <el-table-column label="最近心跳" width="110">
        <template #default="{ row }">
          <RelativeTime :value="row.updatedAt" />
        </template>
      </el-table-column>
      <el-table-column label="注册时间" width="110">
        <template #default="{ row }">
          <RelativeTime :value="row.createdAt" />
        </template>
      </el-table-column>
      <el-table-column v-if="auth.isAdmin" label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="danger" @click="pendingDelete = row">注销</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="clusterStore.clusters.length"
      :page-size="size"
      style="margin-top: 12px"
    />

    <DangerConfirmDialog
      :model-value="pendingDelete !== null"
      title="注销集群注册(L3 危险操作)"
      :confirm-keyword="pendingDelete?.name ?? ''"
      :impact-list="['平台将停止管理该集群', 'Agent 隧道与订阅将断开', '该集群审计记录保留']"
      confirm-label="注销"
      @update:model-value="
        (v: boolean) => {
          if (!v) pendingDelete = null;
        }
      "
      @confirm="onConfirmDelete"
    />
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>

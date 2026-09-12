<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { ElMessage } from 'element-plus';
import {
  deleteResource,
  isClusterScoped,
  listResources,
  type ResourceKind,
} from '@/api/workload';
import type { ResourceItem } from '@/api/types';
import EmptyState from '@/components/EmptyState.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import StatusBadge from '@/components/StatusBadge.vue';
import {
  supportsBatchRestart,
  useBatchActions,
  type TableLike,
} from '@/composables/useBatchActions';
import { usePerms } from '@/composables/usePerms';
import { useResourceListSync } from '@/composables/useResourceListSync';
import { useResourceWatch } from '@/composables/useResourceWatch';
import { useClusterStore } from '@/stores/clusterStore';
import { humanizeError } from '@/utils/errorMessages';
import BatchActionBar from './BatchActionBar.vue';
import ResourceListDialogs from './ResourceListDialogs.vue';
import ResourceListFilters from './ResourceListFilters.vue';
import ResourceRowActions from './ResourceRowActions.vue';

const route = useRoute();
const clusterStore = useClusterStore();

const clusterName = computed(() => String(route.params.cluster));
const kind = computed(() => String(route.params.kind) as ResourceKind);
const namespaceScoped = computed(() => !isClusterScoped(kind.value));

const items = ref<ResourceItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const page = ref(1);
const size = ref(20);
const total = ref(0);
const keyword = ref('');
const labelSelector = ref('');
const ns = ref('');

// ---------- 权限(集群+命名空间维度有效权限;admin 全放行) ----------
const { can } = usePerms(clusterName, ns);
const canWrite = computed(() => can('resources:write'));
const canRead = computed(() => can('resources:read'));
const canTerminal = computed(() => can('terminal:use'));

const detailName = ref('');
const detailNs = ref('');
const yamlName = ref('');
const yamlNs = ref('');
const deleteTarget = ref<ResourceItem | null>(null);
const scaleTarget = ref<ResourceItem | null>(null);
const restartTarget = ref<ResourceItem | null>(null);
const createOpen = ref(false);
const terminalTarget = ref<ResourceItem | null>(null);

// ---------- 批量操作 ----------
const tableRef = ref<TableLike | null>(null);
/** 前向引用:removeRows 实现在下方 useResourceListSync,删除回调仅运行期触发 */
let removeRows: (rows: ResourceItem[]) => void = () => undefined;
const { selected, batchRunning, onSelectionChange, batchDelete, batchRestart } = useBatchActions(
  () => clusterName.value,
  () => kind.value,
  tableRef,
  refresh,
  (rows) => removeRows(rows),
);
const selectedCount = computed(() => selected.value.length);
const batchRestartable = computed(() => supportsBatchRestart(kind.value));

function rowNamespace(row: ResourceItem): string {
  return row.namespace ?? '—';
}

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await listResources(clusterName.value, kind.value, {
      namespace: ns.value === '' ? undefined : ns.value,
      labelSelector: labelSelector.value === '' ? undefined : labelSelector.value,
      // keyword 由后端对 name 做 contains 模糊过滤(内存分页前)
      keyword: keyword.value === '' ? undefined : keyword.value,
      page: page.value,
      size: size.value,
    });
    items.value = res.items;
    total.value = res.total;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

/**
 * 乐观移除行(双保险,不依赖 WS deleted 事件)与 WS 事件对账逻辑在
 * useResourceListSync 中,这里仅接线。
 */
const listSync = useResourceListSync({ items, selected, total, page });
removeRows = listSync.removeRows;

useResourceWatch(() => ({
  cluster: clusterName.value,
  resource: kind.value,
  namespace: ns.value,
  onEvent: listSync.onWatchEvent,
}));

/** 行级操作必须携带该行的 namespace(全部命名空间视图下行 namespace 可能与当前筛选不同) */
function openDetail(row: ResourceItem): void {
  detailName.value = row.name;
  detailNs.value = row.namespace ?? ns.value;
}

function openYaml(row: ResourceItem): void {
  yamlName.value = row.name;
  yamlNs.value = row.namespace ?? ns.value;
}

async function onDelete(): Promise<void> {
  const target = deleteTarget.value;
  if (target === null) return;
  try {
    await deleteResource(clusterName.value, kind.value, target.name, target.namespace);
  } catch (e) {
    // 失败必须显式提示:确认弹窗本身会先弹成功 toast,静默失败会表现为"行不消失"
    ElMessage.error(humanizeError(e));
    return;
  }
  deleteTarget.value = null;
  removeRows([target]);
  await refresh();
}

watch([kind, ns], () => {
  page.value = 1;
  void refresh();
});

onMounted(() => {
  ns.value = namespaceScoped.value ? clusterStore.namespace : '';
  void refresh();
});
</script>

<template>
  <div class="page">
    <div class="head">
      <h1 class="page-title">{{ kind }} 列表</h1>
    </div>

    <el-alert
      v-if="!canRead"
      type="warning"
      :closable="false"
      title="权限不足:resources:read,当前页面为只读模式"
      show-icon
      style="margin-bottom: 12px"
    />

    <ResourceListFilters
      v-model:keyword="keyword"
      v-model:label-selector="labelSelector"
      v-model:ns="ns"
      :namespace-scoped="namespaceScoped"
      :namespaces="clusterStore.namespaces"
      :can-write="canWrite"
      @search="refresh"
      @create="createOpen = true"
    />

    <BatchActionBar
      v-if="selectedCount > 0"
      :selected-count="selectedCount"
      :can-write="canWrite"
      :can-restart="batchRestartable"
      :running="batchRunning"
      @remove="batchDelete"
      @restart="batchRestart"
    />

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      show-icon
      style="margin-bottom: 12px"
    />

    <EmptyState v-if="!loading && items.length === 0 && errorMsg === ''" :title="`暂无 ${kind}`" />

    <el-table
      v-else
      ref="tableRef"
      v-loading="loading"
      :data="items"
      class="table-compact"
      row-key="name"
      @selection-change="onSelectionChange"
    >
      <el-table-column type="selection" width="42" />
      <el-table-column label="状态" width="130">
        <template #default="{ row }">
          <StatusBadge :status="row.status ?? '—'" />
        </template>
      </el-table-column>
      <el-table-column label="名称" min-width="200">
        <template #default="{ row }">
          <el-link type="primary" class="name-link" @click="openDetail(row)">
            {{ row.name }}
          </el-link>
        </template>
      </el-table-column>
      <el-table-column v-if="namespaceScoped" label="Namespace" width="130">
        <template #default="{ row }">{{ rowNamespace(row) }}</template>
      </el-table-column>
      <el-table-column label="AGE" width="90">
        <template #default="{ row }">
          <RelativeTime v-if="row.createdAt" :value="row.createdAt" />
          <span v-else>—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="300" fixed="right">
        <template #default="{ row }">
          <ResourceRowActions
            :kind="kind"
            :can-write="canWrite"
            :can-terminal="canTerminal"
            @detail="openDetail(row)"
            @scale="scaleTarget = row"
            @restart="restartTarget = row"
            @terminal="terminalTarget = row"
            @yaml="openYaml(row)"
            @remove="deleteTarget = row"
          />
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

    <ResourceListDialogs
      v-model:detail-name="detailName"
      v-model:yaml-name="yamlName"
      v-model:delete-target="deleteTarget"
      v-model:create-open="createOpen"
      v-model:terminal-target="terminalTarget"
      v-model:scale-target="scaleTarget"
      v-model:restart-target="restartTarget"
      :cluster="clusterName"
      :kind="kind"
      :ns="ns"
      :detail-ns="detailNs"
      :yaml-ns="yamlNs"
      @confirm-delete="onDelete"
      @refresh="refresh"
    />
  </div>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
}
.name-link {
  font-weight: 500;
}
.name-link :deep(.el-link--inner):hover {
  text-decoration: underline;
}
</style>

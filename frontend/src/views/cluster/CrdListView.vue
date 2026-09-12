<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { crdNamesOf, getResource, listResources } from '@/api/workload';
import type { ResourceItem } from '@/api/types';
import EmptyState from '@/components/EmptyState.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import { humanizeError } from '@/utils/errorMessages';

const route = useRoute();
const router = useRouter();

const clusterName = computed(() => String(route.params.cluster));

const items = ref<ResourceItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const keyword = ref('');
const resolving = ref('');

// 前端分页(一次取 200 条快照后本地分页)
const page = ref(1);
const size = ref(20);
const total = ref(0);

const pagedItems = computed(() =>
  items.value.slice((page.value - 1) * size.value, page.value * size.value),
);

async function refresh(): Promise<void> {
  page.value = 1;
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await listResources(clusterName.value, 'crds', {
      keyword: keyword.value === '' ? undefined : keyword.value,
      page: 1,
      size: 200,
      sortBy: 'name',
      order: 'asc',
    });
    items.value = res.items;
    total.value = res.total;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

/** 点击 CRD:取详情解析 spec.names.plural,跳转通用动态列表页 */
async function openCrd(row: ResourceItem): Promise<void> {
  resolving.value = row.name;
  try {
    const detail = await getResource(clusterName.value, 'crds', row.name);
    const names = crdNamesOf(detail);
    if (names.plural === '') {
      errorMsg.value = `无法解析 ${row.name} 的 plural 名称`;
      return;
    }
    await router.push(`/clusters/${clusterName.value}/resources/${names.plural}`);
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    resolving.value = '';
  }
}

watch([clusterName], () => void refresh());
onMounted(refresh);
</script>

<template>
  <div class="page">
    <h1 class="page-title">自定义资源 (CRD)</h1>
    <p class="hint">点击任一 CRD 进入其动态资源列表(经 discovery 解析 GVR)</p>

    <div class="filters">
      <el-input
        v-model="keyword"
        placeholder="搜索 CRD 名称"
        size="small"
        style="width: 220px"
        clearable
        @change="refresh"
      />
      <el-button size="small" type="primary" @click="refresh">刷新</el-button>
    </div>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      show-icon
      style="margin-bottom: 12px"
    />

    <EmptyState
      v-if="!loading && items.length === 0 && errorMsg === ''"
      title="该集群未安装任何 CRD"
    />

    <el-table v-else v-loading="loading" :data="pagedItems" class="table-compact" row-key="name">
      <el-table-column label="CRD 名称" min-width="260">
        <template #default="{ row }">{{ row.name }}</template>
      </el-table-column>
      <el-table-column prop="status" label=" Established" width="130" />
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          <RelativeTime v-if="row.createdAt" :value="row.createdAt" />
          <span v-else>—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="140" fixed="right">
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            :loading="resolving === row.name"
            @click="openCrd(row)"
          >
            浏览资源
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="total"
      :page-size="size"
      style="margin-top: 12px"
    />
  </div>
</template>

<style scoped>
.hint {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--text-3);
}
.filters {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}
</style>

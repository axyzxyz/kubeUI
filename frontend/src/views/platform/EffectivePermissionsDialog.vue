<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { getEffectivePermissions } from '@/api/rbac';
import { listResources } from '@/api/workload';
import type { PermissionPoint } from '@/api/types';
import { useClusterStore } from '@/stores/clusterStore';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  modelValue: boolean;
  username: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const clusterStore = useClusterStore();

const cluster = ref('');
const namespace = ref('');
const nsOptions = ref<string[]>([]);
const nsLoading = ref(false);
const loading = ref(false);
const errorMsg = ref('');
const permissions = ref<PermissionPoint[] | null>(null);
const queried = ref(false);

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
});

/** namespace 为空表示不限制(即该集群全部命名空间的并集语义,以后端为准) */
const namespaceParam = computed(() => (namespace.value === '' ? undefined : namespace.value));

async function loadNamespaces(): Promise<void> {
  if (cluster.value === '') {
    nsOptions.value = [];
    return;
  }
  nsLoading.value = true;
  try {
    const res = await listResources(cluster.value, 'namespaces', { page: 1, size: 500 });
    nsOptions.value = res.items.map((it) => it.name);
  } catch {
    nsOptions.value = [];
  } finally {
    nsLoading.value = false;
  }
}

function onClusterChange(): void {
  namespace.value = '';
  permissions.value = null;
  queried.value = false;
  void loadNamespaces();
}

async function onQuery(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await getEffectivePermissions(props.username, {
      cluster: cluster.value === '' ? undefined : cluster.value,
      namespace: namespaceParam.value,
    });
    permissions.value = res.permissions;
    queried.value = true;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

watch(visible, (v) => {
  if (!v) return;
  permissions.value = null;
  queried.value = false;
  errorMsg.value = '';
  namespace.value = '';
  if (clusterStore.clusters.length === 0) {
    void clusterStore.fetchClusters();
  }
  cluster.value = clusterStore.currentName;
  void loadNamespaces();
});
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="`有效权限:${props.username}`"
    width="560px"
    append-to-body
  >
    <el-form inline class="filter">
      <el-form-item label="集群">
        <el-select
          v-model="cluster"
          placeholder="全部集群"
          clearable
          style="width: 180px"
          @change="onClusterChange"
        >
          <el-option
            v-for="c in clusterStore.clusters"
            :key="c.name"
            :label="c.name"
            :value="c.name"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="命名空间">
        <el-select
          v-model="namespace"
          placeholder="全部"
          clearable
          filterable
          :loading="nsLoading"
          :disabled="cluster === ''"
          style="width: 180px"
        >
          <el-option v-for="n in nsOptions" :key="n" :label="n" :value="n" />
        </el-select>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="loading" @click="onQuery">查询</el-button>
      </el-form-item>
    </el-form>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />
    <div v-else-if="!queried" class="hint">选择 cluster / namespace 后点击查询</div>
    <div v-else v-loading="loading" class="perm-result">
      <template v-if="permissions !== null && permissions.length > 0">
        <el-tag
          v-for="p in permissions"
          :key="p"
          size="small"
          type="primary"
          class="perm-tag mono"
        >
          {{ p }}
        </el-tag>
      </template>
      <div v-else class="hint">该范围内无有效权限</div>
    </div>
  </el-dialog>
</template>

<style scoped>
.filter :deep(.el-form-item) {
  margin-bottom: 12px;
}
.perm-result {
  min-height: 48px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.perm-tag {
  font-size: 12px;
}
.hint {
  color: var(--text-3);
  font-size: 13px;
  padding: 12px 0;
}
</style>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import {
  deleteGrant,
  listGrants,
  listRoleGroups,
  listRoles,
  listUserGroups,
} from '@/api/rbac';
import { listUsers } from '@/api/user';
import type {
  GrantItem,
  GrantListQuery,
  GrantObjectType,
  GrantSubjectType,
  RoleGroupItem,
  RoleItem,
  Scope,
  UserGroupItem,
  UserItem,
} from '@/api/types';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  /** 父组件在授权弹窗保存后递增,触发本 Tab 刷新 */
  refreshKey: number;
}>();

const emit = defineEmits<{
  create: [];
}>();

const items = ref<GrantItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const deleteTarget = ref<GrantItem | null>(null);

const filterSubjectType = ref<'' | GrantSubjectType>('');
const filterSubjectId = ref<number | undefined>(undefined);
const filterObjectType = ref<'' | GrantObjectType>('');
const filterObjectId = ref<number | undefined>(undefined);

const users = ref<UserItem[]>([]);
const groups = ref<UserGroupItem[]>([]);
const roles = ref<RoleItem[]>([]);
const roleGroups = ref<RoleGroupItem[]>([]);

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    items.value = await listGrants(buildQuery());
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function buildQuery(): GrantListQuery {
  const query: GrantListQuery = {};
  if (filterSubjectType.value !== '') {
    query.subjectType = filterSubjectType.value;
    if (filterSubjectId.value !== undefined) query.subjectId = filterSubjectId.value;
  }
  if (filterObjectType.value !== '') {
    query.objectType = filterObjectType.value;
    if (filterObjectId.value !== undefined) query.objectId = filterObjectId.value;
  }
  return query;
}

async function loadSubjects(): Promise<void> {
  if (filterSubjectType.value === 'user') {
    const res = await listUsers(1, 200);
    users.value = res.items;
    return;
  }
  if (filterSubjectType.value === 'group') {
    groups.value = await listUserGroups();
  }
}

async function loadObjects(): Promise<void> {
  if (filterObjectType.value === 'role') {
    roles.value = await listRoles();
    return;
  }
  if (filterObjectType.value === 'role_group') {
    roleGroups.value = await listRoleGroups();
  }
}

function onSubjectTypeChange(): void {
  filterSubjectId.value = undefined;
  void loadSubjects().catch(() => {
    /* 下拉数据加载失败不阻塞列表,错误在 refresh 中展示 */
  });
  void refresh();
}

function onObjectTypeChange(): void {
  filterObjectId.value = undefined;
  void loadObjects().catch(() => {
    /* 下拉数据加载失败不阻塞列表 */
  });
  void refresh();
}

function scopeText(scope: Scope): string {
  const cluster = scope.cluster === '*' ? '全部集群' : scope.cluster;
  const ns = scope.namespace === '*' ? '所有命名空间' : scope.namespace;
  return `${cluster} / ${ns}`;
}

function scopesText(row: GrantItem): string {
  return row.scopes.map(scopeText).join('; ');
}

function subjectTypeText(v: GrantSubjectType): string {
  return v === 'user' ? '用户' : '用户组';
}

function objectTypeText(v: GrantObjectType): string {
  return v === 'role' ? '角色' : '角色组';
}

const deleteKeyword = computed(() => {
  if (deleteTarget.value === null) return '';
  const t = deleteTarget.value;
  return `${t.objectName}@${t.subjectName}`;
});

watch(
  () => props.refreshKey,
  () => {
    void refresh();
  },
);

async function onDelete(): Promise<void> {
  if (deleteTarget.value === null) return;
  try {
    await deleteGrant(deleteTarget.value.id);
    deleteTarget.value = null;
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
    deleteTarget.value = null;
  }
}

onMounted(refresh);
</script>

<template>
  <div>
    <div class="tab-head">
      <div class="filters">
        <el-select
          v-model="filterSubjectType"
          placeholder="主体类型"
          clearable
          style="width: 120px"
          @change="onSubjectTypeChange"
        >
          <el-option label="用户" value="user" />
          <el-option label="用户组" value="group" />
        </el-select>
        <el-select
          v-model="filterSubjectId"
          :disabled="filterSubjectType === ''"
          :placeholder="filterSubjectType === 'user' ? '选择用户' : '选择用户组'"
          filterable
          clearable
          style="width: 150px"
          @change="refresh"
        >
          <template v-if="filterSubjectType === 'user'">
            <el-option v-for="u in users" :key="u.id" :label="u.username" :value="u.id" />
          </template>
          <template v-else-if="filterSubjectType === 'group'">
            <el-option v-for="g in groups" :key="g.id" :label="g.name" :value="g.id" />
          </template>
        </el-select>
        <el-select
          v-model="filterObjectType"
          placeholder="对象类型"
          clearable
          style="width: 120px"
          @change="onObjectTypeChange"
        >
          <el-option label="角色" value="role" />
          <el-option label="角色组" value="role_group" />
        </el-select>
        <el-select
          v-model="filterObjectId"
          :disabled="filterObjectType === ''"
          :placeholder="filterObjectType === 'role' ? '选择角色' : '选择角色组'"
          filterable
          clearable
          style="width: 150px"
          @change="refresh"
        >
          <template v-if="filterObjectType === 'role'">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </template>
          <template v-else-if="filterObjectType === 'role_group'">
            <el-option v-for="g in roleGroups" :key="g.id" :label="g.name" :value="g.id" />
          </template>
        </el-select>
      </div>
      <el-button type="primary" @click="emit('create')">新建授权</el-button>
    </div>
    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />
    <el-table v-loading="loading" :data="items" class="table-compact" row-key="id">
      <el-table-column label="主体类型" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.subjectType === 'user' ? 'success' : 'warning'">
            {{ subjectTypeText(row.subjectType) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="subjectName" label="主体" min-width="130" />
      <el-table-column label="对象类型" width="100">
        <template #default="{ row }">
          <el-tag size="small" :type="row.objectType === 'role' ? 'primary' : 'info'">
            {{ objectTypeText(row.objectType) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="objectName" label="对象" min-width="130" />
      <el-table-column label="生效范围" min-width="260">
        <template #default="{ row }">
          <span class="mono scope-text">{{ scopesText(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="120">
        <template #default="{ row }">
          <RelativeTime :value="row.createdAt" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="danger" @click="deleteTarget = row">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <DangerConfirmDialog
      :model-value="deleteTarget !== null"
      title="删除授权"
      :confirm-keyword="deleteKeyword"
      :impact-list="['该主体将立即失去此对象在对应范围内的权限']"
      confirm-label="删除"
      @update:model-value="
        (v: boolean) => {
          if (!v) deleteTarget = null;
        }
      "
      @confirm="onDelete"
    />
  </div>
</template>

<style scoped>
.tab-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  gap: 12px;
}
.filters {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.scope-text {
  font-size: 12px;
}
</style>

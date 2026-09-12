<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { createGrant, listRoleGroups, listRoles, listUserGroups } from '@/api/rbac';
import { listUsers } from '@/api/user';
import type {
  GrantObjectType,
  GrantSubjectType,
  RoleGroupItem,
  RoleItem,
  Scope,
  UserGroupItem,
  UserItem,
} from '@/api/types';
import { useClusterStore } from '@/stores/clusterStore';
import { ALL_NAMESPACES } from './grant';
import type { GrantPreset, ScopeRow } from './grant';
import GrantStepObject from './GrantStepObject.vue';
import GrantStepScopes from './GrantStepScopes.vue';
import GrantStepSubject from './GrantStepSubject.vue';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  modelValue: boolean;
  preset: GrantPreset | null;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  saved: [];
}>();

const clusterStore = useClusterStore();

const roles = ref<RoleItem[]>([]);
const roleGroups = ref<RoleGroupItem[]>([]);
const users = ref<UserItem[]>([]);
const groups = ref<UserGroupItem[]>([]);
const loading = ref(false);
const saving = ref(false);
const errorMsg = ref('');

const subjectType = ref<GrantSubjectType>('user');
const subjectId = ref<number | undefined>(undefined);
const objectType = ref<GrantObjectType>('role');
const objectId = ref<number | undefined>(undefined);
const scopes = ref<ScopeRow[]>([]);

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
});

async function loadOptions(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const [roleItems, roleGroupItems, userRes, groupItems] = await Promise.all([
      listRoles(),
      listRoleGroups(),
      listUsers(1, 200),
      listUserGroups(),
    ]);
    roles.value = roleItems;
    roleGroups.value = roleGroupItems;
    users.value = userRes.items;
    groups.value = groupItems;
    if (clusterStore.clusters.length === 0) {
      await clusterStore.fetchClusters();
    }
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function applyPreset(): void {
  const p = props.preset;
  subjectType.value = p?.subjectType ?? 'user';
  subjectId.value = p?.subjectId;
  objectType.value = p?.objectType ?? 'role';
  objectId.value = p?.objectId;
  scopes.value = [{ cluster: '', namespace: '' }];
}

watch(visible, (v) => {
  if (v) {
    applyPreset();
    void loadOptions();
  }
});

function onSubjectTypeChange(v: GrantSubjectType): void {
  subjectType.value = v;
  subjectId.value = undefined;
}

function onObjectTypeChange(v: GrantObjectType): void {
  objectType.value = v;
  objectId.value = undefined;
}

function onClusterChange(index: number, cluster: string): void {
  const row = scopes.value[index];
  if (row === undefined) return;
  row.cluster = cluster;
  row.namespace = cluster === '*' ? ALL_NAMESPACES : '';
}

function onNamespaceChange(index: number, namespace: string): void {
  const row = scopes.value[index];
  if (row === undefined) return;
  row.namespace = namespace;
}

function addScope(): void {
  scopes.value.push({ cluster: '', namespace: '' });
}

function removeScope(index: number): void {
  scopes.value.splice(index, 1);
}

function buildScopes(): Scope[] | null {
  const cleanScopes: Scope[] = [];
  for (const row of scopes.value) {
    if (row.cluster === '') {
      errorMsg.value = '请为每条范围选择集群';
      return null;
    }
    if (row.cluster !== '*' && row.namespace === '') {
      errorMsg.value = '请为每条范围选择命名空间(或"所有命名空间")';
      return null;
    }
    cleanScopes.push({
      cluster: row.cluster,
      namespace: row.cluster === '*' ? ALL_NAMESPACES : row.namespace,
    });
  }
  if (cleanScopes.length === 0) {
    errorMsg.value = '至少添加一条生效范围';
    return null;
  }
  return cleanScopes;
}

async function onSave(): Promise<void> {
  if (subjectId.value === undefined) {
    errorMsg.value = '请选择授权主体';
    return;
  }
  if (objectId.value === undefined) {
    errorMsg.value = '请选择授权对象';
    return;
  }
  const cleanScopes = buildScopes();
  if (cleanScopes === null) return;
  saving.value = true;
  errorMsg.value = '';
  try {
    await createGrant({
      subjectType: subjectType.value,
      subjectId: subjectId.value,
      objectType: objectType.value,
      objectId: objectId.value,
      scopes: cleanScopes,
    });
    ElMessage.success('授权已创建');
    visible.value = false;
    emit('saved');
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <el-dialog v-model="visible" title="新建授权" width="640px" append-to-body>
    <div v-loading="loading">
      <el-alert
        v-if="errorMsg"
        type="error"
        :closable="false"
        :title="errorMsg"
        style="margin-bottom: 12px"
      />
      <GrantStepSubject
        :subject-type="subjectType"
        :subject-id="subjectId"
        :users="users"
        :groups="groups"
        @update:subject-type="onSubjectTypeChange"
        @update:subject-id="(v: number | undefined) => (subjectId = v)"
      />
      <GrantStepObject
        :object-type="objectType"
        :object-id="objectId"
        :roles="roles"
        :role-groups="roleGroups"
        @update:object-type="onObjectTypeChange"
        @update:object-id="(v: number | undefined) => (objectId = v)"
      />
      <GrantStepScopes
        :scopes="scopes"
        :clusters="clusterStore.clusters"
        @add="addScope"
        @remove="removeScope"
        @cluster-change="onClusterChange"
        @namespace-change="onNamespaceChange"
      />
    </div>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="onSave">创建</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { addRoleGroupRole, listRoleGroupRoles, listRoles, removeRoleGroupRole } from '@/api/rbac';
import type { RoleItem } from '@/api/types';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  modelValue: boolean;
  groupId: number;
  groupName: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const members = ref<RoleItem[]>([]);
const allRoles = ref<RoleItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const addRoleIds = ref<number[]>([]);
const adding = ref(false);

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
});

/** 仅展示尚未加入该角色组的角色 */
const candidateRoles = computed(() => {
  const ids = new Set(members.value.map((m) => m.id));
  return allRoles.value.filter((r) => !ids.has(r.id));
});

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const [memberItems, roleItems] = await Promise.all([
      listRoleGroupRoles(props.groupId),
      listRoles(),
    ]);
    members.value = memberItems;
    allRoles.value = roleItems;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

watch(visible, (v) => {
  if (v) {
    addRoleIds.value = [];
    void refresh();
  }
});

async function onAdd(): Promise<void> {
  if (addRoleIds.value.length === 0) return;
  adding.value = true;
  errorMsg.value = '';
  try {
    for (const roleId of addRoleIds.value) {
      await addRoleGroupRole(props.groupId, { roleId });
    }
    addRoleIds.value = [];
    ElMessage.success('已添加成员角色');
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    adding.value = false;
  }
}

async function onRemove(role: RoleItem): Promise<void> {
  try {
    await removeRoleGroupRole(props.groupId, role.id);
    ElMessage.success(`已移除 ${role.name}`);
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  }
}
</script>

<template>
  <el-drawer
    v-model="visible"
    :title="`成员角色:${props.groupName}`"
    size="480px"
    append-to-body
  >
    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />
    <div class="add-row">
      <el-select
        v-model="addRoleIds"
        placeholder="选择要添加的角色(可多选)"
        multiple
        filterable
        style="flex: 1"
      >
        <el-option v-for="r in candidateRoles" :key="r.id" :label="r.name" :value="r.id" />
      </el-select>
      <el-button type="primary" :loading="adding" :disabled="addRoleIds.length === 0" @click="onAdd">
        添加
      </el-button>
    </div>
    <el-table v-loading="loading" :data="members" class="table-compact" row-key="id">
      <el-table-column prop="name" label="角色名" min-width="140" />
      <el-table-column prop="description" label="描述" min-width="160" show-overflow-tooltip />
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="danger" @click="onRemove(row)">移除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-drawer>
</template>

<style scoped>
.add-row {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
</style>

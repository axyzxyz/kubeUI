<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { addGroupMember, listGroupMembers, removeGroupMember } from '@/api/rbac';
import { listUsers } from '@/api/user';
import type { UserItem } from '@/api/types';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  modelValue: boolean;
  groupId: number;
  groupName: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const members = ref<UserItem[]>([]);
const allUsers = ref<UserItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const addUserId = ref<number | undefined>(undefined);

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
});

/** 仅展示尚未加入该组的用户 */
const candidateUsers = computed(() => {
  const ids = new Set(members.value.map((m) => m.id));
  return allUsers.value.filter((u) => !ids.has(u.id));
});

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const [memberItems, userRes] = await Promise.all([
      listGroupMembers(props.groupId),
      listUsers(1, 200),
    ]);
    members.value = memberItems;
    allUsers.value = userRes.items;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

watch(visible, (v) => {
  if (v) {
    addUserId.value = undefined;
    void refresh();
  }
});

async function onAdd(): Promise<void> {
  if (addUserId.value === undefined) return;
  try {
    await addGroupMember(props.groupId, { userId: addUserId.value });
    addUserId.value = undefined;
    ElMessage.success('已添加成员');
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  }
}

async function onRemove(username: string, userId: number): Promise<void> {
  try {
    await removeGroupMember(props.groupId, userId);
    ElMessage.success(`已移除 ${username}`);
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  }
}
</script>

<template>
  <el-drawer
    v-model="visible"
    :title="`成员管理:${props.groupName}`"
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
        v-model="addUserId"
        placeholder="选择要添加的用户"
        filterable
        style="flex: 1"
      >
        <el-option v-for="u in candidateUsers" :key="u.id" :label="u.username" :value="u.id" />
      </el-select>
      <el-button type="primary" :disabled="addUserId === undefined" @click="onAdd">添加</el-button>
    </div>
    <el-table v-loading="loading" :data="members" class="table-compact" row-key="id">
      <el-table-column prop="username" label="用户名" min-width="140" />
      <el-table-column prop="role" label="角色" width="110" />
      <el-table-column label="操作" width="90" fixed="right">
        <template #default="{ row }">
          <el-button link type="danger" @click="onRemove(row.username, row.id)">移除</el-button>
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

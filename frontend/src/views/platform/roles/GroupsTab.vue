<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { createUserGroup, deleteUserGroup, listUserGroups, updateUserGroup } from '@/api/rbac';
import type { UserGroupItem } from '@/api/types';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import type { GrantPreset } from './grant';
import MemberDrawer from './MemberDrawer.vue';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  /** 父组件在授权弹窗保存后递增,触发本 Tab 刷新 */
  refreshKey: number;
}>();

const emit = defineEmits<{
  grant: [preset: GrantPreset];
}>();

const items = ref<UserGroupItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');

const dialogVisible = ref(false);
const editing = ref<UserGroupItem | null>(null);
const form = ref({ name: '', description: '' });
const saving = ref(false);
const deleteTarget = ref<UserGroupItem | null>(null);
const memberTarget = ref<UserGroupItem | null>(null);

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    items.value = await listUserGroups();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function openCreate(): void {
  editing.value = null;
  form.value = { name: '', description: '' };
  dialogVisible.value = true;
}

function openEdit(row: UserGroupItem): void {
  editing.value = row;
  form.value = { name: row.name, description: row.description };
  dialogVisible.value = true;
}

async function onSave(): Promise<void> {
  saving.value = true;
  errorMsg.value = '';
  try {
    const req = { ...form.value };
    if (editing.value === null) {
      await createUserGroup(req);
    } else {
      await updateUserGroup(editing.value.id, req);
    }
    dialogVisible.value = false;
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    saving.value = false;
  }
}

async function onDelete(): Promise<void> {
  if (deleteTarget.value === null) return;
  try {
    await deleteUserGroup(deleteTarget.value.id);
    deleteTarget.value = null;
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
    deleteTarget.value = null;
  }
}

watch(
  () => props.refreshKey,
  () => {
    void refresh();
  },
);

onMounted(refresh);
</script>

<template>
  <div>
    <div class="tab-head">
      <el-button type="primary" @click="openCreate">新建用户组</el-button>
    </div>
    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />
    <el-table v-loading="loading" :data="items" class="table-compact" row-key="id">
      <el-table-column prop="name" label="组名" min-width="160" />
      <el-table-column prop="description" label="描述" min-width="220" show-overflow-tooltip />
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="memberTarget = row">成员</el-button>
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button
            link
            type="primary"
            @click="
              emit('grant', { subjectType: 'group', subjectId: row.id, subjectName: row.name })
            "
          >
            授权
          </el-button>
          <el-button link type="danger" @click="deleteTarget = row">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="dialogVisible"
      :title="editing === null ? '新建用户组' : `编辑用户组:${editing.name}`"
      width="440px"
      append-to-body
    >
      <el-form label-width="80px">
        <el-form-item label="组名"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="form.name === ''" @click="onSave">
          保存
        </el-button>
      </template>
    </el-dialog>

    <DangerConfirmDialog
      :model-value="deleteTarget !== null"
      title="删除用户组"
      :confirm-keyword="deleteTarget?.name ?? ''"
      :impact-list="['该组的授权将一并失效', '组内成员关系将被解除', '操作不可恢复']"
      confirm-label="删除"
      @update:model-value="
        (v: boolean) => {
          if (!v) deleteTarget = null;
        }
      "
      @confirm="onDelete"
    />

    <MemberDrawer
      v-if="memberTarget !== null"
      :model-value="memberTarget !== null"
      :group-id="memberTarget.id"
      :group-name="memberTarget.name"
      @update:model-value="
        (v: boolean) => {
          if (!v) memberTarget = null;
        }
      "
    />
  </div>
</template>

<style scoped>
.tab-head {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}
</style>

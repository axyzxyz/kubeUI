<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { createRole, deleteRole, listRoles, updateRole, PERMISSION_POINTS } from '@/api/rbac';
import type { RoleItem } from '@/api/types';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import type { GrantPreset } from './grant';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  /** 父组件在授权弹窗保存后递增,触发本 Tab 刷新 */
  refreshKey: number;
}>();

const emit = defineEmits<{
  grant: [preset: GrantPreset];
}>();

const items = ref<RoleItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');

const dialogVisible = ref(false);
const editing = ref<RoleItem | null>(null);
const form = ref<{ name: string; description: string; permissions: string[] }>({
  name: '',
  description: '',
  permissions: [],
});
const saving = ref(false);
const deleteTarget = ref<RoleItem | null>(null);

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    items.value = await listRoles();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function openCreate(): void {
  editing.value = null;
  form.value = { name: '', description: '', permissions: [] };
  dialogVisible.value = true;
}

function openEdit(row: RoleItem): void {
  editing.value = row;
  form.value = {
    name: row.name,
    description: row.description,
    permissions: [...row.permissions],
  };
  dialogVisible.value = true;
}

async function onSave(): Promise<void> {
  saving.value = true;
  errorMsg.value = '';
  try {
    const req = { ...form.value };
    if (editing.value === null) {
      await createRole(req);
    } else {
      await updateRole(editing.value.id, req);
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
    await deleteRole(deleteTarget.value.id);
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
      <el-button type="primary" @click="openCreate">新建角色</el-button>
    </div>
    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />
    <el-table v-loading="loading" :data="items" class="table-compact" row-key="id">
      <el-table-column prop="name" label="角色名" min-width="140" />
      <el-table-column label="类型" width="100">
        <template #default="{ row }">
          <el-tag :type="row.builtin ? 'info' : 'primary'" size="small">
            {{ row.builtin ? '内置' : '自定义' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="权限点" width="100">
        <template #default="{ row }">
          <span class="num">{{ row.permissions.length }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <el-button
            link
            type="primary"
            @click="emit('grant', { objectType: 'role', objectId: row.id, objectName: row.name })"
          >
            授权
          </el-button>
          <el-button link type="primary" :disabled="row.builtin" @click="openEdit(row)">
            编辑
          </el-button>
          <el-button link type="danger" :disabled="row.builtin" @click="deleteTarget = row">
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="dialogVisible"
      :title="editing === null ? '新建角色' : `编辑角色:${editing.name}`"
      width="560px"
      append-to-body
    >
      <el-form label-width="80px">
        <el-form-item label="角色名">
          <el-input v-model="form.name" :disabled="editing !== null" />
        </el-form-item>
        <el-form-item label="描述"><el-input v-model="form.description" /></el-form-item>
        <el-form-item label="权限点">
          <el-checkbox-group v-model="form.permissions" class="perm-group">
            <el-checkbox v-for="p in PERMISSION_POINTS" :key="p" :value="p" class="perm-item">
              {{ p }}
            </el-checkbox>
          </el-checkbox-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="saving"
          :disabled="form.name === '' || form.permissions.length === 0"
          @click="onSave"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    <DangerConfirmDialog
      :model-value="deleteTarget !== null"
      title="删除角色"
      :confirm-keyword="deleteTarget?.name ?? ''"
      :impact-list="['关联的授权将一并失效', '操作不可恢复']"
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
  justify-content: flex-end;
  margin-bottom: 12px;
}
.perm-group {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 0 12px;
  width: 100%;
}
.perm-item {
  margin-right: 0;
}
</style>

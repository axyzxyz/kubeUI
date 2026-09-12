<script setup lang="ts">
import { onMounted, ref } from 'vue';
import {
  createUser,
  deleteUser,
  listUsers,
  resetUserPassword,
  setUserStatus,
} from '@/api/user';
import type { ResetPasswordResult, UserItem } from '@/api/types';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import EffectivePermissionsDialog from './EffectivePermissionsDialog.vue';
import { humanizeError } from '@/utils/errorMessages';

const items = ref<UserItem[]>([]);
const loading = ref(false);
const errorMsg = ref('');
const total = ref(0);
const page = ref(1);

const createVisible = ref(false);
const form = ref({ username: '', password: '', role: 'viewer' });
const disableTarget = ref<UserItem | null>(null);
const deleteTarget = ref<UserItem | null>(null);
const resetTarget = ref<UserItem | null>(null);
const resetResult = ref<ResetPasswordResult | null>(null);
const resetVisible = ref(false);
const permTarget = ref<UserItem | null>(null);

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await listUsers(page.value, 20);
    items.value = res.items;
    total.value = res.total;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

async function onCreate(): Promise<void> {
  await createUser({ ...form.value });
  createVisible.value = false;
  form.value = { username: '', password: '', role: 'viewer' };
  await refresh();
}

async function onDisable(): Promise<void> {
  if (disableTarget.value === null) return;
  await setUserStatus(disableTarget.value.username, {
    disabled: !disableTarget.value.disabled,
  });
  disableTarget.value = null;
  await refresh();
}

async function onReset(): Promise<void> {
  if (resetTarget.value === null) return;
  resetResult.value = await resetUserPassword(resetTarget.value.username);
  resetVisible.value = true;
  resetTarget.value = null;
}

async function copyPassword(): Promise<void> {
  if (resetResult.value !== null) {
    await navigator.clipboard.writeText(resetResult.value.password);
  }
}

async function onDelete(): Promise<void> {
  if (deleteTarget.value === null) return;
  await deleteUser(deleteTarget.value.username);
  deleteTarget.value = null;
  await refresh();
}

onMounted(refresh);
</script>

<template>
  <div class="page">
    <div class="head">
      <h1 class="page-title">用户管理</h1>
      <el-button type="primary" @click="createVisible = true">创建用户</el-button>
    </div>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      style="margin-bottom: 12px"
    />

    <el-table v-loading="loading" :data="items" class="table-compact" row-key="username">
      <el-table-column prop="username" label="用户名" min-width="140" />
      <el-table-column prop="role" label="角色" width="160" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.disabled ? 'danger' : 'success'" size="small">
            {{ row.disabled ? '禁用' : '启用' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">
          <RelativeTime :value="row.createdAt" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="permTarget = row">有效权限</el-button>
          <el-button link type="warning" @click="resetTarget = row">重置密码</el-button>
          <el-button
            v-if="row.username !== 'admin'"
            link
            type="danger"
            @click="deleteTarget = row"
            >删除</el-button
          >
          <el-button link type="danger" @click="disableTarget = row">
            {{ row.disabled ? '启用' : '禁用' }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="total"
      :page-size="20"
      style="margin-top: 12px"
      @current-change="refresh"
    />

    <el-dialog v-model="createVisible" title="创建用户" width="420px">
      <el-form label-width="80px">
        <el-form-item label="用户名"><el-input v-model="form.username" /></el-form-item>
        <el-form-item label="密码"
          ><el-input v-model="form.password" type="password" show-password
        /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="form.role" style="width: 100%">
            <el-option label="admin" value="admin" />
            <el-option label="operator" value="operator" />
            <el-option label="viewer" value="viewer" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button
          type="primary"
          :disabled="form.username === '' || form.password === ''"
          @click="onCreate"
        >
          创建
        </el-button>
      </template>
    </el-dialog>

    <DangerConfirmDialog
      :model-value="disableTarget !== null"
      title="禁用/启用用户"
      :confirm-keyword="disableTarget?.username ?? ''"
      :impact-list="['该用户所有会话将失效']"
      confirm-label="确认"
      @update:model-value="
        (v: boolean) => {
          if (!v) disableTarget = null;
        }
      "
      @confirm="onDisable"
    />

    <DangerConfirmDialog
      :model-value="deleteTarget !== null"
      title="删除用户"
      :confirm-keyword="deleteTarget?.username ?? ''"
      :impact-list="['用户记录及其全部会话将被永久删除', '操作不可恢复']"
      confirm-label="删除"
      @update:model-value="
        (v: boolean) => {
          if (!v) deleteTarget = null;
        }
      "
      @confirm="onDelete"
    />

    <DangerConfirmDialog
      :model-value="resetTarget !== null"
      title="重置密码"
      :confirm-keyword="resetTarget?.username ?? ''"
      :impact-list="['原密码立即失效', '该用户全部会话将被吊销']"
      confirm-label="重置"
      @update:model-value="
        (v: boolean) => {
          if (!v) resetTarget = null;
        }
      "
      @confirm="onReset"
    />

    <EffectivePermissionsDialog
      :model-value="permTarget !== null"
      :username="permTarget?.username ?? ''"
      @update:model-value="
        (v: boolean) => {
          if (!v) permTarget = null;
        }
      "
    />

    <el-dialog v-model="resetVisible" title="密码已重置(仅展示一次)" width="440px">
      <template v-if="resetResult">
        <el-alert
          type="warning"
          :closable="false"
          title="请将新密码告知用户,关闭弹窗后无法再次查看"
          style="margin-bottom: 12px"
        />
        <div class="row">
          <code class="mono">{{ resetResult.password }}</code>
          <el-button size="small" @click="copyPassword">复制</el-button>
        </div>
      </template>
      <template #footer>
        <el-button type="primary" @click="resetVisible = false">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.mono {
  font-family: monospace;
  word-break: break-all;
}
.row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>

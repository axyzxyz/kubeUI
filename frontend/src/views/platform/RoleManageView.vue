<script setup lang="ts">
import { computed, ref } from 'vue';
import { useAuthStore } from '@/stores/authStore';
import GrantDialog from './roles/GrantDialog.vue';
import GrantsTab from './roles/GrantsTab.vue';
import GroupsTab from './roles/GroupsTab.vue';
import RoleGroupsTab from './roles/RoleGroupsTab.vue';
import RolesTab from './roles/RolesTab.vue';
import type { GrantPreset } from './roles/grant';

const auth = useAuthStore();

const activeTab = ref('roles');
const grantVisible = ref(false);
const grantPreset = ref<GrantPreset | null>(null);
/** 授权保存后递增,通知各 Tab 联动刷新 */
const grantSavedKey = ref(0);

const noPermission = computed(() => !auth.isAdmin);

function openGrant(preset: GrantPreset | null): void {
  grantPreset.value = preset;
  grantVisible.value = true;
}

function onGrantSaved(): void {
  grantSavedKey.value += 1;
}
</script>

<template>
  <div class="page">
    <h1 class="page-title">权限管理</h1>
    <el-alert
      v-if="noPermission"
      type="warning"
      :closable="false"
      title="仅管理员可访问权限管理"
      description="当前账号无 admin 角色。后端接口同样会做 RBAC 校验。"
    />
    <el-tabs v-else v-model="activeTab">
      <el-tab-pane label="角色" name="roles">
        <RolesTab :refresh-key="grantSavedKey" @grant="openGrant" />
      </el-tab-pane>
      <el-tab-pane label="角色组" name="role-groups">
        <RoleGroupsTab :refresh-key="grantSavedKey" @grant="openGrant" />
      </el-tab-pane>
      <el-tab-pane label="用户组" name="groups">
        <GroupsTab :refresh-key="grantSavedKey" @grant="openGrant" />
      </el-tab-pane>
      <el-tab-pane label="授权总览" name="grants">
        <GrantsTab :refresh-key="grantSavedKey" @create="openGrant(null)" />
      </el-tab-pane>
    </el-tabs>

    <GrantDialog v-model="grantVisible" :preset="grantPreset" @saved="onGrantSaved" />
  </div>
</template>

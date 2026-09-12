<script setup lang="ts">
import { useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/authStore';
import { useUiStore } from '@/stores/uiStore';
import ClusterSwitcher from './ClusterSwitcher.vue';
import NamespaceSwitcher from './NamespaceSwitcher.vue';

const router = useRouter();
const ui = useUiStore();
const auth = useAuthStore();

function onUserCommand(command: string): void {
  if (command === 'logout') {
    auth.logout();
    void router.push('/login');
  } else if (command === 'credentials') {
    void router.push('/credentials');
  }
}
</script>

<template>
  <header class="topbar">
    <div class="logo" @click="router.push('/clusters')">kubeUI</div>
    <ClusterSwitcher />
    <NamespaceSwitcher />
    <div class="spacer" />
    <el-button text size="small" @click="ui.toggleTheme()">
      {{ ui.isDark ? '亮色' : '暗色' }}
    </el-button>
    <el-dropdown @command="onUserCommand">
      <span class="user">👤 {{ auth.user?.name ?? '未登录' }}</span>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item command="credentials">个人凭证</el-dropdown-item>
          <el-dropdown-item command="logout">退出</el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>
  </header>
</template>

<style scoped>
.topbar {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 0 16px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border-1);
}
.logo {
  font-weight: 700;
  font-size: 18px;
  color: var(--brand-primary);
  cursor: pointer;
}
.spacer {
  flex: 1;
}
.user {
  cursor: pointer;
  color: var(--text-1);
}
</style>

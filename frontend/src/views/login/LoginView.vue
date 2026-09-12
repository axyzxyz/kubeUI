<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/authStore';
import { humanizeError } from '@/utils/errorMessages';

const router = useRouter();
const auth = useAuthStore();

const username = ref('admin');
const password = ref('');
const loading = ref(false);
const errorMsg = ref('');

async function onSubmit(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    await auth.login({ username: username.value, password: password.value });
    await router.push('/clusters');
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="login-page">
    <div class="card">
      <h1 class="title">v911 多集群管理平台</h1>
      <el-form label-position="top" @submit.prevent="onSubmit">
        <el-form-item label="用户名">
          <el-input v-model="username" autocomplete="username" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="password"
            type="password"
            autocomplete="current-password"
            show-password
            @keyup.enter="onSubmit"
          />
        </el-form-item>
        <div v-if="errorMsg" class="error">{{ errorMsg }}</div>
        <el-button type="primary" native-type="submit" :loading="loading" style="width: 100%">
          登录
        </el-button>
      </el-form>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-page);
}
.card {
  width: 380px;
  background: var(--bg-surface);
  border: 1px solid var(--border-1);
  border-radius: 8px;
  padding: 32px;
}
.title {
  font-size: 20px;
  margin: 0 0 24px;
  color: var(--text-1);
  text-align: center;
}
.error {
  color: var(--danger);
  margin-bottom: 12px;
  font-size: 13px;
}
</style>

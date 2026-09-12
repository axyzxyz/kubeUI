<script setup lang="ts">
import { useRouter } from 'vue-router';
import { useClusterStore } from '@/stores/clusterStore';
import { useAuthStore } from '@/stores/authStore';

const router = useRouter();
const clusterStore = useClusterStore();
const auth = useAuthStore();

function onChange(name: string): void {
  clusterStore.setCurrent(name);
  void router.push(`/clusters/${name}/overview`);
}

function goRegister(): void {
  void router.push('/clusters/register');
}
</script>

<template>
  <el-select
    :model-value="clusterStore.currentName"
    placeholder="选择集群"
    style="width: 220px"
    @update:model-value="onChange"
  >
    <el-option
      v-for="c in clusterStore.clusters"
      :key="c.name"
      :label="`${c.name} (${c.version ?? ''})`"
      :value="c.name"
    />
    <template #footer>
      <el-button v-if="auth.isAdmin" text size="small" type="primary" @click="goRegister">
        + 注册集群
      </el-button>
    </template>
  </el-select>
</template>

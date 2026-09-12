<script setup lang="ts">
import { computed, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useClusterStore } from '@/stores/clusterStore';
import TopBar from './components/TopBar.vue';
import SideNav from './components/SideNav.vue';

const route = useRoute();
const clusterStore = useClusterStore();

// 路由中的 :cluster 回写 store,保证顶栏集群选择器始终显示当前集群
const routeCluster = computed(() =>
  typeof route.params.cluster === 'string' ? route.params.cluster : '',
);
watch(
  routeCluster,
  (name) => {
    if (name !== '' && clusterStore.currentName !== name) {
      clusterStore.setCurrent(name);
    }
  },
  { immediate: true },
);
</script>

<template>
  <div class="layout">
    <TopBar />
    <div class="body">
      <SideNav />
      <main class="main">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<style scoped>
.layout {
  height: 100vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-page);
}
.body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.main {
  flex: 1;
  overflow: auto;
  min-width: 0;
}
</style>

<script setup lang="ts">
import { computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useClusterStore } from '@/stores/clusterStore';

const route = useRoute();
const router = useRouter();
const clusterStore = useClusterStore();

const clusterName = computed(() => String(route.params.cluster ?? clusterStore.currentName));

const options = computed(() =>
  clusterStore.namespaces.map((ns) => ({ label: ns.name, value: ns.name })),
);

function onChange(ns: string): void {
  clusterStore.setNamespace(ns);
  router.go(0);
}

function load(): void {
  if (clusterName.value !== '') {
    void clusterStore.fetchNamespaces(clusterName.value);
  }
}

watch(clusterName, load);
onMounted(load);
</script>

<template>
  <el-select
    :model-value="clusterStore.namespace"
    placeholder="命名空间"
    style="width: 180px"
    :loading="clusterStore.namespacesLoading"
    filterable
    @update:model-value="onChange"
  >
    <el-option label="全部命名空间" value="" />
    <el-option v-for="o in options" :key="o.value" :label="o.label" :value="o.value" />
  </el-select>
</template>

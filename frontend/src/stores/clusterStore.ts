import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import { listClusters } from '@/api/cluster';
import { listResources } from '@/api/workload';
import type { ClusterInfo, ResourceItem } from '@/api/types';
import { loadNamespace, saveNamespace } from '@/utils/storage';
import { humanizeError } from '@/utils/errorMessages';

export const useClusterStore = defineStore('cluster', () => {
  const clusters = ref<ClusterInfo[]>([]);
  const currentName = ref<string>('');
  const namespace = ref<string>(loadNamespace());
  const loading = ref(false);
  const error = ref<string>('');

  /** namespaces 列表缓存(按集群名),切换集群时失效 */
  const namespaces = ref<ResourceItem[]>([]);
  const namespacesLoading = ref(false);
  const namespacesCache = new Map<string, ResourceItem[]>();

  const current = computed(() => clusters.value.find((c) => c.name === currentName.value));
  const readyClusters = computed(() => clusters.value.filter((c) => c.status === 'ready'));

  function setCurrent(name: string): void {
    if (currentName.value !== name) {
      namespaces.value = namespacesCache.get(name) ?? [];
    }
    currentName.value = name;
  }

  function setNamespace(ns: string): void {
    namespace.value = ns;
    saveNamespace(ns);
  }

  async function fetchClusters(): Promise<void> {
    loading.value = true;
    error.value = '';
    try {
      const res = await listClusters();
      clusters.value = res.items;
      if (currentName.value === '' && res.items.length > 0) {
        const first = res.items[0];
        if (first !== undefined) {
          currentName.value = first.name;
        }
      }
    } catch (e) {
      error.value = humanizeError(e);
    } finally {
      loading.value = false;
    }
  }

  /** 动态拉取集群 namespaces 列表(GET /clusters/:cluster/namespaces),按集群缓存 */
  async function fetchNamespaces(cluster: string, force = false): Promise<void> {
    const cached = namespacesCache.get(cluster);
    if (!force && cached !== undefined) {
      namespaces.value = cached;
      return;
    }
    namespacesLoading.value = true;
    try {
      const res = await listResources(cluster, 'namespaces', { page: 1, size: 500 });
      namespacesCache.set(cluster, res.items);
      namespaces.value = res.items;
    } finally {
      namespacesLoading.value = false;
    }
  }

  return {
    clusters,
    currentName,
    namespace,
    loading,
    error,
    namespaces,
    namespacesLoading,
    current,
    readyClusters,
    setCurrent,
    setNamespace,
    fetchClusters,
    fetchNamespaces,
  };
});

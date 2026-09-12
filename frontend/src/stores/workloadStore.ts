import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import type { ResourceItem, ResourceListQuery } from '@/api/types';
import type { ResourceKind } from '@/api/workload';

/** 资源列表的共享筛选与增量刷新状态(通用 ResourceItem DTO) */
export const useWorkloadStore = defineStore('workload', () => {
  const kind = ref<ResourceKind>('deployments');
  const items = ref<ResourceItem[]>([]);
  const loading = ref(false);
  const error = ref<string>('');
  const query = ref<ResourceListQuery>({});

  const filteredCount = computed(() => items.value.length);

  function reset(nextKind: ResourceKind, nextQuery: ResourceListQuery): void {
    kind.value = nextKind;
    query.value = nextQuery;
    items.value = [];
    error.value = '';
  }

  function applyUpsert(item: ResourceItem): void {
    const idx = items.value.findIndex((it) => it.name === item.name);
    if (idx >= 0 && items.value[idx] !== undefined) {
      items.value[idx] = item;
    } else {
      items.value = [item, ...items.value];
    }
  }

  function applyDelete(name: string): void {
    items.value = items.value.filter((it) => it.name !== name);
  }

  return {
    kind,
    items,
    loading,
    error,
    query,
    filteredCount,
    reset,
    applyUpsert,
    applyDelete,
  };
});

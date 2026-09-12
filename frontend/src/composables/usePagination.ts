import { computed, ref, type ComputedRef, type Ref } from 'vue';

export interface PaginationState {
  page: Ref<number>;
  size: Ref<number>;
  total: Ref<number>;
  offset: ComputedRef<number>;
  reset: () => void;
}

/** 标准分页(page/size)+ total,供管理端列表(用户/审计/事件)使用 */
export function usePagination(defaultSize = 20): PaginationState {
  const page = ref(1);
  const size = ref(defaultSize);
  const total = ref(0);

  const offset = computed(() => (page.value - 1) * size.value);

  function reset(): void {
    page.value = 1;
  }

  return { page, size, total, offset, reset };
}

import { ref } from 'vue';
import { defineStore } from 'pinia';
import type { EventItem } from '@/api/types';
import { listEvents } from '@/api/event';
import { humanizeError } from '@/utils/errorMessages';

/** 事件列表 + WS 实时流共享状态 */
export const useEventStore = defineStore('event', () => {
  const events = ref<EventItem[]>([]);
  const total = ref(0);
  const loading = ref(false);
  const error = ref<string>('');

  async function fetchEvents(
    cluster: string,
    namespace: string,
    page: number,
    size: number,
  ): Promise<void> {
    loading.value = true;
    error.value = '';
    try {
      const res = await listEvents(cluster, {
        namespace: namespace === '' ? undefined : namespace,
        page,
        size,
      });
      events.value = res.items;
      total.value = res.total;
    } catch (e) {
      error.value = humanizeError(e);
    } finally {
      loading.value = false;
    }
  }

  function applyUpsert(item: EventItem): void {
    const idx = events.value.findIndex((e) => e.name === item.name);
    if (idx >= 0 && events.value[idx] !== undefined) {
      events.value[idx] = item;
    } else {
      events.value = [item, ...events.value];
    }
  }

  function applyDelete(name: string): void {
    events.value = events.value.filter((e) => e.name !== name);
  }

  return { events, total, loading, error, fetchEvents, applyUpsert, applyDelete };
});

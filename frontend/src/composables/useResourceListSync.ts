import type { Ref } from 'vue';
import type { ResourceItem } from '@/api/types';
import type { WsEvent } from '@/api/ws';
import { resourceItemOf } from '@/api/workload';

export interface ResourceListSyncOptions {
  items: Ref<ResourceItem[]>;
  selected: Ref<ResourceItem[]>;
  total: Ref<number>;
  page: Ref<number>;
}

/**
 * 资源列表行同步:
 * - onWatchEvent:WS added/modified/deleted 事件与当前行做 upsert/移除对账
 *   (event.object 为 K8s 原生 unstructured 对象,先转成与列表同构的 ResourceItem);
 * - removeRows:删除成功(单个/批量)后的乐观移除,双保险不依赖 WS deleted 事件,
 *   从 items 与 selection 中去掉并同步 total;当前页被清空且非第一页时回退一页,
 *   由调用方 refresh 加载上一页。
 */
export function useResourceListSync(
  opts: ResourceListSyncOptions,
): { removeRows: (rows: ResourceItem[]) => void; onWatchEvent: (ev: WsEvent) => void } {
  function removeRows(rows: ResourceItem[]): void {
    if (rows.length === 0) return;
    const removed = new Set(rows.map((r) => `${r.namespace ?? ''}/${r.name}`));
    opts.items.value = opts.items.value.filter((it) => !removed.has(`${it.namespace ?? ''}/${it.name}`));
    opts.selected.value = opts.selected.value.filter(
      (it) => !removed.has(`${it.namespace ?? ''}/${it.name}`),
    );
    opts.total.value = Math.max(0, opts.total.value - rows.length);
    if (opts.items.value.length === 0 && opts.page.value > 1) {
      opts.page.value -= 1; // 回退一页,由调用方 refresh 加载上一页
    }
  }

  function onWatchEvent(ev: WsEvent): void {
    const item = resourceItemOf(ev.object);
    if (item.name === '') return;
    const sameRow = (it: ResourceItem): boolean =>
      it.name === item.name && (it.namespace ?? '') === item.namespace;
    if (ev.action === 'deleted') {
      opts.items.value = opts.items.value.filter((it) => !sameRow(it));
    } else {
      const idx = opts.items.value.findIndex(sameRow);
      if (idx >= 0 && opts.items.value[idx] !== undefined) {
        opts.items.value[idx] = item;
      } else if (ev.action === 'added') {
        opts.items.value = [item, ...opts.items.value];
      }
    }
  }

  return { removeRows, onWatchEvent };
}

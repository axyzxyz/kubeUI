import { onScopeDispose, watch } from 'vue';
import { wsManager } from '@/api/ws';
import type { WsEvent, WsResource } from '@/api/ws';

export interface WatchOptions {
  cluster: string;
  resource: WsResource;
  namespace?: string;
  onEvent: (event: WsEvent) => void;
  onError?: (code: number, message: string) => void;
}

/**
 * 组件级 watch 订阅:cluster/resource/namespace 变化时自动重订(旧订阅退订),
 * 断线重连由 wsManager 负责,组件卸载(scope 销毁)时自动 unsubscribe。
 */
export function useResourceWatch(options: () => WatchOptions): () => void {
  let unsubscribe: (() => void) | null = null;

  function rebuild(): void {
    if (unsubscribe !== null) unsubscribe();
    const opts = options();
    unsubscribe = wsManager.subscribe(
      { cluster: opts.cluster, namespace: opts.namespace, resource: opts.resource },
      opts.onEvent,
      opts.onError,
    );
  }

  watch(
    () => {
      const opts = options();
      return [opts.cluster, opts.resource, opts.namespace ?? ''] as const;
    },
    rebuild,
    { immediate: true },
  );

  onScopeDispose(() => {
    if (unsubscribe !== null) unsubscribe();
  });

  return () => {
    if (unsubscribe !== null) unsubscribe();
  };
}

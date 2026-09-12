import type { K8sObject } from './types';

/** WS Envelope 判别联合(backend/internal/pkg/wsx/protocol.go) */
export type WsEnvelope =
  | { type: 'subscribe'; requestId: string; payload: WsSubscribe }
  | { type: 'unsubscribe'; requestId: string; payload: WsSubscribe }
  | { type: 'event'; payload: WsEvent }
  | { type: 'pong'; requestId?: string }
  | { type: 'ping' }
  | { type: 'error'; requestId?: string; payload: { code: number; message: string } };

/** SubscribePayload:podlogs 订阅需带 name(可选 container) */
export interface WsSubscribe {
  cluster: string;
  namespace?: string;
  resource: WsResource;
  name?: string;
  container?: string;
}

export type WsResource =
  | 'pods'
  | 'deployments'
  | 'statefulsets'
  | 'daemonsets'
  | 'services'
  | 'ingresses'
  | 'configmaps'
  | 'secrets'
  | 'pvcs'
  | 'pvs'
  | 'namespaces'
  | 'events'
  | 'nodes'
  | 'crds'
  | 'clusters'
  | 'audit'
  | 'podlogs';

/** EventPayload:object 为 K8s 原生 unstructured 对象(与详情端点同构),需前端自行摘要 */
export interface WsEvent {
  resource: string;
  cluster: string;
  namespace: string;
  name: string;
  action: 'added' | 'modified' | 'deleted';
  object: K8sObject;
}

const HEARTBEAT_MS = 25_000;
const RECONNECT_MIN_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

type EventHandler = (event: WsEvent) => void;
type ErrorHandler = (code: number, message: string) => void;

interface Subscription {
  cluster: string;
  namespace: string;
  resource: WsResource;
  /** 同一 key 允许多组件订阅:各持独立回调,禁止相互覆盖 */
  handlers: Set<EventHandler>;
  onError?: ErrorHandler;
}

let seq = 0;
function nextRequestId(): string {
  seq += 1;
  return `r-${seq}`;
}

/**
 * /api/v1/watch 连接管理(?token= 认证):订阅集合、25s 心跳、断线指数退避重连
 * (1s→30s + 抖动),重连成功后自动重放全部订阅。禁止在组件内自行拼 WS URL。
 */
class WsManager {
  private ws: WebSocket | null = null;
  private subs = new Map<string, Subscription>();
  private heartbeatTimer: number | null = null;
  private reconnectTimer: number | null = null;
  private retry = 0;
  private connecting = false;
  // CONNECTING 阶段收到 close():置标志,等 onopen 后再真正关闭
  private pendingClose = false;

  private wsUrl(): string {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const token =
      (JSON.parse(localStorage.getItem('v911.auth') ?? 'null') as { accessToken: string } | null)
        ?.accessToken ?? '';
    return `${proto}://${window.location.host}/api/v1/watch?token=${encodeURIComponent(token)}`;
  }

  private send(msg: WsEnvelope): void {
    if (this.ws !== null && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private subscribeOnWire(sub: Subscription): string {
    const requestId = nextRequestId();
    this.send({
      type: 'subscribe',
      requestId,
      payload: {
        cluster: sub.cluster,
        namespace: sub.namespace === '' ? undefined : sub.namespace,
        resource: sub.resource,
      },
    });
    return requestId;
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = window.setInterval(() => {
      this.send({ type: 'ping' });
    }, HEARTBEAT_MS);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer !== null) {
      window.clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer !== null || this.subs.size === 0) {
      return;
    }
    const base = Math.min(RECONNECT_MIN_MS * 2 ** this.retry, RECONNECT_MAX_MS);
    const delay = base / 2 + Math.random() * (base / 2);
    this.retry += 1;
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private connect(): void {
    if (this.connecting || (this.ws !== null && this.ws.readyState <= WebSocket.OPEN)) {
      return;
    }
    this.connecting = true;
    const ws = new WebSocket(this.wsUrl());
    ws.onopen = () => {
      this.connecting = false;
      this.retry = 0;
      // CONNECTING 期间被 close():建立后立即关闭,避免对 CONNECTING 套接字
      // 调 close() 触发浏览器 "closed before the connection is established" 告警
      if (this.pendingClose) {
        this.pendingClose = false;
        ws.close();
        return;
      }
      this.startHeartbeat();
      for (const sub of this.subs.values()) {
        this.subscribeOnWire(sub);
      }
    };
    ws.onmessage = (ev: MessageEvent) => this.onMessage(ev);
    ws.onclose = () => {
      this.connecting = false;
      this.stopHeartbeat();
      this.ws = null;
      this.scheduleReconnect();
    };
    ws.onerror = () => {
      // 不主动 close():CONNECTING 阶段 close() 会产生浏览器告警;
      // 连接失败时 onclose 必然跟随,由其统一走重连
    };
    this.ws = ws;
  }

  private onMessage(ev: MessageEvent): void {
    let envelope: WsEnvelope;
    try {
      envelope = JSON.parse(String(ev.data)) as WsEnvelope;
    } catch {
      return;
    }
    if (envelope.type === 'event') {
      const payload = envelope.payload;
      for (const sub of this.subs.values()) {
        // namespace 维度过滤:''(全部)接收所有;指定时仅转发同 namespace 事件
        if (sub.namespace !== '' && sub.namespace !== payload.namespace) {
          continue;
        }
        if (sub.cluster === payload.cluster && sub.resource === payload.resource) {
          for (const handler of sub.handlers) {
            handler(payload);
          }
        }
      }
    } else if (envelope.type === 'error') {
      for (const sub of this.subs.values()) {
        sub.onError?.(envelope.payload.code, envelope.payload.message);
      }
    }
  }

  subscribe(
    opts: { cluster: string; namespace?: string; resource: WsResource },
    onEvent: EventHandler,
    onError?: ErrorHandler,
  ): () => void {
    const key = `${opts.cluster}|${opts.namespace ?? ''}|${opts.resource}`;
    const existing = this.subs.get(key);
    if (existing !== undefined) {
      // 复用已有订阅(同 wire 端点):仅追加回调,取消时只移除自身,不影响其他订阅者
      existing.handlers.add(onEvent);
      if (onError !== undefined) {
        existing.onError = onError;
      }
      return () => {
        existing.handlers.delete(onEvent);
        if (existing.handlers.size === 0) {
          this.unsubscribe(key);
        }
      };
    }
    const sub: Subscription = {
      cluster: opts.cluster,
      namespace: opts.namespace ?? '',
      resource: opts.resource,
      handlers: new Set([onEvent]),
      onError,
    };
    this.subs.set(key, sub);
    this.connect();
    this.subscribeOnWire(sub);
    return () => {
      sub.handlers.delete(onEvent);
      if (sub.handlers.size === 0) {
        this.unsubscribe(key);
      }
    };
  }

  private unsubscribe(key: string): void {
    const sub = this.subs.get(key);
    if (sub === undefined) {
      return;
    }
    this.subs.delete(key);
    this.send({
      type: 'unsubscribe',
      requestId: nextRequestId(),
      payload: {
        resource: sub.resource,
        cluster: sub.cluster,
        namespace: sub.namespace === '' ? undefined : sub.namespace,
      },
    });
    if (this.subs.size === 0) {
      this.close();
    }
  }

  close(): void {
    this.stopHeartbeat();
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    const ws = this.ws;
    this.ws = null;
    this.pendingClose = false;
    if (ws === null) {
      return;
    }
    if (ws.readyState === WebSocket.CONNECTING) {
      // 延迟关闭:对 CONNECTING 套接字调 close() 会触发浏览器告警
      this.pendingClose = true;
      ws.onopen = () => {
        ws.close();
      };
      ws.onclose = null;
      ws.onmessage = null;
      ws.onerror = null;
      return;
    }
    ws.onclose = null;
    ws.onmessage = null;
    ws.onerror = null;
    ws.close();
  }
}

export const wsManager = new WsManager();

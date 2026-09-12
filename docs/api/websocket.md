# WebSocket 协议契约

> 适用范围:`/api/v1/watch` 事件订阅与 Pod 终端通道;前端 `src/api/ws.ts` +
> `useResourceWatch`,后端 `internal/pkg/wsx`。
> 依据:`docs/design/04-coding-standards.md` §4.5、`docs/design/01-architecture.md` §6.2。
> 最后更新时间:2026-09-11(按 backend 实现回填:token 认证、SubscribePayload.name、logs/stream 独立端点)
> 注:本文档为设计阶段契约,与代码冲突时以代码现状为准并回改本文档。

---

## 1. 事件流端点:`/api/v1/watch`

- 认证:握手走 JWT,**`?token=` 优先**(浏览器 WS 无法设 header),兼容 `Authorization: Bearer` header;认证失败返回 HTTP 401(error payload)。
- 帧格式:JSON **文本帧**,`type` 判别联合;二进制帧在本端点不使用;
- WS 内不使用 HTTP 状态码,错误以 `{"type":"error","payload":{"code":40401,"message":"cluster not found"}}` 帧回传,code 沿用 rest.md 同一张错误码表。

### 1.1 Envelope 结构

Go(`internal/pkg/wsx/protocol.go`):

```go
type Envelope struct {
    Type      string          `json:"type"`                // subscribe|unsubscribe|event|ping|pong|error
    RequestID string          `json:"requestId,omitempty"` // subscribe/unsubscribe 回执关联
    Payload   json.RawMessage `json:"payload,omitempty"`
}
```

TS(`src/api/ws.ts`):

```ts
export type WsEnvelope =
  | { type: 'subscribe'; requestId: string; payload: WsSubscribe }
  | { type: 'unsubscribe'; requestId: string; payload: WsSubscribe }
  | { type: 'event'; payload: WsEvent }
  | { type: 'pong'; requestId?: string }
  | { type: 'ping' }
  | { type: 'error'; requestId?: string; payload: { code: number; message: string } }

// SubscribePayload(与后端 wsx.SubscribePayload 一致):
// { cluster, namespace?, resource, name?, container? }
```

### 1.2 订阅 / 取消订阅

```json
// → subscribe
{ "type": "subscribe", "requestId": "r-1",
  "payload": { "cluster": "prod-eu", "namespace": "default", "resource": "pods", "name": "" } }
// ← 回执(成功):服务端立即推送一次全量 added 事件,随后为增量
{ "type": "event", "payload": { "resource": "pods", "cluster": "prod-eu",
  "namespace": "default", "name": "web-0", "action": "added", "object": { ... } } }
// ← 回执(失败)
{ "type": "error", "payload": { "code": 40401, "message": "cluster not found" } }
```

- `payload.resource` 枚举:`pods | deployments | statefulsets | daemonsets |
  services | ingresses | configmaps | secrets | pvcs | namespaces | events | nodes |
  crds | clusters | audit | podlogs`
  (`clusters` 为平台集群状态变更,`audit` 为审计事件流,`namespace` 忽略;
  `podlogs` 为日志流订阅,须带 `name` 指定目标 Pod,可选 `container`);
- `namespace` 为空 = 全部命名空间;
- 同一连接可多路订阅;`unsubscribe` 负载与 `subscribe` 同构,按 `cluster + resource (+ namespace)` 维度取消。

### 1.3 EventPayload

```go
type EventPayload struct {
    Resource  string    `json:"resource"`
    Cluster   string    `json:"cluster"`
    Namespace string    `json:"namespace"`
    Name      string    `json:"name"`
    Action    string    `json:"action"` // added|modified|deleted
    Object    any       `json:"object"` // 与 REST 返回的 DTO 相同结构
}
```

### 1.4 心跳与重连(硬性约定)

1. 客户端每 **25s** 发 `{"type":"ping"}`,服务端回 `{"type":"pong"}`;
2. 服务端 **30s** 未收到任何帧即断开;
3. 前端断线自动重连:指数退避 1s→30s + 随机抖动;重连成功后**必须**重放全部订阅;
4. 前端禁止自行拼 WS URL,统一走 `useResourceWatch` composable(管理订阅集合、重连、组件卸载时 unsubscribe)。

---

## 2. 终端通道:`/api/v1/clusters/{cluster}/pods/{name}/terminal`

独立端点,不复用 `/watch`(避免交互流量与事件流互相干扰)。查询参数:
`?container=<容器名>&shell=<shell,默认 /bin/sh>`。

- 认证:JWT;开启前服务端执行平台 RBAC + 目标集群 `SubjectAccessReview`
  委托校验(pods/exec create),拒绝回 `{"type":"error","payload":{"code":40300,...}}`;
- 审计:终端会话开启/关闭均记审计(user、cluster、pod、会话时长;命令内容不记录);
- 帧规则:**文本帧为控制消息,二进制帧为数据字节流**(stdout/stderr 原始字节)。

### 2.1 消息定义

```text
上行(客户端 → 服务端):
  文本帧  {"type":"terminal:stdin","data":"ls\n"}                 // stdin 文本
  文本帧  {"type":"terminal:resize","cols":120,"rows":40}          // 终端尺寸

下行(服务端 → 客户端):
  二进制帧 = stdout/stderr 原始字节
  文本帧  {"type":"terminal:exit","requestId":"","payload":{"code":0}}      // 会话结束
  文本帧  {"type":"error","payload":{"code":40300,"message":"..."}} // 校验/通道失败
```

### 2.2 会话生命周期

1. WS 握手成功后服务端立即发起 SPDY exec;失败则发 `error` 帧并关闭连接;
2. 客户端可随时发 `terminal:resize`(任意多次);
3. 退出路径:远端进程退出 → 服务端发 `terminal:exit` 后关连接;客户端主动关闭
   WS → 服务端终止 exec 会话并记审计;
4. 心跳复用 §1.4 约定(25s ping / 30s 无帧断开);
5. 后端把 client-go SPDY exec 流桥接到 WS 帧;经 Agent 模式集群时,字节流经隧道透传,TLS 端到端保持在 Server 与目标 APIServer 之间。

---

## 3. 实时日志流:独立 WS 端点(已实现)

Pod 实时日志 follow 流走**独立 WS 端点**
`GET /api/v1/clusters/{cluster}/pods/{name}/logs/stream?namespace=&container=&token=`
(认证同 §1:`?token=` 或 Authorization header)。

- **二进制帧 = 日志字节**(按 ~8KB 分块透传,不保证按行分割,客户端自行缓冲拆行);
- 文本帧为控制帧(JSON):流正常结束时服务端发 `{"type":"logstream:close","requestId":"","payload":null}`
  后关闭连接;错误发 `{"type":"error","payload":{"code":...,"message":"..."}}`;
- 客户端无需发业务帧,可发 `ping` 保活(服务端 30s 无帧断开);断线重连由前端负责;
- 历史日志走 REST(见 rest.md §4.2)。

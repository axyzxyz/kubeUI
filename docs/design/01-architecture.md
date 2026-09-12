# 01 - 系统架构设计

> 适用范围:后端架构、多集群管理核心、Agent 反连、客户端接入、安全与部署形态。前端页面设计见 `03-ui-design.md`,编码硬约束见 `04-coding-standards.md`。
> 最后更新时间:2026-09-11
> 注:本文档由 AI 依据项目背景与 `04-coding-standards.md` 既有约定起草,评审通过后去除本标注。

---

## 目录

1. [总体架构](#1-总体架构)
2. [技术选型与决策](#2-技术选型与决策)
3. [多集群管理核心设计](#3-多集群管理核心设计)
4. [Agent 反连模式设计](#4-agent-反连模式设计)
5. [客户端接入设计(kubeconfig 链接 / CLI)](#5-客户端接入设计)
6. [前后端 API 约定概览](#6-前后端-api-约定概览)
7. [安全设计](#7-安全设计)
8. [部署形态](#8-部署形态)
9. [分期里程碑](#9-分期里程碑)

---

## 1. 总体架构

### 1.1 架构图

```text
                                    ┌────────────────────────────────────────────┐
                                    │                 用户侧                      │
                                    │  ┌─────────┐  ┌──────────┐  ┌───────────┐  │
                                    │  │ Browser │  │ kubectl  │  │ v911 CLI  │  │
                                    │  │ (Vue 3) │  │  / Lens  │  │  (可选)    │  │
                                    │  └────┬────┘  └────┬─────┘  └─────┬─────┘  │
                                    └───────┼────────────┼──────────────┼────────┘
                                            │ HTTPS      │ HTTPS        │ HTTPS
                                            │ REST + WS  │ K8s API 代理 │ REST
                                    ┌───────▼────────────▼──────────────▼────────┐
                                    │              v911 Server(单二进制)          │
                                    │  ┌──────────────────────────────────────┐  │
                                    │  │ api 层: handler + middleware          │  │
                                    │  │  /api/v1/*   REST                    │  │
                                    │  │  /api/v1/watch          WebSocket    │  │
                                    │  │  /k8s/{cluster}/*  K8s API 反向代理   │  │
                                    │  │  /api/v1/agent/connect  Agent 接入   │  │
                                    │  ├──────────────────────────────────────┤  │
                                    │  │ service 层: 业务逻辑                   │  │
                                    │  │  auth / user / audit / clusterreg    │  │
                                    │  │  resource / logstream / terminal     │  │
                                    │  │  metrics / kubeconfig-issuer         │  │
                                    │  ├──────────────────────────────────────┤  │
                                    │  │ k8s 层:                agent 层:      │  │
                                    │  │  ClusterManager       TunnelHub      │  │
                                    │  │  ├ rest.Config          ├ 注册/心跳  │  │
                                    │  │  ├ clientset/dynamic    ├ 流转发     │  │
                                    │  │  ├ restMapper 缓存      └ 协程泵     │  │
                                    │  │  └ informer 工厂(懒启动)              │  │
                                    │  ├──────────────────────────────────────┤  │
                                    │  │ 基础设施: logx(slog) / errcode /      │  │
                                    │  │  crypto(AES-GCM) / wsx(WS 协议)      │  │
                                    │  ├──────────────────────────────────────┤  │
                                    │  │ 存储: SQLite(默认)/ PostgreSQL        │  │
                                    │  │ 存储: 前端静态资源(go:embed)          │  │
                                    │  └──────────────────────────────────────┘  │
                                    └──────┬──────────────────────▲──────────────┘
                                           │ 直连(gRPC风格直拨)    │ WebSocket 反向拨号
                                           ▼                      │(无直达网络环境)
                              ┌─────────────┐          ┌─────────┴─────────┐
                              │ Cluster A   │          │ Cluster B          │
                              │ API Server ◄┼──────────┤ API Server ◄─ Agent│
                              └─────────────┘  tunnel  └───────────────────┘
```

### 1.2 模块划分

| 模块 | 位置(遵循 `04-coding-standards.md` §1) | 职责 |
|---|---|---|
| api 层 | `backend/internal/api/` | 路由、参数绑定、统一响应、WS 端点、K8s 反向代理端点、Agent 接入端点 |
| service 层 | `backend/internal/service/` | 用户/认证/RBAC/审计/集群注册/kubeconfig 签发等业务逻辑 |
| k8s 层 | `backend/internal/k8s/` | ClusterManager、client 工厂、informer 工厂、restMapper 缓存、fake 测试辅助 |
| agent 层 | `backend/internal/agent/` | TunnelHub(Agent 注册、心跳、流转发);Agent 自身是同一仓库内 `cmd/agent` 的第二个二进制 |
| 基础设施 | `backend/internal/pkg/` | errcode / logx / pagination / wsx / crypto |
| 配置 | `backend/internal/config/` | YAML + 环境变量加载 |
| 前端 | `frontend/` | Vue 3 + Pinia,静态产物 embed 进服务端二进制 |
| 部署 | `deploy/` | Dockerfile、Helm chart、config-example.yaml |

`cmd/` 下有两个入口:`cmd/server`(主服务,embed 前端产物)与 `cmd/agent`(集群内 Agent)。

---

## 2. 技术选型与决策

所有选型结论如下,只写"选了什么、为什么不选别的"。

| 领域 | 决策 | 理由 |
|---|---|---|
| Go 版本 | 1.23+ | 与 `04-coding-standards.md` 基线一致 |
| Web 框架 | **gin** | 规范文档 §2.2/§4.2 的响应封装已基于 `gin.Context` 实现,生态最成熟、中间件齐全。不选 echo:能力同质化,换框架无收益;不选 kratos/go-zero:面向微服务治理,本项目是单二进制单体,引入其 DI/服务发现体系纯属负担 |
| K8s 访问 | **client-go**(clientset + dynamic + discovery),列表/详情经 ClusterManager 统一出口 | 类型安全 + 动态 CRD 访问兼得。不选 k8s.io/metrics 直连之外的自研封装:metrics-server 访问本身就是 client-go `metricsv` 包 |
| WebSocket | **gorilla/websocket** | 事实标准,已恢复维护,文档与社区案例最多;配 `internal/pkg/wsx` 封装心跳/重连协议(见 §6)。不选 coder/websocket:API 更现代但生态案例少;不选 SSE:不支持双向(终端必须双向) |
| 认证 | **JWT(HS256)Bearer token**,access 2h + refresh 7d;V2 增加 OIDC | 无状态、天然适配反向代理与 CLI/kubeconfig 场景。不选 Session:多实例扩展与 CLI 接入都要额外粘性处理;不选 PAM/LDAP 自研:超出范围。密码存储用 bcrypt。OIDC 列入 V2(接口预留 `auth.mode`) |
| 存储 | **SQLite(纯 Go driver)起步,GORM v2 + 数据库抽象层,可直接切 PostgreSQL** | 单二进制零依赖是硬需求;用 `github.com/glebarez/sqlite`(纯 Go)保住 `CGO_ENABLED=0` 交叉编译。不选 bbolt/文件存储:需要关系查询(审计、用户、集群元数据);不选起步就 PostgreSQL:违反"单二进制开箱即用"。schema 变更放 `backend/migrations/`(golang-migrate 格式) |
| 配置 | **koanf**(YAML 文件 + 环境变量覆盖) | 比 viper 轻、无全局单例、依赖干净;`deploy/config-example.yaml` 即配置即文档 |
| 日志 | **log/slog**(JSON handler),封装 `internal/pkg/logx` | 已由 `04-coding-standards.md` §2.5 定死;标准库零依赖,单二进制不需要 zap 的极限性能 |
| 依赖注入 | **手工构造函数注入**(main 中按序装配) | 规范 §2.9 禁止全局可变单例;项目规模下手工装配约 100 行、编译期可见。不选 wire:代码生成步骤增加构建复杂度,收益为零;不选 fx:运行时反射注入牺牲可调试性 |
| 前端 | Vue 3.4+ / TS 5 strict / Vite 5 / Pinia 2 / vue-router 4 | 已由 `04-coding-standards.md` §3 定死;UI 组件库选 **Element Plus**(K8s 资源表格/表单密集型后台,El-Table + 表单校验最省力,中文生态最好)。不选 Naive UI/Arco:无明显优势差异,Element Plus 社区案例最多 |
| Agent 通信 | **WebSocket 反向拨号**(见 §4) | 决策过程见 §4.1 |
| 终端 | **client-go SPDY exec → WebSocket 桥接** | `remotecommand.NewExecutor` 是唯一官方 exec 通道,后端把 SPDY 流桥接到前端 WS 帧(stdin/stdout/resize 三类消息) |

---

## 3. 多集群管理核心设计

### 3.1 ClusterManager 生命周期

```text
注册 ──► Validating ──► Ready ──► Degraded ──► Reconnecting ──► Ready
                        │                                        ▲
                        └── 注销(Unregister)◄──────────────────┘
```

**Register 流程**(幂等,以集群名为主键):

1. 解析:`clientcmd.Load([]byte)` 解析 kubeconfig,取 current-context;多 context 时要求显式指定,返回 40402;
2. 校验:构造 `rest.Config` 后拨 `/version`(超时 10s),确认可达且凭证有效;403/401 区分错误文案;
3. 加密:对 kubeconfig 原文做 AES-256-GCM 加密落库(见 §7.1);
4. 构建 Cluster 运行时对象(见 3.2),投入 ClusterManager 的并发安全注册表(`sync.Map` + RWMutex 混合);
5. 启动健康检查循环。

**状态机**:Ready(探活连续成功)/ Degraded(探活失败但未超阈值,只读降级:禁用写操作)/ Reconnecting(指数退避重连:1s→2s→…→60s 封顶,永久上限 24h 后转 Offline 并告警事件)。状态变更写审计事件并通过 `/api/v1/watch` 的 `clusters` resource 推送。

**Unregister**:先停 informer 与健康检查 goroutine(等待退出),再删库,最后从注册表移除。删除集群属于危险操作,走审计。

### 3.2 Cluster 运行时对象

```go
type Cluster struct {
    Name        string
    Status      Status                       // ready|degraded|reconnecting|offline
    RestConfig  *rest.Config
    ClientSet   kubernetes.Interface
    DynClient   dynamic.Interface
    Mapper      discoveryrestmapper.ResettableRESTMapper // 见 3.4
    Informers   *InformerPool                            // 见 3.3
    HealthCh    chan struct{}                            // 关闭即触发停止
    // 所有字段在 Register 完成后只读;状态字段由健康检查协程独占写
}
```

### 3.3 Informer 缓存策略(懒启动 + 引用计数)

**决策:不做全量预建 informer,采用"按需懒启动 + 引用计数 + 空闲回收"。**

- **List 场景(默认)**:直接走 API Server(带 labelSelector 与分页),不进 informer——理由:多集群 × 多资源全量 informer 的内存与首次 sync 成本在 100 集群规模下不可控,而管理台列表天然带筛选分页;
- **Watch 场景(WS 订阅、事件流)**:对该 `cluster + GVR` 维度懒启动 `dynamicinformer.DynamicSharedInformerFactory`,引用计数 +1;订阅者归零后空闲 **5 分钟** 停止 informer 并释放缓存;
- **事件流**:`corev1.Event` 的 informer 在集群 Ready 后常驻(事件量可控,且事件页是排查高频入口);
- 缓存上限:单 informer 缓存对象数超过 50k 时只转发事件不做本地全量缓存(`TransformFunc` 裁剪为摘要对象)。

### 3.4 restMapper 缓存

- 使用 `client-go/discovery/cached/memory.MemCacheClient` 包装的 `ResettableRESTMapper`;
- **刷新时机**:① 收到 `meta.NoKindMatchError` / `NoResourceMatchError` 时立即 `Reset()` 后重试一次(处理 CRD 刚安装的场景);② 每 10 分钟定时刷新;③ 集群从 Reconnecting→Ready 迁移时 Reset;
- Mapper 按 cluster 隔离,禁止共享(不同集群 GVR 集合不同)。

### 3.5 健康检查

- 周期 30s,探活请求为 `GET /readyz`(超时 5s,失败不重试、等下个周期);
- 连续 3 次失败 → Degraded;连续 10 次失败 → Reconnecting(退避拨号);恢复即 Ready;
- 探活结果(含 lastTransitionTime、k8s version)缓存在 Cluster 对象中,`GET /clusters/{cluster}/status` 直接读缓存,不透传探测。

---

## 4. Agent 反连模式设计

### 4.1 通信协议决策

**决策:Agent → Server 采用 WebSocket 反向拨号(纯 HTTPS/WSS),不采用 gRPC。**

理由:① 目标环境(无直达网络的内网集群)出网往往只放行 443/HTTP,WebSocket 复用现有 HTTPS 证书、代理与 WAF,而 gRPC 需要 HTTP/2 明文(h2c)支持或额外端口,落地摩擦大;② 数据面流量(终端、日志)本质是字节流,WebSocket 二进制帧透传完全够用,不需要 gRPC 的多路复用与 proto 契约;③ 认证、心跳、审计全部复用服务端已有的 HTTP 中间件体系。代价(单连接头部开销、无内置流控)在本场景(每 Agent 并发隧道 < 100 条)可忽略。

参考 Teleport 的 reverse tunnel 思路:**控制面不信任网络方向,由被管侧主动拨出并维持长连接,所有管理流量经该连接回流**。

### 4.2 通道设计:1 条信令 + N 条数据

```text
Server                                    Agent(集群 B 内)
  │◄──── WSS /api/v1/agent/connect ─────────┤ ① 信令通道(常驻)
  │      首帧: {token, agentVersion, capabilities}
  │      之后: 心跳 ping/pong + 状态上报
  │
  │◄──── WSS /api/v1/agent/tunnel?connId=xx ─┤ ② 数据通道(按需,每条隧道一条)
  │      首帧: {token, connId} 之后: 二进制帧 = 原始 TCP 字节流
```

- **信令通道**(`agent/connect`):Agent 启动后用 Enrollment Token 拨号注册,服务端 TunnelHub 校验 token 绑定的集群 ID,标记该集群 `accessMode=agent` 并置 Ready。心跳 25s(复用 §6.2 的 WS 心跳约定),断开即置 Degraded 并触发 Agent 侧重连(指数退避 1s→60s)。
- **数据通道**(`agent/tunnel`):服务端收到对 Agent 集群的 API 请求时,生成 `connId`,通过信令通道通知 Agent"拨号 `<apiserverHost:port>`",Agent 建立数据通道并回 `connId`,随后两端把 WebSocket 二进制帧当作原始 TCP 字节对拷。**TLS 端到端保持在 Server 与目标 APIServer 之间**(Agent 只透传字节,不持有集群凭证、不做 TLS 终结),与 04 规范"kubeconfig/key 不得出现在 Agent 侧日志"一致。
- Agent 侧重连后信令通道更换会话,服务端丢弃旧 connId 的半开数据通道(30s 生命周期兜底)。

### 4.3 与 ClusterManager 的整合

Cluster 对象增加 `Dialer` 字段:

- `accessMode=direct`:Dialer 为默认 `net.Dialer`(TLS 由 transport 处理);
- `accessMode=agent`:Dialer 为 `TunnelDialer`(向 TunnelHub 申请一条数据通道)。

由此 `rest.Config.WrapTransport` 注入自定义 dial,client-go 的所有能力(list/watch/exec/logs/portforward/metrics)**零改动**经隧道工作——这是选择"透传 TCP"而非"Agent 侧做本地反向代理"的核心原因。

### 4.4 Agent 部署

- 交付物:① `kubectl apply -f` 的静态 YAML(含 RBAC 最小权限:仅需能建到 APIServer 的连接,不需要集群 RBAC,因为它只透传字节);② Helm chart(V1);
- Enrollment Token 由"集群接入向导"页面生成(一次性、有效期 24h),Agent 通过环境变量 `V911_ENROLL_TOKEN` 与 `V911_SERVER_URL` 配置;
- Agent 无状态、无本地存储,升级采用替换镜像滚动重启。

---

## 5. 客户端接入设计

### 5.1 平台 K8s 反向代理(客户端 kubeconfig 的基础)

服务端暴露 `/k8s/{cluster}/*` 端点,行为等同 K8s API 反向代理:

1. 客户端凭证 → 平台认证(JWT / 客户端 token);
2. 平台级 RBAC 判定(用户对该集群是否有 `k8s-proxy:access` 权限);
3. 委托校验:向目标集群发 `SubjectAccessReview`(模拟用户身份),拒绝则返回 40300;
4. 经 ClusterManager 的 Dialer 转发 HTTP(S) 请求(hijack 升级支持 exec/attach/watch)。

### 5.2 可下载 kubeconfig / 临时凭证链接

- `POST /api/v1/clusters/{cluster}/kubeconfigs` 创建签发请求:参数 `ttl`(默认 24h,上限 7d)、`description`;
- 返回一次性下载链接(链接内含 10 分钟有效的 one-time code,换取 kubeconfig 后即失效;链接本身 24h 过期);
- 签发的 kubeconfig 内容:`server: https://<平台地址>/k8s/{cluster}` + `token: <短期 Bearer>`;短期 token 服务端只存 SHA-256 哈希,可撤销、到期自动清理;
- 该能力同时覆盖 kubectl 与 Lens(标准 kubeconfig,无需插件)。V2 增加 `exec` credential plugin 形态(v911 CLI 作为插件按需刷新 token),避免 token 明文落盘。

### 5.3 CLI(P1,可选)

`v911` CLI(Cobra,同仓库 `cmd/cli`):登录、列出集群与资源、生成 kubeconfig、跟随日志。直接消费 `/api/v1` REST,不引入私有协议;终端/日志流在 CLI 中走同一 WS 端点。

---

## 6. 前后端 API 约定概览

完整契约规范以 `04-coding-standards.md` §4 为准,此处只列架构级概览与扩展。

### 6.1 REST 端点总览(均挂 `/api/v1`)

```text
认证与用户
POST   /api/v1/auth/login              POST /api/v1/auth/refresh
GET    /api/v1/users                   POST /api/v1/users        (管理员)

集群管理
GET    /api/v1/clusters                POST /api/v1/clusters           (kubeconfig 注册)
GET    /api/v1/clusters/{cluster}/status
DELETE /api/v1/clusters/{cluster}      PUT  /api/v1/clusters/{cluster}/kubeconfig (轮转)
POST   /api/v1/clusters/{cluster}/kubeconfigs      (签发客户端 kubeconfig 链接)
GET    /api/v1/enroll-tokens           POST /api/v1/enroll-tokens  (Agent 接入 token)

资源浏览(每类资源同构:GET 列表 / GET 详情 / PUT 更新 / DELETE 删除)
GET    /api/v1/clusters/{cluster}/{resource}?namespace=&labelSelector=&page=&size=
       resource ∈ deployments|statefulsets|daemonsets|pods|services|ingresses|
                  configmaps|secrets|pvcs|nodes|crds|{crd-plural}
POST   /api/v1/clusters/{cluster}/pods/{name}/restart
GET    /api/v1/clusters/{cluster}/pods/{name}/yaml    PUT .../yaml

可观测
GET    /api/v1/clusters/{cluster}/pods/{name}/logs      (历史日志)
GET    /api/v1/clusters/{cluster}/metrics/nodes|pods    (metrics-server 快照)
GET    /api/v1/clusters/{cluster}/events?fieldSelector=

审计 / RBAC
GET    /api/v1/audit-logs              GET/POST /api/v1/roles  /api/v1/role-bindings
```

### 6.2 WebSocket 协议

单端点 `/api/v1/watch`,JSON 文本帧,`type` 判别联合(`subscribe|unsubscribe|event|ping|pong`),Envelope 结构与心跳/重连约定见 `04-coding-standards.md` §4.5。resource 枚举扩展为:`pods|deployments|...|events|clusters|audit`。

**扩展:终端通道**(二进制帧与文本帧混用,文本帧为控制):

```text
上行(客户端 → 服务端):{"type":"terminal:stdin","data":"ls\n"}    文本帧
                      {"type":"terminal:resize","cols":120,"rows":40} 文本帧
下行(服务端 → 客户端):二进制帧 = stdout/stderr 原始字节
                      {"type":"terminal:exit","code":0}            文本帧
```

终端是独立端点 `/api/v1/clusters/{cluster}/pods/{name}/terminal?container=&shell=`,不复用 `/watch`(避免音频级交互流量与事件流互相干扰),认证同样走 JWT。

### 6.3 统一响应体与错误码

- 响应体:`{code, message, data}`,见 `04-coding-standards.md` §4.2;
- 错误码分段:40xxx 客户端错误(40001 参数、40100 未认证、40300 禁止、404xx 资源不存在)、50xxx 服务端错误(50000 内部、50100 上游 K8s 错误、50200 Agent 通道不可用);集中定义在 `internal/pkg/errcode/codes.go`;
- WS 中不使用 HTTP 状态码,错误以 `{"type":"error","payload":{"code":40401,"message":"cluster not found"}}` 帧回传,code 沿用同一张错误码表。

---

## 7. 安全设计

### 7.1 kubeconfig 加密存储

- 算法:AES-256-GCM,每次写入随机 nonce,密文格式 `v1:<nonce>:<ciphertext>`,版本前缀支撑未来算法轮转;
- 主密钥(Master Key):默认从配置 `security.masterKey` / 环境变量 `V911_MASTER_KEY` 注入(32 字节,base64);生产推荐 Helm 安装时自动生成并存 Kubernetes Secret;
- 加解密封装在 `internal/pkg/crypto`,任何日志/错误/API 响应中出现 kubeconfig 内容即 CI 门禁失败(04 规范硬性约束);V2 支持外置 KMS(接口预留 `MasterKeyProvider`)。

### 7.2 凭证轮转

- 平台自身凭证:JWT access 2h / refresh 7d,refresh token 服务端存哈希、可吊销,登录即轮转(refresh token rotation);
- 客户端 kubeconfig token:TTL 签发、哈希落库、支持撤销与"列出我的凭证";
- 集群侧凭证:用户替换 kubeconfig(`PUT /clusters/{cluster}/kubeconfig`)时平滑热更新——ClusterManager 用新凭证重建 clientset 与 Dialer,期间状态 Degraded,成功后切回 Ready;Agent 模式下集群凭证在 Agent 侧,平台不持有;
- Enrollment Token:一次性或限次数、24h 过期、可吊销。

### 7.3 RBAC 模型

**决策:平台 RBAC( coarse )先行,目标集群 K8s 原生 RBAC( fine )委托校验,两层都过才放行。**

- 平台级模型:`User —(RoleBinding)— Role — ClusterScope`。内置角色:
  - `admin`:平台全部权限,含用户与集群管理;
  - `operator`:指定集群内的读写(重启、编辑 YAML、删除、终端);
  - `viewer`:指定集群内只读(不含 Secret 明文,Secret 默认脱敏,operator 及以上可见);
  - 角色绑定粒度为 `cluster 级`(可 `*` 全部),namespace 级粒度列入 V2;
- 委托校验:写操作与 Secret 读取执行 `SelfSubjectAccessReview`(以平台注册的 ServiceAccount 身份)或 `SubjectAccessReview`(impersonate 用户名)判定原生 RBAC; impersonation 模式(V2):平台代理请求带 `Impersonate-User` 头,让目标集群用自己的 RBAC 做最终裁决——这是"客户端 kubeconfig 经代理访问"时的默认模式;
- 前端按 `/api/v1/users/me/permissions` 返回的权限集渲染(隐藏无权限操作,后端为准)。

### 7.4 审计

- 审计范围(硬性,04 规范):登录/登出、集群注册/注销/kubeconfig 更新、资源删除/修改、Pod 重启、终端会话开启/关闭(记录 user、cluster、pod、命令不可见但记录会话时长)、kubeconfig 链接种子、Agent token 生成/吊销、RBAC 变更;
- 落库字段:`requestId, userId, action, resource, cluster, namespace, name, sourceIp, userAgent, result(allow/deny), createdAt`;经中间件统一采集,handler 显式标注 `AuditTarget`;
- 审计记录不可修改、无删除接口;支持按操作者/集群/时间导出 CSV(P2)。

---

## 8. 部署形态

### 8.1 服务端

| 形态 | 说明 |
|---|---|
| 单二进制 | `CGO_ENABLED=0` 构建,前端产物 `go:embed` 进二进制;`./v911-server --config config.yaml` 即起,默认 SQLite 文件 + 内存无外部依赖 |
| Docker | 多阶段构建,scratch 基础镜像 + ca-certificates;镜像内含 server 与 agent 两个入口 |
| Helm chart(`deploy/k8s/`) | values 控制:存储 sqlite(EmptyDir/PVC)或 postgresql(external);Ingress(WS 需开启 proxy-read-timeout);Secret 注入 masterKey 与初始管理员密码;支持 replicas>1 时强制外部 PostgreSQL + 共享 JWT secret |
| 反向代理要求 | 生产部署在 Nginx/Ingress 后:WebSocket upgrade 透传;`/k8s/` 端点禁用请求体缓冲(exec 流需要) |

### 8.2 Agent

- 页面生成 `kubectl apply -f <url>`(URL 由服务端 `/api/v1/agent/manifest?token=...` 动态渲染,token 注入环境变量),一键复制;
- Helm 方式:`helm install v911-agent oci://<registry>/v911-agent --set serverUrl= --set enrollToken=`;
- Agent 与服务端版本兼容矩阵:Agent 只依赖信令协议 v1,服务端向后兼容一个 minor 版本。

---

## 9. 分期里程碑

| 里程碑 | 范围 | 验收标准 |
|---|---|---|
| **MVP**(约 4–6 周) | 集群注册/注销/健康检查、直连模式的资源浏览(核心 11 类资源 + YAML 查看/编辑)、Pod 历史日志 + 实时日志流(WS)、events 列表、JWT 登录、内置 admin/viewer 两角色、审计(写操作)、单二进制部署、metrics-server 节点/Pod CPU 内存 | 一个管理员可在 10 分钟内注册 3 个集群并完成"浏览—看日志—改副本数"闭环 |
| **V1**(MVP 后 4–6 周) | Web Terminal(exec)、Agent 反连模式 + 接入向导、客户端 kubeconfig 临时链接(`/k8s/` 代理)、完整平台 RBAC(operator)+ Secret 脱敏、CRD 完整支持(GVR 发现 + 自定义资源浏览)、PostgreSQL 支持、Helm chart、Docker 镜像发布 | Agent 模式集群完成全部 MVP 功能;Lens 用下载的 kubeconfig 经平台代理读写 |
| **V2**(按需迭代) | OIDC 登录、namespace 级 RBAC 粒度、CLI、exec credential plugin、Impersonation 代理模式、审计导出与保留策略、KMS 主密钥、多实例水平扩展(去状态化完善)、资源拓扑图与告警事件规则 | 企业 SSO 接入;平台横向扩展至 100 集群规模(见 02-features 非功能指标) |

明确排除(不做):多租户计费、GitOps 交付、CI/CD 流水线、监控自建(只消费 metrics-server,不内置 Prometheus)。

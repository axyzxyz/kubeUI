# 02 - 功能体系设计

> 适用范围:平台功能全景、各模块功能点清单与优先级、核心用户旅程、非功能需求。架构与技术决策见 `01-architecture.md`,接口契约见 `04-coding-standards.md` §4。
> 最后更新时间:2026-09-11
> 注:本文档由 AI 依据项目背景与既有规范起草,评审通过后去除本标注。

---

## 目录

1. [功能全景图](#1-功能全景图)
2. [功能点清单](#2-功能点清单)
3. [核心用户旅程](#3-核心用户旅程)
4. [非功能需求](#4-非功能需求)

优先级定义:**P0** = MVP 必须;**P1** = V1 必须;**P2** = V2 及以后。里程碑列 M=三选一(MVP/V1/V2)。

---

## 1. 功能全景图

```text
kubeUI 多集群 K8s 管理 Dashboard
├── 1. 平台基座
│   ├── 认证:登录 / JWT 刷新 / (V2)OIDC
│   ├── 多用户与平台 RBAC:admin / operator / viewer
│   ├── 审计日志:写操作 + 敏感读取全程留痕
│   └── 个人设置:凭证管理 / 我的下载链接
├── 2. 集群管理
│   ├── kubeconfig 注册(粘贴 / 上传 / 多 context 指定)
│   ├── 连接健康检查与状态机(Ready/Degraded/Reconnecting)
│   ├── kubeconfig 热更新轮转
│   ├── Agent 反连接入(Agent + 向导 + token 管理)      [V1]
│   └── 集群概览(版本 / 节点数 / 资源统计 / 事件摘要)
├── 3. 资源浏览(全部资源统一:列表/筛选/详情/YAML)
│   ├── 工作负载:Deployment / StatefulSet / DaemonSet / Pod
│   ├── 配置与存储:ConfigMap / Secret(脱敏) / PVC / PV
│   ├── 网络:Service / Ingress / NetworkPolicy          [V2]
│   ├── 集群资源:Node / Namespace / CRD + 自定义资源
│   └── 事件:K8s Event 列表与实时流
├── 4. 可观测性
│   ├── Pod 日志:历史 + 实时流(WS)
│   ├── 监控:metrics-server CPU / 内存(Node/Pod)
│   └── 事件流推送(resource watch)
├── 5. 终端
│   └── Web Terminal:exec 进容器 / 多 shell / 会话审计   [V1]
├── 6. 客户端接入
│   ├── 客户端 kubeconfig 临时下载链接                    [V1]
│   ├── Agent 安装指引页面(one-liner / Helm)             [V1]
│   └── kubeUI CLI(登录/浏览/日志/kubeconfig 签发)         [V2]
└── 7. 运维支撑
    ├── 单二进制 / Docker / Helm 部署
    └── SQLite → PostgreSQL 存储切换                      [V1]
```

---

## 2. 功能点清单

### 2.1 集群管理

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| kubeconfig 注册集群(粘贴/上传) | P0 | MVP | `clientcmd` 解析校验,失败返回 40402;多 context 提示选择 |
| 集群列表与状态展示 | P0 | MVP | 名称、状态、k8s 版本、节点数、accessMode(direct/agent) |
| 连接健康检查与自动重连 | P0 | MVP | 30s 探活、状态机迁移、状态变更 WS 推送 |
| 集群注销 | P0 | MVP | 危险操作,二次确认 + 审计 |
| 集群概览页(版本/节点/资源计数/最近事件) | P1 | V1 | 聚合查询,errgroup 有界并发 |
| kubeconfig 热更新轮转 | P1 | V1 | 替换凭证平滑重建 client,审计 |
| Agent 反连接入(agent 模式集群) | P1 | V1 | 见 01-architecture §4;Agent 模式下功能与直连一致 |
| Agent Enrollment Token 管理(生成/吊销/过期) | P1 | V1 | 一次性、24h 有效,审计 |
| namespace 级资源配额/配额视图(ResourceQuota) | P2 | V2 | — |

### 2.2 资源浏览(工作负载 / 配置存储 / 网络节点 / CRD)

统一交互约定:所有资源列表支持 namespace 过滤、labelSelector、关键字搜索、分页;详情页含元数据、状态、YAML、事件、关联资源;YAML 查看与编辑共用一只读组件 + 编辑态(经 `PUT .../yaml` 提交,冲突时提示 resourceVersion 冲突)。

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| Deployment 列表/详情/YAML 编辑/副本伸缩/重启 | P0 | MVP | 重启 = patch annotation 触发 rollout |
| StatefulSet / DaemonSet 列表/详情/YAML 编辑 | P0 | MVP | 伸缩仅 StatefulSet |
| Pod 列表/详情(容器状态、重启次数、资源限制) | P0 | MVP | — |
| Pod 删除 / 重启(删除重建) | P0 | MVP | 危险操作,审计 |
| Service / Ingress 列表与详情 | P0 | MVP | — |
| ConfigMap 列表/详情/YAML 编辑 | P0 | MVP | — |
| Secret 列表/详情(**默认值脱敏**,operator+ 解码查看) | P0 | MVP | 解码查看是敏感操作,审计 |
| PVC / PV 列表与详情 | P0 | MVP | PV 全局无 namespace |
| Node 列表/详情(状态、Taint、容量/分配) | P0 | MVP | — |
| Namespace 列表/详情 | P0 | MVP | — |
| CRD 列表 + 自定义资源动态浏览 | P1 | V1 | discovery + dynamic client,GVR 发现 |
| 实时资源变更推送(WS watch,列表增量刷新) | P1 | V1 | informer 懒启动,见 01 §3.3 |
| Deployment 回滚历史(ReplicaSet 版本) | P1 | V1 | — |
| NetworkPolicy / RB 对象(Role/Binding)浏览 | P2 | V2 | — |
| YAML diff 对比(编辑前差异预览) | P2 | V2 | — |
| 跨命名空间全局搜索(资源名模糊检索) | P2 | V2 | 依赖 informer 缓存 |

### 2.3 可观测性(日志 / 事件 / 监控)

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| Pod 历史日志(容器选择、tailLines、previous) | P0 | MVP | REST 拉取,下载为文件 |
| Pod 实时日志流(follow) | P0 | MVP | WS `/api/v1/watch` 独立 resource `podlogs`,断线自动重连续传 sinceTime |
| 多容器日志合并 / 颜色区分 | P1 | V1 | — |
| K8s Event 列表(按 namespace/resource 过滤) | P0 | MVP | — |
| Event 实时流推送 | P1 | V1 | 常驻 event informer |
| Node/Pod CPU、内存快照(metrics-server) | P0 | MVP | 列表列内嵌使用率条 + 详情页历史折线(快照轮询拼装,30s 间隔) |
| 集群级仪表盘(节点资源汇总/异常 Pod 计数) | P1 | V1 | — |
| 告警规则与通知 | P2 | V2 | 明确不内置 Prometheus,仅做轻量阈值事件 |

### 2.4 终端

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| Web Terminal(exec 到容器,可选 shell) | P1 | V1 | SPDY↔WS 桥接,xterm.js 前端;resize 支持 |
| 终端会话审计(开启/关闭/时长,记录到审计) | P1 | V1 | 04 规范硬性要求 |
| 多标签会话 / 断线重连提示 | P2 | V2 | — |
| 会话操作录制(全量键盘记录回放) | P2 | V2 | 涉及隐私合规,默认关闭 |

### 2.5 多用户与 RBAC

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| 本地用户登录(JWT) | P0 | MVP | bcrypt 存储密码,登录/登出审计 |
| 用户管理(创建/禁用/改密/重置) | P0 | MVP | admin 专属 |
| 内置角色 admin/viewer + 集群级授权绑定 | P0 | MVP | MVP 仅两角色,绑定到 cluster 或 `*` |
| operator 角色(读写+终端)+ Secret 脱敏联动 | P1 | V1 | — |
| 我的凭证管理(查看/撤销客户端 token) | P1 | V1 | — |
| OIDC 单点登录 | P2 | V2 | `auth.mode=oidc` |
| namespace 级授权粒度 / 自定义角色 | P2 | V2 | — |

### 2.6 审计

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| 写操作与敏感读取审计落库 | P0 | MVP | 中间件采集,字段见 01 §7.4 |
| 审计日志查询页(操作者/集群/动作/时间筛选) | P0 | MVP | 只读,无删除入口 |
| Agent/终端/kubeconfig 签发类事件入审计 | P1 | V1 | 随对应功能落地 |
| 审计导出 CSV 与保留策略 | P2 | V2 | — |

### 2.7 客户端接入

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| 客户端 kubeconfig 签发 + 一次性下载链接 | P1 | V1 | TTL 24h 默认、可撤销、服务端 `/k8s/{cluster}` 代理 |
| Agent 安装指引页面(生成 one-liner YAML / Helm 命令) | P1 | V1 | 页面按集群生成 enroll token 并渲染安装命令 |
| 平台代理兼容 exec/watch/attach 流式端点 | P1 | V1 | kubectl logs -f、exec 经代理可用 |
| kubeUI CLI(登录/集群列表/资源浏览/日志/kubeconfig) | P2 | V2 | Cobra,复用 REST/WS,无私有协议 |
| exec credential plugin(kubeconfig 免明文 token) | P2 | V2 | CLI 作为 credential plugin |

### 2.8 部署与运维支撑

| 功能点 | 优先级 | 里程碑 | 说明 |
|---|---|---|---|
| 单二进制(前端 embed)+ SQLite | P0 | MVP | CGO_ENABLED=0,零外部依赖启动 |
| Dockerfile / 镜像发布 | P0 | MVP | 多阶段构建,scratch 镜像 |
| Helm chart(server + agent) | P1 | V1 | values 可切 PostgreSQL |
| PostgreSQL 存储 | P1 | V1 | GORM dialect 切换 + migrations 双方言验证 |
| 多实例水平扩展(无状态化) | P2 | V2 | 强制外部 DB + 共享 JWT secret |
| 配置项文档(config-example.yaml)与迁移指引 | P0 | MVP | 配置即文档 |

---

## 3. 核心用户旅程

### 旅程 A:管理员注册集群(5 分钟内)

1. 管理员登录平台,进入「集群管理 → 接入集群」;
2. 选择「kubeconfig 注册」,粘贴或上传 kubeconfig;若含多个 context,下拉选择目标 context,服务端校验(40402 给出具体原因:解析失败/连接不可达/凭证被拒);
3. 校验通过,集群进入 Ready;概览页展示版本、节点数、资源统计;
4. 网络不可直达的集群:切换到「Agent 接入」页签,平台生成一次性 Enrollment Token 并渲染 `kubectl apply` 安装命令,管理员在目标集群执行,Agent 反连后集群自动转 Ready。

### 旅程 B:开发者浏览资源

1. 开发者(viewer/operator)登录,顶部集群选择器切换集群,侧边栏选择工作负载;
2. Pod 列表按 namespace/label 过滤,经 WS 订阅增量刷新(无需手动刷新);列表内嵌 CPU/内存使用率条(metrics-server);
3. 进入 Deployment 详情:查看状态/事件/YAML;operator 可伸缩副本、编辑 YAML(冲突时提示 resourceVersion 冲突并 diff)。

### 旅程 C:值班排查问题(端到端)

1. 告警或用户反馈 `payments` 服务异常,值班者按名称检索到 Pod;
2. Pod 详情页先看 **Events**(排查调度/镜像拉取失败)→ 状态异常的容器看 **实时日志流**(关键字过滤,必要时切 previous 容器看崩溃前日志);
3. 日志无法定位时,operator 直接打开 **Web Terminal** exec 进容器,`curl` 验证依赖服务;会话全程审计;
4. 确认是配置问题:查看 ConfigMap 历史变更(审计记录),修改后重启 Pod,日志恢复正常;
5. 若需要本地 kubectl 深度排查:在「客户端接入」页签发 24h 有效 kubeconfig 下载链接,经平台代理完成本地操作,链接到期自动失效;
6. 全过程(登录、Secret 查看、YAML 修改、删除、终端会话)在审计日志中可按操作者与时间线回溯。

---

## 4. 非功能需求

### 4.1 性能指标(目标规模:100 集群 × 平均 50 节点)

| 指标 | 目标 |
|---|---|
| 单资源列表 API(≤20 项/页,直连集群)p95 | < 300ms(不含 K8s API 本身耗时) |
| 平台代理层自身开销(经 `/k8s/` 或 Agent 隧道) | 额外 p95 < 50ms(直连)/ < 100ms(Agent 隧道) |
| 实时日志流端到端延迟 | < 500ms |
| 终端按键回显延迟 | < 200ms(直连) |
| 集群全量状态刷新(100 集群并发探活一轮) | < 30s(errgroup 有界并发,limit 8) |
| 服务端常驻内存 | 基线 < 300MB;每活跃 informer 缓存 ≤ 256MB 上限(超出降级为仅事件转发) |
| WS 并发连接 | 单实例 ≥ 2000 订阅连接 |

### 4.2 可用性与可靠性

- 单实例可用性目标 99.5%(MVP)/ 99.9%(V2 多实例);
- 单个集群故障(K8s API 不可达)不得影响平台其他功能;对故障集群的请求 5s 快速失败返回 50100,不拖垮线程池(全异步,无阻塞等待);
- Agent 断线自动重连(1s→60s 指数退避),重连期间集群 Degraded 可读提示;服务端重启后 Agent 60s 内自动恢复注册;
- 数据库升级走 migrations,前向兼容一版;SQLite 每 24h 自动 VACUUM + 备份文件轮转(保留 7 份)。

### 4.3 兼容性

| 维度 | 范围 |
|---|---|
| Kubernetes 版本 | 服务端 client-go 支持 1.24 – 1.31;Agent 镜像同版本矩阵;CRD 走 dynamic client,apiextensions v1 |
| metrics-server | ≥ v0.6(metrics.k8s.io/v1beta1);未安装时监控功能显式降级提示,不影响其他功能 |
| 浏览器 | Chrome/Edge ≥ 100、Firefox ≥ 100(WebSocket、xterm.js 前提) |
| 认证方式 | 本地用户(MVP)/ OIDC 1.0(V2) |
| 部署环境 | Linux amd64/arm64 单二进制;Docker ≥ 20.10;Helm 3;入口需支持 WS upgrade 与流式透传 |

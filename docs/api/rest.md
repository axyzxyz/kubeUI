# REST API 契约

> 适用范围:前端 `src/api/` 与后端 `internal/api/handler/` 联调的契约基准。
> 依据:`docs/design/01-architecture.md` §6、`docs/design/04-coding-standards.md` §4。
> 最后更新时间:2026-09-12
> 注:本文档为设计阶段契约,与代码冲突时以代码现状为准并回改本文档。

---

## 1. 通用约定

- 所有接口挂 `/api/v1` 前缀,路径版本策略见 04 §4.4(禁止 header/query 版本协商)。
- 认证:`Authorization: Bearer <JWT>`,`/api/v1/auth/login` 与 `/api/v1/auth/refresh` 除外。
- 跨集群资源一律显式带 `{cluster}` 路径段。
- 过滤/排序/分页一律 query 参数。
- 时间字段统一 RFC3339 字符串。

### 1.1 统一响应体

所有 JSON 接口(含 DELETE)返回:

```json
{ "code": 0, "message": "ok", "data": { ... } }
```

- `code = 0` 成功;非 0 见错误码表;失败时 `data` 为 `null`;
- 前端业务判断只看 `code`,HTTP status 仅用于 401/5xx 全局拦截;
- TS 对应类型 `ApiResponse<T>`(`src/api/types.ts`),Go 对应 `internal/api/response.Body`。

### 1.2 分页

请求参数(所有列表接口统一):

| 参数 | 默认 | 约束 |
|---|---|---|
| `page` | 1 | ≥ 1 |
| `size` | 20 | 1–200,超出取 200 |
| `sortBy` | 各接口默认值 | 白名单枚举,非法值返回 40001 |
| `order` | `desc` | `asc` / `desc` |

响应 `data`:

```json
{ "items": [], "total": 137, "page": 2, "size": 20 }
```

所有列表(含资源浏览)均为**内存分页** `PageResult<T>`:`{ items, total, page, size }`,
不使用 continueToken 游标。

### 1.3 错误码表

分段:4xxxx 客户端错误(40001–40499);5xxxx 服务端错误。集中定义于
`backend/internal/pkg/errcode/codes.go`,新增错误码必须同步本表。

| code | HTTP | 含义 |
|---|---|---|
| 40001 | 400 | 参数校验失败(ParamInvalid) |
| 40100 | 401 | 未认证 / token 过期(Unauthorized) |
| 40300 | 403 | 禁止(平台 RBAC 或委托校验未通过,Forbidden) |
| 40400 | 404 | 资源不存在(通用,NotFound) |
| 40401 | 404 | 集群未找到(ClusterNotFound) |
| 40402 | 404 | kubeconfig 非法(ClusterKubeconfigBad) |
| 40403 | 400 | kubeconfig 含多个 context 且未显式指定(ClusterKubeconfigMultiContext) |
| 40404 | 502 | 目标集群 API Server 不可达或凭证无效(ClusterUnreachable) |
| 40405 | 409 | 同名集群已注册(ClusterAlreadyExists) |
| 40406 | 409 | resourceVersion 冲突,并发修改(ResourceConflict,YAML PUT);资源创建时同名已存在(AlreadyExists)同样复用此码 |
| 40407 | 404 | 一次性下载 code 无效、已使用或已过期(DownloadCodeInvalid) |
| 40410 | 404 | 用户不存在(UserNotFound) |
| 40411 | 403 | 用户已被禁用(UserDisabled) |
| 40412 | 409 | 用户名已存在(UserAlreadyExists) |
| 40413 | 404 | 角色名已存在(RoleAlreadyExists) |
| 40414 | 404 | 角色不存在(RoleNotFound) |
| 40415 | 404 | 用户组名已存在(UserGroupAlreadyExists) |
| 40416 | 404 | 用户组不存在(UserGroupNotFound) |
| 40101 | 401 | Agent Enrollment Token 无效、过期或已吊销(EnrollTokenInvalid) |
| 40102 | 401 | 签发的客户端短期 token 无效、过期或已撤销(IssuedTokenInvalid) |
| 50000 | 500 | 内部错误(InternalError,未归类错误对外一律此码) |
| 50100 | 502 | 上游 K8s API 错误(UpstreamK8sError) |
| 50200 | 502 | Agent 通道不可用(AgentTunnelUnavailable) |

> 注:上表与 `backend/internal/pkg/errcode/codes.go` 逐一对应;HTTP 列为约定映射
> (参照 04 §2.2 httpStatusOf 逻辑),实现如有出入以 errcode 包为准并回改本表。

---

## 2. 认证与用户

### 2.1 登录

```text
POST /api/v1/auth/login
{ "username": "admin", "password": "..." }
→ data: TokenPair
{
  "accessToken": "...",          // TTL 见 config auth.accessTokenTTL,默认 2h
  "refreshToken": "...",         // 默认 7d,登录即轮转
  "expiresAt": "..."             // access token 过期时间
}
```

登录响应**不含 user 信息**;前端从 accessToken JWT payload(`{uid,username,role}`)解出用户名与角色。

错误:40100 用户名或密码错误、40411 用户已禁用(不区分提示更佳)。

### 2.2 刷新

```text
POST /api/v1/auth/refresh
{ "refreshToken": "..." }
→ data: 同登录 TokenPair(新 accessToken + 新 refreshToken,旧 refresh 即刻失效)
```

### 2.3 当前用户信息与权限(已实现)

```text
GET /api/v1/users/me
→ data: User { id, username, role, disabled, createdAt, updatedAt }

GET /api/v1/users/me/permissions
→ data: { "role": "operator", "permissions": [...] }
```

权限列表按角色映射:admin = `["*"]`(全部);operator = 读写 + 终端 +
Secret 明文(`clusters:read / resources:read / resources:write / secrets:read /
logs:read / terminal:use / events:read / metrics:read / credentials:issue /
k8s-proxy:use`);viewer = 只读 + Secret 脱敏(`secrets:read-masked` 代替
`secrets:read`,无 `resources:write` / `terminal:use`)。
登录响应仍不含 user 信息,前端兼容两种来源(登录后从 JWT 解析,或调用本端点)。

### 2.4 用户管理(管理员)

```text
GET    /api/v1/users                        # 分页,PageResult<User>
POST   /api/v1/users                        # { username, password, role }  # role 单值
PUT    /api/v1/users/{username}/status      # { disabled: true|false }
PUT    /api/v1/users/me/password            # { oldPassword, newPassword }
POST   /api/v1/users/{username}/reset-password  # admin;→ { username, password } 一次性新密码
DELETE /api/v1/users/{username}             # admin;禁删自己与内置 admin(40300)
```

`User: { id, username, role, disabled, createdAt, updatedAt }`。
重置密码会吊销该用户全部会话,新密码为一次性随机串、仅在响应中出现一次。

默认管理员:首次启动自动创建 `admin`(密码来自 `security.adminPassword`,默认 `admin123`),登录后应立即修改。

---

## 3. 集群管理

### 3.1 注册集群

```text
POST /api/v1/clusters
{ "name": "prod-eu", "kubeconfig": "<yaml 字符串>", "contextName": "", "description": "...", "accessMode": "direct" }
→ data: ClusterInfo
```

`accessMode` 接入方式:`direct`(默认,平台直连)或 `agent`(反连)。
direct 注册为同步校验(拨 `/version`,10s 超时):40402 kubeconfig 非法、40403 多 context
未显式指定、40404 目标集群不可达、40405 同名集群已注册。
agent 注册只校验 kubeconfig 格式并落库,状态置 `offline`(message `waiting for agent
enrollment`),不做拨测;Agent 首次反连成功后经隧道重注册,状态由健康检查收敛为 `ready`。
凭证轮转(PUT /kubeconfig)保持原接入模式不变。

### 3.2 列表 / 状态 / 注销

```text
GET    /api/v1/clusters
→ data: { "items": [ClusterInfo...], "total": n }   # 不分页
  ClusterInfo: { name, status, version?, accessMode: "direct"|"agent", nodeCount?,
                 description?, createdAt, updatedAt }

GET    /api/v1/clusters/{cluster}/status
→ data: { "name", "status": "ready"|"degraded"|"reconnecting"|"offline",
          "version?", "lastTransitionTime", "message?" }   # 读缓存,不透传探测

DELETE /api/v1/clusters/{cluster}                # 危险操作,走审计
PUT    /api/v1/clusters/{cluster}/kubeconfig     # 凭证轮转 { kubeconfig, contextName? } → ClusterInfo
```

### 3.3 客户端 kubeconfig 签发(已实现)

```text
POST /api/v1/clusters/{cluster}/kubeconfigs      # 需认证,走审计
{ "ttl": "24h", "description": "lens 接入" }     # ttl Go duration;默认 24h,上限 7d
→ data: {
  "id": 1, "userId": 7, "cluster": "prod", "description": "lens 接入",
  "expiresAt": "...", "revoked": false, "createdAt": "...",
  "token": "...",                                # 短期 Bearer token,只返回一次
  "downloadUrl": ".../api/v1/clusters/prod/kubeconfigs/download?code=<one-time>",
  "linkExpiresAt": "..."                         # 链接 24h 过期;code 10 分钟一次性
}

GET /api/v1/clusters/{cluster}/kubeconfigs/download?code=<one-time>   # 匿名,code 即鉴权
→ 200 application/yaml(kubeconfig 文件;server 指向平台 /k8s/{cluster})
错误:40407 code 无效/已使用/过期;40102 凭证已撤销或过期。

GET    /api/v1/kubeconfigs          # 当前用户凭证列表(脱敏,不含 token 原文)
→ data: { "items": [ { id, userId, cluster, description, expiresAt, revoked, createdAt } ], "total": n }
DELETE /api/v1/kubeconfigs/{id}     # 撤销(属主或 admin),走审计
```

### 3.4 Agent Enrollment Token(已实现)

```text
GET    /api/v1/enroll-tokens                     # 列出 token(脱敏)
→ data: { "items": [ { id, cluster, expiresAt, noExpiry, revoked, createdAt } ], "total": n }
POST   /api/v1/enroll-tokens                     # admin,走审计
{ "cluster": "prod-eu", "ttl": "24h" }
#   ttl: ""默认 24h;"permanent"长期(永不过期);
#        或 "<n><单位>":h(小时)/d(天)/mo(月=30d)/y(年=365d),上限 30y
#   兼容:旧字段 ttlHours(n>0 时按小时换算)仍接受
→ data: { "id": 1, "cluster": "prod-eu", "expiresAt": "...", "noExpiry": false,
          "revoked": false, "createdAt": "...", "token": "..." }  # token 原文只返回一次
DELETE /api/v1/enroll-tokens/{id}                # 吊销,admin,走审计
#   语义:Validate(校验,不消费)与 Confirm(注册回执成功后写 usedAt)分离;
#   同一 Agent 携同 token 重连幂等放行;吊销/过期一律 40101(长期 token 不受过期约束)
```

### 3.5 Agent 反连通道(WebSocket,已实现)

```text
GET /api/v1/agent/connect            # 信令通道(WS);鉴权 = 首帧 Enrollment Token
  Agent → 平台:{ "type": "register", "payload": { "token", "agentVersion", "capabilities" } }
  平台 → Agent:{ "type": "registered", "payload": { "cluster", "sessionKey" } }
  心跳:Agent 25s 发 { "type": "ping" };30s 无帧服务端断开
  平台 → Agent:{ "type": "dial", "payload": { "connId", "addr" } }   # 请求拨号 apiserver
  平台 → Agent:{ "type": "error", "payload": { "code", "message" } } # 如 40101 token 无效

GET /api/v1/agent/tunnel?connId=&sessionKey=     # 数据通道(WS);二进制帧 = 原始 TCP 字节流
```

注册成功后集群 `accessMode=agent`、状态置 Ready;断开置 Degraded(message
"agent 反连中")。TLS 端到端保持在平台与目标 APIServer 之间,Agent 只透传。
无会话或半开(数据通道 30s 未回连)时平台侧请求返回 50200。

### 3.6 Agent 部署清单(已实现)

```text
GET /api/v1/agent/manifest?token=<enroll-token>   # 匿名;token 校验但不消费
→ data: { "yaml": "<kubectl apply -f 多文档 YAML:Namespace+ServiceAccount+空规则 Role/RoleBinding+Deployment>" }
```

`serverUrl` 取 config `server.externalUrl`;token 注入 Deployment env(响应包含
token 原文,禁止落日志)。Agent 镜像可用 `KUBEUI_AGENT_IMAGE` 覆盖。

### 3.7 平台 K8s 反向代理(已实现)

```text
ANY /k8s/{cluster}/*path            # 挂在 /api/v1 之外
```

- 认证:§3.3 短期签发 token(校验与 `{cluster}` 绑定)或平台 JWT;
- 平台 RBAC:任一内置角色(admin/operator/viewer)可经代理访问;集群内细粒度
  权限由目标集群原生 RBAC 委托裁决;
- 客户端 Authorization 不透传,平台改写为集群 kubeconfig 凭证;
- 转发经 ClusterManager Dialer(direct 集群直连,agent 集群走隧道),支持流式
  (watch)与连接升级(exec SPDY);上游不可达返回 502 + code 50200;
- 每次访问写审计(action=`k8s-proxy`)。

---

## 4. 资源浏览(同构约定)

所有资源走同一组端点,`{resource}` 枚举:
`deployments | statefulsets | daemonsets | pods | services | ingresses |
configmaps | secrets | pvcs | pvs | nodes | namespaces | crds |
roles | role-groups`(后两者为 K8s RBAC 只读展示)。
内置枚举之外的 `{resource}`(即 CRD plural 名)自动经 discovery 解析 GVR 后
走同一组 dynamic client 端点(CRD 动态资源透传,已实现);解析失败返回 40001。

```text
GET    /api/v1/clusters/{cluster}/{resource}?namespace=&labelSelector=&fieldSelector=&keyword=&sortBy=&order=&page=&size=
POST   /api/v1/clusters/{cluster}/{resource}?namespace=        # 创建资源,危险操作,走审计(action=create)
GET    /api/v1/clusters/{cluster}/{resource}/{name}?namespace=&reveal=true&decode=true
GET    /api/v1/clusters/{cluster}/{resource}/{name}/yaml?namespace=
PUT    /api/v1/clusters/{cluster}/{resource}/{name}/yaml?namespace=   # { yaml: "..." }
DELETE /api/v1/clusters/{cluster}/{resource}/{name}?namespace=   # 危险操作,走审计
```

- `keyword`:对 `name` 做大小写不敏感的 contains 模糊过滤(内存分页前应用);
- 列表为内存分页(见 §1.2),`items` 为通用摘要 DTO
  `ResourceItem: { kind?, name, namespace?, status?, createdAt?, labels? }`
  (`status` 按资源类型填充:Pod phase、workload "ready/want"、Node Ready 等);
- 详情返回 **K8s 原生 unstructured 对象 JSON**(Pod 含 `status.containerStatuses`);
- nodes / namespaces / pvs / crds 为集群级资源,不接 `namespace` 参数;
- Secret 默认脱敏(`data` 各项显示 "***");operator 及以上经 `?reveal=true` 明文
  (走审计),`?decode=true` 需与 reveal 同时使用,返回
  `{ name, namespace, data: {...明文 base64 已解码 } }`;
- YAML PUT resourceVersion 冲突返回 code **40406**(HTTP 409)。

### 4.1 YAML 查看 / 编辑

```text
GET /api/v1/clusters/{cluster}/{resource}/{name}/yaml?namespace=   → data: { yaml: "..." }
PUT /api/v1/clusters/{cluster}/{resource}/{name}/yaml?namespace=   # { yaml: "..." } → data: 更新后对象
```

### 4.2 Pod / Deployment 操作

```text
POST /api/v1/clusters/{cluster}/pods/{name}/restart?namespace=          # 走审计,data: null
POST /api/v1/clusters/{cluster}/deployments/{name}/restart?namespace=   # 走审计,data: 对象
PUT  /api/v1/clusters/{cluster}/deployments/{name}/scale?namespace=     # { replicas } → data: 对象
GET  /api/v1/clusters/{cluster}/pods/{name}/logs?namespace=&container=&tailLines=&sinceSeconds=&previous=
→ data: { "logs": "...", "container": "app" }        # 历史日志;实时流走 WS(见 websocket.md)
```

### 4.3 资源创建(已实现)

```text
POST /api/v1/clusters/{cluster}/{resource}?namespace=
Content-Type: application/json | application/yaml
body: K8s 资源对象 JSON,或 YAML 文本(按 Content-Type 判断;服务端两者均兼容)
→ data: 创建后的 K8s 原生 unstructured 对象 JSON
```

- 危险操作,走审计(action=create,审计的 name/namespace 取创建成功对象回填);
- namespace 优先级:`body.metadata.namespace` > `query.namespace`;
  两者皆空且资源为命名空间级 → 40001;集群级资源(nodes/namespaces/pvs/crds)忽略 namespace;
- `metadata.name` 缺失 → 40001;
- `{resource}` 为内置枚举之外的 CRD plural 时,经 discovery 解析 GVR 后走 dynamic client
  透传创建(与列表/详情一致);解析失败 → 40001;
- 同名资源已存在 → **40406**(HTTP 409,复用 ResourceConflict,message
  "resource already exists");集群不存在 → 40401;上游 K8s 错误 → 50100。

---

## 5. 可观测

```text
GET /api/v1/clusters/{cluster}/metrics/nodes      → data: { items: [NodeMetrics...] }
GET /api/v1/clusters/{cluster}/metrics/pods       → data: { items: [PodMetrics...] }
  NodeMetrics: { name, cpu: "750m", memory: "16Gi", cpuPct?: "12.5%", memPct?: "40%" }
  PodMetrics:  { namespace, name, containers: [{ name, cpu, memory }] }
GET /api/v1/clusters/{cluster}/events?namespace=&fieldSelector=&page=&size=&sortBy=&order=
  → data: PageResult<EventItem>
  EventItem: { name, namespace?, type?: "Normal"|"Warning", reason?, message?, source?,
               count?, involvedKind?, involvedName?, firstSeenAt?, lastTimestamp? }
```

metrics-server 未安装时返回 50100,message 注明 "metrics-server not available"。

---

## 6. 审计 / RBAC

```text
GET  /api/v1/audit-logs?username=&cluster=&action=&page=&size=     # 管理员;无 from/to
→ data: PageResult<AuditLog>
  AuditLog: { id, requestId, userId, username, action, resource, resourceType,
              cluster?, namespace?, name?, sourceIp, userAgent,
              result: "allow"|"deny", createdAt }

  字段说明:
  - username: 操作者。认证端点取请求体(登录,失败同样落审计并记 result=deny)/
    响应 JWT(refresh);其余取认证身份。
  - action: 语义化动作,由 HTTP 方法 + 路径推导:
    login | refresh | logout | write | delete | restart | scale |
    reveal-secret(Secret 明文查看)| terminal-open/terminal-close | k8s-proxy。
  - resource: 原始路由 path(如 /api/v1/clusters/:cluster);未知路径即此值。
  - resourceType: 友好资源类型(BUG-07 映射):auth、user、role、role-group、grant、
    audit-log、enroll-token、kubeconfig、cluster、k8s-proxy,以及集群资源
    deployment/statefulset/daemonset/pod/service/ingress/configmap/secret/
    persistent-volume-claim/persistent-volume/node/namespace/crd/event;
    未知路径保留原始 path。

### 6.1 平台 RBAC:角色 / 用户组 / 角色组 / 授权(管理员)

> 契约修订(2026-09-12):授权模型重构为**中心授权表**。旧 `role-groups`
> 端点与 `RoleBindingItem` 形状**废弃并删除**,代之以角色组(role-groups)与
> 授权(grants)端点;所有端点仅 admin 可访问(40300),写操作落审计;
> 列表端点均为**裸数组**(非 PageResult);builtin 角色删/改返回 40300;
> 重复 grant 返回 409(40406 ResourceConflict);角色组名唯一,冲突返回
> 40413(RoleAlreadyExists)。
> 数据模型:`roles`(permissions 存 JSON 数组文本)/ `user_groups` +
> `user_group_members` / `role_groups`(角色组)/ `role_group_roles`(组内角色,
> 联合唯一)/ **`grants`**(中心授权表,唯一关联事实源:subject(user|group) ×
> object(role|role_group),四元组联合唯一)/ `grant_scopes`(`*` 表示全部)。
> 启动时 Seed 内置角色行(存在即跳过);旧 `role_bindings` 表若有数据,启动时
> 自动转换为 `object_type=role` 的 grant(含 scopes)后 DROP(幂等)。

```text
# ---- 角色 ----
GET    /api/v1/roles            → data: RoleItem[](裸数组)
POST   /api/v1/roles            { name, description, permissions: PermissionPoint[] } → RoleItem
PUT    /api/v1/roles/:id        { name, description, permissions } → RoleItem(builtin 角色返回 40300)
DELETE /api/v1/roles/:id        → data: null(builtin 角色返回 40300;级联删除引用该角色的
                                  role_group_roles 与 grants)
  RoleItem: { id, name, description, builtin: bool, permissions: string[] }
  PermissionPoint 枚举(clusters:read/write、resources:read/write、secrets:read、
  secrets:read-masked、logs:read、terminal:use、events:read、metrics:read、
  credentials:issue、k8s-proxy:use、users:manage、enroll-tokens:manage、audit:read)

# ---- 用户组 ----
GET    /api/v1/user-groups            → data: UserGroup[](裸数组)
POST   /api/v1/user-groups            { name, description } → UserGroup
PUT    /api/v1/user-groups/:id        { name, description } → UserGroup
DELETE /api/v1/user-groups/:id        → data: null
GET    /api/v1/user-groups/:id/members → data: UserItem[](契约补充,成员抽屉需要)
POST   /api/v1/user-groups/:id/members       { userId } → data: null
DELETE /api/v1/user-groups/:id/members/:userId      → data: null
  UserGroup: { id, name, description, createdAt, updatedAt }

# ---- 角色组(一组角色)----
GET    /api/v1/role-groups                 → data: RoleGroup[](裸数组)
POST   /api/v1/role-groups                 { name, description } → RoleGroup(名称唯一,重复 40413)
PUT    /api/v1/role-groups/:id             { name, description } → RoleGroup
DELETE /api/v1/role-groups/:id             → data: null(级联删除组内角色关联与以该组为对象的 grants)
GET    /api/v1/role-groups/:id/roles       → data: RoleItem[](裸数组,组内全部角色)
POST   /api/v1/role-groups/:id/roles       { roleId } → data: null(幂等;角色须存在)
DELETE /api/v1/role-groups/:id/roles/:roleId → data: null
  RoleGroup: { id, name, description, createdAt, updatedAt }

# ---- 授权(grants,唯一关联事实源)----
GET    /api/v1/grants?subjectType=&subjectId=&objectType=&objectId= → data: Grant[](裸数组)
POST   /api/v1/grants     { subjectType, subjectId, objectType, objectId, scopes: Scope[] } → Grant
DELETE /api/v1/grants/:id → data: null
  Grant: { id, subjectType: "user"|"group", subjectId, subjectName,
           objectType: "role"|"role_group", objectId, objectName,
           scopes: Scope[], createdAt }
  Scope: { cluster, namespace }   # 值为 "*" 表示全部集群 / 所有命名空间
  过滤参数:四个均可选、可任意组合——
  - 按 subject 查(subjectType/subjectId)= 该用户/组的全部授权;
  - 按 object 查(objectType/objectId)= 该角色/角色组被授给了谁(反向查看)。
  校验:subject 与 object 必须存在(40410/40416/40414/40400);scope ≥ 1 条且
  cluster="*" 时 namespace 必须为 "*"(40001);重复 grant(四元组相同)→ 409(40406)。

# ---- 有效权限 ----
GET /api/v1/users/:username/effective-permissions?cluster=&namespace=   # 本人可查自己,他人需 admin
  → data: { permissions: PermissionPoint[] }
  cluster/namespace 均可省略(全局);返回该用户(含所属用户组聚合)的
  有效权限点列表,与 2.3 的 /users/me/permissions 语义一致。
  授权解析规则(backend internal/service/role.go EffectivePermissions):
  - 内置 admin 恒为 ["*"];
  - 其余 = users.role 列内置角色权限(全局生效)
    ∪ 所有命中该用户的 grant(subject=user 直接命中,或 subject=group 且
    用户为该组成员)展开的权限点,命中部分按 scope 过滤:
    * object_type=role → 该角色的权限点;
    * object_type=role_group → 该组内全部角色权限点的并集;
  - scope 过滤:scope.cluster 为 "*" 或与查询 cluster 相等,且 scope.namespace
    为 "*" 或与查询 namespace 相等;查询参数省略(全局)时不做 scope 过滤;
  - 权限校验经 middleware.RequirePermScope(:cluster + ?namespace=)执行,
    authorizer 未注入时回退静态角色映射(HasPerm)。
```

内置角色:`admin`(平台全部)、`operator`(指定集群读写)、`viewer`(只读,Secret 脱敏),
三条 builtin 角色行由服务端启动时 Seed(存在即跳过)。
审计记录不可修改、无删除接口。

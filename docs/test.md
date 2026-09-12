# kubeUI 多集群管理平台 · 整体功能验证 Bug 报告

- 测试日期:2026-09-12(两轮:第一轮 02:00–03:00,第二轮 11:40–12:50 CST)
- 测试对象:http://localhost:8080(admin/admin123;本地 k3s v1.36.4)
- 用例依据:`test/README.md` + `test/01~05`(验证用例总纲);设计依据 `docs/design/*`、`docs/api/*`
- 测试方式:浏览器 GUI 黑盒(截图+DOM 双取证)+ REST/WS API 实测
- 截图目录:`C:\Users\47910\Downloads\temp\gui-test-screenshots\`
- **重要说明:第二轮开始前 `bin/kubeui-server` 与前端产物被重新构建(11:07,含新前端)。第一轮部分 GUI 结论是对旧 bundle 得出的,已逐条在新版复验并标注状态。**

---

## 一、结论摘要

- 第一轮发现的 8 个 bug 中,**5 个在新构建中已修复**(重启 500、行级 namespace 缺失、时间格式化、监控单位、登录反馈),其余范围收窄。
- 第二轮按用例总纲全量验证,**新发现 3 个 P0/P1 级问题:资源写接口完全无 RBAC 校验(viewer 可删库)、Pod 详情路由不可达(连带终端不可用)、Agent 反连注册即失败且 token 被烧死锁**;另有平台代理不认签发 token 等 2 个 P1。
- 主链路(登录/注册向导/列表/伸缩/事件/审计/Secret 脱敏/单二进制部署形态)基本可用。

| 严重度 | 状态 | 编号 |
|---|---|---|
| P0(RBAC 漏洞) | 新增 | BUG-11 |
| P1(功能不可用) | 新增 | BUG-12、BUG-13、BUG-14 |
| P2 | 新增 | BUG-15、BUG-16、BUG-17、BUG-18 |
| P3 | 新增 | BUG-19 |
| 第一轮已修复 | 5 | BUG-01/02/05/06/08(首轮编号) |
| 第一轮范围收窄 | 2 | BUG-03→BUG-17、BUG-04→BUG-15、BUG-07→BUG-19 |

---

## 二、第一轮 Bug 复验状态(对 11:07 新构建)

| 首轮编号 | 内容 | 新版状态 |
|---|---|---|
| BUG-01 | Deployment 重启 HTTP 500(yaml.Marshal 冒充 JSON patch) | **✅ 已修复**:restart 返回 200,restartedAt 注解打上,Pod 滚动重建 |
| BUG-02 | 全部命名空间视图行级操作缺 namespace(YAML/详情 not found) | **✅ 已修复**:前端 `openYaml`/`onDelete` 传 `row.namespace`(ResourceList.vue:114-125) |
| BUG-03 | 平台管理页面整页交互失效 | **⚠️ 范围收窄→BUG-17**:注册向导页交互已恢复(全流程可走通);/users、/audit 仍无响应 |
| BUG-04 | 伸缩后 WS 增量刷新不推送 | **⚠️ 范围收窄→BUG-15**:pods watch 推送正常;deployments watch 不推送 scale 变更 |
| BUG-05 | 用户管理/审计时间显示原始 RFC3339 | **✅ 已修复**:两页均改用 RelativeTime 相对时间 |
| BUG-06 | 监控指标原始纳核/原始 Ki | **✅ 已修复**:formatCpu/formatBytes 生效(134m、2.7Gi、29Mi);但使用率条恒 0%→BUG-16 |
| BUG-07 | 审计缺操作者/资源类型为原始 path | **✅ 大部分修复**:scale 等资源操作字段齐全;仅 k8s-proxy 记录 username 为空→BUG-19 |
| BUG-08 | 登录错误提示 unauthorized/回车无反馈 | **✅ 已修复**:中文「用户名或密码错误」+ 回车可提交 |
| BUG-09 | 集群列表节点数/总览接入方式为 — | **✅ 已修复**(节点数 1、接入方式"直连") |
| BUG-10 | 顶栏集群选择器不同步 | **✅ 已修复**(显示 `local-k3s (v1.36.4+k3s1)`) |

---

## 三、第二轮新 Bug

### BUG-11【P0·后端·RBAC】资源写接口完全没有权限校验,viewer 可执行全部写操作

- 复现(API,用 viewer 账号 `v1user` 的 token):
  ```
  POST   /api/v1/clusters/local-k3s/configmaps?namespace=default   → 200(创建成功,应 403)
  DELETE /api/v1/clusters/local-k3s/configmaps/viewer-cm3           → 200(删除成功)
  PUT    /api/v1/clusters/local-k3s/configmaps/cm-yaml-test/yaml    → 200(修改成功)
  PUT    /api/v1/clusters/local-k3s/deployments/nginx-test/scale    → 200(伸缩成功)
  POST   /api/v1/clusters/local-k3s/deployments/nginx-test/restart  → 200(重启成功)
  ```
- 预期:P-13/P-17/R-26 — viewer 权限点只有读,`POST /users` 这类管理接口正确返回 40300,但资源写接口全部放行。
- 根因:`backend/internal/api/handler/router.go` 资源路由组只挂了 `middleware.Auth`(JWT 认证),没有任何 `RequirePerm("resources:write")` 类中间件;权限点数据(`GET /users/me/permissions`)本身是正确的。另外 `POST /clusters`(注册)、`DELETE /clusters/:cluster`(注销)、`PUT /clusters/:cluster/kubeconfig`(轮转)也未挂 AdminOnly,任何登录用户可注册/注销集群(代码层面确认)。
- 影响:任意低权账号可删除/改写全部集群资源,属安全漏洞。

### BUG-12【P1·后端】Pod 详情接口路由不可达(连带行级终端不可用)

- 复现:`GET /api/v1/clusters/local-k3s/pods/<pod>?namespace=default` → **404 {"code":40400,"message":"not found"}**(pod 实际存在,logs/yaml/delete 均正常)。
- 对照:同 path 模式下 deployments/configmaps/namespaces/services 详情全部 200;`GET .../pods/:name/yaml` 也 200;唯独裸详情 404。
- 根因(推断):gin 路由树中静态段 `/pods/:name/logs|/terminal` 与通配 `/:resource/:name` 冲突,裸 `GET /pods/:name` 落入 NoRoute(JSON 40400)。修复建议:为 pods 详情单设静态路由或调整通配注册方式。
- 连带影响(UI):pods 行「详情」抽屉显示 "not found";行级「终端」对话框先取 Pod 详情 → 显示 "not found",终端永远打不开(R-14/R-22/R-24 全部被阻塞)。服务端日志:`GET .../pods/nginx-test-78c8f799f9-kpsm4 status:404`。

### BUG-13【P1·后端】Agent 反连注册即失败,一次性 token 被烧,集群永久 Degraded

- 复现(2/2 稳定复现):注册集群 `agent-k3s` → 创建 enroll token → 本机运行 `bin/kubeui-agent`(KUBEUI_SERVER_URL=http://localhost:8080):
  ```
  agent 日志:
  WARN agent connection lost, will retry err="unexpected frame \"dial\" while registering" retry_in=1s
  WARN ... err="register rejected: code=40101 message=invalid enrollment token" retry_in=2s(此后循环)
  ```
- 集群状态:`accessMode=agent, status=degraded, message="agent 反连中"`,永不收敛 ready。
- 分析:注册握手进行中服务端即下发 `dial` 数据帧(疑似注册完成前健康探测/流量已进入隧道),agent 视为协议错误断开;enroll token 一次性语义导致重连永久 40101,只能人工重新签发 token。
- 阻塞:C-15(Agent 上线)、C-16(Agent 模式功能一致)、C-17(断开自动恢复)全部无法验证通过。

### BUG-14【P1·后端】平台代理不认签发的客户端 kubeconfig token(C-23 失败)

- 已通过:C-20 签发(一次性 token+downloadUrl)、C-21 下载(二次下载 40407)、C-22 撤销(revoked=true)。
- 失败:用下载的 kubeconfig(短期 Bearer token)经代理访问:
  ```
  kubectl --kubeconfig client-kc get pods -n default
  → the server has asked for the client to provide credentials(上游 K8s 401)
  ```
- 分析:`proxy.go` Director 未剥离入口请求的 `Authorization` 头即转发;local-k3s 凭证为客户端证书型,无法覆盖已存在的 Authorization 头 → 上游收到平台短期 token → 401。修复:Director 中按上游凭证重写/删除该头。
- 附:k8s-proxy 审计 username 为空(见 BUG-19),也无法追溯使用方。

### BUG-15【P2·后端】deployments 的 WS watch 不推送 scale 变更

- 复现:WS 订阅 `{cluster, namespace:default, resource:deployments}`(初始 added 正常)→ `kubectl scale --replicas=2` → **10s 内无任何 event**;同样方式订阅 pods,删除 Pod 后 `added/modified` 事件正常推送。
- 影响:前端 deployments 列表伸缩后不实时更新(R-05/R-18/O-12 部分失败),与首轮观察一致。

### BUG-16【P2·后端】metrics API 未返回 cpuPct/memPct,使用率条恒 0%

- `GET /api/v1/clusters/:c/metrics/nodes` 只返回 `{cpu:"228696866n", memory:"2928744Ki"}`,无百分比字段;前端 `parsePct(undefined)=0%` → Node 使用率条恒为 0%(与格式化后的 `134m` 并排显示"0%")。
- 设计要求 O-01:「使用率条与 cpuPct/memPct 一致」。

### BUG-17【P2·前端】/users、/audit 页面按钮无响应(渲染正常)

- 现象:两页列表渲染、时间格式化均正常,但「创建用户」「重置密码」「查询」按钮点击/键盘 Enter 均无响应(弹窗不出现);右上角下拉菜单在同页点击也不响应,切到工作负载页则正常。注册向导页(/clusters/register)已恢复正常。
- 自动化无法读取 console,请人工 DevTools 复核;疑似该两个视图存在运行时异常。

### BUG-18【P2·部署】deploy/ 缺 Helm chart

- `deploy/` 目录仅 Dockerfile、config-example.yaml、k8s;无 helm chart(设计 02-features §2.8 P1、D-06/D-07)。Docker 构建与 Helm template 未验证(本机无 helm/docker 构建,建议 CI 补齐)。

### BUG-19【P3·后端】k8s-proxy 审计记录 username 为空

- 审计中所有 k8s-proxy 记录 `username:""`(含 allow 的短期 token 访问),无法归因到用户;与 P-22「字段正确性」不符。

---

## 四、用例总纲执行结果

### 01 平台基座

| 用例 | 结果 | 说明 |
|---|---|---|
| P-01 登录成功 | ✅ | 跳转 /clusters,localStorage 有 token |
| P-02 登录失败文案 | ✅* | UI 显示中文「用户名或密码错误」;审计落 deny(username=admin);API 文案为英文 "invalid username or password"(可改进) |
| P-03 回车提交 | ✅ | 新版回车等同点击 |
| P-04 Token 静默续期 | ✅ | 401 后自动 POST /auth/refresh 并重放(日志可见) |
| P-05 退出登录 | ✅ | 菜单→退出清凭证跳 /login |
| P-06 未登录访问受保护路由 | ✅ | 直接打开 /users → 守卫跳 /login |
| P-07 创建用户 | ✅ | API 创建 operator/viewer 成功 |
| P-08 禁用用户 | ✅ | 禁用后登录 40411 "user is disabled" |
| P-09 重置密码 | ✅ | 一次性密码仅响应出现一次;旧密码失效 |
| P-10 删除用户 | ✅ | 删除后登录 40100 |
| P-11 保护规则(删 admin) | ✅ | 40300 "cannot delete the current user";UI admin 行无删除按钮 |
| P-12 修改自己密码 | ✅* | 端点校验旧密码(401);全流程未测以免破坏环境 |
| P-13 viewer 越权(用户管理) | ✅ | UI/API 均 40300 |
| P-14 时间格式化 | ✅ | 相对时间("10h"/"23m") |
| P-15/16/17 权限点 | ✅ | admin=["*"];operator 含 terminal:use/secrets:read/写;viewer 只读+read-masked |
| P-18 viewer Secret reveal | ✅ | 403 "requires operator privilege" |
| P-19 operator reveal | ✅ | 200 返回解码数据(reveal-secret 审计待核) |
| P-20 viewer 终端 | ⛔ | 被 BUG-12 阻塞(终端本身打不开) |
| P-21~P-24 审计留痕 | ✅* | 登录/伸缩等字段齐全、无删除入口、筛选存在;k8s-proxy 记录 username 空(BUG-19) |
| P-25 审计时间 | ✅ | 相对时间 |
| P-26/27 主题 | ✅ | 亮暗切换即时生效;localStorage kubeUI.theme 持久化 |

### 02 集群管理

| 用例 | 结果 | 说明 |
|---|---|---|
| C-01 注册向导 | ✅ | 4 步向导全流程 UI 走通,wizard-k3s 注册成功 ready |
| C-02 校验失败 | ✅ | 40402 解析失败 / 40404 不可达 |
| C-03 多 context | ✅ | 40403 提示指定 contextName;指定后成功 |
| C-04 列表字段 | ✅ | 接入方式"直连"、节点数真实值、相对时间 |
| C-05 集群概览 | ✅ | 版本/接入方式/Warning 计数 |
| C-06 注销 | ✅ | L3 危险确认(输入名称),列表移除 |
| C-07 状态查询 | ✅ | status/version/lastTransitionTime |
| C-08 kubeconfig 轮转 | ✅ | PUT 后保持 ready |
| C-09 重启恢复 | ✅* | 多次进程重启后集群直接 ready、资源可访问(自动重启循环期间观察) |
| C-10 密钥漂移降级 | ⛔ | 未执行(会破坏加密数据,需单独环境) |
| C-11 旧 token 失效 | ✅* | 重启后旧 access token 401 → 静默 refresh |
| C-12 Enroll Token 管理 | ✅* | 创建/列表/一次性告示 ✅;吊销入口存在,过期自动失效未长测 |
| C-13 viewer 建 token | ✅ | 40300 |
| C-14 安装清单 | ✅ | Namespace/SA/最小 RBAC/Deployment,env 注入正确 |
| C-15/16/17 Agent 上线 | ❌ | BUG-13,100% 失败,集群卡 Degraded |
| C-18 Token 一次性 | ✅* | 二次注册 40101(但首次失败也烧 token=BUG-13 的一部分) |
| C-19 向导 Agent 页签 | ✅ | token+YAML 生成、一次性警示、一键复制 |
| C-20 签发 | ✅ | token/downloadUrl/expiresAt |
| C-21 下载 | ✅ | 一次性 code,二次 40407 |
| C-22 撤销 | ✅ | revoked=true |
| C-23 平台代理 | ❌ | BUG-14,签发 token 被上游 401 |
| C-24 跨集群隔离 | ⛔ | 仅一个集群(代理认证已 401,无法构造对比) |
| C-25 签发/下载审计 | ✅* | 审计有 k8s-proxy 与签发记录;username 空=BUG-19 |

### 03 资源浏览与操作

| 用例 | 结果 | 说明 |
|---|---|---|
| R-01 列表(多类) | ✅ | deployments/pods/configmaps/secrets/nodes/namespaces/pvcs/services/ingresses/crds 渲染与空态正常(statefulsets/daemonsets/pvs/roles 未逐一截图,同构页面) |
| R-02 namespace 筛选 | ✅* | 下拉为动态 namespace 列表;筛选生效 |
| R-03 关键字/labelSelector | ✅* | 输入框存在;模糊匹配未单独断言 |
| R-04 分页 | ✅ | Total 与页码正确 |
| R-05 WS 增量刷新 | ❌ | deployments 不推送(BUG-15);pods 正常 |
| R-06 行级 namespace 透传 | ✅ | 新版已传 row.namespace;后端带 ns 全 200 |
| R-07 时间/单位 | ✅ | 相对时间;监控已格式化 |
| R-08~R-11 新建 | ✅* | 「新建」按钮与模板 Dialog 存在;API 创建 YAML/JSON 双格式 200、重复 40406、缺 name 40001(UI 弹窗内提交未自动化) |
| R-13 CRD 动态资源 | ✅* | CRD 列表 + 浏览资源入口正常 |
| R-14 详情 | ❌ | BUG-12,Pod 详情 404;Deployment 详情 200 |
| R-15 YAML 查看 | ✅ | Secret data `***`;完整 YAML 返回 |
| R-16 YAML 编辑 | ✅ | PUT 生效;外部改动后旧 resourceVersion → 409/40406 冲突提示 |
| R-17 删除 | ✅ | 输入名称确认;删除生效 |
| R-18 伸缩 | ✅ | 实际生效(API/对话框/toast);列表实时性受 BUG-15 |
| R-19 重启 | ✅ | HTTP 200,注解打上(首轮 500 已修复) |
| R-20 StatefulSet 伸缩 | ⛔ | 集群内无 StatefulSet 样本 |
| R-21 Pod 重启(删建) | ✅ | 删除重建成功(WS added/modified 可见) |
| R-22~R-28 终端 | ❌/⛔ | R-22 对话框能弹出但被 BUG-12 阻塞显示 "not found";其余不可测 |
| R-29 历史日志 | ✅ | API 200,内容与容器一致(container/tailLines/previous 参数支持) |
| R-30 实时日志流 | ⛔ | WS stream 端点未单独自动化(GUI 日志入口被详情 404 阻塞) |
| R-31 行级日志入口 | ⛔ | 同上 |

### 04 可观测性

| 用例 | 结果 | 说明 |
|---|---|---|
| O-01 Node 指标 | ✅* | 单位已格式化(134m/2.7Gi);使用率条恒 0%→BUG-16 |
| O-02 容器 Top | ✅ | 排序与格式化正常 |
| O-03 未装 metrics-server | ⛔ | 未构造降级环境 |
| O-04 轮询刷新 | ✅* | 手动刷新数据更新;30s 轮询未长测 |
| O-05 事件列表 | ✅ | Warning 开关、相对时间、involvedObject |
| O-06 事件实时流 | ✅* | pods watch 推送可见(kubectl 删 Pod → added/modified);deployments 不推送=BUG-15 |
| O-07 总览 Warning 计数 | ✅ | 与事件页一致 |
| O-08~O-12 WS 协议 | ✅/❌ | 认证 401/101、订阅回执+初始 added、ping 帧 ✅;deployments watch start 偶发 50100 "start watch failed" 且 scale 变更不推送(BUG-15) |

### 05 部署形态

| 用例 | 结果 | 说明 |
|---|---|---|
| D-01 零配置启动 | ⛔ | 未单独验证(避免与主实例抢 SQLite) |
| D-02 SPA 服务 | ✅ | `/users` 深链 200;未知 /api → JSON 40400 |
| D-03 健康检查 | ✅ | `/healthz` → {"status":"ok"} |
| D-04 优雅关闭 | ✅ | SIGTERM → "agent hub closed"+"server stopped"(日志多处) |
| D-05 CGO | ⛔ | 未执行交叉编译 |
| D-06 Docker 构建 | ⛔ | 本机无 docker |
| D-07 Helm | ❌ | deploy/ 无 chart(BUG-18) |
| D-08 env 覆盖 | ✅ | `KUBEUI_SERVER__ADDR=":8099"` 生效,双下划线嵌套规则可用 |
| D-09 Windows 产物 | ✅* | bin/windows/ 存在 kubeUI-desktop.exe 与 kubeUI-lite.exe(运行时行为未测) |
| D-10~D-13 lite/desktop 运行 | ⛔ | 需 Windows GUI 双击验证,未覆盖 |
| D-14 工程门禁 | ⛔ | go/npm 不在本 shell PATH,建议 CI 执行 |

---

## 五、测试环境与预置操作说明

1. 两轮之间 `bin/kubeui-server`+前端被另一会话重建(11:07),第二轮全部结论基于新构建;第一轮报告中对旧 bundle 的 GUI 结论已在第二节逐条复验修订。
2. 测试期间同机另一会话的 desktop 构建脚本会 `kill kubeui-server`,曾造成第一轮多次 "Failed to fetch" 与懒加载缓存失败;已用自动重启循环保持在线。第一轮"整页点击失效"现象在污染期波及全站,干净环境复验后仅剩 BUG-17(users/audit 两页)。
3. 预置/操作痕迹:API 注册了 `local-k3s`、`wizard-k3s`(UI 向导注册后已注销)、`multi-kc`(已删)、`agent-k3s`(已删);创建用户 op1(已删)、op2/v1user(留存);对 nginx-test 执行过伸缩 1↔2、重启、Pod 删建;创建并删除了 cm-yaml-test/cm-json-test 等测试 ConfigMap;签发 kubeconfig 凭证 2 条(1 条已撤销)。全部为本地测试集群。
4. 阻塞链:BUG-12(Pod 详情 404)→ 阻塞 Pod 详情抽屉、行级终端、实时日志 GUI 入口;BUG-13 → 阻塞 Agent 全链路;BUG-14 → 阻塞客户端 kubeconfig 使用。

# 02 · 集群管理:注册 / 健康状态 / 轮转 / Agent 反连 / 客户端 kubeconfig

## 1. 注册与生命周期

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| C-01 | 注册集群 | `/clusters/register` 粘贴 k3s kubeconfig → 下一步 → 命名 local-k3s | 注册成功;列表出现,状态 ready 徽章;版本显示 `v1.36.x+k3s` |
| C-02 | 校验失败 | 粘贴非法 YAML / 不可达地址的 kubeconfig | 明确错误(40402 解析失败 / 40404 不可达),不落库 |
| C-03 | 多 context | 粘贴含多 context 且未指定的 kubeconfig | 40403,提示选择 contextName |
| C-04 | 列表字段 | 集群列表 | 名称/状态徽章/版本/**接入方式(直连)**/描述/相对时间;「节点数」显示真实数字(非 —) |
| C-05 | 集群概览 | `/clusters/local-k3s/overview` | 版本、接入方式卡片(直连)、Warning 事件计数、节点相关指标正常 |
| C-06 | 注销 | 删除集群(输入名称确认) | 列表移除;资源页全部 40401;审计 action=delete |
| C-07 | 状态查询 | `GET /clusters/local-k3s/status` | 返回 status/version/lastTransitionTime/message,不透传探测 |
| C-08 | kubeconfig 轮转 | `PUT /clusters/:cluster/kubeconfig` 换新凭证 | 平滑热更新;期间 Degraded、成功后 Ready;审计留痕 |

## 2. 服务重启恢复(重点回归)

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| C-09 | 重启后运行时恢复 | 杀掉 kubeui-server 进程并等自动重启 | **无需重新注册**:集群列表直接出现 local-k3s 并收敛为 ready;资源页可正常访问(此前会 40401) |
| C-10 | 密钥漂移降级 | 用空 masterKey 启动后重启(密钥随机) | 集群置 **offline**,message 说明「master key mismatch, rotate kubeconfig」;记录不消失;资源页给明确错误而非静默失败 |
| C-11 | 旧 token 失效 | 重启后用重启前的 accessToken | 401;前端静默 refresh(新的 jwtSecret 下 refresh 也可能失效则跳登录,属预期) |

## 3. Agent 反连模式(V1)

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| C-12 | Enroll Token 管理 | admin 创建 enroll-token(绑集群、ttl) | 列表可见、**token 只显示一次**、可吊销、过期自动失效 |
| C-13 | 非授权 | viewer 创建 enroll-token | 40300 |
| C-14 | 安装清单 | `GET /api/v1/agent/manifest?token=` | 返回可 `kubectl apply` 的 YAML(Namespace/SA/最小 RBAC/Deployment,env 注入 KUBEUI_SERVER_URL/KUBEUI_ENROLL_TOKEN) |
| C-15 | Agent 上线 | 在被管集群 apply 清单,`KUBEUI_SERVER_URL` 指向平台 | 信令通道建立;集群 accessMode 变 **agent**、状态 ready;审计记录 |
| C-16 | Agent 模式功能一致 | agent 集群上浏览资源/日志/终端 | 与直连完全一致(TCP 透传,TLS 端到端) |
| C-17 | Agent 断开 | 杀掉 agent pod | 集群 Degraded,message「agent 反连中」;agent 自动重连(1s→60s 退避)后恢复 ready |
| C-18 | Token 一次性 | 同一 token 二次注册 | 40101 拒绝 |
| C-19 | 注册向导 Agent 页签 | 注册向导第 4 步 | 生成 token + YAML 展示/复制,警示 token 只显示一次 |

## 4. 客户端 kubeconfig 签发(`/credentials`)

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| C-20 | 签发 | 选集群、ttl(默认 24h、上限 7d)、描述 → 签发 | 展示一次性 token/downloadUrl(倒计时);`GET /kubeconfigs` 列表出现该条(revoked/expiresAt 状态) |
| C-21 | 下载 | 用 downloadUrl 的 one-time code 下载 | 返回 kubeconfig 文件(server 指向平台 `/k8s/{cluster}`,token 为短期 Bearer);**同 code 二次下载失败**(40407) |
| C-22 | 撤销 | DELETE 该凭证 | 之后用其 token 调 `/k8s/` 代理返回 401;列表状态 revoked |
| C-23 | 平台代理 | 用下载的 kubeconfig 跑 `kubectl --kubeconfig ... get pods` | 正常返回(经 `/k8s/local-k3s/` 代理);`kubectl logs -f`、`kubectl exec` 流式端点可用 |
| C-24 | 越权隔离 | 用集群 A 签发的 token 访问 `/k8s/集群B/...` | 401/403,不允许跨集群 |
| C-25 | 审计 | 每次签发/下载/代理写操作 | 审计留痕 |

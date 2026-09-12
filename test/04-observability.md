# 04 · 可观测性:监控 / 事件 / WebSocket

## 1. 监控(metrics-server)

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| O-01 | Node 指标 | `/clusters/:c/monitoring` | CPU 显示 `98m` 毫核、内存 `2.7Gi` 等可读单位(禁止 `97804391n`/`2730344Ki` 原始串);使用率条与 `cpuPct/memPct` 一致 |
| O-02 | Pod/容器 Top | 容器用量 Top 表 | 同样格式化;排序合理 |
| O-03 | 未装 metrics-server | 用无 metrics-server 集群(或停掉) | 显式降级提示(50100 文案),不影响其他页面 |
| O-04 | 轮询刷新 | 30s 间隔轮询 | 数据更新,无内存泄漏(长时间停留页面不卡) |

## 2. 事件

| # | 用例 | 预期 |
|---|---|---|
| O-05 | 事件列表 | `/clusters/:c/events`:involvedObject kind/name、type(Warning 过滤开关)、时间相对化、分页 |
| O-06 | 事件实时流 | kubectl delete pod 触发调度事件 → 列表 6s 内自动出现新事件,无需刷新 |
| O-07 | 集群总览摘要 | Overview 页 Warning 事件计数与事件页一致 |

## 3. WebSocket 协议(`/api/v1/watch`)

| # | 用例 | 预期 |
|---|---|---|
| O-08 | 订阅/退订 | subscribe(pods/deployments/events/clusters/audit/podlogs)回执 requestId;组件卸载自动 unsubscribe |
| O-09 | 心跳 | 客户端 25s ping → 服务端 pong;30s 无帧服务端断开;前端指数退避(1s→30s+抖动)重连并**重放订阅** |
| O-10 | 认证 | 无 token/失效 token 连接被拒(error 帧 40100);`?token=` 有效时正常 |
| O-11 | clusters 推送 | 集群状态迁移(如杀 agent 或断网集群)→ watch 订阅者收到 clusters 事件 |
| O-12 | 事件对象 | event.object 为 K8s 原生对象,前端归一化后行数据与 REST 列表 DTO 字段一致(name/namespace/status) |

# v911 多集群管理平台 · 验证用例总纲

> 面向 AI/人工验证人员:按模块拆分的全功能验证清单,每条用例含**操作步骤**与**预期结果**。
> 发现问题请写入 `docs/test.md`(格式参照该文件既有 Bug 条目:编号、严重度、复现步骤、API/截图证据、根因定位)。

## 测试环境

- 服务:`./bin/v911-server --config deploy/config-example.yaml`,监听 `:8080`,SPA 与 API 同端口
- 账号:`admin / admin123`(首启自动创建);可经 UI 或 `POST /api/v1/users` 创建 operator/viewer 测试账号
- 集群:本机 k3s(kubeconfig `/etc/rancher/k3s/k3s.yaml`),注册名 `local-k3s`;建议预先部署一个多副本 nginx deployment 用于行级操作
- API 约定:所有响应 `{code, message, data}`,`code=0` 成功;错误码表见 `docs/api/rest.md`;JWT 放 `Authorization: Bearer <token>`
- WebSocket:认证用 `?token=<accessToken>` query 参数

## 验证方式要求

1. UI 验证用浏览器黑盒操作(截图 + Console 取证);API 验证用 curl 实测并贴响应。
2. 每条用例标注 ✅/❌;❌ 必须附最小复现与证据。
3. 危险操作(删除/重启/注销)统一有**输入资源名称确认**弹窗,不输入或输错时按钮禁用。
4. 时间显示:列表一律相对时间,悬停 title 显示完整 RFC3339;不允许出现未格式化原始串。
5. 指标显示:CPU 用 `m`(毫核)、内存用 Ki/Mi/Gi,不允许 `97804391n` 这类原始值。

## 文件索引

| 文件 | 范围 |
|---|---|
| [01-platform.md](01-platform.md) | 登录/JWT 会话、用户管理、RBAC 权限点、审计日志、主题 |
| [02-cluster.md](02-cluster.md) | 集群注册/列表/概览/注销、kubeconfig 轮转、服务重启恢复、Agent 反连与接入向导、客户端 kubeconfig 签发 |
| [03-resources.md](03-resources.md) | 14 类资源列表/新建/详情/YAML 编辑/删除、伸缩、重启、行级终端 exec、Pod 日志 |
| [04-observability.md](04-observability.md) | 监控(metrics-server)、事件列表与实时流、WS 资源增量刷新 |
| [05-deploy.md](05-deploy.md) | 单二进制、健康检查、Docker/Helm、嵌入前端 SPA |

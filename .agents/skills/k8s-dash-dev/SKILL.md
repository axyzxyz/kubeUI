---
name: k8s-dash-dev
description: 本项目(K8s 多集群管理 Dashboard,Go + Vue3)的开发工作流与规范入口。凡在本仓库内写后端 Go 代码、前端 Vue/TS 代码、设计 API、新增页面、写测试或构建部署时,都必须先加载本 skill,即使没有明确提到"规范"或"工作流"。
---

# k8s-dash-dev:项目开发工作流

多集群 Kubernetes 管理 Dashboard(类 Kubeoard/Lens)。前后端分离:后端 Go(client-go,单二进制),前端 Vue 3 + TypeScript + Vite。通过 kubeconfig 注册集群;支持 Agent 反连与客户端 kubeconfig 下载。

## 第一步:先读设计文档

写任何代码前,先按需阅读 `docs/design/` 下的设计文档(按任务选择,不必全读):

| 任务 | 必读文档 |
|---|---|
| 后端模块/接口/集群管理逻辑 | `docs/design/01-architecture.md` |
| 确认功能范围与优先级(该不该做、属于哪期) | `docs/design/02-features.md` |
| 写页面/组件/调整布局样式 | `docs/design/03-ui-design.md` |
| 命名、错误处理、日志、目录结构、提交信息 | `docs/design/04-coding-standards.md` |

设计文档与代码冲突时,以代码现状为准,并在回复中向用户指出冲突点,不要静默改文档。

## 目录速查

```text
backend/    Go 后端(cmd/ 入口,internal/ 私有包,pkg/ 可导出)
frontend/   Vue 3 前端(src/api 统一请求层,src/views 页面,src/stores Pinia)
docs/       设计文档与 ADR
deploy/     Dockerfile / Helm chart
```

详细规则见 `docs/design/04-coding-standards.md`。

## 常用命令

```bash
# 后端
cd backend && make lint test build   # golangci-lint / go test ./... / 单二进制构建
go run ./cmd/server                  # 本地启动 Web 服务

# 前端
cd frontend && npm run lint && npm run build
npm run dev                          # Vite dev server,代理 /api 到后端

# 根目录
make all                             # 前后端一起检查 + 构建
```

提交信息遵循 Conventional Commits(`feat:`, `fix:`, `refactor:` …),范围用模块名,如 `feat(backend): cluster health check`。

## 硬性约束(违反即返工)

- API 一律挂 `/api/v1` 下,统一响应体结构(见规范文档第 4 节);新增接口必须同步更新双端类型定义。
- 组件内禁止裸 `fetch`/`axios` 直调,必须走 `src/api` 封装层。
- Go 侧禁止忽略 error、禁止在库代码里直接 `log.Fatal`;goroutine 必须有退出路径。
- kubeconfig 属于敏感凭证:任何日志、错误信息、API 响应中不得出现其中的证书/key 内容。
- 危险操作(删除资源、下线集群)的后端接口必须可审计,走审计日志模块。

## 验证方式

- 改后端:`make lint test` 必须绿;涉及 API 变化时手工 curl 验证一次并在回复中贴结果。
- 改前端:`npm run lint && npm run build` 必须绿;UI 改动用浏览器实际打开页面截图确认,不要只看代码。
- 涉及真实集群的操作优先用 `kind` 或 `k3d` 起本地集群测试,不要碰用户的生产 kubeconfig。

# 05 · 部署形态:单二进制 / Docker / Helm

## 1. 单二进制与嵌入式 SPA

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| D-01 | 零配置启动 | `./bin/v911-server` 无 config | 默认 :8080 + SQLite,自动创建 admin/admin123 并日志提示;随机 dev masterKey/jwtSecret 时有 WARN |
| D-02 | SPA 服务 | `GET /` 与 `/users` 等深链 | 返回 index.html(200);`/assets/*.js|css` 200;未知 `/api/xxx` 返回 JSON 40400(不落 HTML) |
| D-03 | 健康检查 | `GET /healthz` | `{"status":"ok"}`(不得被 SPA 回退吞掉) |
| D-04 | 优雅关闭 | SIGTERM | 日志「server stopped」;agent hub/健康循环/informer 全部退出(无 goroutine 泄漏) |
| D-05 | CGO | `CGO_ENABLED=0` 构建 | 成功;amd64/arm64 可交叉编译 |

## 2. Docker / Helm

| # | 用例 | 预期 |
|---|---|---|
| D-06 | Docker 构建 | `docker build -f deploy/Dockerfile -t v911 .` 成功;镜像含 server+agent 双入口;scratch/alpine + ca-certificates |
| D-07 | Helm template | `helm template` 语法正确;values 可切 postgres;ingress 带 WS 注解(read-timeout、`/k8s/` 禁请求体缓冲) |
| D-08 | config-example | 字段与 `backend/internal/config` 一一对应;env 覆盖规则 `V911_*`、嵌套 `V911_SERVER__ADDR`/`V911_DATABASE__DSN`(双下划线)实测生效 |

## 3. 工程门禁

| # | 用例 | 预期 |
|---|---|---|
| D-14 | 后端门禁 | `gofmt -l` 空、`go vet ./...` 零输出、`go test -race ./...` 全绿 |
| D-15 | 前端门禁 | `npm run lint`(0 warning)、`vue-tsc --noEmit` 无错、`npm run build` 成功 |
| D-16 | 组件规模 | 每个 .vue ≤ 300 行;组件/store 内无裸 fetch/any/console.log |
| D-17 | 日志安全 | 全链路日志与 API 响应中不出现 kubeconfig 证书/key、Secret 明文、token 内容 |

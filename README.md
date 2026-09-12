# v911

多集群 Kubernetes 管理 Dashboard:一个 Go 单二进制(server)+ Vue 3 前端(embed 进二进制),通过 kubeconfig 注册集群,支持 Agent 反连与客户端 kubeconfig 下载。

架构一句话:**v911 Server 单二进制同时提供 REST API(`/api/v1`)、WebSocket(`/api/v1/watch`)与 K8s API 反向代理(`/k8s/{cluster}/*`),直连或经 Agent WSS 反连访问多集群;前端产物 go:embed 进同一二进制,默认 SQLite 零外部依赖。**

## 快速开始

```bash
make build && ./bin/v911-server
# 或指定配置:
./bin/v911-server --config deploy/config-example.yaml
```

打开 `http://localhost:8080`。

**默认管理员**:`admin / admin123`(首次启动自动创建,由 `security.adminPassword` 配置,登录后请立即修改)。

## 功能

### MVP
- 集群注册/注销/健康检查(kubeconfig 注册,状态机 ready/degraded/reconnecting/offline)
- 资源浏览:11 类核心资源列表 + YAML 查看/编辑
- Pod 历史日志 + 实时日志流(WS)、events 列表
- JWT 登录(access 2h + refresh 7d)、内置 admin/viewer 角色、写操作审计
- metrics-server 节点/Pod CPU 内存
- 单二进制部署(SQLite,CGO_ENABLED=0,前端产物 go:embed 内嵌 + SPA history 回退)

### V1
- Web Terminal(exec)
- Agent 反连模式 + 接入向导(无直达网络的内网集群)
- 客户端 kubeconfig 临时链接(`/k8s/` 代理,kubectl / Lens 直接可用)
- 完整平台 RBAC(operator)+ Secret 脱敏
- 用户管理:roles/role-bindings 只读、admin 重置密码/删除用户、`/users/me/permissions` 权限点
- CRD 动态资源透传(discovery 解析 GVR)+ CRD 浏览页
- PostgreSQL 支持、Helm chart、Docker 镜像发布

## 配置

> **kubectl/Lens 经平台代理访问(客户端 kubeconfig)必须启用 HTTPS**:kubectl 1.31+ 出于安全策略不通过明文 HTTP 发送 Bearer 凭证。在 `deploy/config-example.yaml` 设置 `server.tls.certFile/keyFile` 即以 HTTPS 监听(自签证书可用 `openssl req -x509 -newkey rsa:2048 -nodes -keyout tls.key -out tls.crt -days 365 -subj /CN=localhost` 生成)。

复制 `deploy/config-example.yaml` 并修改,字段即文档(监听地址、db.driver sqlite/postgres、dsn、security.masterKey、auth.jwtSecret、log level/format 等)。敏感字段支持环境变量覆盖,如 `V911_MASTER_KEY`、`V911_AUTH_JWT_SECRET`。

生成 kubeconfig 加密主密钥:`openssl rand -base64 32`。


## 开发

```bash
make lint        # 后端 gofmt/vet + 前端 eslint(容错)
make test        # 后端 go test -race + 前端测试
make build       # 前端 dist + bin/v911-server + bin/v911-agent
make all         # lint + test + build
make clean
```

提交信息遵循 Conventional Commits(`feat(backend): ...`),由 `commitlint.config.mjs` 校验。

## 目录结构

```text
backend/    Go 后端(cmd/server 主服务 + cmd/agent 反连代理;internal/ 私有包)
frontend/   Vue 3 + TS + Vite(src/api 统一请求层,src/views 页面,src/stores Pinia)
docs/
├── design/   设计文档(架构、特性、UI、编码规范)
└── api/      REST / WebSocket 契约文档(联调基准)
deploy/
├── Dockerfile            多阶段构建(server + agent 双入口)
├── config-example.yaml   配置示例(即配置即文档)
└── k8s/                  Helm chart(server + 可选 agent)
Makefile    唯一任务入口
```

## Docker / K8s 部署

```bash
docker build -f deploy/Dockerfile -t v911 .
# agent 独立镜像:
docker build -f deploy/Dockerfile --target agent -t v911-agent .

# Helm(本机无 helm 可用 docker 跑 alpine/helm template 验证):
helm install v911 deploy/k8s --set secret.masterKey=$(openssl rand -base64 32)
```

生产部署在 Nginx/Ingress 后需:WebSocket upgrade 透传;`/k8s/` 端点禁用请求体缓冲(chart 的 Ingress 模板已内置注解)。

## Agent 部署(被管集群)

在"集群接入向导"生成 Enrollment Token 后:

```bash
helm template v911 deploy/k8s --set agent.enabled=true \
  --set agent.serverUrl=https://v911.example.com \
  --set agent.enrollToken=<token> | kubectl apply -f -
```

或直接运行二进制(参数优先,环境变量 `V911_SERVER_URL`/`V911_ENROLL_TOKEN` 兜底):

```bash
./bin/v911-agent -server https://v911.example.com -token <token>
# 测试环境可加 -insecure 跳过平台 TLS 校验
```

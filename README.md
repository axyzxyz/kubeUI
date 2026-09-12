# kubeUI

多集群 Kubernetes 管理 Dashboard:一个 Go 单二进制(server)+ Vue 3 前端(embed 进二进制),通过 kubeconfig 注册集群,支持 Agent 反连与客户端 kubeconfig 下载。

架构一句话:**kubeUI Server 单二进制同时提供 REST API(`/api/v1`)、WebSocket(`/api/v1/watch`)与 K8s API 反向代理(`/k8s/{cluster}/*`),直连或经 Agent WSS 反连访问多集群;前端产物 go:embed 进同一二进制,默认 SQLite 零外部依赖。**

## 快速开始

**方式一:Docker(推荐,无需装 Go/Node)**

```bash
docker run -d --name kubeui -p 8080:8080 -v kubeui-data:/var/lib/kubeui/data \
  ghcr.io/axyzxyz/kubeui:latest
```

**方式二:二进制** — 从 [Releases](https://github.com/axyzxyz/kubeUI/releases) 下载压缩包解压后运行,或本地构建:

```bash
./kubeui-server-linux-amd64
# 或:
make build && ./bin/kubeui-server
# 指定配置:
./kubeui-server --config deploy/config-example.yaml
```

打开 `http://localhost:8080`。**默认管理员**:`admin / admin123`(首次启动自动创建,由 `security.adminPassword` 配置,登录后请立即修改)。

## 下载

**二进制压缩包**:[https://github.com/axyzxyz/kubeUI/releases](https://github.com/axyzxyz/kubeUI/releases)(打 `v*` tag 后 CI 自动构建发布)

| 产物 | 平台 | 内容 |
|---|---|---|
| `kubeui-<ver>-linux-amd64.tar.gz` | Linux x86_64 | kubeui-server + kubeui-agent(前端已内嵌) |
| `kubeui-<ver>-windows-amd64.zip` | Windows x86_64 | kubeui-server.exe + kubeui-agent.exe(前端已内嵌) |

**容器镜像**(ghcr.io,随 Release 同步发布):

| 镜像 | 说明 |
|---|---|
| `ghcr.io/axyzxyz/kubeui:<tag>` / `:latest` | server 运行镜像(内含 kubeui-agent 二进制,可 `--entrypoint` 复用) |
| `ghcr.io/axyzxyz/kubeui-agent:<tag>` / `:latest` | 纯 agent 镜像(用于被管集群) |

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

复制 `deploy/config-example.yaml` 并修改,字段即文档(监听地址、db.driver sqlite/postgres、dsn、security.masterKey、auth.jwtSecret、log level/format 等)。敏感字段支持环境变量覆盖,如 `KUBEUI_MASTER_KEY`、`KUBEUI_AUTH_JWT_SECRET`。

生成 kubeconfig 加密主密钥:`openssl rand -base64 32`。


## 开发

```bash
make lint        # 后端 gofmt/vet + 前端 eslint(容错)
make test        # 后端 go test -race + 前端测试
make build       # 前端 dist + bin/kubeui-server + bin/kubeui-agent
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

## Docker 部署

**运行 server**(零配置即可启动,SQLite 数据落容器 `/var/lib/kubeui/data`):

```bash
docker run -d --name kubeui -p 8080:8080 -v kubeui-data:/var/lib/kubeui/data \
  ghcr.io/axyzxyz/kubeui:latest
```

自定义配置(密钥等生产必配项见「配置」一节):

```bash
docker run -d --name kubeui -p 8080:8080 -v kubeui-data:/var/lib/kubeui/data \
  -v $(pwd)/config.yaml:/etc/kubeui/config.yaml \
  ghcr.io/axyzxyz/kubeui:latest --config /etc/kubeui/config.yaml
```

**运行 agent**(在被管集群/能同时访问平台与目标 APIServer 的机器上,参数优先、环境变量兜底):

```bash
docker run -d --name kubeui-agent --restart=always \
  ghcr.io/axyzxyz/kubeui-agent:latest \
  -server https://kubeui.example.com -token <enroll-token>
# 等价环境变量形式:
#   -e KUBEUI_SERVER_URL=https://kubeui.example.com -e KUBEUI_ENROLL_TOKEN=<token>
```

**自建镜像**(网络受限环境可加 `--build-arg GOPROXY=https://goproxy.cn,direct`):

```bash
docker build -f deploy/Dockerfile --target server -t kubeui .        # server 镜像
docker build -f deploy/Dockerfile --target agent -t kubeui-agent .   # agent 镜像
```

## Helm / K8s 部署

```bash
helm install kubeui deploy/k8s --set secret.masterKey=$(openssl rand -base64 32)
```

生产部署在 Nginx/Ingress 后需:WebSocket upgrade 透传;`/k8s/` 端点禁用请求体缓冲(chart 的 Ingress 模板已内置注解)。

## Agent 部署(被管集群)

在"集群接入向导"生成 Enrollment Token 后:

```bash
helm template kubeUI deploy/k8s --set agent.enabled=true \
  --set agent.serverUrl=https://kubeUI.example.com \
  --set agent.enrollToken=<token> | kubectl apply -f -
```

或直接运行二进制(参数优先,环境变量 `KUBEUI_SERVER_URL`/`KUBEUI_ENROLL_TOKEN` 兜底):

```bash
./kubeui-agent -server https://kubeui.example.com -token <token>
# 测试环境可加 -insecure 跳过平台 TLS 校验
# Docker 方式见「Docker 部署 · 运行 agent」
```

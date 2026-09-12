# kubeui 多阶段 Dockerfile
# 构建镜像内同时包含 server 与 agent 两个入口二进制:
#   默认 ENTRYPOINT 为 server:kubeui-server --config ...
#   agent 方式一(同一镜像): docker run <image> kubeui-agent
#   agent 方式二(独立 target): docker build --target agent -t kubeui-agent .
#
# 注:本文件由 AI 按 docs/design/01-architecture.md §8 编写,后端模块仍在并行开发中。

# ---------- 阶段 1:构建前端 ----------
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json* ./
# 前端依赖仍在并行开发,lockfile 可能缺失:存在则 frozen,否则退化为普通 install
RUN if [ -f package-lock.json ]; then npm ci --no-audit --no-fund; \
    else npm install --no-audit --no-fund; fi
COPY frontend/ ./
RUN npm run build \
    || { echo "ERROR: frontend build failed (npm run build)"; exit 1; }

# ---------- 阶段 2:构建后端(server + agent) ----------
FROM golang:1.26-alpine AS backend
# GOPROXY 可覆盖:网络受限环境用 --build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
# 前端产物 embed:backend/internal/web 以 go:embed all:dist 打包前端(history 路由回退 index.html)
COPY --from=frontend /src/frontend/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/kubeui-server ./cmd/server \
    || { echo "ERROR: kubeui-server build failed"; exit 1; }
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/kubeui-agent ./cmd/agent \
    || { echo "ERROR: kubeui-agent build failed"; exit 1; }

# ---------- 阶段 3(server target):server 运行镜像 ----------
FROM alpine:3.20 AS server
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 kubeui \
 && mkdir -p /var/lib/kubeui/data && chown -R kubeui:kubeui /var/lib/kubeui
COPY --from=backend /out/kubeui-server /usr/local/bin/kubeui-server
COPY --from=backend /out/kubeui-agent  /usr/local/bin/kubeui-agent
COPY deploy/config-example.yaml /etc/kubeui/config-example.yaml
# SQLite 默认落 ./data/kubeui.db(相对工作目录);持久化: -v kubeui-data:/var/lib/kubeui/data
WORKDIR /var/lib/kubeui
VOLUME ["/var/lib/kubeui/data"]
USER kubeui
EXPOSE 8080
# 零配置启动;自定义配置: -v /path/config.yaml:/etc/kubeui/config.yaml + CMD 覆盖
ENTRYPOINT ["/usr/local/bin/kubeui-server"]
CMD []
# 运行 agent: docker run <image> --entrypoint /usr/local/bin/kubeui-agent <args>

# ---------- 阶段 3b(可选 target):纯 agent 镜像 ----------
FROM alpine:3.20 AS agent
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 kubeui
COPY --from=backend /out/kubeui-agent /usr/local/bin/kubeui-agent
USER kubeui
# Agent 无需监听端口,主动 WSS 反连 server
ENTRYPOINT ["/usr/local/bin/kubeui-agent"]

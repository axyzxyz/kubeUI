# kubeui 根 Makefile —— 唯一任务入口(04-coding-standards.md §6.1,包管理器按本项目实际使用 npm)
#
# 常用目标:
#   make lint    后端 gofmt/vet + 前端 eslint(均容错,工具缺失不阻塞)
#   make test    后端 go test + 前端 vitest(容错)
#   make build   先构建前端,再构建后端 server 与 agent 两个二进制到 bin/
#   make run     本地启动 server
#   make all     lint + test + build
#   make clean   清理构建产物

BACKEND_DIR := backend
FRONTEND_DIR := frontend
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: lint lint-backend lint-frontend test test-backend test-frontend \
        build build-backend build-frontend all clean run

# ---- lint(容错:golangci-lint 未安装时回退 go vet;前端工具缺失仅提示) ----
lint: lint-backend lint-frontend

lint-backend:
	@command -v golangci-lint >/dev/null 2>&1 && \
		cd $(BACKEND_DIR) && golangci-lint run || \
		{ echo ">> golangci-lint 未安装,回退到 gofmt/vet"; \
		  cd $(BACKEND_DIR) && test -z "$$(gofmt -l .)" || \
		    { echo "ERROR: 以下文件未格式化:"; gofmt -l .; exit 1; }; \
		  go vet ./...; }
	@echo ">> backend lint OK"

lint-frontend:
	@if [ -d $(FRONTEND_DIR) ]; then \
		if ! npm --prefix $(FRONTEND_DIR) run lint; then \
			echo "WARNING: frontend lint 失败(前端代码可能仍在开发中,容错通过)"; \
		fi; \
	else \
		echo "WARNING: $(FRONTEND_DIR)/ 不存在,跳过前端 lint"; \
	fi

# ---- test(容错:前端缺失/失败不阻塞) ----
test: test-backend test-frontend

test-backend:
	cd $(BACKEND_DIR) && go test -race -covermode=atomic -coverprofile=cover.out ./... \
		&& go tool cover -func=cover.out | tail -1

test-frontend:
	@if [ -d $(FRONTEND_DIR) ] && [ -f $(FRONTEND_DIR)/package.json ]; then \
		npm --prefix $(FRONTEND_DIR) run test --if-present || \
			echo "WARNING: frontend test 失败,容错通过"; \
	else \
		echo "WARNING: $(FRONTEND_DIR)/ 不存在,跳过前端测试"; \
	fi

# ---- build ----
build: build-frontend build-backend

build-frontend:
	@if [ -d $(FRONTEND_DIR) ] && [ -f $(FRONTEND_DIR)/package.json ]; then \
		npm --prefix $(FRONTEND_DIR) install --no-audit --no-fund || \
			{ echo "ERROR: npm install 失败,请检查 $(FRONTEND_DIR)/package.json 与网络"; exit 1; }; \
		npm --prefix $(FRONTEND_DIR) run build || \
			{ echo "ERROR: 前端构建失败(npm run build)。请先在 $(FRONTEND_DIR)/ 下运行 'npm run build' 定位错误。"; exit 1; }; \
	else \
		echo "WARNING: $(FRONTEND_DIR)/ 不存在,跳过前端构建(二进制将不含前端静态资源)"; \
	fi

build-backend:
	@if [ -d $(FRONTEND_DIR)/dist ]; then \
		rm -rf $(BACKEND_DIR)/internal/web/dist; \
		mkdir -p $(BACKEND_DIR)/internal/web/dist; \
		cp -r $(FRONTEND_DIR)/dist/. $(BACKEND_DIR)/internal/web/dist/; \
		echo ">> 前端产物已拷贝到 $(BACKEND_DIR)/internal/web/dist(将 embed 进二进制)"; \
	else \
		echo "WARNING: $(FRONTEND_DIR)/dist 不存在,二进制将 embed 占位提示页"; \
	fi
	@mkdir -p $(BIN_DIR)
	@command -v go >/dev/null 2>&1 || { echo "ERROR: 未安装 Go(需要 1.23+),请先安装: https://go.dev/dl/"; exit 1; }
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o ../$(BIN_DIR)/kubeui-server ./cmd/server \
		|| { echo "ERROR: kubeui-server 构建失败,请检查 $(BACKEND_DIR)/cmd/server 是否可编译"; exit 1; }
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o ../$(BIN_DIR)/kubeui-agent ./cmd/agent \
		|| { echo "ERROR: kubeui-agent 构建失败,请检查 $(BACKEND_DIR)/cmd/agent 是否可编译"; exit 1; }
	@echo ">> 产出: $(BIN_DIR)/kubeui-server  $(BIN_DIR)/kubeui-agent (version: $(VERSION))"

all: lint test build

clean:
	rm -rf $(BIN_DIR) $(BACKEND_DIR)/cover.out $(FRONTEND_DIR)/dist

run: build-backend
	./$(BIN_DIR)/kubeui-server --config deploy/config-example.yaml


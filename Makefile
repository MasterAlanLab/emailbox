# Go + React 全栈项目 Makefile
# 提供统一的项目管理命令

# 工具版本统一在这里定义。
# golangci-lint 版本必须与 .github/workflows/ci.yml 中 golangci-lint-action 的
# version 保持一致：本地与 CI 版本不同会出现「本地绿、CI 红」的假结果。
GOLANGCI_LINT_VERSION := v2.12.2
SQLC_VERSION := v1.30.0
AIR_VERSION := v1.61.7

.PHONY: help deps sqlc-generate sqlc-verify test lint lint-go lint-web lint-web-fix build build-go build-web dev run clean docker tools check

# 默认目标
help: ## 显示帮助信息
	@echo "Go + React 全栈项目管理命令:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | grep -v -E '^(lint-|build-go|build-web)' | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""

# 安装依赖
deps: ## 安装项目依赖
	@echo "📦 安装项目依赖..."
	@echo "🔧 安装 Go 依赖..."
	@# 只 download 不 tidy：安装依赖不应带有改写 go.mod/go.sum 的副作用
	go mod download
	@echo "🔧 安装前端依赖..."
	cd web && bun install --frozen-lockfile
	@echo "✅ 依赖安装完成"

gen-key: ## 生成一个新的 ENCRYPTION_KEY
	@go run ./scripts/genkey

sqlc-generate: ## 生成 SQLite/PostgreSQL 查询代码
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

sqlc-verify: sqlc-generate ## 检查 sqlc 生成代码是否最新
	@# 用 git status 而非 git diff：新生成但未 git add 的文件是未跟踪状态，git diff 看不见
	@test -z "$$(git status --porcelain -- db/generated)" || { \
		echo "❌ db/generated 与 db/query 不一致，请提交 sqlc 生成结果:"; \
		git status --porcelain -- db/generated; \
		git diff -- db/generated; exit 1; }

test: ## 运行前后端测试
	@echo "🧪 运行后端测试..."
	@# -race 与 CI (ci.yml) 保持一致，否则数据竞争只会在推送后才暴露
	go test -race $$(go list ./... | grep -v /web/node_modules)
	@echo "🧪 运行前端测试..."
	cd web && bun run test

# 代码检查
lint: lint-go lint-web ## 运行所有代码检查

lint-go: ## 运行 Go 代码检查
	@echo "🔍 运行 Go 代码检查..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "❌ golangci-lint 未安装"; \
		echo "📦 安装方式: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi

lint-web: ## 运行前端代码检查
	@echo "🔍 运行前端代码检查..."
	@# 格式检查与 CI (ci.yml) 保持一致，否则 prettier 问题只会在推送后才暴露
	cd web && bun run format:check
	cd web && bun run lint

lint-web-fix: ## 自动修复前端格式与 lint 问题
	@echo "🔧 格式化前端代码..."
	cd web && bun run format
	@echo "🔧 修复前端 lint 问题..."
	cd web && bun run lint --fix

# 构建
build: ## 构建项目
	@echo "🔨 构建项目..."
	./scripts/build.sh

build-go: ## 仅构建 Go 后端
	@echo "🔨 构建 Go 后端..."
	@# modernc.org/sqlite 为纯 Go 实现，无需 CGO
	CGO_ENABLED=0 go build -o server main.go

build-web: ## 仅构建前端
	@echo "🔨 构建前端..."
	cd web && bun run build

# 开发
dev: ## 启动前后端开发环境（同时启动后端和前端）
	@echo "🚀 启动前后端开发环境..."
	@echo "📡 后端将使用 Air 热重载，地址: http://localhost:1323"
	@echo "🌐 前端将使用 Vite，地址: http://localhost:5173"
	@trap 'kill 0' EXIT; \
	( \
		if command -v air >/dev/null 2>&1; then \
			echo "🔥 启动后端热重载..."; \
			air; \
		else \
			echo "❌ air 未安装，退化为无热重载模式 (go run)"; \
			echo "📦 安装 air: make tools"; \
			go run main.go; \
		fi \
	) & \
	(cd web && bun run dev) & \
	wait

# 运行
run: ## 运行项目（需要先构建）
	@echo "🚀 启动服务器..."
	@if [ -f "server" ]; then \
		./server; \
	else \
		echo "❌ 服务器未构建，请先运行: make build"; \
		exit 1; \
	fi

# 清理
clean: ## 清理构建文件
	@echo "🧹 清理构建文件..."
	rm -rf web/dist
	rm -rf static
	rm -rf tmp
	rm -f server
	@echo "✅ 清理完成"

# Docker
docker: ## 构建 Docker 镜像
	@echo "🐳 构建 Docker 镜像..."
	@# 不需要先本地构建：Dockerfile 自带前后端构建阶段，
	@# 且 .dockerignore 会排除 web/dist、static、server，本地产物根本进不了镜像
	docker build -t emailbox .

# 工具安装
tools: ## 安装开发工具
	@echo "🔧 安装开发工具..."
	@echo "📦 安装 golangci-lint..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "📦 安装 air (热重载)..."
	go install github.com/air-verse/air@$(AIR_VERSION)
	@echo "✅ 开发工具安装完成"

# 检查工具
check: ## 检查开发工具是否安装
	@echo "🔍 检查开发工具安装状态..."
	@echo "\n📋 核心工具:"
	@# 回退分支必须包住整条管道：若写成 `cmd | cut || echo`，退出码取自 cut，
	@# 工具缺失时既不报错也不输出，看起来像检查通过。
	@printf "  %-15s " "Go:"; { go version 2>/dev/null || echo "x x ❌未安装"; } | cut -d' ' -f3
	@printf "  %-15s " "Bun:"; bun --version 2>/dev/null || echo "❌ 未安装"
	@echo "\n🔧 开发工具:"
	@printf "  %-15s " "golangci-lint:"; { golangci-lint version 2>/dev/null || echo "x x x ❌未安装"; } | head -1 | cut -d' ' -f4
	@# air -v 输出多行 ASCII 艺术字，版本号夹在中间，需要提取而非取某一行
	@printf "  %-15s " "air:"; { air -v 2>/dev/null || echo "❌ 未安装"; } | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+|❌ 未安装' | head -1
	@echo "\n🐳 容器工具:"
	@printf "  %-15s " "Docker:"; { docker --version 2>/dev/null || echo "x x ❌未安装"; } | cut -d' ' -f3 | tr -d ','
	@printf "  %-15s " "docker-compose:"; { docker compose version --short 2>/dev/null || echo "❌ 未安装"; }
	@echo "💡 安装缺失工具: make tools"

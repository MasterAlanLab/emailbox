# 第一阶段：前端构建阶段
# --platform=$BUILDPLATFORM：前端产物与目标架构无关，多架构构建时
# 固定在宿主架构上只构建一次，避免在 QEMU 模拟下重复慢速构建
FROM --platform=$BUILDPLATFORM oven/bun:1.3.14-alpine AS frontend-builder

WORKDIR /app

# 复制前端依赖清单（锁文件保证构建可重现）
COPY web/package.json web/bun.lock ./

# 安装前端依赖
RUN bun install --frozen-lockfile

# 复制前端源码并构建
COPY web/ ./
RUN bun run build

# 第二阶段：后端构建阶段
# 同样固定在宿主架构上运行，通过 GOOS/GOARCH 交叉编译出目标架构的二进制
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder

WORKDIR /app

# 默认使用官方模块代理；国内本地构建可覆盖：
#   docker build --build-arg GOPROXY=https://goproxy.cn,direct .
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY

# 复制 Go 模块文件并下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制后端源码
COPY . .

# 构建后端：
# - modernc.org/sqlite 为纯 Go 实现，无需 CGO，可直接交叉编译
# - TARGETOS/TARGETARCH 由 buildx 按 --platform 自动注入
# - -trimpath -ldflags="-s -w" 去掉本机路径与调试符号，减小体积
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o server main.go

# 第三阶段：运行阶段
# 固定版本而非 latest：既保证可重现，dependabot 也才能跟踪升级
FROM alpine:3.24

WORKDIR /app

# 以非 root 用户运行；uid/gid 固定为 1000，方便宿主机为挂载卷设置权限。
# /app/data 是 SQLite 数据卷挂载点（见 docker-compose.yml）
RUN addgroup -g 1000 app && adduser -D -u 1000 -G app app \
    && mkdir -p /app/data && chown app:app /app/data

# 二进制与静态文件保持 root 属主（app 用户只读、可执行），无需 chmod：
# COPY 会保留构建阶段的可执行权限位
COPY --from=backend-builder /app/server /app/server
COPY --from=frontend-builder /app/dist /app/static

USER app

# 配置默认值 DB_PATH=app.db 是相对路径，会落在 root 属主的 /app 下，
# 非 root 用户无法创建；镜像里显式指向 app 属主的数据目录，
# 让不带任何环境变量的 docker run 也能启动（compose 设置的是同一个值）
ENV DB_PATH=/app/data/app.db

# 暴露端口
EXPOSE 1323

# 启动应用
CMD ["/app/server"]

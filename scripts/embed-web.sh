#!/bin/bash

# 构建前端并把产物同步进 pkg/webui/static，供 go:embed 编译进二进制。
#
# 单独成脚本是因为有三个调用方：Web 构建（scripts/build.sh）、桌面构建
# （make build-desktop）和 CI。产物落点写错一次就会得到一个「能启动但打不开页面」
# 的二进制，而那种失败要等到运行时才看得见。

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EMBED_DIR="$PROJECT_ROOT/pkg/webui/static"

echo "🔨 构建前端 React 项目..."
cd "$PROJECT_ROOT/web"
bun run build

if [ ! -f "$PROJECT_ROOT/web/dist/index.html" ]; then
    echo "❌ 前端构建失败，web/dist/index.html 不存在"
    exit 1
fi

echo "📦 同步前端产物到 $EMBED_DIR ..."
# 整个目录重建而不是覆盖：改名后的旧哈希资源留在里面只会白白撑大二进制。
rm -rf "$EMBED_DIR"
mkdir -p "$EMBED_DIR"
# .gitkeep 必须始终在场：目录空了 go:embed 会以「no matching files」直接编译失败，
# 而「还没构建前端」是这个仓库最常见的状态（make dev 下前端在 Vite 上）。
touch "$EMBED_DIR/.gitkeep"
cp -R "$PROJECT_ROOT/web/dist/." "$EMBED_DIR/"

if [ ! -f "$EMBED_DIR/index.html" ]; then
    echo "❌ 前端产物同步失败，$EMBED_DIR/index.html 不存在"
    exit 1
fi

echo "✅ 前端产物已就位（$(find "$EMBED_DIR" -type f | wc -l | tr -d ' ') 个文件）"

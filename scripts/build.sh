#!/bin/bash

# Go + React 全栈项目构建脚本
# 构建前端静态文件并打包成单一二进制程序

set -e  # 遇到错误立即退出

echo "🚀 开始构建 Go + React 全栈项目..."

# 项目根目录
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "📁 项目根目录: $PROJECT_ROOT"

# 清理之前的构建文件
echo "🧹 清理之前的构建文件..."
rm -rf "$PROJECT_ROOT/web/dist"
rm -f "$PROJECT_ROOT/server"

# 构建前端并同步到 pkg/webui/static（由 go:embed 编译进二进制）
"$PROJECT_ROOT/scripts/embed-web.sh"

cd "$PROJECT_ROOT"

# 代码质量检查
# 工具缺失时必须失败而不是跳过：否则构建会带着未经检查的代码报告成功，
# 与 make lint-go 的行为也不一致。脚本已 set -e，检查不通过会直接终止。
echo "🔍 运行代码质量检查..."
if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "❌ golangci-lint 未安装，无法完成构建前的代码检查"
    echo "   安装方式: make tools（版本统一由 Makefile 定义，与 CI 一致）"
    exit 1
fi
echo "📋 运行 golangci-lint 检查..."
golangci-lint run
echo "✅ 代码质量检查通过"

# 构建后端 Go 程序（构建命令统一维护在 make build-go 里）
make build-go

if [ ! -f "server" ]; then
    echo "❌ 后端构建失败"
    exit 1
fi

echo "✅ 后端构建完成"

# 显示构建结果
echo ""
echo "🎉 构建完成！"
echo "📊 构建结果:"
echo "   - 可执行文件: $PROJECT_ROOT/server（前端已嵌入，无需附带静态目录）"
echo "   - 文件大小: $(du -h server | cut -f1)"
echo ""
echo "🚀 运行方式:"
echo "   ./server"
echo ""
echo "🌐 访问地址:"
echo "   http://localhost:1323"

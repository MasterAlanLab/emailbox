#!/bin/bash

# Go 代码质量检查脚本
# 使用 golangci-lint 进行代码质量检查

set -e  # 遇到错误立即退出

echo "🔍 Go 代码质量检查..."

# 项目根目录
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../" && pwd)"
echo "📁 项目根目录: $PROJECT_ROOT"

cd "$PROJECT_ROOT"

# 检查 golangci-lint 是否安装
if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "❌ golangci-lint 未安装"
    echo "📦 安装方式:"
    echo "   方式1: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2"
    echo "   方式2: brew install golangci-lint (macOS)"
    echo "   方式3: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin v2.12.2"
    exit 1
fi

echo "📋 运行 golangci-lint 检查..."
echo "⚙️  配置文件: .golangci.yml"
echo ""

# 运行 golangci-lint
# 脚本已 set -e：检查不通过会立即退出，因此无需再判断 $?（此前的 else 分支永远执行不到）
trap 'echo ""; echo "❌ 代码质量检查失败"; echo "💡 自动修复格式问题: golangci-lint run --fix"' ERR
golangci-lint run

echo ""
echo "✅ 代码质量检查通过！"

echo ""
echo "📊 检查统计:"
echo "   - 配置文件: .golangci.yml"
echo "   - 检查目录: $(pwd)"
echo "   - 排除目录: web/, static/, vendor/"
echo ""
echo "🚀 下一步:"
echo "   - 运行构建: ./scripts/build.sh"
echo "   - 运行测试: go test ./..."
echo "   - 启动服务: ./server"
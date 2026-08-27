# Go 代码检查

项目使用 [golangci-lint](https://golangci-lint.run/) 检查 Go 代码，配置在 `.golangci.yml`。

## 版本必须与 CI 一致

CI 固定使用 **v2.12.2**（`.github/workflows/ci.yml`）。本地用不同版本会得到不同结果——
新版本放行的问题旧版本可能拒绝，导致「本地通过、推上去失败」。

```bash
make tools   # 安装 golangci-lint 与 air，版本与 CI 对齐
```

或手动安装（注意是 `/v2/` 路径，v1 的模块路径读不了本项目的 v2 配置）：

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
```

> Homebrew 安装的版本通常比 CI 新，不建议用于验证是否能通过 CI。

如果本地 Go 版本比 CI 新，预编译的 golangci-lint 可能在类型检查阶段崩溃
（报 `file requires newer Go version`）。此时下载与 `go.mod` 相同的 Go SDK，
并用 `GOTOOLCHAIN=local` 运行。

## 使用

```bash
make lint-go             # 检查 Go 代码
make lint                # 前后端一起检查
golangci-lint run --fix  # 自动修复可修复的问题（主要是格式）
golangci-lint config verify  # 校验 .golangci.yml 本身是否合法
```

## 配置要点

`.golangci.yml` 使用 v2 schema（`version: "2"`），与 v1 的写法不兼容：
linter 参数放在 `linters.settings` 下，格式化工具单独放在顶层 `formatters`。

启用的 linter 见配置中的 `linters.enable`，涵盖正确性（`errcheck`、`staticcheck`、
`govet`）、安全（`gosec`）、复杂度（`gocyclo`、`dupl`）和风格（`revive`、`misspell`）。

**测试文件同样会被检查。** 早期配置把 `.*_test.go$` 放进了 `exclusions.paths`，
结果所有测试代码永远不被任何 linter 和格式化工具检查，且没有任何提示。
若需要对测试放宽某几个 linter，用配置末尾的按路径规则：

```yaml
- path: _test\.go
  linters: [dupl, errcheck, goconst, gocyclo, gosec]
```

## 抑制单条告警

优先修复问题；确需保留时在代码里就近说明原因：

```go
//nolint:gosec // 这里的路径来自内部常量，不受用户输入影响
```

不要使用不带 linter 名和原因的裸 `//nolint`。

## 相关文档

- [golangci-lint 官方文档](https://golangci-lint.run/)
- [v1 → v2 配置迁移指南](https://golangci-lint.run/docs/product/migration-guide/)

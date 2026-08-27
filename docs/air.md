# Air 热重载

[Air](https://github.com/air-verse/air) 在保存 Go 文件后自动重新编译并重启后端，
配置在项目根目录的 `.air.toml`。

> Air 的仓库已从 `cosmtrek/air` 迁移到 `air-verse/air`，旧路径仅靠重定向工作，
> 安装时请使用新路径。

## 安装

```bash
make tools   # 同时安装 golangci-lint 和 air，版本与项目对齐
```

或单独安装：

```bash
go install github.com/air-verse/air@v1.61.7
```

## 使用

```bash
make dev   # 同时启动后端（air 热重载）和前端（vite）
air        # 只启动后端热重载
```

- 后端：<http://localhost:1323>
- 前端：<http://localhost:5173>，vite 会把 `/api` 代理到后端

`make dev` 在 air 未安装时会退化为普通启动模式并提示安装方式。按 `Ctrl+C` 停止。

## 配置要点

`.air.toml` 中与本项目直接相关的几项：

| 配置 | 当前值 | 说明 |
| --- | --- | --- |
| `cmd` | `go build -o ./tmp/main .` | 变更后执行的构建命令 |
| `bin` | `tmp/main` | 构建产物，必须与 `cmd` 的输出路径一致 |
| `include_ext` | `go` `tpl` `tmpl` `html` | 触发重建的文件后缀 |
| `exclude_dir` | 含 `web`、`static`、`tmp` | 前端由 vite 自己热更新，重复监听只会造成无谓重启 |
| `exclude_regex` | `_test\.go` | 改测试文件不重启服务 |
| `delay` | `1000` | 防抖毫秒数，批量保存时避免连续重启 |

修改 `.air.toml` 后需要重启 air 才会生效。

## 常见问题

**改了文件没重启**：确认后缀在 `include_ext` 内，且所在目录不在 `exclude_dir` 中。
注意 `docs`、`.github` 也在排除列表里。

**反复重启**：通常是构建产物落在了被监视的目录。确保 `bin` 指向 `tmp/`，
且 `tmp` 在 `exclude_dir` 内。

**端口被占用**：上次的进程没退干净，`lsof -i :1323` 找到后结束即可。

**只想跑后端、不需要热重载**：`make build && make run`。

## 相关文档

- [Air 官方仓库](https://github.com/air-verse/air)
- [完整配置示例](https://github.com/air-verse/air/blob/master/air_example.toml)

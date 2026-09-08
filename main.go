// Emailbox 的 Web / Docker 入口：装配服务并监听配置里的地址。
//
// 真正的装配在 pkg/server，桌面版（desktop 子模块）用的是同一份。
// 这里只剩「初始化日志 → 起服务 → 出错就响亮地退出」。
package main

import (
	"context"
	"log/slog"
	"os"

	"emailbox/pkg/server"
)

func main() {
	// echo 内部使用 log/slog，应用日志保持一致，避免同一份 stdout 里混两种格式。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	srv, err := server.New(server.Options{})
	if err != nil {
		fatal("服务初始化失败", err)
	}
	if err := srv.Run(context.Background(), nil); err != nil {
		fatal("服务器异常退出", err)
	}
}

// fatal 记录错误后退出。不用 log.Fatal 是为了统一走 slog，
// 也避免 os.Exit 跳过已注册的 defer。
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

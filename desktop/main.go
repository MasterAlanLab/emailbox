// Emailbox 桌面版入口。
//
// 它把 pkg/server 装配出来的服务跑在进程内的一个随机本地端口上，再用系统
// WebView 打开那个地址——没有第二套后端，桌面版和 Web 版跑的是同一份代码。
//
// 独立成子模块（而不是主模块里的一个 cmd/）是因为 wails/v3 的 go.mod 把它的
// CLI 工具链（bubbletea、go-git、nfpm、task 等上百个模块）一并带进依赖图。
// 放进主模块会让 server 二进制的构建、Docker 镜像和 CI 全都背上这些依赖，
// 而它们对服务端没有任何用处。
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"emailbox/pkg/server"
)

const (
	// listenAddress 让内核分配端口。不写死是因为用户很可能已经在本机用
	// Docker 跑着一份 Emailbox，抢 1323 会让桌面版直接起不来。
	listenAddress = "127.0.0.1:0"
	// listenTimeout 是等待内嵌服务开始监听的上限。
	listenTimeout = 30 * time.Second
	// shutdownWait 是退出时等待服务收尾的上限。
	// 超时也要放行：卡住不退会让用户以为应用死了，转而强杀进程——
	// 那比这里少等几秒危险得多，SQLite 可能正写到一半。
	shutdownWait = 20 * time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	srv, err := server.New(server.Options{Desktop: true, Address: listenAddress})
	if err != nil {
		fatal("服务初始化失败", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan net.Addr, 1)
	runErr := make(chan error, 1)
	go func() {
		runErr <- srv.Run(ctx, func(addr net.Addr) { addrCh <- addr })
	}()

	addr, err := waitForListener(addrCh, runErr)
	if err != nil {
		cancel()
		fatal("内嵌服务启动失败", err)
	}
	entryURL := "http://" + addr.String() + srv.DesktopEntryPath()
	slog.Info("内嵌服务已就绪", "addr", addr.String())

	app := application.New(application.Options{
		Name:        "Emailbox",
		Description: "批量邮箱托管与收信",
		// 关窗即退出：单窗口应用留一个没有窗口的后台进程只会让用户困惑，
		// 而且下次点图标时会看到「已在运行」而不是一个新窗口。
		Mac:   application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		Linux: application.LinuxOptions{ProgramName: "emailbox"},
		// 收尾必须挂在这里而不是 app.Run() 之后：macOS 上 Run() 不保证返回，
		// 写在后面的清理逻辑在那个平台上永远不会执行，数据库也就永远不会被正常关闭。
		OnShutdown: func() {
			cancel()
			select {
			case <-runErr:
			case <-time.After(shutdownWait):
				slog.Warn("内嵌服务未在超时内退出，仍继续关闭应用")
			}
		},
	})

	application.NewWindow(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Emailbox",
		Width:     1280,
		Height:    860,
		MinWidth:  960,
		MinHeight: 600,
		// 直接打开自动登录入口：它换出会话 Cookie 后 302 回首页。
		URL:             entryURL,
		InitialPosition: application.WindowCentered,
		BackgroundType:  application.BackgroundTypeSolid,
	})

	if err := app.Run(); err != nil {
		fatal("桌面应用异常退出", err)
	}
}

// waitForListener 等内嵌服务真正开始监听，并取回它拿到的端口。
//
// 三路等待缺一不可：只等地址的话，「服务起不来」会表现为窗口永远不出现；
// 只等错误的话，正常启动会一直阻塞。超时兜住的是两者都不发生的情况。
func waitForListener(addrCh <-chan net.Addr, runErr <-chan error) (net.Addr, error) {
	select {
	case addr := <-addrCh:
		return addr, nil
	case err := <-runErr:
		if err == nil {
			err = context.Canceled
		}
		return nil, err
	case <-time.After(listenTimeout):
		return nil, context.DeadlineExceeded
	}
}

// fatal 记录错误后退出。与服务端入口保持一致：统一走 slog，不用 log.Fatal。
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

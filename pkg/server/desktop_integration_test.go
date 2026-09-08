package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"testing"
	"time"

	"emailbox/configs"
	"emailbox/pkg/middleware"
	"emailbox/pkg/webui"
)

// TestDesktopModeAutoLogin 一次性钉住桌面形态的四件事：随机端口、数据目录、
// 首启密钥、以及「打开窗口就已登录」。
//
// 桌面窗口本身没法在 CI 里点，但窗口做的事只有一件——打开 DesktopEntryPath()。
// 那一步之后的全部行为都在这里覆盖到了。
func TestDesktopModeAutoLogin(t *testing.T) {
	if !webui.Built() {
		t.Skip("前端产物未嵌入；桌面流水线会先跑 make embed-web 再执行本用例")
	}
	dir := t.TempDir()
	// os.UserConfigDir 在 macOS 上取 $HOME，在 Linux 上取 $XDG_CONFIG_HOME，两个都要设。
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	// 显式清空，避免开发机上真实存在的环境变量把用例带偏。
	for _, key := range []string{"DB_PATH", "ENCRYPTION_KEY", "SESSION_EXPIRE_HOUR", "BOOTSTRAP_ADMIN_USERNAME", "BOOTSTRAP_ADMIN_PASSWORD"} {
		t.Setenv(key, "")
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("DB_DRIVER", "sqlite")

	srv, err := New(Options{Desktop: true, Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("桌面模式初始化失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, func(addr net.Addr) { addrCh <- addr }) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("服务未在超时内退出")
		}
	})

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("服务未开始监听就退出了: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("服务在超时内没有开始监听")
	}

	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("监听地址不是 TCP 地址: %v", addr)
	}
	// 只监听回环：桌面版的服务不该被同网段的其他机器访问到。
	if !tcpAddr.IP.IsLoopback() {
		t.Fatalf("桌面版监听在了非回环地址 %s", tcpAddr.IP)
	}
	// 端口由内核分配，不能是写死的 1323——用户很可能已经用 Docker 占着它。
	if tcpAddr.Port == 0 || tcpAddr.Port == 1323 {
		t.Fatalf("端口 %d 不是内核分配的随机端口", tcpAddr.Port)
	}

	base := "http://" + addr.String()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar 失败: %v", err)
	}
	// 不自动跟随重定向：要亲眼看到入口返回的是 302 且带上了会话 Cookie。
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	t.Run("错误的 nonce 换不到会话", func(t *testing.T) {
		resp := get(t, client, base+desktopSessionPath+"?nonce=0000000000000000")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d，期望 401", resp.StatusCode)
		}
		for _, c := range resp.Cookies() {
			if c.Name == middleware.SessionCookie && c.Value != "" {
				t.Fatal("nonce 错误时仍然下发了会话 Cookie")
			}
		}
	})

	t.Run("入口地址换出会话并跳首页", func(t *testing.T) {
		resp := get(t, client, base+srv.DesktopEntryPath())
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("状态码 = %d，期望 302", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "/" {
			t.Fatalf("Location = %q，期望 /", loc)
		}
		var session *http.Cookie
		for _, c := range resp.Cookies() {
			if c.Name == middleware.SessionCookie {
				session = c
			}
		}
		if session == nil || session.Value == "" {
			t.Fatal("入口没有下发会话 Cookie")
		}
		// 桌面版走的是同一套会话机制，HttpOnly 不能因为「反正是本地」就丢掉：
		// 邮件正文里的脚本一旦逃逸，能读到的就是一把完整的会话令牌。
		if !session.HttpOnly {
			t.Fatal("会话 Cookie 缺少 HttpOnly")
		}
	})

	t.Run("带着会话可以访问受保护接口", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1/auth/session")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200；自动登录没有生效", resp.StatusCode)
		}
	})

	// 前端靠这个字段决定给不给「退出」入口。它是 false 的话，桌面版会显示一个
	// 点下去就把人锁在登录页的按钮——本地账号的密码是随机生成、从不展示的。
	t.Run("会话响应带上桌面形态标志", func(t *testing.T) {
		resp := get(t, client, base+"/api/v1/auth/session")
		defer resp.Body.Close()
		var body struct {
			Data struct {
				Desktop bool `json:"desktop"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("解析会话响应失败: %v", err)
		}
		if !body.Data.Desktop {
			t.Fatal("会话响应里 desktop = false，桌面版会显示一个退出后无法再登录的入口")
		}
	})

	t.Run("首页由嵌入的前端产物提供", func(t *testing.T) {
		resp := get(t, client, base+"/")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d，期望 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct == "" {
			t.Fatal("首页没有 Content-Type")
		}
	})

	// 密钥必须在 configs.Init 之前就位。晚一步的话配置层会认定「没有密钥」，
	// 于是回落到明文 Cipher——邮箱密码和 refresh_token 就都以明文入库了，
	// 而唯一的迹象只是启动日志里一条一闪而过的 WARN。
	t.Run("凭据加密已启用", func(t *testing.T) {
		if configs.AppConfig.Crypto.Key == "" {
			t.Fatal("桌面模式下 ENCRYPTION_KEY 为空，凭据会以明文存储")
		}
	})

	t.Run("数据落在用户数据目录", func(t *testing.T) {
		dataDir, err := DataDir()
		if err != nil {
			t.Fatalf("解析数据目录失败: %v", err)
		}
		// 必须在临时 HOME 之下，否则说明桌面版把库写到了工作目录——
		// 装进 /Applications 之后那里是只读的。
		if rel, err := filepath.Rel(dir, dataDir); err != nil || rel == ".." || filepath.IsAbs(rel) {
			t.Fatalf("数据目录 %s 不在预期的用户目录 %s 之下", dataDir, dir)
		}
		for _, name := range []string{desktopDBFile, desktopKeyFile} {
			if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
				t.Errorf("数据目录里缺少 %s: %v", name, err)
			}
		}
	})
}

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("请求 %s 失败: %v", url, err)
	}
	return resp
}

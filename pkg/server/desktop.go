package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"emailbox/pkg/crypto"
	"emailbox/pkg/handler"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

const (
	// desktopDirName 是各平台用户目录下的应用目录名。
	desktopDirName = "emailbox"
	// desktopKeyFile 存放首次启动生成的 ENCRYPTION_KEY。
	desktopKeyFile = "encryption.key"
	// desktopDBFile 是桌面版的 SQLite 数据文件名。
	desktopDBFile = "app.db"
	// desktopUsername 是桌面版自动登录所用的本地账号。
	desktopUsername = "local"
	// desktopSessionPath 是自动登录入口。它只在桌面形态下挂载。
	desktopSessionPath = "/desktop/session"
	// desktopSessionExpireHour 是桌面版的会话有效期（一年）。
	//
	// 默认的 24 小时在这里是错的：桌面版没有登录界面，会话一旦过期，
	// 用户看到的是一个自己填不出密码的登录页。把它拉长到「不会在一次
	// 使用周期内到期」，真正的续期发生在每次启动——启动就铸一个新会话。
	desktopSessionExpireHour = 24 * 365
)

// DataDir 返回桌面版的数据目录，并确保它已经存在。
//
// 各平台落点由 os.UserConfigDir 决定：macOS 是 ~/Library/Application Support，
// Windows 是 %AppData%，Linux 是 $XDG_CONFIG_HOME（缺省 ~/.config）。
// 不能沿用服务端「相对工作目录」的默认值：桌面应用被装进 /Applications 或
// C:\Program Files 之后，工作目录对普通用户是只读的，SQLite 建不出库就直接启动失败。
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录: %w", err)
	}
	dir := filepath.Join(base, desktopDirName)
	// 0700：这个目录里放的是邮箱凭据库和加密密钥，同机器上的其他用户不该读得到。
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建数据目录 %s 失败: %w", dir, err)
	}
	return dir, nil
}

// loadOrCreateEncryptionKey 读取数据目录里的密钥，不存在则生成一把并落盘。
//
// 桌面版没有 .env 可填，密钥必须由程序自己管起来。这里刻意不做「读不到就
// 用明文」的回落：那会让一次读取失败静默降级成「邮箱密码明文入库」，
// 而用户对此毫无察觉。读失败、内容非法一律报错，让启动响亮地失败。
func loadOrCreateEncryptionKey(dir string) (string, error) {
	keyPath := filepath.Join(dir, desktopKeyFile)
	// #nosec G304 -- 路径由 DataDir 拼出，不含用户输入。
	raw, err := os.ReadFile(keyPath)
	switch {
	case err == nil:
		key := strings.TrimSpace(string(raw))
		if _, err := crypto.ParseKey(key); err != nil {
			return "", fmt.Errorf("密钥文件 %s 内容非法: %w；它是解开本机全部邮箱凭据的唯一凭证，请勿删除或改写", keyPath, err)
		}
		return key, nil
	case errors.Is(err, os.ErrNotExist):
		key, err := crypto.GenerateKey()
		if err != nil {
			return "", err
		}
		// 0600：密钥落盘就必须只有本人可读，这一步失败不能继续——
		// 否则下次启动会读到一把别人也能读的密钥，且没人会发现。
		if err := os.WriteFile(keyPath, []byte(key+"\n"), 0o600); err != nil {
			return "", fmt.Errorf("写入密钥文件 %s 失败: %w", keyPath, err)
		}
		slog.Info("已生成本机加密密钥", "path", keyPath)
		return key, nil
	default:
		return "", fmt.Errorf("读取密钥文件 %s 失败: %w", keyPath, err)
	}
}

// prepareDesktopEnv 为桌面形态准备数据目录与密钥，并把结果写进环境变量。
//
// 必须在 configs.Init 之前调用，而且必须是「写环境变量」而不是「事后改 AppConfig」。
// 两个理由：
//
//   - configs.Init 里的 validateCrypto 会在没有 ENCRYPTION_KEY 时打出
//     「邮箱凭据将以明文存储」。事后再补密钥的话，桌面版每次启动都会先打一条
//     与事实相反的警告——而这条警告恰恰是用来判断凭据到底有没有被加密的。
//   - 走环境变量意味着生成出来的密钥同样要过 validateCrypto 的校验，
//     不会出现「配置文件里的密钥被校验、程序自己生成的却不被校验」这种双标。
//
// 只在对应变量**没有显式设置**时才写：桌面版正常运行时环境里什么都没有，走的是
// 这里的默认值；排查问题时仍然可以用 DB_PATH 指向另一份库，不必改代码。
func prepareDesktopEnv() error {
	dir, err := DataDir()
	if err != nil {
		return err
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(dir, desktopDBFile)
		if err := os.Setenv("DB_PATH", dbPath); err != nil {
			return fmt.Errorf("设置 DB_PATH 失败: %w", err)
		}
	}
	if os.Getenv("ENCRYPTION_KEY") == "" {
		key, err := loadOrCreateEncryptionKey(dir)
		if err != nil {
			return err
		}
		if err := os.Setenv("ENCRYPTION_KEY", key); err != nil {
			return fmt.Errorf("设置 ENCRYPTION_KEY 失败: %w", err)
		}
	}
	if os.Getenv("SESSION_EXPIRE_HOUR") == "" {
		if err := os.Setenv("SESSION_EXPIRE_HOUR", strconv.Itoa(desktopSessionExpireHour)); err != nil {
			return fmt.Errorf("设置 SESSION_EXPIRE_HOUR 失败: %w", err)
		}
	}
	slog.Info("桌面模式已就绪", "data_dir", dir, "db_path", dbPath)
	return nil
}

// desktopEntry 是本次进程的自动登录凭据。
//
// nonce 只存在于内存和窗口自己的地址栏里，同机器上的其他进程读不到它，
// 因此它足以把这个「换会话」的入口和任何其他本地程序隔开。
//
// 它**不是一次性的**：webview 在启动时重载一次页面是常见行为（WebView2 尤其），
// 用过即废会让那种无害的重载直接把应用打成登录页。改成进程存活期内有效，
// 既不降低隔离强度（泄露面没有变），又顺带让「误点退出登录」可以靠重开窗口恢复。
type desktopEntry struct {
	nonce string
	token string
}

// path 返回窗口应当打开的首个地址。
func (d *desktopEntry) path() string {
	return desktopSessionPath + "?nonce=" + url.QueryEscape(d.nonce)
}

// newDesktopEntry 建出（或复用）本地账号，并为它铸一个会话。
func newDesktopEntry(ctx context.Context, store *repo.Store, auth *service.AuthService) (*desktopEntry, error) {
	user, err := store.GetUserByUsername(ctx, desktopUsername)
	if errors.Is(err, repo.ErrNotFound) {
		// 首次启动：随机密码建号。密码从不展示也从不留存——桌面版的身份
		// 由「能打开这个应用」决定，再要一道用户自己设的密码只是徒增麻烦。
		password, genErr := randomHex(24)
		if genErr != nil {
			return nil, genErr
		}
		if user, err = auth.CreateBootstrapAdmin(ctx, desktopUsername, password); err != nil {
			return nil, fmt.Errorf("创建本地账号失败: %w", err)
		}
		slog.Info("已创建本地账号", "username", desktopUsername, "user_id", user.ID)
	} else if err != nil {
		return nil, fmt.Errorf("读取本地账号失败: %w", err)
	}

	token, err := auth.CreateLocalSession(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("创建本地会话失败: %w", err)
	}
	nonce, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	return &desktopEntry{nonce: nonce, token: token}, nil
}

// handleDesktopSession 用 nonce 换会话 Cookie，然后跳到首页。
//
// 走 Cookie 而不是绕过认证中间件：桌面版的每个请求依然要过同一套会话校验，
// 租户隔离、权限矩阵、审计都照常生效。这里省掉的只是「输入密码」那一步。
func (s *Server) handleDesktopSession(c *echo.Context) error {
	// 定长比较，避免用比较耗时反推 nonce。
	if subtle.ConstantTimeCompare([]byte(c.QueryParam("nonce")), []byte(s.desktop.nonce)) != 1 {
		return echo.NewHTTPError(http.StatusUnauthorized, "入口凭据无效")
	}
	handler.SetSessionCookie(c, s.desktop.token)
	return c.Redirect(http.StatusFound, "/")
}

// randomHex 返回 n 字节随机数的十六进制表示。
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

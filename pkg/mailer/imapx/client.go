package imapx

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"time"

	"emailbox/pkg/mailer"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const defaultTimeout = 60 * time.Second

// Config 是 IMAP 通道的配置。
type Config struct {
	// Channel 决定通道形态：imap（密码鉴权）/ imap_new / imap_old（两条 OAuth 通道）。
	Channel string
	// TokenURL 留空时按 Channel 选生产端点；测试用它覆盖。
	TokenURL string
	Timeout  time.Duration
	// OnTokenRefresh 在微软返回新的 refresh_token 时调用。
	OnTokenRefresh func(email, refreshToken string)
	// DialFunc 供测试注入进程内的 IMAP 服务器。留空时按代理配置真实拨号。
	DialFunc func(ctx context.Context, host string, port int, proxyURL string) (net.Conn, error)
}

// Client 是 IMAP 通道的实现。
type Client struct {
	cfg Config
}

var _ mailer.Client = (*Client)(nil)

// New 构造 IMAP 通道。channel 取 mailer.ChannelIMAP / ChannelIMAPNew / ChannelIMAPOld。
func New(cfg Config) *Client {
	if cfg.Channel == "" {
		cfg.Channel = mailer.ChannelIMAP
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return &Client{cfg: cfg}
}

// Channel 实现 mailer.Client。
func (c *Client) Channel() string { return c.cfg.Channel }

// session 是一条已经登录好的 IMAP 连接。
type session struct {
	imap  *imapclient.Client
	proxy string
	// uidCapable 表示该连接上可以用 UID 命令。几乎所有服务器都支持，
	// 但这个标记要如实带到 Message.IDMode 上——混用 UID 与序列号会取到错误的邮件。
	uidCapable bool
}

func (s *session) close() {
	if s.imap == nil {
		return
	}
	// LOGOUT 失败无所谓，紧接着的 Close 会把连接收掉；
	// 连接已经断了的情况下报错反而是常态。
	//nolint:errcheck // 故意丢弃：连接就要关了，LOGOUT 的结果没有意义
	s.imap.Logout().Wait()
	_ = s.imap.Close()
}

func (s *session) idMode() string {
	if s.uidCapable {
		return mailer.IDModeUID
	}
	return mailer.IDModeSequence
}

// withSession 依次尝试代理候选，在每个候选上建连接、鉴权，然后执行 fn。
func withSession[T any](
	ctx context.Context, c *Client, cred mailer.Credential,
	fn func(context.Context, *session) (T, error),
) (T, error) {
	var zero T
	candidates := mailer.ProxyCandidates(cred.Proxy, cred.Email)
	attempts := make([]mailer.Attempt, 0, len(candidates))
	var lastErr error

	for _, proxyURL := range candidates {
		if err := ctx.Err(); err != nil {
			return zero, newError(c.cfg.Channel, mailer.ErrKindCanceled, "请求已取消", err)
		}

		result, err := runOnProxy(ctx, c, cred, proxyURL, fn)
		if err == nil {
			return result, nil
		}
		lastErr = err
		attempts = append(attempts, mailer.Attempt{
			Channel: c.cfg.Channel,
			Proxy:   mailer.MaskProxy(proxyURL),
			Kind:    mailer.KindOf(err),
			Message: err.Error(),
		})
		// 与 Graph 通道同样的判断：只有连不上才换代理。
		// 代理配置本身写错（协议不支持、URL 非法）时 NewDialer 会直接报 proxy_failed，
		// 那种情况不该顺着候选链滑到直连——用户以为走着代理，实际是裸连。
		if !mailer.RetriableWithAnotherProxy(err) || isProxyConfigError(err) {
			return zero, attachAttempts(err, attempts)
		}
	}

	if lastErr == nil {
		return zero, newError(c.cfg.Channel, mailer.ErrKindProxyFailed, "没有可用的出站通道", nil)
	}
	if mailer.KindOf(lastErr) == mailer.ErrKindNetwork && len(candidates) > 1 {
		wrapped := newError(c.cfg.Channel, mailer.ErrKindProxyFailed, "全部代理候选均不可用", lastErr)
		wrapped.Attempts = attempts
		return zero, wrapped
	}
	return zero, attachAttempts(lastErr, attempts)
}

// runOnProxy 在一个代理候选上建连接并执行 fn。
// 写成自由函数是因为 Go 不允许方法带类型参数。
func runOnProxy[T any](
	ctx context.Context, c *Client, cred mailer.Credential, proxyURL string,
	fn func(context.Context, *session) (T, error),
) (T, error) {
	var zero T
	s, err := c.connect(ctx, cred, proxyURL)
	if err != nil {
		return zero, err
	}
	defer s.close()
	return fn(ctx, s)
}

// connect 建立连接、发 ID、鉴权。
func (c *Client) connect(ctx context.Context, cred mailer.Credential, proxyURL string) (*session, error) {
	host, port := imapServer(c.cfg.Channel, cred)
	if host == "" {
		return nil, newError(c.cfg.Channel, mailer.ErrKindProviderError,
			"账号没有 IMAP 主机，且无法从邮箱域名推断", nil)
	}

	conn, err := c.dial(ctx, host, port, proxyURL)
	if err != nil {
		return nil, err
	}

	client := imapclient.New(conn, &imapclient.Options{WordDecoder: wordDecoder})
	s := &session{imap: client, proxy: proxyURL}

	c.identify(client)
	if err := c.authenticate(ctx, client, cred, proxyURL); err != nil {
		s.close()
		return nil, err
	}
	s.uidCapable = true
	return s, nil
}

func (c *Client) dial(ctx context.Context, host string, port int, proxyURL string) (net.Conn, error) {
	if c.cfg.DialFunc != nil {
		return c.cfg.DialFunc(ctx, host, port, proxyURL)
	}
	dialer, err := mailer.NewDialer(proxyURL, c.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	return mailer.DialTLS(ctx, dialer, host, port)
}

// identify 发送 IMAP ID 命令。
//
// 163/126 强制要求客户端先发 ID，否则报 Unsafe Login。发失败不算致命错误：
// 不支持 ID 的服务器会回 BAD，而它们本来也不需要这条命令。
func (c *Client) identify(client *imapclient.Client) {
	if !client.Caps().Has(imap.CapID) {
		return
	}
	// 只记不拦：不支持 ID 的服务器会回 BAD，而它们本来也不需要这条命令；
	// 真正需要 ID 的服务器会在后面的 LOGIN 上报 Unsafe Login，那条错误更有指向性。
	if _, err := client.ID(&imap.IDData{
		Name:    "emailbox",
		Version: appVersion,
		Vendor:  "emailbox",
	}).Wait(); err != nil {
		slog.Debug("IMAP ID 命令失败，继续鉴权",
			"channel", c.cfg.Channel, "error", err)
	}
}

// appVersion 会出现在 IMAP ID 里，服务商用它做客户端识别。
const appVersion = "1.0"

func (c *Client) authenticate(
	ctx context.Context, client *imapclient.Client, cred mailer.Credential, proxyURL string,
) error {
	if c.cfg.Channel == mailer.ChannelIMAP {
		password := cred.IMAPPassword
		if strings.TrimSpace(password) == "" {
			password = cred.Password
		}
		if strings.TrimSpace(password) == "" {
			return newError(c.cfg.Channel, mailer.ErrKindAuthFailed, "账号没有 IMAP 密码或授权码", nil)
		}
		if err := client.Login(cred.Email, password).Wait(); err != nil {
			return classifyAuthError(c.cfg.Channel, err)
		}
		return nil
	}

	// OAuth 通道：先换 access token，再走 XOAUTH2。
	hc, err := mailer.NewHTTPClient(proxyURL, c.cfg.Timeout)
	if err != nil {
		return err
	}
	token, err := c.fetchAccessToken(ctx, hc, cred)
	if err != nil {
		return err
	}
	if err := client.Authenticate(&xoauth2Client{username: cred.Email, token: token}); err != nil {
		return classifyAuthError(c.cfg.Channel, err)
	}
	return nil
}

// classifyAuthError 把各家五花八门的鉴权错误翻译成可操作的中文文案。
func classifyAuthError(channel string, err error) error {
	kind, message := mailer.ClassifyIMAPAuthError(err.Error())
	return newError(channel, kind, message, err)
}

func isProxyConfigError(err error) bool {
	var e *mailer.Error
	if !asMailerError(err, &e) {
		return false
	}
	return e.Kind == mailer.ErrKindProxyFailed
}

func asMailerError(err error, target **mailer.Error) bool {
	for err != nil {
		if e, ok := err.(*mailer.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func attachAttempts(err error, attempts []mailer.Attempt) error {
	var e *mailer.Error
	if asMailerError(err, &e) {
		e.Attempts = attempts
		return e
	}
	wrapped := mailer.NewError(mailer.KindOf(err), "", err.Error(), err)
	wrapped.Attempts = attempts
	return wrapped
}

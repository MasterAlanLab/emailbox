package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

var errProxyAuthentication = errors.New("proxy authentication failed")

// MailPlaceholder 是代理 URL 里的邮箱占位符。
// 配置原样入库、编辑时原样回显，只在出站时展开——
// 这样同一份分组代理配置可以让每个账号用不同的代理身份。
const MailPlaceholder = "{mail}"

// DirectProxy 是代理候选链末尾的直连标记。
const DirectProxy = ""

// ProxyConfig 是一个账号生效的代理配置：主代理 + 两个备用。
type ProxyConfig struct {
	URL       string
	Fallback1 string
	Fallback2 string
}

// ResolveProxy 按「账号 → 分组 → 逐级父分组 → 直连」的优先级挑出生效配置。
//
// 关键点是**整组一起取**：账号只要自己填了主代理，就连它自己的两个备用一起用，
// 不会出现「主代理用账号的、备用用分组的」这种混搭——那会让 failover 跑到
// 一个完全无关的出口上，排障时极难理解。
// 来源：outlookEmail `02_groups_accounts.py:1173-1299`。
func ResolveProxy(account ProxyConfig, groupChain []ProxyConfig) ProxyConfig {
	if strings.TrimSpace(account.URL) != "" {
		return account
	}
	for _, g := range groupChain {
		if strings.TrimSpace(g.URL) != "" {
			return g
		}
	}
	return ProxyConfig{}
}

// ExpandMailPlaceholder 把 URL 里的 {mail} 替换成邮箱 local-part 的规范化形式：
// 去掉所有非字母数字字符并转小写。
//
//	user.name+tag@outlook.com  →  usernametag
func ExpandMailPlaceholder(rawURL, email string) string {
	if !strings.Contains(rawURL, MailPlaceholder) {
		return rawURL
	}
	local := email
	if at := strings.Index(email, "@"); at >= 0 {
		local = email[:at]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.ReplaceAll(rawURL, MailPlaceholder, b.String())
}

// ProxyCandidates 返回按顺序尝试的代理候选，去空去重，末尾补一个直连。
//
// 直连兜底是有意的：代理全挂时至少还有机会连上，总好过整批账号一起失败。
// 若部署要求「必须走代理」，由上层在配置中禁用直连，而不是在这里省掉。
func ProxyCandidates(cfg ProxyConfig, email string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, raw := range []string{cfg.URL, cfg.Fallback1, cfg.Fallback2} {
		expanded := ExpandMailPlaceholder(strings.TrimSpace(raw), email)
		if expanded == "" || seen[expanded] {
			continue
		}
		seen[expanded] = true
		out = append(out, expanded)
	}
	return append(out, DirectProxy)
}

// MaskProxy 把代理 URL 里的口令替换成 ****，用于日志与错误详情。
// 代理串常带认证口令，任何输出路径都必须先过这里。
func MaskProxy(raw string) string {
	if raw == DirectProxy {
		return "direct"
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		// 解析失败时不能原样返回——那正是最容易漏掉口令的分支。
		if !strings.Contains(raw, "@") {
			return raw
		}
		at := strings.LastIndex(raw, "@")
		scheme := ""
		if i := strings.Index(raw, "://"); i >= 0 {
			scheme = raw[:i+3]
		}
		return scheme + "****" + raw[at:]
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return u.String()
	}
	u.User = url.UserPassword(u.User.Username(), "****")
	return strings.ReplaceAll(u.String(), "%2A%2A%2A%2A", "****")
}

// IsProxyAuthenticationError 识别发生在 HTTP CONNECT 或 SOCKS5 握手阶段的认证失败。
// net/http 默认会丢掉 HTTPS CONNECT 的状态码，只把可自定义的 reason phrase 放进 Do 的 error；
// NewHTTPClient 用回调先转成 sentinel。若当成普通网络故障，候选链会继续到直连。
func IsProxyAuthenticationError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errProxyAuthentication) {
		return true
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{
		"proxy authentication required",
		"username/password authentication failed",
		"no acceptable authentication methods",
		"invalid username/password",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// Dialer 抽象出「怎么建立 TCP 连接」，是本包能真正并发的关键。
//
// outlookEmail 用 socks.set_default_proxy() 全局改写 socket.socket，
// 并靠进程锁保护，结果是所有 IMAP 连接被迫串行。这里每个连接自带 dialer，
// 没有任何全局状态，worker pool 里可以真正并发，不同账号还能同时走不同代理。
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// NewDialer 按代理 URL 构造 dialer。空串返回直连 dialer。
func NewDialer(proxyURL string, timeout time.Duration) (Dialer, error) {
	direct := &net.Dialer{Timeout: timeout}
	if proxyURL == DirectProxy {
		return direct, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, newError(ErrKindProxyFailed, "", "代理地址格式非法", err)
	}
	switch strings.ToLower(u.Scheme) {
	// socks5h 与 socks5 的区别是域名在哪一端解析。golang.org/x/net/proxy 的
	// SOCKS5 实现本来就把主机名原样交给代理（即 socks5h 语义），两者同样处理。
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, direct)
		if err != nil {
			return nil, newError(ErrKindProxyFailed, "", "构造 SOCKS5 代理失败", err)
		}
		ctxDialer, ok := d.(proxy.ContextDialer)
		if !ok {
			return nil, newError(ErrKindProxyFailed, "", "SOCKS5 代理不支持带 context 的拨号", nil)
		}
		return ctxDialer, nil
	case "http", "https":
		// HTTP 代理由 http.Transport 的 Proxy 字段处理，不走这里；
		// IMAP 要经 HTTP 代理需要 CONNECT 隧道，暂不支持。
		return nil, newError(ErrKindProxyFailed, "",
			fmt.Sprintf("IMAP 不支持 %s 代理，请改用 socks5", u.Scheme), nil)
	default:
		return nil, newError(ErrKindProxyFailed, "",
			fmt.Sprintf("不支持的代理协议 %q", u.Scheme), nil)
	}
}

// NewHTTPClient 按代理 URL 构造 HTTP 客户端。
// 每次调用都构造独立的 Transport，避免不同代理身份之间串连接池。
func NewHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	transport := &http.Transport{
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		// 协议层是短连接为主的批量调用，保持少量空闲连接即可。
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
		// CONNECT 的 reason phrase 可由代理任意填写，不能靠错误字符串识别 407。
		OnProxyConnectResponse: func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
			if response.StatusCode == http.StatusProxyAuthRequired {
				return errProxyAuthentication
			}
			return nil
		},
	}
	if proxyURL != DirectProxy {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, newError(ErrKindProxyFailed, "", "代理地址格式非法", err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			transport.Proxy = http.ProxyURL(u)
		case "socks5", "socks5h":
			dialer, err := NewDialer(proxyURL, timeout)
			if err != nil {
				return nil, err
			}
			transport.DialContext = dialer.DialContext
		default:
			return nil, newError(ErrKindProxyFailed, "",
				fmt.Sprintf("不支持的代理协议 %q", u.Scheme), nil)
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

// DialTLS 通过给定 dialer 建立 TLS 连接，供 IMAP 使用。
func DialTLS(ctx context.Context, dialer Dialer, host string, port int) (net.Conn, error) {
	address := net.JoinHostPort(host, fmt.Sprint(port))
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if IsProxyAuthenticationError(err) {
			return nil, newError(ErrKindProxyFailed, "", "代理认证失败，请检查代理账号和密码", err)
		}
		return nil, newError(ErrKindNetwork, "", "连接 "+host+" 失败", err)
	}
	// ServerName 必须显式设置：经 SOCKS5 拨号时对端地址是代理，
	// 不设的话 TLS 会拿代理地址去校验证书而失败。
	tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, newError(ErrKindNetwork, "", "与 "+host+" 的 TLS 握手失败", err)
	}
	return tlsConn, nil
}

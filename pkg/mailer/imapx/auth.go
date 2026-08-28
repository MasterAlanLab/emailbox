package imapx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"emailbox/pkg/mailer"

	"github.com/emersion/go-sasl"
)

// xoauth2Client 实现 XOAUTH2。
//
// go-sasl 只有 OAUTHBEARER，微软要的是 XOAUTH2——两者不通用，所以自己实现。
// 初始响应的格式来源：outlookEmail `outlook_mail_reader.py:157,263`：
//
//	"user=" + email + "\x01auth=Bearer " + accessToken + "\x01\x01"
type xoauth2Client struct {
	username string
	token    string
	done     bool
}

func (c *xoauth2Client) Start() (string, []byte, error) {
	ir := []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01")
	return "XOAUTH2", ir, nil
}

// Next 处理失败时服务器回的那一轮。
//
// XOAUTH2 失败时服务器不会直接给 NO，而是先发一个带 JSON 错误详情的挑战，
// 等客户端回一个空串才结束。不回这个空串，连接会一直挂着——
// 批量刷新时表现为「卡死」而不是「失败」，比失败难查得多。
func (c *xoauth2Client) Next([]byte) ([]byte, error) {
	if c.done {
		return nil, sasl.ErrUnexpectedServerChallenge
	}
	c.done = true
	return []byte{}, nil
}

// tokenEndpoint 返回该通道用的 token 端点与 scope。
//
// login.live.com 的请求体里**不能带 scope**，带了反而会失败——
// 这是旧版 IMAP 通道专用的历史端点。来源：outlookEmail `outlook_mail_reader.py:113-119`。
func tokenEndpoint(channel string) (endpoint, scope string) {
	if channel == mailer.ChannelIMAPOld {
		return mailer.TokenURLLive, ""
	}
	return mailer.TokenURLIMAP, mailer.ScopeIMAP
}

// imapServer 返回该通道默认的 IMAP 主机。
func imapServer(channel string, cred mailer.Credential) (string, int) {
	host, port := strings.TrimSpace(cred.IMAPHost), cred.IMAPPort
	if port <= 0 {
		port = mailer.DefaultIMAPPort
	}
	switch channel {
	case mailer.ChannelIMAPNew:
		return mailer.IMAPServerNew, port
	case mailer.ChannelIMAPOld:
		return mailer.IMAPServerOld, port
	}
	if host != "" {
		return host, port
	}
	provider := mailer.ProviderForEmail(cred.Email)
	if provider.IMAPHost != "" {
		return provider.IMAPHost, port
	}
	return "", port
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// fetchAccessToken 用 refresh_token 换 IMAP 用的 access token。
//
// 与 Graph 通道不同，IMAP 的 scope 是固定的，没有降级空间：
// 拿不到就是拿不到，降级只会多打两次 token 端点。
func (c *Client) fetchAccessToken(
	ctx context.Context, hc *http.Client, cred mailer.Credential,
) (string, error) {
	clientID := strings.TrimSpace(cred.ClientID)
	if clientID == "" {
		clientID = mailer.DefaultOAuthClientID
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return "", newError(c.cfg.Channel, mailer.ErrKindAuthFailed,
			"账号缺少 refresh_token，需要重新授权", nil)
	}

	endpoint, scope := tokenEndpoint(c.cfg.Channel)
	if c.cfg.TokenURL != "" {
		endpoint = c.cfg.TokenURL
	}
	form := url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {cred.RefreshToken},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	if cred.ClientSecret != "" {
		form.Set("client_secret", cred.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", newError(c.cfg.Channel, mailer.ErrKindProviderError, "构造令牌请求失败", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	status, body, err := c.postToken(ctx, hc, req)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		kind, message := mailer.ClassifyOAuthError(status, string(body))
		e := newError(c.cfg.Channel, kind, message, nil)
		e.StatusCode = status
		return "", e
	}
	return c.acceptToken(cred, body)
}

func (c *Client) postToken(
	ctx context.Context, hc *http.Client, req *http.Request,
) (int, []byte, error) {
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, newError(c.cfg.Channel, mailer.ErrKindCanceled, "请求已取消", err)
		}
		return 0, nil, newError(c.cfg.Channel, mailer.ErrKindNetwork, "请求令牌端点失败", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 限制读取量：异常时（比如代理返回了一整页 HTML）不设上限，
	// 一次批量刷新就能把内存吃光。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, newError(c.cfg.Channel, mailer.ErrKindNetwork, "读取令牌响应失败", err)
	}
	return resp.StatusCode, body, nil
}

func (c *Client) acceptToken(cred mailer.Credential, body []byte) (string, error) {
	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", newError(c.cfg.Channel, mailer.ErrKindProviderError, "解析令牌响应失败", err)
	}
	if parsed.AccessToken == "" {
		return "", newError(c.cfg.Channel, mailer.ErrKindAuthFailed, "令牌响应里没有 access_token", nil)
	}
	// 微软会轮换 refresh_token，漏存下次就失效了。
	rotated := parsed.RefreshToken != "" && parsed.RefreshToken != cred.RefreshToken
	if rotated && c.cfg.OnTokenRefresh != nil {
		c.cfg.OnTokenRefresh(cred.Email, parsed.RefreshToken)
	}
	return parsed.AccessToken, nil
}

// newError 构造带通道名的结构化错误。
func newError(channel string, kind mailer.ErrKind, message string, cause error) *mailer.Error {
	err := mailer.NewError(kind, channel, message, cause)
	if cause != nil {
		err.Detail = cause.Error()
	}
	return err
}

func errorf(channel string, kind mailer.ErrKind, cause error, format string, args ...any) *mailer.Error {
	return newError(channel, kind, fmt.Sprintf(format, args...), cause)
}

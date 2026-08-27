package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"emailbox/pkg/mailer"
)

// scope 候选。**这是本通道最容易被低估的一段逻辑。**
//
// 同一个 client_id 下，不同账号实际同意过的权限并不一样：有人只点了只读，
// 有人是被管理员统一授权的。拿一套写死的 scope 去换 token，那些账号会直接失败，
// 而报错（AADSTS90023 之类）看上去像是配置错误，很难联想到「换个 scope 就好了」。
// 来源：outlookEmail `03_mail_helpers.py:355-444`。
func scopeCandidates() []string {
	configured := append([]string{"offline_access"}, mailer.OAuthGraphScopes...)
	read := make([]string, 0, len(configured))
	for _, s := range configured {
		if strings.HasSuffix(s, "/Mail.ReadWrite") {
			continue
		}
		read = append(read, s)
	}
	candidates := []string{
		strings.Join(configured, " "),
		strings.Join(read, " "),
		mailer.ScopeGraphDefault,
	}
	// 去重但保序：Mail.ReadWrite 若哪天从 OAuthGraphScopes 里去掉，
	// 前两个候选会变成同一串，白白多打一次 token 端点。
	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, s := range candidates {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// scopeDegradeMarkers 是「这次失败是 scope 问题、值得换更低的 scope 再试」的判据。
// 全部来自实战积累，凭常识猜几乎一定会漏。
var scopeDegradeMarkers = []string{
	"invalid_scope",
	"invalid scope",
	"consent_required",
	"interaction_required",
	"consent",
	"aadsts90023",
	"aadsts70000",
	"aadsts70011",
	"no applicable permissions",
	"requested are unauthorized or expired",
}

// shouldDegradeScope 判断是否换下一个 scope 重试。
//
// 只有 400/401/403 才考虑降级：5xx 是服务商侧问题，换 scope 无意义，
// 而且多打两次 token 端点只会在服务商抖动时雪上加霜。
func shouldDegradeScope(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
	default:
		return false
	}
	lower := bytes.ToLower(body)
	for _, marker := range scopeDegradeMarkers {
		if bytes.Contains(lower, []byte(marker)) {
			return true
		}
	}
	return false
}

// tokenResponse 是 token 端点的成功响应。
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// fetchToken 用 refresh_token 换 access token，必要时逐级降级 scope。
func (c *Client) fetchToken(ctx context.Context, hc *http.Client, cred mailer.Credential) (string, error) {
	clientID := strings.TrimSpace(cred.ClientID)
	if clientID == "" {
		clientID = mailer.DefaultOAuthClientID
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return "", newError(mailer.ErrKindAuthFailed, "账号缺少 refresh_token，需要重新授权", 0, nil)
	}

	var lastErr error
	for _, scope := range scopeCandidates() {
		status, body, err := c.postToken(ctx, hc, clientID, cred.RefreshToken, scope)
		if err != nil {
			return "", err
		}
		if status >= 200 && status < 300 {
			return c.acceptToken(cred, status, body)
		}

		kind, message := mailer.ClassifyOAuthError(status, string(body))
		// 被封与令牌失效换 scope 也是同样的结果，立刻停手。
		if kind == mailer.ErrKindBanned || kind == mailer.ErrKindAuthFailed {
			return "", newError(kind, message, status, nil)
		}
		if !shouldDegradeScope(status, body) {
			return "", newError(kind, message, status, nil)
		}
		degraded := newError(kind, message, status, nil)
		degraded.Detail = "scope=" + scope
		lastErr = degraded
	}

	if lastErr == nil {
		return "", newError(mailer.ErrKindConsentRequired, "没有可用的权限范围", 0, nil)
	}
	return "", lastErr
}

// acceptToken 解析成功响应，并在 refresh_token 被轮换时通知上层。
func (c *Client) acceptToken(cred mailer.Credential, status int, body []byte) (string, error) {
	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", newError(mailer.ErrKindProviderError, "解析令牌响应失败", status, err)
	}
	if parsed.AccessToken == "" {
		return "", newError(mailer.ErrKindAuthFailed, "令牌响应里没有 access_token", status, nil)
	}
	// 微软会轮换 refresh_token。不落库的话下次刷新就失效了，
	// 而且要等到下一轮任务才会暴露。
	rotated := parsed.RefreshToken != "" && parsed.RefreshToken != cred.RefreshToken
	if rotated && c.cfg.OnTokenRefresh != nil {
		c.cfg.OnTokenRefresh(cred.Email, parsed.RefreshToken)
	}
	return parsed.AccessToken, nil
}

// postToken 打一次 token 端点，返回状态码与响应体。
// 响应体读成 []byte 一次给两处用：JSON 解析与小写子串匹配。
func (c *Client) postToken(
	ctx context.Context, hc *http.Client, clientID, refreshToken, scope string,
) (int, []byte, error) {
	form := url.Values{
		"client_id":     {clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, newError(mailer.ErrKindProviderError, "构造令牌请求失败", 0, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, newError(mailer.ErrKindCanceled, "请求已取消", 0, err)
		}
		return 0, nil, newError(mailer.ErrKindNetwork, "请求令牌端点失败", 0, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 限制读取量：token 响应正常只有几 KB，异常时（比如代理返回了一整页 HTML）
	// 不设上限会让一次批量刷新把内存吃光。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, newError(mailer.ErrKindNetwork, "读取令牌响应失败", resp.StatusCode, err)
	}
	return resp.StatusCode, body, nil
}

// RefreshToken 只做一次令牌交换，不打任何业务端点。
//
// 批量「刷新 Token」要的就是这件事：确认 refresh_token 还能换出 access_token，
// 并在微软轮换了 refresh_token 时通过 OnTokenRefresh 把新值交出去。
// 走的是与拉信完全相同的代理候选与 scope 降级逻辑（withSession + fetchToken），
// 所以刷新成功而拉信失败这类不一致不会因为两套实现而出现。
func (c *Client) RefreshToken(ctx context.Context, cred mailer.Credential) error {
	_, err := withSession(ctx, c, cred, func(context.Context, *session) (struct{}, error) {
		// withSession 在调用这里之前已经成功取到了 token，
		// 也就是说走到这一步刷新就算成功了，没有别的事要做。
		return struct{}{}, nil
	})
	return err
}

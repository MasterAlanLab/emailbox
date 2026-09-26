// Package graph 是 Microsoft Graph 通道，实现 mailer.Client。
//
// 与 outlookEmail 的三处关键差异：
//   - 没有任何全局状态：代理与 HTTP 连接池随调用构造，worker pool 里可以真正并发
//   - 批量标已读/删除走 $batch，20 条一个往返（Python 是逐条循环）
//   - 处理 429 的 Retry-After（Python 完全没管，批量场景下会被越限越狠）
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emailbox/pkg/mailer"
)

// DefaultBaseURL 是 Graph 的 v1.0 根地址。
const DefaultBaseURL = "https://graph.microsoft.com/v1.0"

// 单次 HTTP 请求的默认超时。整条回退链的总时长由调用方的 context 控制。
const defaultTimeout = 30 * time.Second

// 限流退避的上限与次数。Graph 偶尔会给出几分钟的 Retry-After，
// 那种情况下等下去不如让本次调用失败、把账号留给下一轮任务。
const (
	maxRetryAfter = 30 * time.Second
	maxRetries    = 2
)

// Config 是 Graph 通道的配置。
type Config struct {
	// BaseURL 与 TokenURL 留空时用生产地址；测试用 httptest.Server 覆盖。
	BaseURL  string
	TokenURL string
	// Timeout 是单次 HTTP 请求的超时。
	Timeout time.Duration
	// OnTokenRefresh 在微软返回了新的 refresh_token 时调用。
	//
	// 持久化最新值，后续续期不再依赖旧值的剩余寿命；轮换不代表旧值立即失效。
	// 允许为 nil（cmd/mailprobe 这类不落库的调用方）。
	OnTokenRefresh func(email, refreshToken string)
	// sleep 供测试注入，避免真的等 Retry-After。
	sleep func(context.Context, time.Duration) error
}

// Client 是 Graph 通道的实现。
type Client struct {
	cfg Config
}

var _ mailer.Client = (*Client)(nil)

// New 构造 Graph 通道。
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = mailer.TokenURLGraph
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.sleep == nil {
		cfg.sleep = sleepCtx
	}
	return &Client{cfg: cfg}
}

// Channel 实现 mailer.Client。
func (c *Client) Channel() string { return mailer.ChannelGraph }

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// session 是一次调用内已经建好的出站通道：某个代理候选 + 该代理下拿到的 access token。
// 一次公开方法调用只建一次，之后的多个 HTTP 请求（详情 + 附件列表、$batch 分批）复用它。
type session struct {
	http  *http.Client
	token string
	proxy string
}

// withSession 依次尝试代理候选，在每个候选上取 token 并执行 fn。
//
// 只有「连不上」类的错误才换下一个代理：认证失败换代理不会变好，
// 反而会把同一个坏账号重复提交给上游，加重风控。
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
			return zero, newError(mailer.ErrKindCanceled, "请求已取消", 0, err)
		}
		// 构造不出客户端 = 代理配置写错了（协议不支持、URL 非法）。这类错误必须当场报出来：
		// 顺着候选链滑到直连的话，用户以为流量走着代理，实际是从服务器公网 IP 直连的，
		// 而这恰恰是最容易触发服务商风控、也最难被发现的一种情况。
		// 「配置正确但连不上」才走 failover——那是 ProxyCandidates 末尾留直连兜底的本意。
		hc, err := mailer.NewHTTPClient(proxyURL, c.cfg.Timeout)
		if err != nil {
			attempts = append(attempts, mailer.Attempt{
				Channel: mailer.ChannelGraph,
				Proxy:   mailer.MaskProxy(proxyURL),
				Kind:    mailer.KindOf(err),
				Message: err.Error(),
			})
			return zero, attachAttempts(err, attempts)
		}

		result, err := func() (T, error) {
			token, err := c.fetchToken(ctx, hc, cred)
			if err != nil {
				return zero, err
			}
			return fn(ctx, &session{http: hc, token: token, proxy: proxyURL})
		}()
		if err == nil {
			return result, nil
		}
		lastErr = err
		attempts = append(attempts, mailer.Attempt{
			Channel: mailer.ChannelGraph,
			Proxy:   mailer.MaskProxy(proxyURL),
			Kind:    mailer.KindOf(err),
			Message: err.Error(),
		})
		// 407 等代理配置/认证错误必须响亮失败，滑到直连会让用户误以为请求仍经过代理。
		if !mailer.RetriableWithAnotherProxy(err) || mailer.KindOf(err) == mailer.ErrKindProxyFailed {
			return zero, attachAttempts(err, attempts)
		}
	}

	if lastErr == nil {
		return zero, newError(mailer.ErrKindProxyFailed, "没有可用的出站通道", 0, nil)
	}
	// 全部候选（含直连）都因连不上而失败，归因到代理而不是网络：
	// 前者提示用户去检查代理配置，后者只会让人怀疑自己的网。
	if mailer.KindOf(lastErr) == mailer.ErrKindNetwork && len(candidates) > 1 {
		wrapped := newError(mailer.ErrKindProxyFailed, "全部代理候选均不可用", 0, lastErr)
		wrapped.Attempts = attempts
		return zero, wrapped
	}
	return zero, attachAttempts(lastErr, attempts)
}

// newError 构造带通道名的结构化错误。
func newError(kind mailer.ErrKind, message string, status int, cause error) *mailer.Error {
	err := mailer.NewError(kind, mailer.ChannelGraph, message, cause)
	err.StatusCode = status
	if cause != nil {
		err.Detail = cause.Error()
	}
	return err
}

func attachAttempts(err error, attempts []mailer.Attempt) error {
	var e *mailer.Error
	if errors.As(err, &e) {
		e.Attempts = attempts
		return e
	}
	wrapped := newError(mailer.KindOf(err), err.Error(), 0, err)
	wrapped.Attempts = attempts
	return wrapped
}

// doJSON 发一个请求并把 2xx 响应体解成 out（out 为 nil 时丢弃）。
//
// 429/503 带 Retry-After 时按其值退避重试，最多两次。这段是 outlookEmail 完全没有的：
// 批量场景下不退避会让微软把限流窗口越拉越长，最终整批失败。
func (c *Client) doJSON(ctx context.Context, s *session, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return newError(mailer.ErrKindProviderError, "构造请求体失败", 0, err)
		}
	}

	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, s, method, path, payload)
		if err != nil {
			return err
		}
		resp, err := s.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return newError(mailer.ErrKindCanceled, "请求已取消", 0, err)
			}
			if mailer.IsProxyAuthenticationError(err) {
				return newError(mailer.ErrKindProxyFailed,
					"代理认证失败，请检查代理账号和密码", 0, err)
			}
			return newError(mailer.ErrKindNetwork, "请求 Graph 失败", 0, err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return decodeSuccess(resp.StatusCode, raw, readErr, out)
		}

		if wait, ok := retryAfter(resp); ok && attempt < maxRetries {
			if err := c.cfg.sleep(ctx, wait); err != nil {
				return newError(mailer.ErrKindCanceled, "请求已取消", resp.StatusCode, err)
			}
			continue
		}
		return classifyAPIError(resp.StatusCode, raw)
	}
}

// decodeSuccess 处理 2xx 响应体。out 为 nil 时只确认读取成功（DELETE 之类没有响应体）。
func decodeSuccess(status int, raw []byte, readErr error, out any) error {
	if readErr != nil {
		return newError(mailer.ErrKindNetwork, "读取 Graph 响应失败", status, readErr)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return newError(mailer.ErrKindProviderError, "解析 Graph 响应失败", status, err)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, s *session, method, path string, payload []byte) (*http.Request, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, reader)
	if err != nil {
		return nil, newError(mailer.ErrKindProviderError, "构造请求失败", 0, err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// retryAfter 判断这个响应是否值得退避重试，以及等多久。
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return 0, false
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		// Retry-After 也允许是 HTTP-date；这种形式在 Graph 上没见过，
		// 真出现时按不重试处理，总好过解析错了睡很久。
		return 0, false
	}
	wait := time.Duration(seconds) * time.Second
	if wait > maxRetryAfter {
		return 0, false
	}
	return wait, true
}

// apiError 是 Graph 的标准错误响应体。
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// classifyAPIError 把 Graph 的错误响应翻译成结构化错误。
func classifyAPIError(status int, raw []byte) error {
	kind, message := classifyAPIStatus(status, raw)
	err := newError(kind, message, status, nil)

	var parsed apiError
	// 响应体不是 JSON（代理返回的 HTML 错误页之类）时 parsed 保持零值，
	// 下面自然回落到截断原文，不需要中断分类。
	if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
		parsed = apiError{}
	}
	detail := strings.TrimSpace(parsed.Error.Message)
	if detail == "" {
		detail = truncate(string(raw), 300)
	}
	if parsed.Error.Code != "" {
		detail = parsed.Error.Code + ": " + detail
	}
	err.Detail = detail
	return err
}

func classifyAPIStatus(status int, raw []byte) (mailer.ErrKind, string) {
	if strings.Contains(strings.ToLower(string(raw)), "service abuse mode") {
		return mailer.ErrKindBanned, "账号已被服务商封禁"
	}
	switch {
	case status == http.StatusProxyAuthRequired:
		return mailer.ErrKindProxyFailed, "代理认证失败，请检查代理账号和密码"
	case status == http.StatusTooManyRequests:
		return mailer.ErrKindRateLimited, "请求过于频繁，已被限流"
	case status == http.StatusUnauthorized:
		return mailer.ErrKindAuthFailed, "访问令牌无效或已过期"
	case status == http.StatusForbidden:
		// 403 在 Graph 上几乎都是权限不足，而不是「这个资源不给你看」。
		return mailer.ErrKindConsentRequired, "应用权限不足，需要重新授权"
	case status == http.StatusNotFound:
		return mailer.ErrKindFolderUnavailable, "邮件或邮件夹不存在"
	case status >= 500:
		return mailer.ErrKindProviderError, "服务商暂时不可用"
	default:
		return mailer.ErrKindProviderError, fmt.Sprintf("Graph 请求失败（HTTP %d）", status)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

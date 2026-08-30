package mailer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrKind 是结构化的失败原因。它直接落到 job_items.error_kind 与
// mail_refresh_logs.error_kind，前端据此做筛选与聚合统计
// （「本次刷新失败 312 个，其中被封 47 个、认证失败 210 个、代理故障 55 个」）——
// 这是只有自由文本 error_message 时做不到的。
type ErrKind string

const (
	ErrKindAuthFailed        ErrKind = "auth_failed"
	ErrKindBanned            ErrKind = "banned"
	ErrKindConsentRequired   ErrKind = "consent_required"
	ErrKindProxyFailed       ErrKind = "proxy_failed"
	ErrKindNetwork           ErrKind = "network"
	ErrKindRateLimited       ErrKind = "rate_limited"
	ErrKindFolderUnavailable ErrKind = "folder_unavailable"
	ErrKindProviderError     ErrKind = "provider_error"
	ErrKindCanceled          ErrKind = "canceled"
)

// Attempt 记录一次通道或代理的尝试，供排障时还原整条回退路径。
type Attempt struct {
	Channel string  `json:"channel"`
	Proxy   string  `json:"proxy"` // 已打码
	Kind    ErrKind `json:"kind"`
	Message string  `json:"message"`
}

// Error 是协议层的结构化错误。
type Error struct {
	Kind       ErrKind
	Channel    string
	Message    string // 面向用户的中文文案
	StatusCode int
	Detail     string // 已脱敏，供排障
	Attempts   []Attempt
	cause      error
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("[%s] %s", e.Kind, e.Message)
	}
	return fmt.Sprintf("[%s] %s (%s)", e.Kind, e.Message, e.Detail)
}

func (e *Error) Unwrap() error { return e.cause }

// Is 让 errors.Is(err, &Error{Kind: X}) 能按 Kind 匹配。
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return t.Kind == "" || t.Kind == e.Kind
}

// newError 构造结构化错误。
func newError(kind ErrKind, channel, message string, cause error) *Error {
	return &Error{Kind: kind, Channel: channel, Message: message, cause: cause}
}

// NewError 供 graph / imapx 等子包构造同一形态的错误。
//
// cause 必须传：Error 的 cause 字段不导出，子包自己 new 一个 Error 的话
// errors.Is 就断在这里了——底层的 context.Canceled、net.Error 全都找不回来。
func NewError(kind ErrKind, channel, message string, cause error) *Error {
	return newError(kind, channel, message, cause)
}

// KindOf 取出错误的分类；非本包的错误按网络/取消归类。
func KindOf(err error) ErrKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrKindCanceled
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ErrKindNetwork
	}
	return ErrKindProviderError
}

// Retriable 判断该错误是否值得换一条通道重试。
//
// 不该继续回退的情形会白白浪费三倍时间，并放大对上游的请求量、加剧风控：
//   - banned：账号已被封，换通道也是封的，而且每试一次都在加重风控
//   - auth_failed：该通道的授权已经不成立，重试同一条通道没有意义
//   - consent_required：当前通道的权限不足，重复请求同一权限没有意义
//   - canceled：调用方主动取消或整体超时，继续试只会拖长响应
//
// 回退链请用 RetriableFrom：Graph 的授权与权限错误仍可能在 IMAP 通道成功。
func Retriable(err error) bool {
	switch KindOf(err) {
	case ErrKindBanned, ErrKindAuthFailed, ErrKindConsentRequired, ErrKindCanceled:
		return false
	default:
		return true
	}
}

// RetriableFrom 判断**某条通道上**的失败是否值得换下一条通道重试。
//
// Graph 与 IMAP 使用不同的 token 端点和权限请求，新版 IMAP 申请 IMAP scope，
// 旧版 login.live.com 请求省略 scope。某个 OAuth 端点拒绝当前通道，不代表其余通道都失效。
// Error.StatusCode > 0 表示错误来自 HTTP token/API 响应；IMAP XOAUTH2 登录失败没有 HTTP 状态码。
// 因此只放宽前者，密码/令牌缺失与 IMAP 登录失败仍立即停手。banned 与 canceled 永不放宽。
func RetriableFrom(channel string, err error) bool {
	kind := KindOf(err)
	if kind == ErrKindAuthFailed || kind == ErrKindConsentRequired {
		var upstream *Error
		switch channel {
		case ChannelGraph, ChannelIMAPNew, ChannelIMAPOld:
			return errors.As(err, &upstream) && upstream.StatusCode > 0
		}
	}
	return Retriable(err)
}

// RetriableWithAnotherProxy 判断该错误是否值得换一个代理重试。
//
// 只有「连不上」类的错误才换代理。认证失败、业务 4xx 换代理没有意义，
// 反而会让同一个坏账号被反复提交给上游。
func RetriableWithAnotherProxy(err error) bool {
	switch KindOf(err) {
	case ErrKindProxyFailed, ErrKindNetwork:
		return true
	default:
		return false
	}
}

// abuseMarker 是微软在账号被封时返回的特征串。
// 来源：outlookEmail `outlook_mail_reader.py:124`。
const abuseMarker = "service abuse mode"

// ClassifyIMAPAuthError 把各家 IMAP 服务器五花八门的鉴权错误翻译成可操作的中文文案。
// 来源：outlookEmail 的 normalize_imap_auth_error。
//
// QQ/163 返回的是自家提示语，原样透出用户看不懂该怎么办；
// 这里统一成「该去哪开什么开关」。
func ClassifyIMAPAuthError(raw string) (ErrKind, string) {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, abuseMarker):
		return ErrKindBanned, "账号已被服务商封禁"
	case strings.Contains(lower, "unsafe login"):
		// 网易系要求客户端先发 IMAP ID 命令，没发就报这个。
		return ErrKindProviderError, "服务商要求客户端标识，请重试；若持续失败请在邮箱设置中开启 IMAP"
	case strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "invalid credentials"),
		strings.Contains(lower, "login fail"),
		strings.Contains(lower, "auth"):
		return ErrKindAuthFailed, "授权码错误，或未在邮箱设置中开启 IMAP 服务"
	case strings.Contains(lower, "not enabled"), strings.Contains(lower, "disabled"):
		return ErrKindAuthFailed, "该邮箱未开启 IMAP 服务"
	default:
		return ErrKindAuthFailed, "IMAP 登录失败：" + raw
	}
}

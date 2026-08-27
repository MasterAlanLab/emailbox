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
// （「本次刷新失败 312 个，其中被封 47 个、令牌失效 210 个、代理故障 55 个」）——
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
//   - consent_required 由 Graph 内部的 scope 降级处理，到这一层说明降级也失败了
//   - canceled：调用方主动取消或整体超时，继续试只会拖长响应
//
// 回退链请改用 RetriableFrom：Graph 的 auth_failed 是唯一一种「换条通道就可能好」
// 的授权失败，原因见那里。
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
// 与 Retriable 只差一种情形：Graph 的 auth_failed 要继续回退。
//
// 这里曾经的推断是「三条通道用的是同一个 refresh_token，一条认证失败则三条都会失败」。
// token 确实是同一个，但**申请的 scope 不是**：
//
//	Graph → https://graph.microsoft.com/...
//	IMAP  → https://outlook.office.com/IMAP.AccessAsUser.All（见 provider.go 的 ScopeIMAP）
//
// 微软完全可能拒掉其中一套而照常签发另一套——管理员回收了 Graph 权限、
// 应用注册变更、或该账号本就只被授予过 IMAP scope，都会打到这个组合上。
// 按旧逻辑，这类账号在 Graph 一步就被判死，尽管两条 IMAP 通道拿同一个 token 拉信完全正常。
// 实测确认过：mailprobe 逐通道试时 Graph 报 auth_failed 而 IMAP 两条都成功拉到邮件。
//
// 放宽只针对 Graph，风控代价可控：
//   - banned 仍然立即停手，这才是会被越试越严的那一类
//   - IMAP 侧的 auth_failed（授权码错误、未开启 IMAP 服务）仍然立即停手，
//     那是账号自身的配置问题，换一条 IMAP 通道结果一样
func RetriableFrom(channel string, err error) bool {
	if channel == ChannelGraph && KindOf(err) == ErrKindAuthFailed {
		return true
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

// ClassifyOAuthError 从 token 端点的响应体判断失败原因。
//
// 这些判据全部来自 outlookEmail 的实战积累，凭常识猜几乎一定会错：
// 微软对「账号被封」和「令牌失效」返回的都是 400，只能靠响应体里的特征串区分，
// 而这两者的正确处置完全相反——前者要立刻停手并标记账号，后者要提示用户重新授权。
func ClassifyOAuthError(statusCode int, body string) (ErrKind, string) {
	lower := strings.ToLower(body)

	switch {
	case strings.Contains(lower, abuseMarker):
		return ErrKindBanned, "账号已被服务商封禁"
	case strings.Contains(lower, "invalid_grant"):
		return ErrKindAuthFailed, "refresh_token 已失效，需要重新授权"
	case strings.Contains(lower, "invalid_client"):
		return ErrKindAuthFailed, "client_id 无效"
	// AADSTS65001/AADSTS90008 等是「用户或管理员未同意」，属于 scope 问题，
	// 换更低的 scope 重试可能成功，因此单独归类。
	case strings.Contains(lower, "aadsts65001"), strings.Contains(lower, "aadsts90008"),
		strings.Contains(lower, "consent_required"), strings.Contains(lower, "interaction_required"):
		return ErrKindConsentRequired, "应用权限不足，需要重新授权"
	case strings.Contains(lower, "aadsts"):
		// 其它 AADSTS 多与 scope/租户配置有关，值得降级重试。
		return ErrKindConsentRequired, "Azure 拒绝了本次请求，可能是权限范围不匹配"
	case statusCode == 429:
		return ErrKindRateLimited, "请求过于频繁，已被限流"
	case statusCode >= 500:
		return ErrKindProviderError, "服务商暂时不可用"
	default:
		return ErrKindProviderError, fmt.Sprintf("令牌请求失败（HTTP %d）", statusCode)
	}
}

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

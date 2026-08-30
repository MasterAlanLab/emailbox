package mailer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// 相同的 invalid_grant 包含过期、权限和登录策略等不同原因，分类与处置文案都要钉住。
func TestClassifyOAuthError(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		want        ErrKind
		wantMessage string
	}{
		{"被封（abuse mode）", 400, `{"error":"invalid_grant","error_description":"...service abuse mode..."}`, ErrKindBanned, "封禁"},
		{"被封的大小写变体", 400, "SERVICE ABUSE MODE detected", ErrKindBanned, "封禁"},
		{"仅 invalid_grant 不推断过期", 400, `{"error":"invalid_grant","error_description":"token expired"}`, ErrKindAuthFailed, "当前通道的令牌交换未通过"},
		{"过期码优先于笼统授权失败", 400, `{"error":"invalid_grant","error_codes":[70008]}`, ErrKindAuthFailed, "因长期未使用而过期"},
		{"结构化码优先于描述中的码", 400, `{"error":"invalid_grant","error_codes":[700082],"error_description":"AADSTS65001"}`, ErrKindAuthFailed, "AADSTS700082"},
		{"固定寿命到期", 400, `{"error":"invalid_grant","error_codes":[700084]}`, ErrKindAuthFailed, "SPA 固定有效期"},
		{"授权撤销", 400, `{"error":"invalid_grant","error_codes":[50173]}`, ErrKindAuthFailed, "授权已被撤销"},
		{"多因素验证", 400, `{"error":"invalid_grant","error_codes":[50076]}`, ErrKindAuthFailed, "多因素验证"},
		{"条件访问", 400, `{"error":"invalid_grant","error_codes":[53003]}`, ErrKindAuthFailed, "条件访问策略"},
		{"登录频率策略", 400, `{"error":"invalid_grant","error_codes":[70043]}`, ErrKindAuthFailed, "登录频率策略"},
		{"交互登录不等于权限不足", 400, `{"error":"interaction_required"}`, ErrKindAuthFailed, "交互式登录验证"},
		{"客户端认证失败属于应用配置", 400, `{"error":"invalid_client"}`, ErrKindProviderError, "客户端认证失败"},
		{"客户端密钥过期属于应用配置", 400, `{"error":"invalid_client","error_codes":[7000222]}`, ErrKindProviderError, "client_secret 已过期"},
		{"invalid_grant 不遮盖权限诊断", 400, `{"error":"invalid_grant","error_codes":[65001]}`, ErrKindConsentRequired, "尚未获得所需权限"},
		{"旧端点描述中的权限诊断", 400, `{"error":"invalid_grant","error_description":"AADSTS65001: Consent missing"}`, ErrKindConsentRequired, "AADSTS65001"},
		{"未同意授权 AADSTS90008", 400, "AADSTS90008", ErrKindConsentRequired, "所需权限"},
		{"consent_required", 400, `{"error":"consent_required"}`, ErrKindConsentRequired, "所需权限"},
		{"范围不匹配", 400, "AADSTS70011: scope is not valid", ErrKindConsentRequired, "权限范围不匹配"},
		{"结构化描述中的无可用权限", 403, `{"error":"invalid_grant","error_description":"No applicable permissions found"}`, ErrKindConsentRequired, "权限范围不匹配"},
		{"结构化描述中的权限已失效", 401, `{"error":"invalid_grant","error_description":"The permissions requested are unauthorized or expired"}`, ErrKindConsentRequired, "权限范围不匹配"},
		{"普通 AADSTS 不推断为权限错误", 400, "AADSTS90023: invalid request", ErrKindProviderError, "请求参数不正确"},
		{"代理认证错误", 407, `{"error":"invalid_grant"}`, ErrKindProxyFailed, "代理认证失败"},
		{"限流优先于正文认证错误", 429, `{"error":"invalid_grant","error_description":"service abuse mode"}`, ErrKindRateLimited, "稍后重试"},
		{"服务商 5xx 优先于正文认证错误", 503, `{"error":"invalid_grant","error_codes":[700082]}`, ErrKindProviderError, "服务商暂时不可用"},
		{"未知 4xx", 418, "teapot", ErrKindProviderError, "HTTP 418"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, message := ClassifyOAuthError(c.status, c.body)
			if got != c.want {
				t.Errorf("分类 = %q，期望 %q", got, c.want)
			}
			if !strings.Contains(message, c.wantMessage) {
				t.Errorf("文案 = %q，期望包含 %q", message, c.wantMessage)
			}
		})
	}
}

func TestOAuthErrorMessagesNeverEchoProviderResponse(t *testing.T) {
	const secret = "PRIVATE-CREDENTIAL-MARKER"
	for _, body := range []string{
		`{"error":"invalid_grant","error_codes":[700082],"error_description":"` + secret + `"}`,
		`{"error":"invalid_grant","error_codes":[999999],"error_description":"AADSTS999999: ` + secret + `"}`,
		`{"error":"` + secret + `","trace_id":"` + secret + `"}`,
		"AADSTS65001: " + secret,
		secret,
	} {
		_, message := ClassifyOAuthError(400, body)
		if strings.Contains(message, secret) || strings.Contains(message, "AADSTS999999") {
			t.Fatalf("响应包含未允许回显的上游字段：%q", message)
		}
	}
}

// abuse mode 的判定必须排在 invalid_grant 之前：微软的封号响应体里两者同时出现，
// 顺序反了会把封号误判成「重新授权即可」，然后用户一遍遍重试、风控越来越重。
func TestClassifyOAuthErrorPrefersAbuseOverInvalidGrant(t *testing.T) {
	body := `{"error":"invalid_grant","error_description":"Application is in service abuse mode"}`
	if got, _ := ClassifyOAuthError(400, body); got != ErrKindBanned {
		t.Fatalf("分类 = %q，期望 banned", got)
	}
}

func TestClassifyIMAPAuthError(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want ErrKind
	}{
		{"被封", "Service abuse mode", ErrKindBanned},
		{"网易未发 IMAP ID", "EOF Unsafe Login. Please contact kefu@188.com", ErrKindProviderError},
		{"授权码错误", "Authentication failed", ErrKindAuthFailed},
		{"凭据无效", "Invalid credentials (Failure)", ErrKindAuthFailed},
		{"登录失败", "login fail", ErrKindAuthFailed},
		{"未开启服务", "IMAP service is not enabled", ErrKindAuthFailed},
		{"无法归类时也不当成成功", "something went wrong", ErrKindAuthFailed},
	}
	for _, c := range cases {
		got, message := ClassifyIMAPAuthError(c.raw)
		if got != c.want {
			t.Errorf("%s: 分类 = %q，期望 %q", c.name, got, c.want)
		}
		if message == "" {
			t.Errorf("%s: 文案为空", c.name)
		}
	}
}

// Unsafe Login 必须归到 provider_error 而不是 auth_failed：它是「客户端没发 IMAP ID」
// 造成的，重试就能过，归成 auth_failed 会让账号被误标为凭据失效。
func TestUnsafeLoginIsNotAuthFailure(t *testing.T) {
	kind, _ := ClassifyIMAPAuthError("NO Unsafe Login. Please contact kefu")
	if kind != ErrKindProviderError {
		t.Fatalf("分类 = %q，期望 provider_error", kind)
	}
	if !Retriable(newError(kind, "", "", nil)) {
		t.Fatal("Unsafe Login 应当允许换通道重试")
	}
}

func TestKindOf(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrKind
	}{
		{"本包的结构化错误", newError(ErrKindBanned, "", "", nil), ErrKindBanned},
		{"包了一层的结构化错误", fmt.Errorf("上层: %w", newError(ErrKindBanned, "", "", nil)), ErrKindBanned},
		{"context 取消", context.Canceled, ErrKindCanceled},
		{"context 超时", context.DeadlineExceeded, ErrKindCanceled},
		{"网络错误", &net.OpError{Op: "dial", Err: errors.New("refused")}, ErrKindNetwork},
		{"其它错误", errors.New("boom"), ErrKindProviderError},
		{"nil", nil, ErrKindProviderError},
	}
	for _, c := range cases {
		if got := KindOf(c.err); got != c.want {
			t.Errorf("%s: KindOf = %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestRetriable(t *testing.T) {
	cases := map[ErrKind]bool{
		ErrKindBanned:            false,
		ErrKindAuthFailed:        false,
		ErrKindConsentRequired:   false,
		ErrKindCanceled:          false,
		ErrKindNetwork:           true,
		ErrKindProxyFailed:       true,
		ErrKindRateLimited:       true,
		ErrKindFolderUnavailable: true,
		ErrKindProviderError:     true,
	}
	for kind, want := range cases {
		if got := Retriable(newError(kind, "", "", nil)); got != want {
			t.Errorf("Retriable(%s) = %v，期望 %v", kind, got, want)
		}
	}
}

// OAuth 端点的授权和权限失败可换资源通道；本地缺凭据与 IMAP 登录失败仍立即停止。
func TestRetriableFrom(t *testing.T) {
	cases := []struct {
		name    string
		channel string
		kind    ErrKind
		status  int
		want    bool
	}{
		{"Graph OAuth 认证失败", ChannelGraph, ErrKindAuthFailed, 400, true},
		{"新版 IMAP OAuth 认证失败", ChannelIMAPNew, ErrKindAuthFailed, 400, true},
		{"旧版 IMAP OAuth 认证失败", ChannelIMAPOld, ErrKindAuthFailed, 400, true},
		{"Graph 本地缺凭据", ChannelGraph, ErrKindAuthFailed, 0, false},
		{"新版 IMAP 登录失败", ChannelIMAPNew, ErrKindAuthFailed, 0, false},
		{"密码 IMAP 登录失败", ChannelIMAP, ErrKindAuthFailed, 0, false},
		// 被封才是越试越严的那一类，Graph 上也必须立即停手。
		{"Graph 封禁", ChannelGraph, ErrKindBanned, 400, false},
		{"Graph 权限不足", ChannelGraph, ErrKindConsentRequired, 403, true},
		{"新版 IMAP 权限不足", ChannelIMAPNew, ErrKindConsentRequired, 400, true},
		{"调用取消", ChannelGraph, ErrKindCanceled, 400, false},
		{"Graph 网络错误", ChannelGraph, ErrKindNetwork, 0, true},
		{"IMAP 网络错误", ChannelIMAPNew, ErrKindNetwork, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := newError(c.kind, c.channel, "", nil)
			err.StatusCode = c.status
			if got := RetriableFrom(c.channel, err); got != c.want {
				t.Errorf("RetriableFrom(%s, %s, HTTP %d) = %v，期望 %v", c.channel, c.kind, c.status, got, c.want)
			}
		})
	}
}

// 换代理只对「连不上」有意义。认证失败换代理不会变好，只会让同一个坏账号被反复提交。
func TestRetriableWithAnotherProxy(t *testing.T) {
	cases := map[ErrKind]bool{
		ErrKindProxyFailed:   true,
		ErrKindNetwork:       true,
		ErrKindAuthFailed:    false,
		ErrKindBanned:        false,
		ErrKindRateLimited:   false,
		ErrKindProviderError: false,
	}
	for kind, want := range cases {
		if got := RetriableWithAnotherProxy(newError(kind, "", "", nil)); got != want {
			t.Errorf("RetriableWithAnotherProxy(%s) = %v，期望 %v", kind, got, want)
		}
	}
}

func TestErrorIsMatchesByKind(t *testing.T) {
	err := fmt.Errorf("包一层: %w", newError(ErrKindBanned, ChannelGraph, "账号已被封禁", nil))
	if !errors.Is(err, &Error{Kind: ErrKindBanned}) {
		t.Error("按 Kind 匹配失败")
	}
	if errors.Is(err, &Error{Kind: ErrKindAuthFailed}) {
		t.Error("不同 Kind 不应匹配")
	}
	// 空 Kind 表示「只要是本包的错误就算匹配」。
	if !errors.Is(err, &Error{}) {
		t.Error("空 Kind 应当匹配任意本包错误")
	}
	if errors.Is(errors.New("boom"), &Error{Kind: ErrKindBanned}) {
		t.Error("非本包错误不应匹配")
	}
}

func TestErrorUnwrapKeepsCause(t *testing.T) {
	cause := errors.New("底层原因")
	err := newError(ErrKindNetwork, ChannelGraph, "连接失败", cause)
	if !errors.Is(err, cause) {
		t.Error("原始错误应当可以被 errors.Is 找到")
	}
	if err.Error() == "" {
		t.Error("错误文本为空")
	}
	withDetail := newError(ErrKindNetwork, ChannelGraph, "连接失败", cause)
	withDetail.Detail = "dial tcp 1.2.3.4:993"
	if withDetail.Error() == err.Error() {
		t.Error("Detail 应当出现在错误文本里")
	}
}

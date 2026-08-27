package mailer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

// 微软对「账号被封」和「令牌失效」返回的都是 400，只能靠响应体里的特征串区分，
// 而两者的正确处置完全相反——判错一次就是「把已封的账号反复提交给上游」
// 或者「把可修复的授权问题报成封号」。这张表是这层判断的全部依据。
func TestClassifyOAuthError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   ErrKind
	}{
		{"被封（abuse mode）", 400, `{"error":"invalid_grant","error_description":"...service abuse mode..."}`, ErrKindBanned},
		{"被封的大小写变体", 400, "SERVICE ABUSE MODE detected", ErrKindBanned},
		{"令牌失效", 400, `{"error":"invalid_grant","error_description":"token expired"}`, ErrKindAuthFailed},
		{"client_id 无效", 400, `{"error":"invalid_client"}`, ErrKindAuthFailed},
		{"未同意授权 AADSTS65001", 400, "AADSTS65001: The user or administrator has not consented", ErrKindConsentRequired},
		{"未同意授权 AADSTS90008", 400, "AADSTS90008", ErrKindConsentRequired},
		{"consent_required", 400, `{"error":"consent_required"}`, ErrKindConsentRequired},
		{"其它 AADSTS 也值得降级重试", 400, "AADSTS70011: scope is not valid", ErrKindConsentRequired},
		{"限流", 429, "too many requests", ErrKindRateLimited},
		{"服务商 5xx", 503, "service unavailable", ErrKindProviderError},
		{"未知 4xx", 418, "teapot", ErrKindProviderError},
	}
	for _, c := range cases {
		got, message := ClassifyOAuthError(c.status, c.body)
		if got != c.want {
			t.Errorf("%s: 分类 = %q，期望 %q", c.name, got, c.want)
		}
		if message == "" {
			t.Errorf("%s: 文案为空，用户看不到任何可操作的信息", c.name)
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

// RetriableFrom 与 Retriable 只差一处：Graph 的 auth_failed 要继续回退，
// 因为 Graph 与 IMAP 申请的是不同 scope，微软可以只拒其中一套。
func TestRetriableFrom(t *testing.T) {
	cases := []struct {
		channel string
		kind    ErrKind
		want    bool
	}{
		{ChannelGraph, ErrKindAuthFailed, true},
		{ChannelIMAPNew, ErrKindAuthFailed, false},
		{ChannelIMAPOld, ErrKindAuthFailed, false},
		{ChannelIMAP, ErrKindAuthFailed, false},
		// 被封才是越试越严的那一类，Graph 上也必须立即停手。
		{ChannelGraph, ErrKindBanned, false},
		{ChannelGraph, ErrKindConsentRequired, false},
		{ChannelGraph, ErrKindCanceled, false},
		{ChannelGraph, ErrKindNetwork, true},
		{ChannelIMAPNew, ErrKindNetwork, true},
	}
	for _, c := range cases {
		if got := RetriableFrom(c.channel, newError(c.kind, c.channel, "", nil)); got != c.want {
			t.Errorf("RetriableFrom(%s, %s) = %v，期望 %v", c.channel, c.kind, got, c.want)
		}
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

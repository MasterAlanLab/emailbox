package server

import "testing"

func TestSanitizedLogURIHidesOAuthAuthorizationCode(t *testing.T) {
	got := sanitizedLogURI("/api/v1/oauth/microsoft/callback?code=secret-code&state=secret-state")
	if got != "/api/v1/oauth/microsoft/callback" {
		t.Fatalf("OAuth 回调日志泄露了查询参数: %q", got)
	}
	ordinary := "/api/v1/tenants/t/mail/accounts?page=2"
	if sanitizedLogURI(ordinary) != ordinary {
		t.Fatal("普通查询参数不应被改写")
	}
}

// 桌面版的 nonce 能直接换出一个完整会话，落进访问日志等于把会话写进了日志文件。
func TestSanitizedLogURIHidesDesktopNonce(t *testing.T) {
	got := sanitizedLogURI(desktopSessionPath + "?nonce=deadbeef")
	if got != desktopSessionPath {
		t.Fatalf("桌面自动登录入口泄露了 nonce: %q", got)
	}
}

package main

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

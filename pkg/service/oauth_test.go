package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"emailbox/configs"
	"emailbox/pkg/crypto"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

func oauthFixture(t *testing.T, identity string) (*service.OAuthService, *service.AccountService, *repo.Store, string, string, string, crypto.Cipher) {
	t.Helper()
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	registered, _, err := service.NewAuthService(store).Register(context.Background(),
		model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	accountService := service.NewAccountService(store, cipher, quota.NewService(store))
	account, err := accountService.Create(context.Background(), registered.Tenants[0].ID, model.CreateMailAccountRequest{
		Email: "user@outlook.com", ClientID: "old-client", RefreshToken: "old-refresh", Remark: "keep-remark",
	})
	if err != nil {
		t.Fatal(err)
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			if r.Form.Get("grant_type") == "authorization_code" {
				if r.Form.Get("code_verifier") == "" {
					t.Error("授权码交换缺少 PKCE verifier")
				}
				_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"new-refresh"}`))
				return
			}
			if r.Form.Get("refresh_token") != "new-refresh" {
				t.Errorf("验证的 token 错误: %q", r.Form.Get("refresh_token"))
			}
			_, _ = w.Write([]byte(`{"access_token":"verified","refresh_token":"rotated-refresh"}`))
		case "/me":
			if r.Header.Get("Authorization") != "Bearer access" {
				t.Error("Graph /me 未使用交换得到的 access token")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"mail": identity})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	messages := service.NewMessageService(store, cipher, quota.NewService(store), service.ChainOptions{})
	oauth := service.NewOAuthService(store, cipher, quota.NewService(store), messages, service.OAuthOptions{
		Enabled: true, ClientID: "platform-client", RedirectURI: "http://localhost:8080",
		AuthorizeURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", GraphBaseURL: provider.URL,
	})
	return oauth, accountService, store, registered.Tenants[0].ID, registered.User.ID, account.ID, cipher
}

func TestOAuthReauthorizationOnlyReplacesVerifiedCredentials(t *testing.T) {
	oauth, accounts, store, tenantID, userID, accountID, cipher := oauthFixture(t, "user@outlook.com")
	ctx := context.Background()
	started, err := oauth.Start(ctx, tenantID, accountID, userID)
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL, err := url.Parse(started.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if authorizeURL.Query().Get("code_challenge_method") != "S256" || authorizeURL.Query().Get("state") == "" {
		t.Fatalf("授权链接缺少 state/PKCE: %s", started.AuthorizationURL)
	}
	flow, err := store.GetOAuthAuthorization(ctx, tenantID, started.FlowID)
	if err != nil {
		t.Fatal(err)
	}
	if !crypto.IsEncrypted(flow.CodeVerifierEnc) || strings.Contains(flow.CodeVerifierEnc, authorizeURL.Query().Get("code_challenge")) {
		t.Error("PKCE verifier 应只以密文落库")
	}

	redirected := "http://localhost:8080/?code=one-time-code&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
	exchanged, err := oauth.ExchangeRedirectedURL(ctx, redirected)
	if err != nil {
		t.Fatal(err)
	}
	if exchanged.FlowID != started.FlowID {
		t.Fatalf("交换了错误流程: %+v", exchanged)
	}
	before, err := accounts.Credentials(ctx, tenantID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if before.RefreshToken != "old-refresh" {
		t.Errorf("完成验证前旧 token 被覆盖: %q", before.RefreshToken)
	}

	if _, err := oauth.Complete(ctx, tenantID, accountID, userID, started.FlowID); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := cipher.Decrypt(after.RefreshTokenEnc)
	if err != nil {
		t.Fatal(err)
	}
	if after.ClientID != "platform-client" || plain != "rotated-refresh" || after.AuthChannel != "graph" {
		t.Errorf("授权凭据写回错误: client=%q token=%q channel=%q", after.ClientID, plain, after.AuthChannel)
	}
	if after.Remark != "keep-remark" || after.LastRefreshStatus != model.RefreshSuccess || after.LastRefreshErrorKind != "" {
		t.Errorf("窄更新覆盖了其它字段或未清刷新状态: %+v", after)
	}
	if _, err := oauth.Complete(ctx, tenantID, accountID, userID, started.FlowID); !errors.Is(err, service.ErrOAuthFlowInvalid) {
		t.Errorf("一次性流程被重复使用: %v", err)
	}
}

func TestOAuthIdentityMismatchKeepsOldCredential(t *testing.T) {
	oauth, accounts, _, tenantID, userID, accountID, _ := oauthFixture(t, "other@outlook.com")
	started, err := oauth.Start(context.Background(), tenantID, accountID, userID)
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL, _ := url.Parse(started.AuthorizationURL)
	redirected := "http://localhost:8080/?code=one-time-code&state=" + url.QueryEscape(authorizeURL.Query().Get("state"))
	if _, err := oauth.ExchangeRedirectedURL(context.Background(), redirected); !errors.Is(err, service.ErrOAuthIdentityMismatch) {
		t.Fatalf("邮箱身份不一致应被拒绝: %v", err)
	}
	credential, err := accounts.Credentials(context.Background(), tenantID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.RefreshToken != "old-refresh" {
		t.Errorf("身份核对失败后旧 token 被覆盖: %q", credential.RefreshToken)
	}
}

func TestOAuthFlowIsTenantAndActorBound(t *testing.T) {
	oauth, _, store, tenantID, userID, accountID, _ := oauthFixture(t, "user@outlook.com")
	started, err := oauth.Start(context.Background(), tenantID, accountID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOAuthAuthorization(context.Background(), "another-tenant", started.FlowID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("跨租户读取流程应返回 not found: %v", err)
	}
	if _, err := oauth.Complete(context.Background(), tenantID, accountID, "another-user", started.FlowID); !errors.Is(err, service.ErrOAuthFlowInvalid) {
		t.Fatalf("其它用户不应完成该流程: %v", err)
	}
}

func TestOAuthReauthorizationRejectsPasswordIMAPAccount(t *testing.T) {
	oauth, accounts, _, tenantID, userID, _, _ := oauthFixture(t, "user@outlook.com")
	imap, err := accounts.Create(context.Background(), tenantID, model.CreateMailAccountRequest{
		Email: "user@gmail.com", AccountType: "imap", IMAPPassword: "app-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oauth.Start(context.Background(), tenantID, imap.ID, userID); !errors.Is(err, service.ErrOAuthAccountType) {
		t.Fatalf("密码 IMAP 账号不应进入 Microsoft OAuth: %v", err)
	}
}

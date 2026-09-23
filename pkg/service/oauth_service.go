package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emailbox/pkg/crypto"
	"emailbox/pkg/mailer"
	"emailbox/pkg/mailer/imapx"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

const oauthFlowTTL = 10 * time.Minute

var (
	ErrOAuthDisabled         = errors.New("该邮箱服务商的 OAuth 未启用")
	ErrOAuthAccountType      = errors.New("该账号类型不支持 OAuth 重新授权")
	ErrOAuthFlowInvalid      = errors.New("授权流程无效或已过期，请重新发起")
	ErrOAuthIdentityMismatch = errors.New("OAuth 返回的邮箱与当前账号不一致")
)

// OAuthProviderOptions 描述一个服务商的 OAuth 应用。
type OAuthProviderOptions struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	Tenant       string
	RedirectURI  string
	AuthorizeURL string
	TokenURL     string
	IdentityURL  string
	Scope        string
}

// OAuthOptions 同时承载 Microsoft IMAP OAuth 与 Gmail IMAP OAuth。
type OAuthOptions struct {
	Microsoft OAuthProviderOptions
	Google    OAuthProviderOptions
	ReturnURL string
	Timeout   time.Duration
}

type OAuthService struct {
	store    *repo.Store
	cipher   crypto.Cipher
	quota    *quota.Service
	messages *MessageService
	opt      OAuthOptions
}

func NewOAuthService(store *repo.Store, cipher crypto.Cipher, q *quota.Service, messages *MessageService, opt OAuthOptions) *OAuthService {
	if opt.Microsoft.Tenant == "" {
		opt.Microsoft.Tenant = "common"
	}
	if opt.Microsoft.AuthorizeURL == "" {
		opt.Microsoft.AuthorizeURL = "https://login.microsoftonline.com/" + url.PathEscape(opt.Microsoft.Tenant) + "/oauth2/v2.0/authorize"
	}
	if opt.Microsoft.TokenURL == "" {
		opt.Microsoft.TokenURL = "https://login.microsoftonline.com/" + url.PathEscape(opt.Microsoft.Tenant) + "/oauth2/v2.0/token"
	}
	if opt.Microsoft.Scope == "" {
		opt.Microsoft.Scope = "openid profile email offline_access " + mailer.ScopeIMAP
	}
	if opt.Microsoft.IdentityURL == "" {
		// OIDC 身份端点只用于核对邮箱，不读取邮件；邮件本身始终走 IMAP。
		opt.Microsoft.IdentityURL = "https://graph.microsoft.com/oidc/userinfo"
	}
	if opt.Google.AuthorizeURL == "" {
		opt.Google.AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if opt.Google.TokenURL == "" {
		opt.Google.TokenURL = mailer.TokenURLGoogle
	}
	if opt.Google.Scope == "" {
		opt.Google.Scope = "openid email profile " + mailer.ScopeGmailIMAP
	}
	if opt.Google.IdentityURL == "" {
		// OIDC userinfo 只用于核对授权账号，不依赖 Gmail API 的额外 scope。
		opt.Google.IdentityURL = "https://openidconnect.googleapis.com/v1/userinfo"
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 30 * time.Second
	}
	return &OAuthService{store: store, cipher: cipher, quota: q, messages: messages, opt: opt}
}

type OAuthStartResult struct {
	FlowID           string    `json:"flow_id"`
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type OAuthExchangeResult struct {
	FlowID, TenantID, AccountID string
}

type OAuthCompleteResult struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
}

func (s *OAuthService) providerOptions(account *model.MailAccount) (*OAuthProviderOptions, string, error) {
	if strings.EqualFold(strings.TrimSpace(account.Provider), "gmail") {
		if account.RefreshTokenEnc == "" && account.IMAPPasswordEnc != "" {
			return nil, "", ErrOAuthAccountType
		}
		if !s.opt.Google.Enabled {
			return nil, "", ErrOAuthDisabled
		}
		return &s.opt.Google, "gmail", nil
	}
	if strings.EqualFold(strings.TrimSpace(account.Provider), "outlook") || account.AccountType == string(mailer.AccountTypeOutlook) {
		if !s.opt.Microsoft.Enabled {
			return nil, "", ErrOAuthDisabled
		}
		return &s.opt.Microsoft, "outlook", nil
	}
	return nil, "", ErrOAuthAccountType
}

func (s *OAuthService) Start(ctx context.Context, tenantID, accountID, actorUserID string) (*OAuthStartResult, error) {
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	cfg, provider, err := s.providerOptions(account)
	if err != nil {
		return nil, err
	}
	if cfg.ClientID == "" || cfg.RedirectURI == "" {
		return nil, ErrOAuthDisabled
	}
	if _, err := s.store.DeleteExpiredOAuthAuthorizations(ctx, tenantID); err != nil {
		return nil, fmt.Errorf("清理过期 OAuth 流程失败: %w", err)
	}

	flowID := uuid.NewString()
	secret, err := randomURLToken(32)
	if err != nil {
		return nil, fmt.Errorf("生成 OAuth state: %w", err)
	}
	state := flowID + "." + tenantID + "." + secret
	verifier, err := randomURLToken(48)
	if err != nil {
		return nil, fmt.Errorf("生成 PKCE verifier: %w", err)
	}
	verifierEnc, err := s.cipher.Encrypt(verifier)
	if err != nil {
		return nil, fmt.Errorf("加密 PKCE verifier: %w", err)
	}
	expiresAt := time.Now().UTC().Add(oauthFlowTTL)
	if err := s.store.CreateOAuthAuthorization(ctx, repo.OAuthAuthorization{
		ID: flowID, TenantID: tenantID, AccountID: accountID, ActorUserID: actorUserID,
		StateHash: crypto.HashToken(state), CodeVerifierEnc: verifierEnc, ExpiresAt: expiresAt,
	}); err != nil {
		return nil, err
	}

	challengeSum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {cfg.ClientID}, "response_type": {"code"}, "redirect_uri": {cfg.RedirectURI},
		"response_mode": {"query"}, "scope": {cfg.Scope}, "state": {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challengeSum[:])},
		"code_challenge_method": {"S256"},
	}
	if provider == "gmail" {
		q.Set("access_type", "offline")
		q.Set("prompt", "consent select_account")
	} else {
		q.Set("prompt", "select_account")
	}
	return &OAuthStartResult{FlowID: flowID, AuthorizationURL: cfg.AuthorizeURL + "?" + q.Encode(), ExpiresAt: expiresAt}, nil
}

// ExchangeRedirectedURL 兼容 localhost 回调：用户把地址栏里的最终地址粘贴回来。
func (s *OAuthService) ExchangeRedirectedURL(ctx context.Context, redirectedURL string) (*OAuthExchangeResult, error) {
	if len(redirectedURL) == 0 || len(redirectedURL) > 16*1024 {
		return nil, ErrOAuthFlowInvalid
	}
	u, err := url.Parse(redirectedURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, ErrOAuthFlowInvalid
	}
	q := u.Query()
	return s.ExchangeCallback(ctx, q.Get("state"), q.Get("code"), q.Get("error_description"))
}

// ExchangeCallback 校验一次性 state，用授权码换令牌并核对身份。
//
//nolint:gocyclo // OAuth callback intentionally keeps validation, identity checks, and token persistence in one transaction boundary.
func (s *OAuthService) ExchangeCallback(ctx context.Context, state, code, providerError string) (*OAuthExchangeResult, error) {
	flowID, tenantID, flow, err := s.callbackFlow(ctx, state)
	if err != nil {
		return nil, err
	}
	account, err := s.store.GetMailAccount(ctx, tenantID, flow.AccountID)
	if err != nil {
		return nil, err
	}
	cfg, provider, err := s.providerOptions(account)
	if err != nil {
		return nil, err
	}
	if providerError != "" {
		s.recordOAuthFailure(ctx, tenantID, flowID, "用户未完成 OAuth 授权")
		return nil, errors.New("授权未完成，请重新发起")
	}
	if code == "" || len(code) > 8*1024 {
		return nil, ErrOAuthFlowInvalid
	}
	verifier, err := s.cipher.Decrypt(flow.CodeVerifierEnc)
	if err != nil {
		return nil, ErrCredentialUndecryptable
	}
	hc, err := s.httpClient(ctx, tenantID, account)
	if err != nil {
		return nil, err
	}
	token, err := s.exchangeCode(ctx, hc, *cfg, code, verifier)
	if err != nil {
		s.recordOAuthFailure(ctx, tenantID, flowID, truncateError(err.Error()))
		return nil, err
	}
	email, err := s.fetchIdentity(ctx, hc, *cfg, provider, token.AccessToken)
	if err != nil {
		s.recordOAuthFailure(ctx, tenantID, flowID, "OAuth 账号身份校验失败")
		return nil, err
	}
	if !s.identityMatches(ctx, tenantID, account, email) {
		s.recordOAuthFailure(ctx, tenantID, flowID, "OAuth 账号身份不一致")
		return nil, ErrOAuthIdentityMismatch
	}
	refreshToken := strings.TrimSpace(token.RefreshToken)
	if refreshToken == "" && account.RefreshTokenEnc != "" {
		refreshToken, err = s.cipher.Decrypt(account.RefreshTokenEnc)
		if err != nil {
			return nil, ErrCredentialUndecryptable
		}
	}
	if refreshToken == "" {
		s.recordOAuthFailure(ctx, tenantID, flowID, "OAuth 未返回 refresh token")
		return nil, errors.New("OAuth 未返回 refresh_token，请确认已授予离线访问权限")
	}
	tokenEnc, err := s.cipher.Encrypt(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("加密 refresh token: %w", err)
	}
	if err := s.store.MarkOAuthAuthorizationExchanged(ctx, tenantID, flowID, tokenEnc, email); err != nil {
		return nil, ErrOAuthFlowInvalid
	}
	return &OAuthExchangeResult{FlowID: flowID, TenantID: tenantID, AccountID: flow.AccountID}, nil
}

func (s *OAuthService) callbackFlow(ctx context.Context, state string) (string, string, *repo.OAuthAuthorization, error) {
	flowID, tenantID, ok := parseOAuthState(state)
	if !ok {
		return "", "", nil, ErrOAuthFlowInvalid
	}
	flow, err := s.store.GetOAuthAuthorizationByState(ctx, tenantID, flowID, crypto.HashToken(state))
	if err != nil || flow.Status != "started" || time.Now().After(flow.ExpiresAt) {
		return "", "", nil, ErrOAuthFlowInvalid
	}
	return flowID, tenantID, flow, nil
}

func (s *OAuthService) recordOAuthFailure(ctx context.Context, tenantID, flowID, message string) {
	if err := s.store.MarkOAuthAuthorizationFailed(context.WithoutCancel(ctx), tenantID, flowID, message); err != nil {
		slog.Warn("记录 OAuth 失败状态失败", "tenant_id", tenantID, "flow_id", flowID, "error", err)
	}
}

//nolint:gocyclo // Completion coordinates credential checks, protocol refresh, quota accounting, and narrow persistence.
func (s *OAuthService) Complete(ctx context.Context, tenantID, accountID, actorUserID, flowID string) (*OAuthCompleteResult, error) {
	flow, err := s.store.GetOAuthAuthorization(ctx, tenantID, flowID)
	if err != nil {
		return nil, err
	}
	if flow.AccountID != accountID || flow.ActorUserID != actorUserID || flow.Status != "exchanged" || time.Now().After(flow.ExpiresAt) {
		return nil, ErrOAuthFlowInvalid
	}
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	cfg, provider, err := s.providerOptions(account)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.cipher.Decrypt(flow.RefreshTokenEnc)
	if err != nil {
		return nil, ErrCredentialUndecryptable
	}
	proxy, err := s.messages.resolveProxy(ctx, tenantID, account)
	if err != nil {
		return nil, err
	}
	latestToken := refreshToken
	channel := mailer.ChannelIMAPNew
	if provider == "gmail" {
		channel = mailer.ChannelIMAPGmail
	}
	client := imapx.New(imapx.Config{Channel: channel, TokenURL: cfg.TokenURL, Timeout: s.opt.Timeout,
		OnTokenRefresh: func(_ string, rotated string) { latestToken = rotated }})
	cred := mailer.Credential{Email: account.Email, Provider: provider,
		AccountType: mailer.AccountType(account.AccountType), ClientID: cfg.ClientID,
		ClientSecret: cfg.ClientSecret, RefreshToken: refreshToken, Proxy: proxy}
	if err := s.quota.Record(ctx, tenantID, model.MetricTokenRefresh, 1); err != nil {
		return nil, err
	}
	if err := client.RefreshToken(ctx, cred); err != nil {
		return nil, err
	}
	tokenEnc, err := s.cipher.Encrypt(latestToken)
	if err != nil {
		return nil, fmt.Errorf("加密 refresh token: %w", err)
	}
	if err := s.store.WithTx(ctx, func(tx *repo.Store) error {
		if err := tx.UpdateMailAccountAuthorization(ctx, tenantID, accountID, cfg.ClientID, tokenEnc, channel); err != nil {
			return err
		}
		return tx.ConsumeOAuthAuthorization(ctx, tenantID, flowID)
	}); err != nil {
		return nil, err
	}
	return &OAuthCompleteResult{AccountID: accountID, Email: account.Email, Status: "success"}, nil
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (s *OAuthService) exchangeCode(ctx context.Context, hc *http.Client, cfg OAuthProviderOptions, code, verifier string) (oauthTokenResponse, error) {
	form := url.Values{"client_id": {cfg.ClientID}, "grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {cfg.RedirectURI}, "code_verifier": {verifier}}
	if cfg.Scope != "" {
		form.Set("scope", cfg.Scope)
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return oauthTokenResponse{}, errors.New("连接 OAuth 令牌服务失败")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return oauthTokenResponse{}, errors.New("读取 OAuth 令牌响应失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, message := mailer.ClassifyOAuthError(resp.StatusCode, string(body))
		return oauthTokenResponse{}, errors.New(message)
	}
	var raw oauthTokenResponse
	if err := json.Unmarshal(body, &raw); err != nil || raw.AccessToken == "" {
		return oauthTokenResponse{}, errors.New("OAuth 令牌响应格式错误")
	}
	return raw, nil
}

func (s *OAuthService) fetchIdentity(ctx context.Context, hc *http.Client, cfg OAuthProviderOptions, provider, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.IdentityURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := hc.Do(req)
	if err != nil {
		return "", errors.New("连接 OAuth 身份服务失败")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("读取 OAuth 账号身份失败")
	}
	var raw struct {
		EmailAddress      string `json:"emailAddress"`
		Email             string `json:"email"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		PreferredUsername string `json:"preferred_username"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return "", fmt.Errorf("%s 账号身份响应格式错误", provider)
	}
	for _, candidate := range []string{raw.EmailAddress, raw.Email, raw.Mail, raw.UserPrincipalName, raw.PreferredUsername} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate), nil
		}
	}
	return "", errors.New("OAuth 账号身份中没有邮箱地址")
}

func (s *OAuthService) identityMatches(ctx context.Context, tenantID string, account *model.MailAccount, email string) bool {
	if strings.EqualFold(strings.TrimSpace(account.Email), strings.TrimSpace(email)) {
		return true
	}
	aliases, err := s.store.ListMailAliases(ctx, tenantID, []string{account.ID})
	if err != nil {
		return false
	}
	for _, alias := range aliases[account.ID] {
		if strings.EqualFold(strings.TrimSpace(alias), strings.TrimSpace(email)) {
			return true
		}
	}
	return false
}

func (s *OAuthService) httpClient(ctx context.Context, tenantID string, account *model.MailAccount) (*http.Client, error) {
	proxy, err := s.messages.resolveProxy(ctx, tenantID, account)
	if err != nil {
		return nil, err
	}
	candidates := mailer.ProxyCandidates(proxy, account.Email)
	return mailer.NewHTTPClient(candidates[0], s.opt.Timeout)
}

func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parseOAuthState(state string) (flowID, tenantID string, ok bool) {
	if len(state) == 0 || len(state) > 1024 {
		return "", "", false
	}
	parts := strings.Split(state, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

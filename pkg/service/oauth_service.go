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
	"emailbox/pkg/mailer/graph"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

const oauthFlowTTL = 10 * time.Minute

var (
	ErrOAuthDisabled         = errors.New("微软 OAuth 重新授权未启用")
	ErrOAuthAccountType      = errors.New("该账号不是 Outlook OAuth 账号")
	ErrOAuthFlowInvalid      = errors.New("授权流程无效或已过期，请重新发起")
	ErrOAuthIdentityMismatch = errors.New("微软返回的邮箱与当前账号不一致")
)

type OAuthOptions struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	Tenant       string
	RedirectURI  string
	AuthorizeURL string
	TokenURL     string
	GraphBaseURL string
	Timeout      time.Duration
}

type OAuthService struct {
	store    *repo.Store
	cipher   crypto.Cipher
	quota    *quota.Service
	messages *MessageService
	opt      OAuthOptions
}

func NewOAuthService(store *repo.Store, cipher crypto.Cipher, q *quota.Service, messages *MessageService, opt OAuthOptions) *OAuthService {
	if opt.Tenant == "" {
		opt.Tenant = "common"
	}
	if opt.AuthorizeURL == "" {
		opt.AuthorizeURL = "https://login.microsoftonline.com/" + url.PathEscape(opt.Tenant) + "/oauth2/v2.0/authorize"
	}
	if opt.TokenURL == "" {
		opt.TokenURL = "https://login.microsoftonline.com/" + url.PathEscape(opt.Tenant) + "/oauth2/v2.0/token"
	}
	if opt.GraphBaseURL == "" {
		opt.GraphBaseURL = graph.DefaultBaseURL
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

func (s *OAuthService) Start(ctx context.Context, tenantID, accountID, actorUserID string) (*OAuthStartResult, error) {
	if !s.opt.Enabled {
		return nil, ErrOAuthDisabled
	}
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if mailer.AccountType(account.AccountType) != mailer.AccountTypeOutlook {
		return nil, ErrOAuthAccountType
	}
	// 按租户顺手清理历史流程，既不需要一条漏 tenant_id 的全表清理 SQL，
	// 也避免 PKCE verifier 密文与失败信息无限增长。
	if _, err := s.store.DeleteExpiredOAuthAuthorizations(ctx, tenantID); err != nil {
		slog.Warn("清理过期 OAuth 流程失败", "tenant_id", tenantID, "error", err)
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
		"client_id": {s.opt.ClientID}, "response_type": {"code"}, "redirect_uri": {s.opt.RedirectURI},
		"response_mode": {"query"}, "scope": {strings.Join(mailer.OAuthAuthorizeScopes, " ")},
		"state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challengeSum[:])},
		"code_challenge_method": {"S256"}, "prompt": {"select_account"},
	}
	return &OAuthStartResult{FlowID: flowID, AuthorizationURL: s.opt.AuthorizeURL + "?" + q.Encode(), ExpiresAt: expiresAt}, nil
}

// ExchangeRedirectedURL 兼容参考应用已注册的 localhost 回调：用户把地址栏里的最终地址粘贴回来。
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

// ExchangeCallback 校验一次性 state，用授权码换令牌并核对 `/me` 身份。
// 它只把令牌密文写入短期流程，账号旧凭据要到 Complete 验证后才会更新。
func (s *OAuthService) ExchangeCallback(ctx context.Context, state, code, providerError string) (*OAuthExchangeResult, error) {
	if !s.opt.Enabled {
		return nil, ErrOAuthDisabled
	}
	flowID, tenantID, flow, err := s.callbackFlow(ctx, state)
	if err != nil {
		return nil, err
	}
	if providerError != "" {
		s.recordOAuthFailure(ctx, tenantID, flowID, "用户未完成微软授权")
		return nil, errors.New("微软授权未完成，请重新发起")
	}
	if code == "" || len(code) > 8*1024 {
		return nil, ErrOAuthFlowInvalid
	}

	account, err := s.store.GetMailAccount(ctx, tenantID, flow.AccountID)
	if err != nil {
		return nil, err
	}
	verifier, err := s.cipher.Decrypt(flow.CodeVerifierEnc)
	if err != nil {
		return nil, ErrCredentialUndecryptable
	}
	hc, err := s.httpClient(ctx, tenantID, account)
	if err != nil {
		return nil, err
	}
	token, err := s.exchangeCode(ctx, hc, code, verifier)
	if err != nil {
		s.recordOAuthFailure(ctx, tenantID, flowID, truncateError(err.Error()))
		return nil, err
	}
	email, err := s.fetchIdentity(ctx, hc, token.AccessToken)
	if err != nil {
		s.recordOAuthFailure(ctx, tenantID, flowID, "Microsoft 账号身份校验失败")
		return nil, err
	}
	if !s.identityMatches(ctx, tenantID, account, email) {
		s.recordOAuthFailure(ctx, tenantID, flowID, "Microsoft 账号身份不一致")
		return nil, ErrOAuthIdentityMismatch
	}
	if token.RefreshToken == "" {
		s.recordOAuthFailure(ctx, tenantID, flowID, "Microsoft 未返回 refresh token")
		return nil, errors.New("微软未返回 refresh_token，请确认已授予 offline_access")
	}
	tokenEnc, err := s.cipher.Encrypt(token.RefreshToken)
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
		slog.Warn("记录 OAuth 流程失败状态失败", "tenant_id", tenantID, "flow_id", flowID, "error", err)
	}
}

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
	if mailer.AccountType(account.AccountType) != mailer.AccountTypeOutlook {
		return nil, ErrOAuthAccountType
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
	client := graph.New(graph.Config{TokenURL: s.opt.TokenURL, Timeout: s.opt.Timeout, OnTokenRefresh: func(_ string, rotated string) { latestToken = rotated }})
	cred := mailer.Credential{Email: account.Email, AccountType: mailer.AccountTypeOutlook,
		ClientID: s.opt.ClientID, ClientSecret: s.opt.ClientSecret, RefreshToken: refreshToken, Proxy: proxy}
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
		if err := tx.UpdateMailAccountAuthorization(ctx, tenantID, accountID, s.opt.ClientID, tokenEnc, mailer.ChannelGraph); err != nil {
			return err
		}
		return tx.ConsumeOAuthAuthorization(ctx, tenantID, flowID)
	}); err != nil {
		return nil, err
	}
	return &OAuthCompleteResult{AccountID: accountID, Email: account.Email, Status: "success"}, nil
}

type oauthTokenResponse struct {
	AccessToken, RefreshToken string
}

func (s *OAuthService) exchangeCode(ctx context.Context, hc *http.Client, code, verifier string) (oauthTokenResponse, error) {
	form := url.Values{"client_id": {s.opt.ClientID}, "grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {s.opt.RedirectURI}, "scope": {strings.Join(mailer.OAuthAuthorizeScopes, " ")}, "code_verifier": {verifier}}
	if s.opt.ClientSecret != "" {
		form.Set("client_secret", s.opt.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.opt.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return oauthTokenResponse{}, errors.New("连接 Microsoft 令牌服务失败")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return oauthTokenResponse{}, errors.New("读取 Microsoft 令牌响应失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, message := mailer.ClassifyOAuthError(resp.StatusCode, string(body))
		return oauthTokenResponse{}, errors.New(message)
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || raw.AccessToken == "" {
		return oauthTokenResponse{}, errors.New("微软令牌响应格式错误")
	}
	return oauthTokenResponse{AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken}, nil
}

func (s *OAuthService) fetchIdentity(ctx context.Context, hc *http.Client, accessToken string) (string, error) {
	endpoint := strings.TrimRight(s.opt.GraphBaseURL, "/") + "/me?$select=mail,userPrincipalName"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := hc.Do(req)
	if err != nil {
		return "", errors.New("连接 Microsoft Graph 失败")
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("读取 Microsoft 账号身份失败")
	}
	var raw struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return "", errors.New("微软账号身份响应格式错误")
	}
	if strings.TrimSpace(raw.Mail) != "" {
		return strings.TrimSpace(raw.Mail), nil
	}
	if strings.TrimSpace(raw.UserPrincipalName) != "" {
		return strings.TrimSpace(raw.UserPrincipalName), nil
	}
	return "", errors.New("微软账号身份中没有邮箱地址")
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

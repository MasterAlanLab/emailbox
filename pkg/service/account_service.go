package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"emailbox/pkg/crypto"
	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

const (
	maxRemarkLen  = 500
	maxAliasCount = 20
	// importBatchSize 是导入时每个事务提交的行数。
	// 整批放一个事务会长时间持锁——SQLite 只有一个连接，会直接卡死其它请求。
	importBatchSize = 500
)

// AccountService 管理邮箱账号。凭据字段进出库都经过加解密，
// 且明文永不出现在列表/详情接口里。
type AccountService struct {
	store  *repo.Store
	cipher crypto.Cipher
	quota  *quota.Service
}

func NewAccountService(store *repo.Store, cipher crypto.Cipher, q *quota.Service) *AccountService {
	return &AccountService{store: store, cipher: cipher, quota: q}
}

// List 按筛选条件分页返回账号，并一次性带上别名与标签（避免 N+1）。
func (s *AccountService) List(ctx context.Context, tenantID string, filter model.AccountFilter) ([]model.MailAccountResponse, int, error) {
	accounts, err := s.store.ListMailAccountsPage(ctx, tenantID, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.store.CountMailAccountsFiltered(ctx, tenantID, filter)
	if err != nil {
		return nil, 0, err
	}
	responses, err := s.decorate(ctx, tenantID, accounts)
	return responses, total, err
}

// Get 返回单个账号的详情（凭据脱敏）。
func (s *AccountService) Get(ctx context.Context, tenantID, accountID string) (*model.MailAccountResponse, error) {
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	list, err := s.decorate(ctx, tenantID, []model.MailAccount{*account})
	if err != nil {
		return nil, err
	}
	return &list[0], nil
}

// decorate 把账号补齐成对外表示：凭据只回「有没有」，代理打码，
// 别名与标签用两条批量查询一次取回。
func (s *AccountService) decorate(ctx context.Context, tenantID string, accounts []model.MailAccount) ([]model.MailAccountResponse, error) {
	out := make([]model.MailAccountResponse, 0, len(accounts))
	if len(accounts) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	aliases, err := s.store.ListMailAliases(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		resp := model.MailAccountResponse{
			MailAccount:     a,
			HasPassword:     a.PasswordEnc != "",
			HasRefreshToken: a.RefreshTokenEnc != "",
			HasIMAPPassword: a.IMAPPasswordEnc != "",
			// 代理是加密存的，必须先解密再打码——直接对密文打码会把
			// "enc:v1:..." 原样显示到界面上。
			ProxyURLMasked:          s.maskStoredProxy(a.ProxyURL),
			FallbackProxyURL1Masked: s.maskStoredProxy(a.FallbackProxyURL1),
			FallbackProxyURL2Masked: s.maskStoredProxy(a.FallbackProxyURL2),
			Aliases:                 aliases[a.ID],
		}
		if resp.Aliases == nil {
			resp.Aliases = []string{}
		}
		// 密文绝不出接口：即便 json 标签已是 "-"，这里再清一次，
		// 避免将来有人复用这个结构体去做序列化时把密文带出去。
		resp.PasswordEnc, resp.RefreshTokenEnc, resp.IMAPPasswordEnc = "", "", ""
		resp.ProxyURL, resp.FallbackProxyURL1, resp.FallbackProxyURL2 = "", "", ""
		out = append(out, resp)
	}
	return out, nil
}

// maskStoredProxy 解密代理地址后打码。解密失败时不回显任何内容——
// 宁可界面上少一行，也不能把密文或残缺的口令暴露出去。
func (s *AccountService) maskStoredProxy(ciphertext string) string {
	if ciphertext == "" {
		return ""
	}
	plain, err := s.cipher.Decrypt(ciphertext)
	if err != nil {
		return "(无法解密)"
	}
	return MaskProxyURL(plain)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Create 新建单个账号。
func validateCreateAccount(req model.CreateMailAccountRequest) (email string, status model.AccountStatus, err error) {
	email = normalizeEmail(req.Email)
	if !strings.Contains(email, "@") {
		return "", "", errors.New("邮箱格式不正确")
	}
	if len(req.Remark) > maxRemarkLen {
		return "", "", fmt.Errorf("备注长度不能超过 %d 个字符", maxRemarkLen)
	}
	status = req.Status
	if status == "" {
		status = model.AccountStatusActive
	}
	if !model.ValidAccountStatus(status) {
		return "", "", errors.New("账号状态取值非法")
	}
	return email, status, nil
}

// ensureAccountQuota 校验新增 n 个账号是否会超出配额。
func (s *AccountService) ensureAccountQuota(ctx context.Context, tenantID string, n int) error {
	limits, err := s.quota.Effective(ctx, tenantID)
	if err != nil {
		return err
	}
	current, err := s.store.CountMailAccounts(ctx, tenantID)
	if err != nil {
		return err
	}
	return quota.CheckCount(limits.MaxAccounts, current, n, "邮箱账号")
}

// ensureEmailAvailable 确认该邮箱在本租户内尚未被占用。
func (s *AccountService) ensureEmailAvailable(ctx context.Context, tenantID, email string) error {
	_, err := s.store.GetMailAccountByEmail(ctx, tenantID, email)
	if err == nil {
		return fmt.Errorf("%w: 邮箱 %s 已存在", repo.ErrConflict, email)
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return err
	}
	return nil
}

func (s *AccountService) Create(ctx context.Context, tenantID string, req model.CreateMailAccountRequest) (*model.MailAccountResponse, error) {
	email, status, err := validateCreateAccount(req)
	if err != nil {
		return nil, err
	}
	group, err := s.resolveGroup(ctx, tenantID, req.GroupID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAccountQuota(ctx, tenantID, 1); err != nil {
		return nil, err
	}
	if err := s.ensureEmailAvailable(ctx, tenantID, email); err != nil {
		return nil, err
	}

	provider := mailer.ProviderByCode(req.Provider)
	if req.Provider == "" {
		provider = mailer.ProviderForEmail(email)
	}
	accountID := uuid.NewString()
	aliases, err := s.validateAliases(ctx, tenantID, accountID, email, req.Aliases)
	if err != nil {
		return nil, err
	}

	account := &model.MailAccount{
		ID: accountID, TenantID: tenantID, GroupID: group.ID,
		Email: strings.TrimSpace(req.Email), EmailNormalized: email,
		Provider: provider.Code, AccountType: string(provider.Type),
		ClientID: strings.TrimSpace(req.ClientID),
		IMAPHost: firstNonEmpty(strings.TrimSpace(req.IMAPHost), provider.IMAPHost),
		IMAPPort: firstNonZero(req.IMAPPort, provider.IMAPPort),
		Status:   status, Remark: strings.TrimSpace(req.Remark),
	}
	if req.AccountType != "" {
		account.AccountType = req.AccountType
	}
	if err := s.encryptInto(account, req.Password, req.RefreshToken, req.IMAPPassword,
		req.ProxyURL, req.FallbackProxyURL1, req.FallbackProxyURL2); err != nil {
		return nil, err
	}

	err = s.store.WithTx(ctx, func(tx *repo.Store) error {
		if e := tx.CreateMailAccount(ctx, account); e != nil {
			return e
		}
		return s.writeAliases(ctx, tx, tenantID, accountID, aliases)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, accountID)
}

// resolveGroup 校验分组归属；未指定时落到系统默认分组。
func (s *AccountService) resolveGroup(ctx context.Context, tenantID, groupID string) (*model.MailGroup, error) {
	if groupID == "" {
		return s.store.GetSystemMailGroup(ctx, tenantID)
	}
	return s.store.GetMailGroup(ctx, tenantID, groupID)
}

// encryptInto 把明文凭据加密后写进账号。
func (s *AccountService) encryptInto(a *model.MailAccount, password, refreshToken, imapPassword, proxy, fb1, fb2 string) error {
	fields := []struct {
		plaintext string
		target    *string
	}{
		{password, &a.PasswordEnc},
		{refreshToken, &a.RefreshTokenEnc},
		{imapPassword, &a.IMAPPasswordEnc},
		// 代理串里可能带认证口令，整串加密
		{proxy, &a.ProxyURL},
		{fb1, &a.FallbackProxyURL1},
		{fb2, &a.FallbackProxyURL2},
	}
	for _, f := range fields {
		enc, err := s.cipher.Encrypt(strings.TrimSpace(f.plaintext))
		if err != nil {
			return fmt.Errorf("加密凭据失败: %w", err)
		}
		*f.target = enc
	}
	return nil
}

// validateAliases 校验别名并返回规范化后的列表。
//
// 四种冲突都必须挡住，否则对外 API 按别名反查账号时会命中错误的账号
// ——那等于把 A 用户的邮件投递给 B：
//  1. 别名等于自己的主邮箱（冗余且会让反查产生歧义）
//  2. 别名与本租户内任一账号的主邮箱相同
//  3. 别名已被其它账号占用
//  4. 同一次请求里重复
func (s *AccountService) validateAliases(ctx context.Context, tenantID, accountID, ownEmail string, aliases []string) ([]string, error) {
	if len(aliases) > maxAliasCount {
		return nil, fmt.Errorf("单个账号最多 %d 个别名", maxAliasCount)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(aliases))
	for _, raw := range aliases {
		alias := normalizeEmail(raw)
		if alias == "" {
			continue
		}
		if !strings.Contains(alias, "@") {
			return nil, fmt.Errorf("别名 %q 不是合法邮箱", raw)
		}
		if alias == ownEmail {
			return nil, fmt.Errorf("别名 %s 不能与账号主邮箱相同", alias)
		}
		if seen[alias] {
			return nil, fmt.Errorf("别名 %s 重复", alias)
		}
		seen[alias] = true

		// 与其它账号的主邮箱冲突
		if other, err := s.store.GetMailAccountByEmail(ctx, tenantID, alias); err == nil {
			if other.ID != accountID {
				return nil, fmt.Errorf("别名 %s 已是账号 %s 的主邮箱", alias, other.Email)
			}
		} else if !errors.Is(err, repo.ErrNotFound) {
			return nil, err
		}
		out = append(out, alias)
	}
	// 与其它账号的别名冲突。放在最后做一次批量检查，避免逐个查询。
	if len(out) > 0 {
		n, err := s.store.CountConflictingAliases(ctx, tenantID, accountID, out)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, errors.New("其中有别名已被其它账号占用")
		}
	}
	return out, nil
}

func (s *AccountService) writeAliases(ctx context.Context, tx *repo.Store, tenantID, accountID string, aliases []string) error {
	if err := tx.DeleteMailAliasesByAccount(ctx, tenantID, accountID); err != nil {
		return err
	}
	for _, alias := range aliases {
		if err := tx.CreateMailAlias(ctx, uuid.NewString(), tenantID, accountID, alias, alias); err != nil {
			return err
		}
	}
	return nil
}

// Update 修改账号。凭据字段为 nil 表示保持原值——前端不回显密文，
// 把「没传」当成「清空」会导致用户改个备注就把 refresh_token 弄丢。
func (s *AccountService) Update(ctx context.Context, tenantID, accountID string, req model.UpdateMailAccountRequest) (*model.MailAccountResponse, error) {
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.applyScalarUpdates(ctx, tenantID, account, req); err != nil {
		return nil, err
	}
	if err := s.applySecretUpdates(account, req); err != nil {
		return nil, err
	}

	var aliases []string
	if req.Aliases != nil {
		aliases, err = s.validateAliases(ctx, tenantID, accountID, account.EmailNormalized, *req.Aliases)
		if err != nil {
			return nil, err
		}
	}

	err = s.store.WithTx(ctx, func(tx *repo.Store) error {
		if e := tx.UpdateMailAccount(ctx, account); e != nil {
			return e
		}
		if req.Aliases == nil {
			return nil
		}
		return s.writeAliases(ctx, tx, tenantID, accountID, aliases)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, accountID)
}

// applyScalarUpdates 应用非凭据字段。指针为 nil 表示该字段未提供，保持原值。
func (s *AccountService) applyScalarUpdates(ctx context.Context, tenantID string, account *model.MailAccount, req model.UpdateMailAccountRequest) error {
	if req.GroupID != nil {
		group, err := s.resolveGroup(ctx, tenantID, *req.GroupID)
		if err != nil {
			return err
		}
		account.GroupID = group.ID
	}
	if req.Provider != nil {
		account.Provider = mailer.ProviderByCode(*req.Provider).Code
	}
	if req.Status != nil {
		if !model.ValidAccountStatus(*req.Status) {
			return errors.New("账号状态取值非法")
		}
		account.Status = *req.Status
	}
	if req.Remark != nil {
		if len(*req.Remark) > maxRemarkLen {
			return fmt.Errorf("备注长度不能超过 %d 个字符", maxRemarkLen)
		}
		account.Remark = strings.TrimSpace(*req.Remark)
	}
	if req.ClientID != nil {
		account.ClientID = strings.TrimSpace(*req.ClientID)
	}
	if req.IMAPHost != nil {
		account.IMAPHost = strings.TrimSpace(*req.IMAPHost)
	}
	if req.IMAPPort != nil {
		account.IMAPPort = *req.IMAPPort
	}
	return nil
}

// applySecretUpdates 应用凭据与代理字段。
func (s *AccountService) applySecretUpdates(account *model.MailAccount, req model.UpdateMailAccountRequest) error {
	targets := []struct {
		field     *string
		plaintext *string
	}{
		{&account.PasswordEnc, req.Password},
		{&account.RefreshTokenEnc, req.RefreshToken},
		{&account.IMAPPasswordEnc, req.IMAPPassword},
		{&account.ProxyURL, req.ProxyURL},
		{&account.FallbackProxyURL1, req.FallbackProxyURL1},
		{&account.FallbackProxyURL2, req.FallbackProxyURL2},
	}
	for _, t := range targets {
		if err := s.updateSecret(t.field, t.plaintext); err != nil {
			return err
		}
	}
	return nil
}

// updateSecret 把可选的明文凭据加密写入目标字段；nil 表示保持原值。
func (s *AccountService) updateSecret(target *string, plaintext *string) error {
	if plaintext == nil {
		return nil
	}
	enc, err := s.cipher.Encrypt(strings.TrimSpace(*plaintext))
	if err != nil {
		return fmt.Errorf("加密凭据失败: %w", err)
	}
	*target = enc
	return nil
}

// Delete 软删除账号。凭据密文由 SQL 在同一条语句里清空。
func (s *AccountService) Delete(ctx context.Context, tenantID, accountID string) error {
	return s.store.SoftDeleteMailAccount(ctx, tenantID, accountID)
}

// Credentials 是解密后的凭据，只在内存里流转，供协议层使用。
type Credentials struct {
	Email             string
	Password          string
	ClientID          string
	RefreshToken      string
	IMAPHost          string
	IMAPPort          int
	IMAPPassword      string
	ProxyURL          string
	FallbackProxyURL1 string
	FallbackProxyURL2 string
}

// Credentials 解密账号凭据。调用方只有协议层与导出接口，
// 且导出接口另需 account:secret 权限与审计。
func (s *AccountService) Credentials(ctx context.Context, tenantID, accountID string) (*Credentials, error) {
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	return s.decryptCredentials(account)
}

func (s *AccountService) decryptCredentials(a *model.MailAccount) (*Credentials, error) {
	c := &Credentials{
		Email: a.Email, ClientID: a.ClientID,
		IMAPHost: a.IMAPHost, IMAPPort: a.IMAPPort,
	}
	fields := []struct {
		ciphertext string
		target     *string
		label      string
	}{
		{a.PasswordEnc, &c.Password, "密码"},
		{a.RefreshTokenEnc, &c.RefreshToken, "refresh_token"},
		{a.IMAPPasswordEnc, &c.IMAPPassword, "IMAP 密码"},
		{a.ProxyURL, &c.ProxyURL, "代理地址"},
		{a.FallbackProxyURL1, &c.FallbackProxyURL1, "备用代理 1"},
		{a.FallbackProxyURL2, &c.FallbackProxyURL2, "备用代理 2"},
	}
	for _, f := range fields {
		plain, err := s.cipher.Decrypt(f.ciphertext)
		if err != nil {
			// 解密失败必须报错而不是返回空串：静默失败会让批量刷新
			// 把上万个账号误标为「令牌无效」。
			return nil, fmt.Errorf("解密%s失败: %w", f.label, err)
		}
		*f.target = plain
	}
	return c, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

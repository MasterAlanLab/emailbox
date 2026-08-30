package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"emailbox/pkg/crypto"
	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
)

// 邮件相关的业务错误。handler 层据此映射 HTTP 状态码。
var (
	ErrInvalidFolder = errors.New("不支持的邮件夹")
	// ErrAccountBanned 是已知被封的账号。已经封了还继续打上游只会加重风控，
	// 所以在进协议层之前就挡下。
	ErrAccountBanned   = errors.New("账号已被服务商封禁")
	ErrAccountDisabled = errors.New("账号已被停用")
	// ErrCredentialUndecryptable 通常意味着 ENCRYPTION_KEY 换过了。
	// 必须与「密码错误」区分开：报成密码错误的话用户会去改密码，越改越乱。
	ErrCredentialUndecryptable = errors.New("凭据无法解密，请检查 ENCRYPTION_KEY 是否变更")
)

// maxTop 是单次拉取的上限。再大对用户没有意义，却会显著拉长远端调用时间，
// 也更容易撞上服务商限流。
const maxTop = 50

// MessageService 把「从库里取账号 → 解密 → 调协议层 → 写回结果」串起来。
//
// mailer 包不碰数据库，这一层就是它与业务之间唯一的接缝。
type MessageService struct {
	store        *repo.Store
	cipher       crypto.Cipher
	quota        *quota.Service
	chainOptions ChainOptions
	// chainFor 按账号构造回退链。做成字段是为了让测试注入假通道——
	// 真实实现要连微软，单测里跑不了。
	chainFor func(account *model.MailAccount) mailer.Client
}

func NewMessageService(
	store *repo.Store, cipher crypto.Cipher, q *quota.Service, opt ChainOptions,
) *MessageService {
	s := &MessageService{store: store, cipher: cipher, quota: q, chainOptions: opt}
	s.chainFor = defaultChainFactory(s, opt)
	return s
}

// WithChainFactory 换掉回退链的构造方式，只给测试用：
// 真实的链要连微软与各家 IMAP，单测里跑不了。
func (s *MessageService) WithChainFactory(f func(*model.MailAccount) mailer.Client) *MessageService {
	s.chainFor = f
	return s
}

// MessageListResult 是列表接口的响应体。
type MessageListResult struct {
	Items []mailer.Message `json:"items"`
	// Channel 是本次实际走通的通道，前端用它显示「通过 Graph / IMAP 获取」。
	Channel string `json:"channel"`
}

// List 拉取邮件列表。
//
// folder=all 在这里拆成收件箱 + 垃圾箱两次调用后归并——协议层不认识 all，
// 让它认识的话每个通道都要实现一遍归并逻辑。
func (s *MessageService) List(
	ctx context.Context, tenantID, accountID string, opt mailer.ListOptions,
) (*MessageListResult, error) {
	if opt.Top <= 0 || opt.Top > maxTop {
		opt.Top = min(max(opt.Top, 1), maxTop)
	}
	if opt.Skip < 0 {
		opt.Skip = 0
	}
	if !mailer.ValidFolder(opt.Folder) {
		return nil, ErrInvalidFolder
	}

	account, cred, err := s.credential(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	// 配额在走远端之前扣：扣完才发请求，超额时一个远端调用都不产生。
	if err := s.quota.CheckAndConsume(ctx, tenantID, model.MetricMailFetch, 1); err != nil {
		return nil, err
	}

	client, record := s.clientFor(ctx, tenantID, account)
	if opt.Folder == mailer.FolderAll {
		return s.listAll(ctx, client, record, cred, opt)
	}

	messages, err := client.List(ctx, cred, opt)
	record(err)
	if err != nil {
		return nil, err
	}
	return &MessageListResult{
		Items:   nonNil(messages),
		Channel: channelOf(err, account),
	}, nil
}

// listAll 聚合收件箱与垃圾箱，按时间倒序取前 top 条。
//
// 两个邮件夹各取 skip+top 条再归并：只取 top 条的话，
// 当一个邮件夹的邮件明显更新时，翻到第二页会漏掉另一个邮件夹的邮件。
func (s *MessageService) listAll(
	ctx context.Context, client mailer.Client, record func(error),
	cred mailer.Credential, opt mailer.ListOptions,
) (*MessageListResult, error) {
	merged := make([]mailer.Message, 0, opt.Top*2)
	var firstErr error
	for _, folder := range []mailer.Folder{mailer.FolderInbox, mailer.FolderJunk} {
		part, err := client.List(ctx, cred, mailer.ListOptions{
			Folder: folder,
			Skip:   0,
			Top:    opt.Skip + opt.Top,
		})
		if err != nil {
			// 一个邮件夹取不到不该让整个请求失败：多数账号的垃圾箱是可有可无的，
			// 而收件箱拿到了就该展示出来。全都失败时才报错。
			if firstErr == nil {
				firstErr = err
			}
			slog.Debug("folder=all 的某个邮件夹拉取失败",
				"folder", folder, "email", mailer.MaskEmail(cred.Email), "error", err)
			continue
		}
		merged = append(merged, part...)
	}
	if len(merged) == 0 && firstErr != nil {
		record(firstErr)
		return nil, firstErr
	}
	record(nil)

	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].ReceivedAt.After(merged[j].ReceivedAt)
	})
	if opt.Skip < len(merged) {
		merged = merged[opt.Skip:]
	} else {
		merged = nil
	}
	if len(merged) > opt.Top {
		merged = merged[:opt.Top]
	}
	return &MessageListResult{Items: nonNil(merged)}, nil
}

// Detail 取一封邮件的详情。
func (s *MessageService) Detail(
	ctx context.Context, tenantID, accountID string, folder mailer.Folder, id, idMode string,
) (*mailer.Detail, error) {
	if folder == mailer.FolderAll || !mailer.ValidFolder(folder) {
		return nil, ErrInvalidFolder
	}
	account, cred, err := s.credential(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if err := s.quota.CheckAndConsume(ctx, tenantID, model.MetricMailFetch, 1); err != nil {
		return nil, err
	}
	client, record := s.clientFor(ctx, tenantID, account)
	detail, err := client.Detail(ctx, cred, folder, id, idMode)
	record(err)
	// 没有附件时协议层返回的是 nil slice，encoding/json 会把它写成 `attachments: null`，
	// 而前端的类型声明是非空数组——于是「打开一封没有附件的邮件」会在
	// AttachmentList 里 null.filter 崩掉整棵 React 树。绝大多数邮件都没有附件，
	// 所以这条路径几乎是必经的。在这里补齐，两个协议实现就都不必各自记得。
	if detail != nil && detail.Attachments == nil {
		detail.Attachments = []mailer.AttachmentMeta{}
	}
	return detail, err
}

// Attachment 下载单个附件。
func (s *MessageService) Attachment(
	ctx context.Context, tenantID, accountID string,
	folder mailer.Folder, msgID, idMode, attID string,
) (*mailer.Attachment, error) {
	if folder == mailer.FolderAll || !mailer.ValidFolder(folder) {
		return nil, ErrInvalidFolder
	}
	account, cred, err := s.credential(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	client, record := s.clientFor(ctx, tenantID, account)
	att, err := client.Attachment(ctx, cred, folder, msgID, idMode, attID)
	record(err)
	return att, err
}

// MarkRead 批量标已读。
func (s *MessageService) MarkRead(
	ctx context.Context, tenantID, accountID string, items []mailer.MessageRef,
) (mailer.BatchResult, error) {
	return s.batch(ctx, tenantID, accountID, items,
		func(c mailer.Client, cred mailer.Credential, refs []mailer.MessageRef) (mailer.BatchResult, error) {
			return c.MarkRead(ctx, cred, refs)
		})
}

// Delete 批量删除。
func (s *MessageService) Delete(
	ctx context.Context, tenantID, accountID string, items []mailer.MessageRef,
) (mailer.BatchResult, error) {
	return s.batch(ctx, tenantID, accountID, items,
		func(c mailer.Client, cred mailer.Credential, refs []mailer.MessageRef) (mailer.BatchResult, error) {
			return c.Delete(ctx, cred, refs)
		})
}

func (s *MessageService) batch(
	ctx context.Context, tenantID, accountID string, items []mailer.MessageRef,
	run func(mailer.Client, mailer.Credential, []mailer.MessageRef) (mailer.BatchResult, error),
) (mailer.BatchResult, error) {
	if len(items) == 0 {
		return mailer.BatchResult{}, nil
	}
	for _, item := range items {
		if item.Folder == mailer.FolderAll || !mailer.ValidFolder(item.Folder) {
			return mailer.BatchResult{}, ErrInvalidFolder
		}
	}
	account, cred, err := s.credential(ctx, tenantID, accountID)
	if err != nil {
		return mailer.BatchResult{}, err
	}
	client, record := s.clientFor(ctx, tenantID, account)
	result, err := run(client, cred, items)
	record(err)
	return result, err
}

// credential 取出账号并解密成协议层要的 Credential。
//
// 明文凭据只在这里到 mailer 之间的内存里存在，绝不进日志、不进响应体。
func (s *MessageService) credential(
	ctx context.Context, tenantID, accountID string,
) (*model.MailAccount, mailer.Credential, error) {
	account, err := s.store.GetMailAccount(ctx, tenantID, accountID)
	if err != nil {
		return nil, mailer.Credential{}, err
	}
	if account.Status == model.AccountStatusBanned {
		// 已知被封的账号不该再打上游：每试一次都在加重风控。
		return nil, mailer.Credential{}, ErrAccountBanned
	}
	if account.Status == model.AccountStatusDisabled {
		return nil, mailer.Credential{}, ErrAccountDisabled
	}

	cred := mailer.Credential{
		Email:       account.Email,
		Provider:    account.Provider,
		AccountType: mailer.AccountType(account.AccountType),
		ClientID:    account.ClientID,
		IMAPHost:    account.IMAPHost,
		IMAPPort:    account.IMAPPort,
		AuthChannel: account.AuthChannel,
	}
	if s.chainOptions.OAuthClientSecret != "" && account.ClientID == s.chainOptions.OAuthClientID {
		cred.ClientSecret = s.chainOptions.OAuthClientSecret
	}
	for _, field := range []struct {
		enc string
		dst *string
	}{
		{account.PasswordEnc, &cred.Password},
		{account.RefreshTokenEnc, &cred.RefreshToken},
		{account.IMAPPasswordEnc, &cred.IMAPPassword},
	} {
		if field.enc == "" {
			continue
		}
		plain, err := s.cipher.Decrypt(field.enc)
		if err != nil {
			// 解密失败通常意味着 ENCRYPTION_KEY 换过了。必须报错而不是当成空值，
			// 否则表现成「密码错误」，用户会去改密码，越改越乱。
			return nil, mailer.Credential{}, ErrCredentialUndecryptable
		}
		*field.dst = plain
	}

	proxy, err := s.resolveProxy(ctx, tenantID, account)
	if err != nil {
		return nil, mailer.Credential{}, err
	}
	cred.Proxy = proxy
	return account, cred, nil
}

// resolveProxy 按「账号 → 分组」取生效的代理配置。
func (s *MessageService) resolveProxy(
	ctx context.Context, tenantID string, account *model.MailAccount,
) (mailer.ProxyConfig, error) {
	accountProxy, err := s.decryptProxy(mailer.ProxyConfig{
		URL:       account.ProxyURL,
		Fallback1: account.FallbackProxyURL1,
		Fallback2: account.FallbackProxyURL2,
	})
	if err != nil {
		return mailer.ProxyConfig{}, err
	}
	// 账号自己配了就整组用它的，不必再查分组。
	if strings.TrimSpace(accountProxy.URL) != "" {
		return accountProxy, nil
	}

	// 分组取不到就按「这个账号没有分组代理」处理：账号本身的配置已经在上面
	// 判过了，为了一次代理查询失败就让整封信取不回来并不划算。
	group, err := s.store.GetMailGroup(ctx, tenantID, account.GroupID)
	if err != nil {
		return mailer.ResolveProxy(accountProxy, nil), nil
	}
	groupProxy, err := s.decryptProxy(mailer.ProxyConfig{
		URL:       group.ProxyURL,
		Fallback1: group.FallbackProxyURL1,
		Fallback2: group.FallbackProxyURL2,
	})
	if err != nil {
		return mailer.ProxyConfig{}, err
	}
	return mailer.ResolveProxy(accountProxy, []mailer.ProxyConfig{groupProxy}), nil
}

// decryptProxy 解密代理三列。代理串常带认证口令，所以是加密存的。
func (s *MessageService) decryptProxy(cfg mailer.ProxyConfig) (mailer.ProxyConfig, error) {
	out := cfg
	for _, field := range []*string{&out.URL, &out.Fallback1, &out.Fallback2} {
		if *field == "" {
			continue
		}
		plain, err := s.cipher.Decrypt(*field)
		if err != nil {
			return mailer.ProxyConfig{}, ErrCredentialUndecryptable
		}
		*field = plain
	}
	return out, nil
}

// clientFor 返回该账号的回退链，以及一个记录本次访问结果的回调。
func (s *MessageService) clientFor(
	ctx context.Context, tenantID string, account *model.MailAccount,
) (mailer.Client, func(error)) {
	client := s.chainFor(account)
	record := func(callErr error) {
		s.recordResult(ctx, tenantID, account, callErr)
	}
	return client, record
}

// recordResult 写回本次访问的成败。
//
// 写回失败只记日志：拉信本身已经成功了，不能因为记账失败就把结果丢掉。
func (s *MessageService) recordResult(
	ctx context.Context, tenantID string, account *model.MailAccount, callErr error,
) {
	status, message, errorKind := string(model.RefreshSuccess), "", ""
	if callErr != nil {
		status = string(model.RefreshFailed)
		message = truncateError(callErr.Error())
		errorKind = string(mailer.KindOf(callErr))
	}
	if err := s.store.UpdateMailAccountRefreshResult(ctx, tenantID, account.ID, status, message, errorKind); err != nil {
		slog.Warn("写回账号访问结果失败", "account_id", account.ID, "error", err)
	}

	// 账号被封时立刻改状态，后续请求在 credential 里就会被挡下，不再打上游。
	if callErr != nil && mailer.KindOf(callErr) == mailer.ErrKindBanned {
		if _, err := s.store.BatchUpdateMailAccountStatus(ctxDetached(ctx), tenantID,
			string(model.AccountStatusBanned), []string{account.ID}); err != nil {
			slog.Warn("标记封禁账号失败", "account_id", account.ID, "error", err)
		}
	}
}

// OnChannelSuccess 供回退链的 OnSuccess 回调使用：把成功通道写回账号。
func (s *MessageService) OnChannelSuccess(
	ctx context.Context, tenantID, accountID, previousChannel, channel string,
) {
	if channel == "" || channel == previousChannel {
		return
	}
	if err := s.store.UpdateMailAccountAuthChannel(ctx, tenantID, accountID, channel); err != nil {
		slog.Warn("写回成功通道失败", "account_id", accountID, "error", err)
	}
}

// OnTokenRotated 供协议层的轮换回调使用：加密后持久化新的 refresh_token。
func (s *MessageService) OnTokenRotated(ctx context.Context, tenantID, accountID, refreshToken string) {
	enc, err := s.cipher.Encrypt(refreshToken)
	if err != nil {
		slog.Error("加密轮换后的 refresh_token 失败", "account_id", accountID, "error", err)
		return
	}
	if err := s.store.UpdateMailAccountRefreshToken(ctx, tenantID, accountID, enc); err != nil {
		slog.Error("持久化轮换后的 refresh_token 失败", "account_id", accountID, "error", err)
	}
}

func channelOf(err error, account *model.MailAccount) string {
	if err != nil {
		return ""
	}
	return account.AuthChannel
}

func nonNil(items []mailer.Message) []mailer.Message {
	if items == nil {
		return []mailer.Message{}
	}
	return items
}

const maxRecordedErrorLen = 500

func truncateError(s string) string {
	if len(s) <= maxRecordedErrorLen {
		return s
	}
	return s[:maxRecordedErrorLen]
}

// ctxDetached 在原 context 已取消时换一个干净的，
// 用于「必须落库」的收尾写入（比如标记封禁）。
func ctxDetached(ctx context.Context) context.Context {
	if ctx.Err() == nil {
		return ctx
	}
	return context.WithoutCancel(ctx)
}

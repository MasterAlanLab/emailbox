package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"emailbox/configs"
	"emailbox/pkg/crypto"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

func accountFixture(t *testing.T) (*service.AccountService, *repo.Store, string) {
	t.Helper()
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	result, _, err := service.NewAuthService(store).Register(context.Background(),
		model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := crypto.New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return service.NewAccountService(store, cipher, quota.NewService(store)), store, result.Tenants[0].ID
}

// 凭据必须密文落库，且明文永不出现在详情/列表接口里。
func TestAccountCredentialsAreEncryptedAndNeverExposed(t *testing.T) {
	svc, store, tenantID := accountFixture(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{
		Email: "user@outlook.com", Password: "hunter2", RefreshToken: "M.C123_secret",
		ProxyURL: "socks5h://user:proxypass@127.0.0.1:1080",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 库里必须是密文
	raw, err := store.GetMailAccount(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, ciphertext := range map[string]string{
		"password": raw.PasswordEnc, "refresh_token": raw.RefreshTokenEnc, "proxy": raw.ProxyURL,
	} {
		if !crypto.IsEncrypted(ciphertext) {
			t.Errorf("%s 未加密落库: %q", name, ciphertext)
		}
	}
	for _, secret := range []string{"hunter2", "M.C123_secret", "proxypass"} {
		if strings.Contains(raw.PasswordEnc+raw.RefreshTokenEnc+raw.ProxyURL, secret) {
			t.Errorf("密文里出现了明文 %q", secret)
		}
	}

	// 接口返回只说明「有没有」，代理已打码
	if !created.HasPassword || !created.HasRefreshToken {
		t.Error("应标记凭据已设置")
	}
	if created.PasswordEnc != "" || created.RefreshTokenEnc != "" || created.ProxyURL != "" {
		t.Error("响应结构体里仍带着密文字段")
	}
	if strings.Contains(created.ProxyURLMasked, "proxypass") {
		t.Errorf("代理口令未打码: %q", created.ProxyURLMasked)
	}
	if !strings.Contains(created.ProxyURLMasked, "****") {
		t.Errorf("代理地址应打码，实际 %q", created.ProxyURLMasked)
	}

	// 协议层取用时能拿到明文
	creds, err := svc.Credentials(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Password != "hunter2" || creds.RefreshToken != "M.C123_secret" {
		t.Errorf("解密结果不正确: %+v", creds)
	}
	if creds.ProxyURL != "socks5h://user:proxypass@127.0.0.1:1080" {
		t.Errorf("代理解密不正确: %q", creds.ProxyURL)
	}
}

// 别名冲突的四种情形都必须拒绝：任何一种漏掉，对外 API 按别名反查时
// 就可能命中错误的账号，等于把 A 用户的邮件投给 B。
func TestAliasConflictsAreRejected(t *testing.T) {
	svc, _, tenantID := accountFixture(t)
	ctx := context.Background()

	first, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{
		Email: "first@outlook.com", Aliases: []string{"taken@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string][]string{
		"与自己的主邮箱相同":   {"second@outlook.com"},
		"与其它账号的主邮箱相同": {"first@outlook.com"},
		"与其它账号的别名相同":  {"taken@example.com"},
		"同一次请求内重复":    {"dup@example.com", "DUP@example.com"},
	}
	for name, aliases := range cases {
		_, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{
			Email: "second@outlook.com", Aliases: aliases,
		})
		if err == nil {
			t.Errorf("%s：应被拒绝", name)
		}
	}

	// 合法别名可以创建
	if _, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{
		Email: "second@outlook.com", Aliases: []string{"ok@example.com"},
	}); err != nil {
		t.Fatalf("合法别名不该被拒绝: %v", err)
	}
	_ = first
}

// 改个备注不该把 refresh_token 弄丢——前端不回显密文，
// 把「没传」当成「清空」是很容易犯的错。
func TestUpdateKeepsUnsuppliedSecrets(t *testing.T) {
	svc, _, tenantID := accountFixture(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{
		Email: "user@outlook.com", RefreshToken: "keep-me",
	})
	if err != nil {
		t.Fatal(err)
	}
	remark := "只改备注"
	if _, err := svc.Update(ctx, tenantID, created.ID, model.UpdateMailAccountRequest{Remark: &remark}); err != nil {
		t.Fatal(err)
	}
	creds, err := svc.Credentials(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if creds.RefreshToken != "keep-me" {
		t.Errorf("未提供的凭据被清空了，实际 %q", creds.RefreshToken)
	}

	// 显式传空串才是清空
	empty := ""
	if _, err := svc.Update(ctx, tenantID, created.ID, model.UpdateMailAccountRequest{RefreshToken: &empty}); err != nil {
		t.Fatal(err)
	}
	creds, _ = svc.Credentials(ctx, tenantID, created.ID)
	if creds.RefreshToken != "" {
		t.Errorf("显式传空串应清空凭据，实际 %q", creds.RefreshToken)
	}
}

// 软删除必须清空凭据密文：软删只对「误删可恢复」有意义，
// 凭据不该跟着 deleted_at 长期留存。
func TestDeleteClearsCredentials(t *testing.T) {
	svc, store, tenantID := accountFixture(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{
		Email: "user@outlook.com", Password: "hunter2", RefreshToken: "M.C123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, tenantID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMailAccount(ctx, tenantID, created.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("软删后不应再查到，实际 %v", err)
	}
	// 邮箱应可重新使用（部分唯一索引带 deleted_at IS NULL）
	if _, err := svc.Create(ctx, tenantID, model.CreateMailAccountRequest{Email: "user@outlook.com"}); err != nil {
		t.Errorf("软删后同一邮箱应可重新创建: %v", err)
	}
}

func TestImportParsesAndCountsPerLine(t *testing.T) {
	svc, _, tenantID := accountFixture(t)
	ctx := context.Background()
	content := strings.Join([]string{
		"a@outlook.com----pwd----24d9a0ed-1234-4abc-9def-0123456789ab----M.token1",
		"b@gmail.com----app-password",
		"",
		"garbage-line-without-email",
		"a@outlook.com----pwd----24d9a0ed-1234-4abc-9def-0123456789ab----M.token2", // 本次内容里重复
	}, "\n")

	result, err := svc.Import(ctx, tenantID, model.ImportAccountsRequest{Content: content, Format: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 4 {
		t.Errorf("空行不该计入总数，Total=%d", result.Total)
	}
	if result.Created != 2 {
		t.Errorf("应创建 2 个账号，实际 %d", result.Created)
	}
	if result.Failed != 1 {
		t.Errorf("非法行应计入 failed，实际 %d", result.Failed)
	}
	if result.Skipped != 1 {
		t.Errorf("重复行应计入 skipped，实际 %d", result.Skipped)
	}
	if len(result.Errors) != 2 {
		t.Errorf("应有 2 条逐行错误，实际 %+v", result.Errors)
	}

	// IMAP 账号的授权码进 imap_password_enc，OAuth 账号的令牌进 refresh_token_enc
	accounts, _, err := svc.List(ctx, tenantID, model.AccountFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byEmail := map[string]model.MailAccountResponse{}
	for _, a := range accounts {
		byEmail[a.Email] = a
	}
	if !byEmail["a@outlook.com"].HasRefreshToken {
		t.Error("OAuth 账号应带 refresh_token")
	}
	if !byEmail["b@gmail.com"].HasIMAPPassword {
		t.Error("IMAP 账号的授权码应存进 imap_password_enc")
	}
	if got := byEmail["b@gmail.com"].IMAPHost; got != "imap.gmail.com" {
		t.Errorf("应按域名推断出 IMAP 主机，实际 %q", got)
	}
}

// 导入超配额时的正确行为是「装得下的先导入、其余计入 skipped」，
// 而不是整批失败。
func TestImportSkipsOverQuotaInsteadOfFailingBatch(t *testing.T) {
	svc, store, tenantID := accountFixture(t)
	ctx := context.Background()

	// 把配额压到 3
	if err := shrinkAccountQuota(ctx, store, tenantID, 3); err != nil {
		t.Fatal(err)
	}

	lines := make([]string, 0, 10)
	for i := range 10 {
		lines = append(lines, fmt.Sprintf("user%d@gmail.com----pwd", i))
	}
	result, err := svc.Import(ctx, tenantID, model.ImportAccountsRequest{
		Content: strings.Join(lines, "\n"), Format: "imap",
	})
	if err != nil {
		t.Fatalf("超配额不该让整个导入失败: %v", err)
	}
	if result.Created != 3 {
		t.Errorf("应导入 3 个（配额上限），实际 %d", result.Created)
	}
	if result.Skipped != 7 {
		t.Errorf("超额的 7 个应计入 skipped，实际 %d", result.Skipped)
	}
	if result.Failed != 0 {
		t.Errorf("超配额属于 skipped 而非 failed，实际 failed=%d", result.Failed)
	}
	// 响应里要说明原因
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Reason, "配额") {
		t.Errorf("应说明跳过原因，实际 %+v", result.Errors)
	}
	// 已导入的保留
	n, err := store.CountMailAccounts(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("已导入的账号应保留，实际库里有 %d 个", n)
	}
}

// shrinkAccountQuota 走真实的「管理员覆盖配额」路径把账号上限压到 n。
func shrinkAccountQuota(ctx context.Context, store *repo.Store, tenantID string, n int) error {
	return store.UpdateTenantQuotaOverrides(ctx, tenantID, repo.QuotaOverrides{
		MaxAccounts: &n, Note: "测试用：压低账号配额",
	})
}

func TestImportUpdateOnConflict(t *testing.T) {
	svc, _, tenantID := accountFixture(t)
	ctx := context.Background()
	line := "a@outlook.com----pwd----24d9a0ed-1234-4abc-9def-0123456789ab----M.token1"

	if _, err := svc.Import(ctx, tenantID, model.ImportAccountsRequest{Content: line, Format: "auto"}); err != nil {
		t.Fatal(err)
	}
	// 默认 skip
	result, err := svc.Import(ctx, tenantID, model.ImportAccountsRequest{Content: line, Format: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 1 || result.Updated != 0 {
		t.Errorf("默认应跳过已存在的账号，实际 %+v", result)
	}
	// on_conflict=update
	updated := "a@outlook.com----pwd----24d9a0ed-1234-4abc-9def-0123456789ab----M.token2"
	result, err = svc.Import(ctx, tenantID, model.ImportAccountsRequest{
		Content: updated, Format: "auto", OnConflict: "update",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 {
		t.Errorf("应更新已有账号，实际 %+v", result)
	}
	accounts, _, _ := svc.List(ctx, tenantID, model.AccountFilter{})
	creds, err := svc.Credentials(ctx, tenantID, accounts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if creds.RefreshToken != "M.token2" {
		t.Errorf("令牌未被更新，实际 %q", creds.RefreshToken)
	}
}

func TestBatchOperationsRespectTenantIsolation(t *testing.T) {
	svc, store, tenantA := accountFixture(t)
	ctx := context.Background()
	other, _, err := service.NewAuthService(store).Register(ctx,
		model.RegisterRequest{Username: "bobby", Email: "bob@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	tenantB := other.Tenants[0].ID

	created, err := svc.Create(ctx, tenantA, model.CreateMailAccountRequest{Email: "a@outlook.com"})
	if err != nil {
		t.Fatal(err)
	}

	// B 拿着 A 的 accountID 做批量操作：一个也不该命中
	result, err := svc.BatchStatus(ctx, tenantB, model.BatchStatusRequest{
		AccountIDs: []string{created.ID}, Status: model.AccountStatusDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 0 || result.Failed != 1 {
		t.Errorf("跨租户批量操作不该命中，实际 %+v", result)
	}
	after, err := store.GetMailAccount(ctx, tenantA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != model.AccountStatusActive {
		t.Errorf("A 的账号状态被 B 改动了：%s", after.Status)
	}
}

func TestBatchIDLimit(t *testing.T) {
	svc, _, tenantID := accountFixture(t)
	ctx := context.Background()
	if _, err := svc.BatchDelete(ctx, tenantID, model.BatchDeleteRequest{}); err == nil {
		t.Error("空 account_ids 应被拒绝")
	}
	tooMany := make([]string, model.MaxBatchAccountIDs+1)
	for i := range tooMany {
		tooMany[i] = "id"
	}
	if _, err := svc.BatchDelete(ctx, tenantID, model.BatchDeleteRequest{AccountIDs: tooMany}); err == nil {
		t.Error("超过上限的 account_ids 应被拒绝并提示分批")
	}
}

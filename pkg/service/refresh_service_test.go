package service

import (
	"context"
	"database/sql"
	"testing"

	"emailbox/db/migrations"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"

	_ "modernc.org/sqlite"
)

// collectAccounts 必须完整翻页；用 5001 个账号守住旧的 5000 截断边界，
// 但不启动 worker，避免把一个分页测试变成数千次任务写入。
func TestCollectAccountsDoesNotTruncateLargeGroup(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/accounts.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Up(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	store := repo.NewStore(db, "sqlite")
	ctx := context.Background()
	if err := store.CreateUser(ctx, &model.User{
		ID: "user", Username: "user", Email: "user@example.com", PasswordHash: "hash",
		Status: model.UserStatusActive, PlatformRole: model.PlatformRoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTenant(ctx, &model.Tenant{
		ID: "tenant", Name: "Tenant", Slug: "tenant", CreatedBy: "user", Kind: model.TenantKindPersonal,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMailGroup(ctx, &model.MailGroup{
		ID: "group", TenantID: "tenant", Name: "large", Color: model.GroupColorGray,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`WITH RECURSIVE seq(n) AS (
		SELECT 0 UNION ALL SELECT n + 1 FROM seq WHERE n < 5000
	)
	INSERT INTO mail_accounts
		(id, tenant_id, group_id, email, email_normalized, provider, account_type, refresh_token_enc, status)
	SELECT printf('account-%05d', n), 'tenant', 'group', printf('large%05d@outlook.com', n),
		printf('large%05d@outlook.com', n), 'outlook', 'outlook', 'refresh-token', 'active'
	FROM seq`)
	if err != nil {
		t.Fatal(err)
	}

	accounts, err := collectAccounts(ctx, store, "tenant", model.AccountFilter{GroupIDs: []string{"group"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 5001 {
		t.Fatalf("收集到 %d 个账号，期望 5001 个", len(accounts))
	}
}

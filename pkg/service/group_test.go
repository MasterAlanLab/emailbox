package service_test

import (
	"context"
	"strings"
	"testing"

	"emailbox/configs"
	"emailbox/pkg/crypto"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

// groupFixture 注册一个用户并返回其个人工作空间的 GroupService 上下文。
func groupFixture(t *testing.T) (*service.GroupService, *repo.Store, string) {
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
	return service.NewGroupService(store, cipher, quota.NewService(store)), store, result.Tenants[0].ID
}

// 注册事务必须建出默认分组：没有它，删除任何分组都会因为账号无处回落而失败。
func TestRegisterCreatesSystemGroup(t *testing.T) {
	_, store, tenantID := groupFixture(t)
	group, err := store.GetSystemMailGroup(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("默认分组不存在: %v", err)
	}
	if !group.IsSystem {
		t.Errorf("默认分组应是系统分组，实际 %+v", group)
	}
}

// 删除分组时账号必须回落到默认分组：mail_accounts.group_id 是 NOT NULL 外键，
// 少了这一步，删分组要么被外键拒绝，要么把账号一起带走。
func TestGroupDeleteMovesAccountsToSystemGroup(t *testing.T) {
	svc, store, tenantID := groupFixture(t)
	ctx := context.Background()
	group, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "客户 A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateMailAccount(ctx, &model.MailAccount{
		ID: "acct-1", TenantID: tenantID, GroupID: group.ID,
		Email: "a@x.com", EmailNormalized: "a@x.com",
		Provider: "outlook", AccountType: "outlook",
		Status: model.AccountStatusActive, IMAPPort: 993,
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, tenantID, group.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMailGroup(ctx, tenantID, group.ID); err == nil {
		t.Error("分组未被删除")
	}
	account, err := store.GetMailAccount(ctx, tenantID, "acct-1")
	if err != nil {
		t.Fatalf("账号不应随分组消失: %v", err)
	}
	system, err := store.GetSystemMailGroup(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if account.GroupID != system.ID {
		t.Errorf("账号应回落到默认分组，实际在 %s", account.GroupID)
	}
}

func TestGroupDeleteRejectsSystemGroup(t *testing.T) {
	svc, store, tenantID := groupFixture(t)
	ctx := context.Background()
	system, err := store.GetSystemMailGroup(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, tenantID, system.ID); err == nil {
		t.Error("默认分组不应被删除")
	}
}

// 分组是平的一层：列表里每个分组只报自己的账号数，没有「含子树」这个口径。
func TestGroupListCountsAccounts(t *testing.T) {
	svc, store, tenantID := groupFixture(t)
	ctx := context.Background()
	group, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "客户 A"})
	if err != nil {
		t.Fatal(err)
	}
	for i, email := range []string{"a@x.com", "b@x.com"} {
		if err := store.CreateMailAccount(ctx, &model.MailAccount{
			ID: "acct-" + email, TenantID: tenantID, GroupID: group.ID,
			Email: email, EmailNormalized: email,
			Provider: "outlook", AccountType: "outlook",
			Status: model.AccountStatusActive, SortOrder: i, IMAPPort: 993,
		}); err != nil {
			t.Fatal(err)
		}
	}

	groups, err := svc.List(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	// 默认分组 + 新建的那个
	if len(groups) != 2 {
		t.Fatalf("应有 2 个分组，实际 %d", len(groups))
	}
	counts := map[string]int{}
	for _, g := range groups {
		counts[g.ID] = g.AccountCount
	}
	if counts[group.ID] != 2 {
		t.Errorf("客户 A 应有 2 个账号，实际 %d", counts[group.ID])
	}
	system, err := store.GetSystemMailGroup(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if counts[system.ID] != 0 {
		t.Errorf("默认分组应没有账号，实际 %d", counts[system.ID])
	}
}

func TestGroupNameValidation(t *testing.T) {
	svc, _, tenantID := groupFixture(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "   "}); err == nil {
		t.Error("空白名称应被拒绝")
	}
	if _, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: strings.Repeat("x", 101)}); err == nil {
		t.Error("超长名称应被拒绝")
	}
	if _, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "组", Color: "neon"}); err == nil {
		t.Error("非法颜色应被拒绝")
	}
	if _, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "重名"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "重名"}); err == nil {
		t.Error("同名分组应被拒绝")
	}
}

// 跨租户访问必须落到 404，而不是静默成功或读到别人的数据。
func TestGroupTenantIsolation(t *testing.T) {
	svc, store, tenantA := groupFixture(t)
	ctx := context.Background()
	other, _, err := service.NewAuthService(store).Register(ctx,
		model.RegisterRequest{Username: "bobby", Email: "bob@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	tenantB := other.Tenants[0].ID

	group, err := svc.Create(ctx, tenantA, model.CreateMailGroupRequest{Name: "A 的分组"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMailGroup(ctx, tenantB, group.ID); err == nil {
		t.Error("B 租户不应读到 A 的分组")
	}
	name := "被改名"
	if _, err := svc.Update(ctx, tenantB, group.ID, model.UpdateMailGroupRequest{Name: &name}); err == nil {
		t.Error("B 租户不应能修改 A 的分组")
	}
	if err := svc.Delete(ctx, tenantB, group.ID); err == nil {
		t.Error("B 租户不应能删除 A 的分组")
	}
}

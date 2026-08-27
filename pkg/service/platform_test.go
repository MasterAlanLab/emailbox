package service_test

import (
	"context"
	"errors"
	"testing"

	"emailbox/configs"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

func registerUser(t *testing.T, store *repo.Store, username, email string) *model.UserResponse {
	t.Helper()
	result, _, err := service.NewAuthService(store).Register(context.Background(),
		model.RegisterRequest{Username: username, Email: email, Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	return &result.User
}

func TestBootstrapAdminPromotesConfiguredUser(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	user := registerUser(t, store, "alice", "alice@example.com")

	platform := service.NewPlatformService(store, service.NewAuthService(store))
	// 空白不该影响匹配——配置文件里很容易多打一个空格。
	// 大小写**会**影响：用户名是原样存的（注册时只 TrimSpace），
	// 这里若擅自转小写，用户名带大写字母的人就永远提不了权。
	if err := platform.BootstrapAdmin(ctx, "  alice  ", ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPlatformAdmin() {
		t.Fatalf("用户未被提升为管理员，platform_role=%q", got.PlatformRole)
	}

	// 重复执行必须幂等：每次启动都会跑一遍。
	if err := platform.BootstrapAdmin(ctx, "alice", ""); err != nil {
		t.Fatal(err)
	}
}

// 配了密码时，启动直接把管理员建出来——否则「注册 → 改配置 → 重启」这三步
// 对一个刚 docker compose up 的人来说完全不明显，他只会看到后台进不去。
func TestBootstrapAdminCreatesUserWhenPasswordGiven(t *testing.T) {
	configs.AppConfig = &configs.Config{
		Session: configs.SessionConfig{ExpireHour: 24},
		// 关闭注册也要能引导：这是部署者在自己机器上开门，不是公开注册。
		SaaS: configs.SaaSConfig{RegistrationMode: configs.RegistrationClosed},
	}
	store := testStore(t)
	ctx := context.Background()
	platform := service.NewPlatformService(store, service.NewAuthService(store))

	if err := platform.BootstrapAdmin(ctx, "admin", "admin123.."); err != nil {
		t.Fatal(err)
	}
	user, err := store.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("管理员没被建出来: %v", err)
	}
	if !user.IsPlatformAdmin() {
		t.Errorf("建出来的用户不是管理员，platform_role=%q", user.PlatformRole)
	}
	// 建出来的必须是**能用**的账号：缺了工作空间或默认分组的用户，
	// 登录后页面就是坏的，而且没法自助修复。
	tenants, err := store.ListTenants(ctx, user.ID)
	if err != nil || len(tenants) != 1 {
		t.Fatalf("应有一个个人工作空间，实际 %d 个（%v）", len(tenants), err)
	}
	if _, err := store.GetSystemMailGroup(ctx, tenants[0].ID); err != nil {
		t.Errorf("默认分组缺失: %v", err)
	}
	if _, err := store.GetEffectiveQuota(ctx, tenants[0].ID); err != nil {
		t.Errorf("配额记录缺失: %v", err)
	}

	// 已存在时**不碰密码**：配置文件不该能悄悄接管一个已有账号，
	// 也不该在用户改完密码后于下次重启把它静默改回去。
	before := user.PasswordHash
	if err := platform.BootstrapAdmin(ctx, "admin", "totally-different"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if after.PasswordHash != before {
		t.Error("已存在的用户密码被配置改掉了")
	}
}

// 用户还没注册是正常情况：等他注册后下次启动生效，不该让启动失败。
func TestBootstrapAdminToleratesMissingUser(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	if err := service.NewPlatformService(store, service.NewAuthService(store)).BootstrapAdmin(context.Background(), "nobody@example.com", ""); err != nil {
		t.Fatalf("用户不存在不应报错: %v", err)
	}
}

// 撤销最后一个管理员会让后台永久锁死，且没有自助恢复途径。
func TestSetPlatformRoleProtectsLastAdmin(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	alice := registerUser(t, store, "alice", "alice@example.com")
	bob := registerUser(t, store, "bobby", "bob@example.com")
	platform := service.NewPlatformService(store, service.NewAuthService(store))

	if err := platform.SetPlatformRole(ctx, alice.ID, model.PlatformRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := platform.SetPlatformRole(ctx, alice.ID, model.PlatformRoleUser); !errors.Is(err, service.ErrLastAdmin) {
		t.Fatalf("撤销最后一个管理员应返回 ErrLastAdmin，实际 %v", err)
	}

	// 有第二个管理员之后就可以降级了。
	if err := platform.SetPlatformRole(ctx, bob.ID, model.PlatformRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := platform.SetPlatformRole(ctx, alice.ID, model.PlatformRoleUser); err != nil {
		t.Fatalf("存在其它管理员时应允许降级: %v", err)
	}
}

func TestSetPlatformRoleRejectsUnknownRole(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	user := registerUser(t, store, "alice", "alice@example.com")
	if err := service.NewPlatformService(store, service.NewAuthService(store)).SetPlatformRole(ctx, user.ID, model.PlatformRole("superuser")); err == nil {
		t.Fatal("未知平台角色应被拒绝")
	}
}

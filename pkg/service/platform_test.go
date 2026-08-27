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

	platform := service.NewPlatformService(store)
	// 空白不该影响匹配——配置文件里很容易多打一个空格。
	// 大小写**会**影响：用户名是原样存的（注册时只 TrimSpace），
	// 这里若擅自转小写，用户名带大写字母的人就永远提不了权。
	if err := platform.BootstrapAdmin(ctx, "  alice  "); err != nil {
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
	if err := platform.BootstrapAdmin(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
}

// 用户还没注册是正常情况：等他注册后下次启动生效，不该让启动失败。
func TestBootstrapAdminToleratesMissingUser(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	if err := service.NewPlatformService(store).BootstrapAdmin(context.Background(), "nobody@example.com"); err != nil {
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
	platform := service.NewPlatformService(store)

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
	if err := service.NewPlatformService(store).SetPlatformRole(ctx, user.ID, model.PlatformRole("superuser")); err == nil {
		t.Fatal("未知平台角色应被拒绝")
	}
}

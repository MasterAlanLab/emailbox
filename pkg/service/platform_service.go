package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"
)

// PlatformService 管理平台角色（users.platform_role）。
type PlatformService struct{ store *repo.Store }

func NewPlatformService(store *repo.Store) *PlatformService { return &PlatformService{store: store} }

// ErrLastAdmin 表示该操作会让系统失去最后一个平台管理员。
var ErrLastAdmin = errors.New("不能撤销最后一个平台管理员")

// BootstrapAdmin 在启动时把 BOOTSTRAP_ADMIN_USERNAME 对应的用户提升为平台管理员。
//
// 刻意不采用「第一个注册的人自动成为管理员」——那在开放注册的平台上是明显的抢注漏洞。
// 用户还不存在时不做任何事，等他注册后下次启动生效。
// 返回错误只在数据库出问题时；配置缺失或用户不存在都属于正常情况。
//
// 认的是**用户名**而不是邮箱：000008 之后邮箱可以不填，按邮箱找的话，
// 一个所有人都没填邮箱的部署将永远产生不出管理员，后台也就永远进不去。
func (s *PlatformService) BootstrapAdmin(ctx context.Context, username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return s.warnIfNoAdmin(ctx)
	}
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			slog.Warn("BOOTSTRAP_ADMIN_USERNAME 对应的用户尚不存在，等其注册后下次启动生效", "username", username)
			return s.warnIfNoAdmin(ctx)
		}
		return err
	}
	if user.IsPlatformAdmin() {
		return nil
	}
	if err := s.store.UpdateUserPlatformRole(ctx, user.ID, model.PlatformRoleAdmin); err != nil {
		return err
	}
	slog.Info("已将用户提升为平台管理员", "username", username, "user_id", user.ID)
	return nil
}

// warnIfNoAdmin 在系统里一个管理员都没有时打日志。
// 静默地没有管理员意味着后台完全进不去，且没有任何自助恢复途径。
func (s *PlatformService) warnIfNoAdmin(ctx context.Context) error {
	n, err := s.store.CountPlatformAdmins(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		slog.Warn("系统中没有任何平台管理员，管理后台将无法进入；请配置 BOOTSTRAP_ADMIN_USERNAME")
	}
	return nil
}

// SetPlatformRole 授予或撤销平台管理员。
func (s *PlatformService) SetPlatformRole(ctx context.Context, userID string, role model.PlatformRole) error {
	switch role {
	case model.PlatformRoleUser, model.PlatformRoleAdmin:
	default:
		return errors.New("平台角色只能是 user 或 admin")
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.PlatformRole == role {
		return nil
	}
	// 撤销最后一个管理员会让后台永久锁死，且没有自助恢复途径
	// （BOOTSTRAP_ADMIN_USERNAME 需要重启才生效）。
	if role == model.PlatformRoleUser {
		n, err := s.store.CountPlatformAdmins(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}
	return s.store.UpdateUserPlatformRole(ctx, userID, role)
}

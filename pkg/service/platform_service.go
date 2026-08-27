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
//
// 它还持有 AuthService：引导管理员时可能要把这个用户**建出来**，
// 而「用户 + 工作空间 + 成员 + 默认分组 + 配额」这五件套只有一处实现
// （AuthService.createAccount），抄第二遍迟早漏掉其中一件。
type PlatformService struct {
	store *repo.Store
	auth  *AuthService
}

func NewPlatformService(store *repo.Store, auth *AuthService) *PlatformService {
	return &PlatformService{store: store, auth: auth}
}

// ErrLastAdmin 表示该操作会让系统失去最后一个平台管理员。
var ErrLastAdmin = errors.New("不能撤销最后一个平台管理员")

// BootstrapAdmin 在启动时准备好平台管理员：把 BOOTSTRAP_ADMIN_USERNAME 对应的用户
// 提升为管理员；该用户还不存在且配了 BOOTSTRAP_ADMIN_PASSWORD 时，先把他建出来。
//
// 刻意不采用「第一个注册的人自动成为管理员」——那在开放注册的平台上是明显的抢注漏洞。
// 返回错误只在数据库出问题时；配置缺失属于正常情况。
//
// 认的是**用户名**而不是邮箱：000008 之后邮箱可以不填，按邮箱找的话，
// 一个所有人都没填邮箱的部署将永远产生不出管理员，后台也就永远进不去。
//
// **用户已存在时不碰他的密码。** 每次启动都按配置重置密码的话，配置文件就成了
// 一个能悄悄接管已有账号的入口，而且改一次密码会在下次重启时被静默改回去。
func (s *PlatformService) BootstrapAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return s.warnIfNoAdmin(ctx)
	}
	user, err := s.store.GetUserByUsername(ctx, username)
	if errors.Is(err, repo.ErrNotFound) {
		if strings.TrimSpace(password) == "" {
			slog.Warn("BOOTSTRAP_ADMIN_USERNAME 对应的用户尚不存在；配 BOOTSTRAP_ADMIN_PASSWORD 可在启动时自动建号，或等其自行注册后下次启动生效",
				"username", username)
			return s.warnIfNoAdmin(ctx)
		}
		if user, err = s.auth.CreateBootstrapAdmin(ctx, username, password); err != nil {
			// 建不出来不该挡住服务启动：其余用户照常能用，后台进不去而已。
			slog.Error("按 BOOTSTRAP_ADMIN_USERNAME 建号失败", "username", username, "error", err)
			return s.warnIfNoAdmin(ctx)
		}
		slog.Warn("已按配置创建管理员账号，请立刻登录改密码，并把 BOOTSTRAP_ADMIN_PASSWORD 从环境里删掉",
			"username", username, "user_id", user.ID)
	} else if err != nil {
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

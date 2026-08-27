package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// AdminService 是管理后台的业务层：用户、租户、套餐、配额与平台概览。
//
// 它**不**包含任何跨租户的邮箱逻辑——那些直接复用 AccountService / MessageService，
// 差别只在 tenantID 的来源（URL 而非 session）与鉴权中间件。
// 这是 08 文档 §2.3 最重要的一条收敛：SQL 层永远只有一条隔离规则，从不放宽。
type AdminService struct {
	store    *repo.Store
	platform *PlatformService
	quota    *quota.Service
}

func NewAdminService(store *repo.Store, platform *PlatformService, q *quota.Service) *AdminService {
	return &AdminService{store: store, platform: platform, quota: q}
}

var (
	// ErrSelfTarget 挡住管理员对自己动手的两个操作。
	ErrSelfTarget = errors.New("不能对自己执行该操作")
	// ErrPlanInUse 表示套餐还挂着租户，删掉会让那些租户失去配额来源。
	ErrPlanInUse = errors.New("仍有租户在使用该套餐")
	// ErrPlanIsDefault 表示这是默认套餐，删掉之后新用户注册会找不到套餐可挂。
	ErrPlanIsDefault = errors.New("默认套餐不能删除，请先把另一个套餐设为默认")
	// ErrQuotaNoteRequired 强制管理员写明调额原因。
	ErrQuotaNoteRequired = errors.New("调整配额必须填写原因")
)

// ---------- 用户 ----------

func (s *AdminService) ListUsers(ctx context.Context, f model.AdminUserFilter) ([]model.AdminUser, int, error) {
	return s.store.ListAdminUsers(ctx, f)
}

func (s *AdminService) GetUser(ctx context.Context, userID string) (*model.AdminUser, error) {
	return s.store.GetAdminUser(ctx, userID)
}

// UserUpdate 是一次用户变更请求，nil 表示该项不改。
type UserUpdate struct {
	Status       *model.UserStatus
	PlatformRole *model.PlatformRole
}

// UpdateUser 改用户状态与平台角色，返回实际发生变化的字段名（供审计写进 details）。
//
// 禁用时必须同时清空该用户的全部会话：只改 status 而留着 session，
// 已登录的那一个浏览器仍然畅通无阻，「禁用」就成了个摆设。
func (s *AdminService) UpdateUser(ctx context.Context, actorID, userID string, u UserUpdate) ([]string, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	changed := make([]string, 0, 2)

	if u.PlatformRole != nil && *u.PlatformRole != user.PlatformRole {
		// 最后一个管理员的守卫在 PlatformService 里，跟 BootstrapAdmin 放在一处，
		// 免得两边各写一份判断然后慢慢跑偏。
		if err := s.platform.SetPlatformRole(ctx, userID, *u.PlatformRole); err != nil {
			return nil, err
		}
		changed = append(changed, "platform_role")
	}

	if u.Status != nil && *u.Status != user.Status {
		if *u.Status == model.UserStatusDisabled && userID == actorID {
			return nil, ErrSelfTarget
		}
		if err := s.store.UpdateUserStatus(ctx, userID, *u.Status); err != nil {
			return nil, err
		}
		if *u.Status != model.UserStatusActive {
			if err := s.store.DeleteUserSessions(ctx, userID); err != nil {
				return nil, err
			}
		}
		changed = append(changed, "status")
	}

	return changed, nil
}

// ResetPassword 生成一个临时密码，一次性返回给管理员，并清空该用户全部会话。
//
// 返回明文是有意的：平台还没有发信能力（P4 才有 SMTP），只能由管理员转交。
// 因此它只在这一次响应里出现，不落库、不进日志、不写审计 details。
func (s *AdminService) ResetPassword(ctx context.Context, userID string) (string, error) {
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return "", err
	}
	password, err := randomPassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	if err := s.store.UpdateUserPassword(ctx, userID, string(hash)); err != nil {
		return "", err
	}
	// 密码换了但旧会话还在，等于没换：拿到旧 cookie 的人照样是登录状态。
	if err := s.store.DeleteUserSessions(ctx, userID); err != nil {
		return "", err
	}
	return password, nil
}

// randomPassword 生成 16 字符的临时密码。用 URL 安全字母表，
// 免得管理员复制粘贴时被 + / = 之类的字符坑到。
func randomPassword() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DeleteUser 软删用户，并连带处理他的个人工作空间与邮箱。
//
// 关键在于凭据：邮箱账号的软删语句会在同一条 UPDATE 里清空三个密文列
// （08 文档 §6 第 6 条）。软删只为「误删可恢复」，而第三方邮箱的凭据密文
// 不该跟着一个 deleted_at 标记长期留在库里。恢复后需要用户重新填凭据。
//
// 返回被清理的邮箱数量，供审计记录规模。
func (s *AdminService) DeleteUser(ctx context.Context, actorID, userID string) (int, error) {
	if userID == actorID {
		return 0, ErrSelfTarget
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	// 删掉最后一个管理员和撤销最后一个管理员是同一个后果：后台永久锁死。
	if user.IsPlatformAdmin() {
		n, err := s.store.CountPlatformAdmins(ctx)
		if err != nil {
			return 0, err
		}
		if n <= 1 {
			return 0, ErrLastAdmin
		}
	}

	accounts := 0
	err = s.store.WithTx(ctx, func(tx *repo.Store) error {
		tenant, err := tx.GetPersonalTenantByOwner(ctx, userID)
		switch {
		case err == nil:
			n, err := tx.SoftDeleteMailAccountsByTenant(ctx, tenant.ID)
			if err != nil {
				return err
			}
			accounts = n
			if err := tx.DeleteTenant(ctx, tenant.ID); err != nil {
				return err
			}
		case errors.Is(err, repo.ErrNotFound):
			// 000002_saas 之前建的老账号没有个人空间，跳过这一段即可。
		default:
			return err
		}
		if err := tx.SoftDeleteUser(ctx, userID); err != nil {
			return err
		}
		return tx.DeleteUserSessions(ctx, userID)
	})
	if err != nil {
		return 0, err
	}
	return accounts, nil
}

// ---------- 套餐 ----------

func (s *AdminService) ListPlans(ctx context.Context) ([]model.Plan, error) {
	return s.store.ListPlans(ctx)
}

// CreatePlan 新建套餐。设为默认时，其余套餐的默认标记在同一个事务里清掉——
// 两个 is_default=1 会让新注册用户拿到哪个套餐取决于 created_at 的先后。
func (s *AdminService) CreatePlan(ctx context.Context, p model.Plan) (*model.Plan, error) {
	p.Code = strings.ToLower(strings.TrimSpace(p.Code))
	if p.Code == "" || strings.TrimSpace(p.Name) == "" {
		return nil, errors.New("套餐 code 与名称不能为空")
	}
	p.ID = uuid.NewString()
	err := s.store.WithTx(ctx, func(tx *repo.Store) error {
		if err := tx.CreatePlan(ctx, p); err != nil {
			return err
		}
		if p.IsDefault {
			return tx.ClearDefaultPlanExcept(ctx, p.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetPlanByID(ctx, p.ID)
}

// UpdatePlan 改套餐。code 不可改，调用方传进来的会被忽略。
func (s *AdminService) UpdatePlan(ctx context.Context, p model.Plan) (*model.Plan, error) {
	if strings.TrimSpace(p.Name) == "" {
		return nil, errors.New("套餐名称不能为空")
	}
	current, err := s.store.GetPlanByID(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	// 取消最后一个默认套餐会让新注册流程找不到套餐可挂，注册直接失败。
	if current.IsDefault && !p.IsDefault {
		return nil, errors.New("必须有一个默认套餐，请先把另一个设为默认")
	}
	err = s.store.WithTx(ctx, func(tx *repo.Store) error {
		if err := tx.UpdatePlan(ctx, p); err != nil {
			return err
		}
		if p.IsDefault {
			return tx.ClearDefaultPlanExcept(ctx, p.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetPlanByID(ctx, p.ID)
}

// DeletePlan 删套餐。还有租户挂在上面时拒绝——那些租户的生效配额是
// 「套餐值 COALESCE 覆盖值」，套餐没了就查不出配额，等于所有操作被拒。
func (s *AdminService) DeletePlan(ctx context.Context, planID string) error {
	plan, err := s.store.GetPlanByID(ctx, planID)
	if err != nil {
		return err
	}
	if plan.IsDefault {
		return ErrPlanIsDefault
	}
	n, err := s.store.CountTenantsByPlan(ctx, planID)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrPlanInUse
	}
	return s.store.DeletePlan(ctx, planID)
}

// ---------- 配额 ----------

// QuotaUpdate 是管理员对单个租户的配额调整。
// PlanID 非空表示换套餐；Overrides 里 nil 的项表示「不覆盖，取套餐值」。
type QuotaUpdate struct {
	PlanID    string
	Overrides repo.QuotaOverrides
}

// UpdateTenantQuota 换套餐并写入覆盖值。note 是硬性要求：
// 一个没写原因的调额，三个月后没人说得清是促销、补偿还是处罚。
func (s *AdminService) UpdateTenantQuota(ctx context.Context, tenantID, actorID string, u QuotaUpdate) error {
	if strings.TrimSpace(u.Overrides.Note) == "" {
		return ErrQuotaNoteRequired
	}
	if _, err := s.store.GetTenantByID(ctx, tenantID); err != nil {
		return err
	}
	if u.PlanID != "" {
		if _, err := s.store.GetPlanByID(ctx, u.PlanID); err != nil {
			return err
		}
	}
	u.Overrides.UpdatedBy = &actorID
	return s.store.WithTx(ctx, func(tx *repo.Store) error {
		if u.PlanID != "" {
			if err := tx.UpdateTenantPlan(ctx, tenantID, u.PlanID); err != nil {
				return err
			}
		}
		return tx.UpdateTenantQuotaOverrides(ctx, tenantID, u.Overrides)
	})
}

// ---------- 概览 ----------

// Stats 返回平台概览。
//
// 「今日」必须问 quota 服务要，不能自己 time.Now().Format()：
// usage_counters 的 day 是按租户时区算的，两边口径不一致时，
// 每天跨零点后的那几个小时，概览上的拉信量会显示成 0。
func (s *AdminService) Stats(ctx context.Context) (*model.PlatformStats, error) {
	return s.store.GetPlatformStats(ctx, s.quota.Today())
}

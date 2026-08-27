package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"emailbox/configs"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// 用于在邮箱不存在时消耗与真实校验相当的时间，避免暴露账号是否存在。
var dummyPasswordHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}()

type AuthService struct{ store *repo.Store }

func NewAuthService(store *repo.Store) *AuthService { return &AuthService{store: store} }

// 长度上限对齐 PostgreSQL schema 中最窄的列定义。
// SQLite 的 TEXT 没有长度限制，不在此处校验会导致同一份输入在两种数据库上行为不同：
// SQLite 静默写入，PostgreSQL 报驱动错误。
const (
	maxUsernameLen = 50
	maxEmailLen    = 255
	maxTenantName  = 100
	maxTenantSlug  = 100
	// bcrypt 只处理前 72 字节，超出会直接返回错误。
	maxPasswordLen = 72
)

func validateUsername(username string) error {
	if len(username) < 3 || len(username) > maxUsernameLen {
		return fmt.Errorf("用户名长度必须在3-%d个字符之间", maxUsernameLen)
	}
	return nil
}

func validateEmail(email string) error {
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("邮箱格式不正确")
	}
	if len(email) > maxEmailLen {
		return fmt.Errorf("邮箱长度不能超过 %d 个字符", maxEmailLen)
	}
	return nil
}

func validateTenantName(name string) error {
	if name == "" {
		return errors.New("租户名称不能为空")
	}
	if len(name) > maxTenantName {
		return fmt.Errorf("租户名称长度不能超过 %d 个字符", maxTenantName)
	}
	return nil
}

func validateCredentials(username, email, password string) error {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(strings.ToLower(email))
	// 注册必须要求用户名：空用户名会占用 users.username 的唯一索引，
	// 之后所有人都无法再用空用户名注册。
	if err := validateUsername(username); err != nil {
		return err
	}
	// 邮箱可以不填（000008 起它只是资料字段），填了才校验格式。
	if email != "" {
		if err := validateEmail(email); err != nil {
			return err
		}
	}
	if len(password) < 6 {
		return errors.New("密码长度不能少于6个字符")
	}
	if len(password) > maxPasswordLen {
		return fmt.Errorf("密码长度不能超过 %d 个字节", maxPasswordLen)
	}
	return nil
}
func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// defaultPlan 取 DEFAULT_PLAN_CODE 指定的套餐；该 code 不存在时退回 is_default=1 的套餐。
// 配置写错不该让整个注册入口瘫痪。
func (s *AuthService) defaultPlan(ctx context.Context) (*model.Plan, error) {
	if code := configs.AppConfig.SaaS.DefaultPlanCode; code != "" {
		plan, err := s.store.GetPlanByCode(ctx, code)
		if err == nil {
			return plan, nil
		}
		if !errors.Is(err, repo.ErrNotFound) {
			return nil, err
		}
		slog.Warn("DEFAULT_PLAN_CODE 指向的套餐不存在，改用默认套餐", "code", code)
	}
	return s.store.GetDefaultPlan(ctx)
}

func (s *AuthService) Register(ctx context.Context, req model.RegisterRequest) (*model.AuthResponse, string, error) {
	if configs.AppConfig.SaaS.RegistrationMode == configs.RegistrationClosed {
		return nil, "", errors.New("本站已关闭注册")
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Username = strings.TrimSpace(req.Username)
	if err := validateCredentials(req.Username, req.Email, req.Password); err != nil {
		return nil, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	plan, err := s.defaultPlan(ctx)
	if err != nil {
		slog.Error("读取默认套餐失败", "error", err)
		return nil, "", errors.New("注册失败，请稍后重试")
	}
	user, tenant, err := s.createAccount(ctx, req.Username, req.Email, string(hash), plan.ID)
	if err != nil {
		return nil, "", err
	}
	token, err := s.createSession(ctx, user.ID, &tenant.ID)
	if err != nil {
		return nil, "", err
	}
	return &model.AuthResponse{User: user.ToResponse(), Tenants: []model.Tenant{*tenant}, ActiveTenantID: &tenant.ID}, token, nil
}

// createAccount 建出「用户 + 个人工作空间 + 成员关系 + 默认邮箱分组 + 配额」这五件套。
//
// 注册与启动时的管理员引导共用它：两条路径必须建出**完全一样**的东西。
// 抄第二遍迟早会漏掉其中一件，而漏掉任何一件的用户登录后页面就是坏的，
// 且无法自助修复——没有默认分组时，删除任何分组都会因为账号无处回落而失败。
// 这也是它们必须同生共死（一个事务）的原因。
func (s *AuthService) createAccount(
	ctx context.Context, username, email, passwordHash, planID string,
) (*model.User, *model.Tenant, error) {
	user := &model.User{ID: uuid.NewString(), Username: username, Email: email, PasswordHash: passwordHash, Status: model.UserStatusActive, PlatformRole: model.PlatformRoleUser}
	tenant := &model.Tenant{ID: uuid.NewString(), Name: username + " 的工作空间", Slug: slugify(username) + "-" + uuid.NewString()[:8], Kind: model.TenantKindPersonal, CreatedBy: user.ID}
	member := &model.TenantMember{ID: uuid.NewString(), TenantID: tenant.ID, UserID: user.ID, Role: model.TenantRoleOwner}

	err := s.store.WithTx(ctx, func(tx *repo.Store) error {
		if e := tx.CreateUser(ctx, user); e != nil {
			return e
		}
		if e := tx.CreateTenant(ctx, tenant); e != nil {
			return e
		}
		if e := tx.CreateMember(ctx, member); e != nil {
			return e
		}
		if e := EnsureDefaultGroup(ctx, tx, tenant.ID); e != nil {
			return e
		}
		return tx.CreateTenantQuota(ctx, tenant.ID, planID)
	})
	if err != nil {
		if errors.Is(err, repo.ErrConflict) {
			return nil, nil, errors.New("用户名已被使用")
		}
		// 原始错误只记日志：它会被当作 400 返回，绕过 handler 层的 5xx 脱敏。
		slog.Error("创建账号失败", "username", username, "error", err)
		return nil, nil, errors.New("注册失败，请稍后重试")
	}
	if user, err = s.store.GetUserByID(ctx, user.ID); err != nil {
		return nil, nil, err
	}
	if tenant, err = s.store.GetTenantByID(ctx, tenant.ID); err != nil {
		return nil, nil, err
	}
	return user, tenant, nil
}

// CreateBootstrapAdmin 按 BOOTSTRAP_ADMIN_USERNAME / _PASSWORD 建出第一个账号。
//
// 绕过 REGISTRATION_MODE：这是部署者在自己的机器上引导后台入口，
// 不是公开注册。密码仍然走同一套校验与 bcrypt。
func (s *AuthService) CreateBootstrapAdmin(ctx context.Context, username, password string) (*model.User, error) {
	if err := validateCredentials(username, "", password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	plan, err := s.defaultPlan(ctx)
	if err != nil {
		return nil, err
	}
	user, _, err := s.createAccount(ctx, username, "", string(hash), plan.ID)
	return user, err
}

func (s *AuthService) Login(ctx context.Context, req model.LoginRequest) (*model.AuthResponse, string, error) {
	user, err := s.store.GetUserByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		// 用户名不存在时也要跑一次 bcrypt：否则响应耗时相差数万倍，
		// 攻击者可据此判断某个用户名是否已注册。结果必然不匹配，无需检查。
		// errcheck 配了 check-blank，`_ =` 也会被拦，所以这里显式豁免：
		// 这次调用只为消耗等量时间，结果必然不匹配。
		//nolint:errcheck // 故意丢弃：只为恒定耗时
		bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(req.Password))
		return nil, "", errors.New("用户名或密码错误")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		return nil, "", errors.New("用户名或密码错误")
	}
	if user.Status != model.UserStatusActive {
		return nil, "", errors.New("账户已停用")
	}
	tenants, err := s.store.ListTenants(ctx, user.ID)
	if err != nil {
		return nil, "", err
	}
	var active *string
	if len(tenants) > 0 {
		active = &tenants[0].ID
	}
	token, err := s.createSession(ctx, user.ID, active)
	if err != nil {
		return nil, "", err
	}
	// 记一次登录时间给管理后台看。失败不影响登录本身——
	// 用户已经通过了认证，不该因为一次统计写入失败被挡在门外。
	if err := s.store.TouchUserLastLogin(ctx, user.ID); err != nil {
		slog.Warn("记录最后登录时间失败", "user_id", user.ID, "error", err)
	}
	return &model.AuthResponse{User: user.ToResponse(), Tenants: tenants, ActiveTenantID: active}, token, nil
}

func (s *AuthService) createSession(ctx context.Context, userID string, active *string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	v := &model.Session{ID: uuid.NewString(), UserID: userID, TokenHash: hex.EncodeToString(sum[:]), ActiveTenantID: active, ExpiresAt: time.Now().UTC().Add(time.Duration(configs.AppConfig.Session.ExpireHour) * time.Hour)}
	if err := s.store.CreateSession(ctx, v); err != nil {
		return "", err
	}
	return token, nil
}
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *AuthService) Session(ctx context.Context, token string) (*model.AuthResponse, *model.Session, error) {
	session, err := s.store.GetSession(ctx, TokenHash(token))
	if err != nil {
		return nil, nil, err
	}
	user, err := s.store.GetUserByID(ctx, session.UserID)
	if err != nil || user.Status != model.UserStatusActive {
		return nil, nil, repo.ErrNotFound
	}
	tenants, err := s.store.ListTenants(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return &model.AuthResponse{User: user.ToResponse(), Tenants: tenants, ActiveTenantID: session.ActiveTenantID}, session, nil
}
func (s *AuthService) Logout(ctx context.Context, token string) error {
	v, err := s.store.GetSession(ctx, TokenHash(token))
	if err != nil {
		return nil
	}
	return s.store.DeleteSession(ctx, v.ID)
}

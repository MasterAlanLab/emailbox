// Package quota 计算并强制租户配额。
//
// 配额分两类：
//   - 计数类（账号数、分组数）——量小，创建时实时 COUNT 判断，无需计数表
//   - 频次类（每日取件）——按天累加进 usage_counters
//
// 取件额度对**所有来源一视同仁**：网页、API Key、管理员共用同一个计数器与同一条上限。
// 只限 API 是行不通的——会话 Cookie 同样能写进脚本，那等于留了一个「逆向网页就能绕开」的口子。
//
// usage_counters 里还有一个 token_refresh，它**只记账、没有上限**：
// 令牌刷新是账号能不能用的前提，卡住它等于让用户的账号批量失效。
//
// 强制点必须在 service 层。只在前端拦截等于没拦截。
package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"
)

// ErrQuotaExceeded 表示本次操作会超出配额。handler 层应映射为 403。
var ErrQuotaExceeded = errors.New("QUOTA_EXCEEDED")

// Service 是无状态的配额服务，所有状态都在数据库里。
type Service struct {
	store *repo.Store
	// 计数跨天用固定时区。原打算按租户设置读取，但承载它的 tenant_settings
	// 随 P5 一起从方案里删了，于是全平台统一用这一个。
	location *time.Location
}

// DefaultTimezone 是租户设置里 timezone 的默认值。
const DefaultTimezone = "Asia/Shanghai"

func NewService(store *repo.Store) *Service {
	loc, err := time.LoadLocation(DefaultTimezone)
	if err != nil {
		// 容器镜像缺 tzdata 时退回 UTC：配额跨天点偏移几小时，
		// 远好过整个配额体系不可用。
		loc = time.UTC
	}
	return &Service{store: store, location: loc}
}

// Effective 返回租户的生效配额。
func (s *Service) Effective(ctx context.Context, tenantID string) (*model.Limits, error) {
	return s.store.GetEffectiveQuota(ctx, tenantID)
}

// Today 返回按租户时区计算的计数日期键（YYYY-MM-DD）。
func (s *Service) Today() string {
	return time.Now().In(s.location).Format("2006-01-02")
}

// Usage 返回今天某个指标的已用量。
func (s *Service) Usage(ctx context.Context, tenantID, metric string) (int, error) {
	return s.store.GetUsageCount(ctx, tenantID, s.Today(), metric)
}

// Record 只记账、不判上限，用于「要看用量但不设限」的指标：目前只有令牌刷新。
//
// 与 CheckAndConsume 分开是有意的：一个叫 Check 的函数如果在某些指标上从不拒绝，
// 读代码的人会以为那里有限额而实际没有。
func (s *Service) Record(ctx context.Context, tenantID, metric string, n int) error {
	if n <= 0 {
		return nil
	}
	_, err := s.store.ConsumeUsage(ctx, tenantID, s.Today(), metric, n)
	return err
}

// CheckAndConsume 预扣 n 次某个频次类配额。超额时返回 ErrQuotaExceeded 且不留下任何用量。
//
// 先加后判、超额回滚，而不是先读后写——后者在并发下会双双读到未超额的旧值，
// 两个请求都放行。这里靠事务把「累加」与「判定」绑成一个原子操作。
func (s *Service) CheckAndConsume(ctx context.Context, tenantID, metric string, n int) error {
	if n <= 0 {
		return nil
	}
	limits, err := s.Effective(ctx, tenantID)
	if err != nil {
		return err
	}
	limit := limits.LimitFor(metric)
	if limit == model.Unlimited {
		// 不限的指标仍然记账，用于用量展示。
		_, err := s.store.ConsumeUsage(ctx, tenantID, s.Today(), metric, n)
		return err
	}
	day := s.Today()
	return s.store.WithTx(ctx, func(tx *repo.Store) error {
		used, err := tx.ConsumeUsage(ctx, tenantID, day, metric, n)
		if err != nil {
			return err
		}
		if used > limit {
			return fmt.Errorf("%w: %s 今日额度 %d 已用尽（本次需要 %d）", ErrQuotaExceeded, metric, limit, n)
		}
		return nil
	})
}

// CheckCount 在创建资源前校验计数类配额。current 是当前已有数量，n 是本次新增数量。
func CheckCount(limit, current, n int, resource string) error {
	if limit == model.Unlimited {
		return nil
	}
	if current+n > limit {
		return fmt.Errorf("%w: %s 数量上限为 %d，当前 %d", ErrQuotaExceeded, resource, limit, current)
	}
	return nil
}

// Allowance 返回在配额内还能新增多少个资源，至多 want 个。
// 批量导入用它决定「导入前 N 个、其余计入 skipped」，
// 而不是因为超额几个就让整批失败。
func Allowance(limit, current, want int) int {
	if limit == model.Unlimited {
		return want
	}
	remaining := limit - current
	if remaining < 0 {
		return 0
	}
	if remaining > want {
		return want
	}
	return remaining
}

package repo

import (
	"context"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// GetDefaultPlan 返回 is_default=1 的套餐，新注册租户按它建立配额。
func (s *Store) GetDefaultPlan(ctx context.Context) (*model.Plan, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetDefaultPlan(ctx)
		return mapSQLitePlan(v), normalize(e)
	}
	v, e := s.postgres.GetDefaultPlan(ctx)
	return mapPostgresPlan(v), normalize(e)
}

func (s *Store) GetPlanByCode(ctx context.Context, code string) (*model.Plan, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetPlanByCode(ctx, code)
		return mapSQLitePlan(v), normalize(e)
	}
	v, e := s.postgres.GetPlanByCode(ctx, code)
	return mapPostgresPlan(v), normalize(e)
}

func (s *Store) ListPlans(ctx context.Context) ([]model.Plan, error) {
	out := []model.Plan{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListPlans(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, *mapSQLitePlan(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, *mapPostgresPlan(r))
	}
	return out, nil
}

// CreateTenantQuota 把租户挂到某个套餐上。覆盖值全部留 NULL，表示完全按套餐取值。
func (s *Store) CreateTenantQuota(ctx context.Context, tenantID, planID string) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateTenantQuota(ctx, sqlitedb.CreateTenantQuotaParams{TenantID: tenantID, PlanID: planID})
	} else {
		err = s.postgres.CreateTenantQuota(ctx, postgresdb.CreateTenantQuotaParams{TenantID: tenantID, PlanID: planID})
	}
	return normalize(err)
}

// GetEffectiveQuota 返回租户的生效配额（套餐值经租户覆盖值 COALESCE 之后的结果）。
func (s *Store) GetEffectiveQuota(ctx context.Context, tenantID string) (*model.Limits, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetEffectiveQuota(ctx, tenantID)
		if e != nil {
			return nil, normalize(e)
		}
		return &model.Limits{
			PlanCode: v.PlanCode, PlanName: v.PlanName,
			MaxAccounts: int(v.MaxAccounts), MaxGroups: int(v.MaxGroups),
			DailyMailFetch: int(v.DailyMailFetch),
		}, nil
	}
	v, e := s.postgres.GetEffectiveQuota(ctx, tenantID)
	if e != nil {
		return nil, normalize(e)
	}
	return &model.Limits{
		PlanCode: v.PlanCode, PlanName: v.PlanName,
		MaxAccounts: int(v.MaxAccounts), MaxGroups: int(v.MaxGroups),
		DailyMailFetch: int(v.DailyMailFetch),
	}, nil
}

// GetUsageCount 返回某天某指标的已用量；无记录时返回 0。
func (s *Store) GetUsageCount(ctx context.Context, tenantID, day, metric string) (int, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetUsageCount(ctx, sqlitedb.GetUsageCountParams{TenantID: tenantID, Day: day, Metric: metric})
		return int(v), zeroIfMissing(e)
	}
	v, e := s.postgres.GetUsageCount(ctx, postgresdb.GetUsageCountParams{TenantID: tenantID, Day: day, Metric: metric})
	return int(v), zeroIfMissing(e)
}

// ConsumeUsage 把用量加上 n 并返回累加后的值。
// 调用方需在事务内使用：判定超额后回滚即可撤销本次消费。
func (s *Store) ConsumeUsage(ctx context.Context, tenantID, day, metric string, n int) (int, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.ConsumeUsage(ctx, sqlitedb.ConsumeUsageParams{TenantID: tenantID, Day: day, Metric: metric, Count: int64(n)})
		return int(v), normalize(e)
	}
	v, e := s.postgres.ConsumeUsage(ctx, postgresdb.ConsumeUsageParams{TenantID: tenantID, Day: day, Metric: metric, Count: int32(n)})
	return int(v), normalize(e)
}

// zeroIfMissing 把「没有计数记录」翻译为 0 用量，而不是错误。
func zeroIfMissing(err error) error {
	if e := normalize(err); e != nil && e != ErrNotFound {
		return e
	}
	return nil
}

func mapSQLitePlan(p sqlitedb.Plan) *model.Plan {
	return &model.Plan{
		ID: p.ID, Code: p.Code, Name: p.Name, IsDefault: p.IsDefault != 0,
		MaxAccounts: int(p.MaxAccounts), MaxGroups: int(p.MaxGroups),
		DailyMailFetch: int(p.DailyMailFetch),
		CreatedAt:      p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func mapPostgresPlan(p postgresdb.Plan) *model.Plan {
	return &model.Plan{
		ID: p.ID, Code: p.Code, Name: p.Name, IsDefault: p.IsDefault != 0,
		MaxAccounts: int(p.MaxAccounts), MaxGroups: int(p.MaxGroups),
		DailyMailFetch: int(p.DailyMailFetch),
		CreatedAt:      p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// QuotaOverrides 是管理员针对单个租户的配额覆盖值。
// nil 表示该项不覆盖，取套餐的基线值。
type QuotaOverrides struct {
	MaxAccounts    *int
	MaxGroups      *int
	DailyMailFetch *int
	Note           string
	UpdatedBy      *string
}

// UpdateTenantQuotaOverrides 写入配额覆盖值。
// 调低配额不追溯删除已有数据，只阻止新增。
func (s *Store) UpdateTenantQuotaOverrides(ctx context.Context, tenantID string, o QuotaOverrides) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateTenantQuotaOverrides(ctx, sqlitedb.UpdateTenantQuotaOverridesParams{
			MaxAccounts: nullInt64(o.MaxAccounts), MaxGroups: nullInt64(o.MaxGroups),
			DailyMailFetch: nullInt64(o.DailyMailFetch),
			Note:           o.Note, UpdatedBy: nullableString(o.UpdatedBy), TenantID: tenantID,
		})
	} else {
		n, e = s.postgres.UpdateTenantQuotaOverrides(ctx, postgresdb.UpdateTenantQuotaOverridesParams{
			MaxAccounts: nullInt32(o.MaxAccounts), MaxGroups: nullInt32(o.MaxGroups),
			DailyMailFetch: nullInt32(o.DailyMailFetch),
			Note:           o.Note, UpdatedBy: nullableString(o.UpdatedBy), TenantID: tenantID,
		})
	}
	return rowsAffected(n, e)
}

// UpdateTenantPlan 改挂租户的套餐。
func (s *Store) UpdateTenantPlan(ctx context.Context, tenantID, planID string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateTenantPlan(ctx, sqlitedb.UpdateTenantPlanParams{PlanID: planID, TenantID: tenantID})
	} else {
		n, e = s.postgres.UpdateTenantPlan(ctx, postgresdb.UpdateTenantPlanParams{PlanID: planID, TenantID: tenantID})
	}
	return rowsAffected(n, e)
}

// GetPlanByID 取单个套餐。
func (s *Store) GetPlanByID(ctx context.Context, id string) (*model.Plan, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetPlanByID(ctx, id)
		return mapSQLitePlan(v), normalize(e)
	}
	v, e := s.postgres.GetPlanByID(ctx, id)
	return mapPostgresPlan(v), normalize(e)
}

// CreatePlan 新建套餐。
func (s *Store) CreatePlan(ctx context.Context, p model.Plan) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreatePlan(ctx, sqlitedb.CreatePlanParams{
			ID: p.ID, Code: p.Code, Name: p.Name, IsDefault: boolToInt64(p.IsDefault),
			MaxAccounts: int64(p.MaxAccounts), MaxGroups: int64(p.MaxGroups),
			DailyMailFetch: int64(p.DailyMailFetch),
		})
	} else {
		err = s.postgres.CreatePlan(ctx, postgresdb.CreatePlanParams{
			ID: p.ID, Code: p.Code, Name: p.Name, IsDefault: boolToInt32(p.IsDefault),
			MaxAccounts: int32(p.MaxAccounts), MaxGroups: int32(p.MaxGroups),
			DailyMailFetch: int32(p.DailyMailFetch),
		})
	}
	return normalize(err)
}

// UpdatePlan 改套餐。code 不可改：它是别处引用套餐的稳定标识。
func (s *Store) UpdatePlan(ctx context.Context, p model.Plan) error {
	if s.driver == "sqlite" {
		return rowsAffected(s.sqlite.UpdatePlan(ctx, sqlitedb.UpdatePlanParams{
			Name: p.Name, IsDefault: boolToInt64(p.IsDefault),
			MaxAccounts: int64(p.MaxAccounts), MaxGroups: int64(p.MaxGroups),
			DailyMailFetch: int64(p.DailyMailFetch), ID: p.ID,
		}))
	}
	return rowsAffected(s.postgres.UpdatePlan(ctx, postgresdb.UpdatePlanParams{
		Name: p.Name, IsDefault: boolToInt32(p.IsDefault),
		MaxAccounts: int32(p.MaxAccounts), MaxGroups: int32(p.MaxGroups),
		DailyMailFetch: int32(p.DailyMailFetch), ID: p.ID,
	}))
}

// DeletePlan 删套餐。调用方必须先用 CountTenantsByPlan 确认没人在用。
func (s *Store) DeletePlan(ctx context.Context, id string) error {
	if s.driver == "sqlite" {
		return rowsAffected(s.sqlite.DeletePlan(ctx, id))
	}
	return rowsAffected(s.postgres.DeletePlan(ctx, id))
}

// CountTenantsByPlan 返回还挂在这个套餐上的租户数。
func (s *Store) CountTenantsByPlan(ctx context.Context, planID string) (int, error) {
	var (
		n   int64
		err error
	)
	if s.driver == "sqlite" {
		n, err = s.sqlite.CountTenantsByPlan(ctx, planID)
	} else {
		n, err = s.postgres.CountTenantsByPlan(ctx, planID)
	}
	return int(n), normalize(err)
}

// ClearDefaultPlanExcept 把除 keepID 之外的默认标记全部清掉。
// 默认套餐必须唯一，否则 GetDefaultPlan 的结果取决于 created_at 的先后，
// 新注册用户拿到哪个套餐会变成运气问题。
func (s *Store) ClearDefaultPlanExcept(ctx context.Context, keepID string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.ClearDefaultPlanExcept(ctx, keepID))
	}
	return normalize(s.postgres.ClearDefaultPlanExcept(ctx, keepID))
}

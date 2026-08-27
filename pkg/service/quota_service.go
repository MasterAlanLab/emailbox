package service

import (
	"context"

	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
)

// QuotaUsage 是配额页要展示的一切：生效上限 + 当前用量。
// 计数类是实时 COUNT，频次类取今天的累加值。
type QuotaUsage struct {
	Limits model.Limits `json:"limits"`
	Usage  struct {
		Accounts     int `json:"accounts"`
		Groups       int `json:"groups"`
		MailFetch    int `json:"mail_fetch"`
		TokenRefresh int `json:"token_refresh"`
	} `json:"usage"`
	// Day 是频次类用量的统计日期（按租户时区），供前端说明「明日重置」。
	Day string `json:"day"`
}

// QuotaService 供用户查看自己工作空间的配额与用量。
type QuotaService struct {
	store *repo.Store
	quota *quota.Service
}

func NewQuotaService(store *repo.Store, q *quota.Service) *QuotaService {
	return &QuotaService{store: store, quota: q}
}

func (s *QuotaService) Usage(ctx context.Context, tenantID string) (*QuotaUsage, error) {
	limits, err := s.quota.Effective(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := &QuotaUsage{Limits: *limits, Day: s.quota.Today()}

	if out.Usage.Accounts, err = s.store.CountMailAccounts(ctx, tenantID); err != nil {
		return nil, err
	}
	if out.Usage.Groups, err = s.store.CountMailGroups(ctx, tenantID); err != nil {
		return nil, err
	}
	if out.Usage.MailFetch, err = s.quota.Usage(ctx, tenantID, model.MetricMailFetch); err != nil {
		return nil, err
	}
	if out.Usage.TokenRefresh, err = s.quota.Usage(ctx, tenantID, model.MetricTokenRefresh); err != nil {
		return nil, err
	}
	return out, nil
}

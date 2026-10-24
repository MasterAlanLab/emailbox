package service

import (
	"context"

	"emailbox/pkg/model"
)

// PublicPricing 与结账共用可售价格和真实套餐额度，管理员改价或调额后公开页面随之更新。
func (s *BillingService) PublicPricing(ctx context.Context) (*model.PublicPricing, error) {
	settings, err := s.store.GetBillingSettings(ctx)
	if err != nil {
		return nil, err
	}
	free, err := defaultPlan(ctx, s.store)
	if err != nil {
		return nil, err
	}
	out := &model.PublicPricing{
		BillingEnabled: settings.Enabled,
		FreePlan:       publicPlan(*free),
		PaidPlans:      []model.PublicPaidPlan{},
	}
	// 关闭购买入口不会改变已有订阅，只是不再向访客展示当前不能购买的价格。
	if !settings.Enabled {
		return out, nil
	}
	plans, err := s.store.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	prices, err := s.store.ListPlanPrices(ctx, true)
	if err != nil {
		return nil, err
	}
	byPlan := make(map[string][]model.PublicPrice)
	for _, price := range prices {
		// active 查询已排除草稿、同步失败和下架价格；缺供应商产品的记录也无法结账。
		if price.ProviderProductID == "" {
			continue
		}
		byPlan[price.PlanID] = append(byPlan[price.PlanID], model.PublicPrice{
			BillingPeriod: price.BillingPeriod,
			Currency:      price.Currency,
			Amount:        price.Amount,
		})
	}
	for _, plan := range plans {
		if plan.IsDefault || plan.ID == free.ID || len(byPlan[plan.ID]) == 0 {
			continue
		}
		out.PaidPlans = append(out.PaidPlans, model.PublicPaidPlan{
			PublicPlan: publicPlan(plan),
			Prices:     byPlan[plan.ID],
		})
	}
	return out, nil
}

func publicPlan(p model.Plan) model.PublicPlan {
	return model.PublicPlan{
		Name:           p.Name,
		MaxAccounts:    p.MaxAccounts,
		MaxGroups:      p.MaxGroups,
		DailyMailFetch: p.DailyMailFetch,
	}
}

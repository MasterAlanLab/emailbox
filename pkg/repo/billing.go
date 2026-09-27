package repo

import (
	"context"
	"database/sql"
	"time"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

func (s *Store) GetBillingSettings(ctx context.Context) (*model.BillingSettings, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetBillingSettings(ctx)
		if err != nil {
			return nil, normalize(err)
		}
		return &model.BillingSettings{Enabled: v.Enabled != 0, Mode: v.Mode, DefaultCurrency: v.DefaultCurrency, UpdatedBy: nullableValue(v.UpdatedBy), UpdatedAt: v.UpdatedAt}, nil
	}
	v, err := s.postgres.GetBillingSettings(ctx)
	if err != nil {
		return nil, normalize(err)
	}
	return &model.BillingSettings{Enabled: v.Enabled != 0, Mode: v.Mode, DefaultCurrency: v.DefaultCurrency, UpdatedBy: nullableValue(v.UpdatedBy), UpdatedAt: v.UpdatedAt}, nil
}

func nullableValue(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

func (s *Store) UpdateBillingSettings(ctx context.Context, v model.BillingSettings) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.UpdateBillingSettings(ctx, sqlitedb.UpdateBillingSettingsParams{Enabled: boolToInt64(v.Enabled), Mode: v.Mode, DefaultCurrency: v.DefaultCurrency, UpdatedBy: nullableString(nilIfEmpty(v.UpdatedBy))}))
	}
	return normalize(s.postgres.UpdateBillingSettings(ctx, postgresdb.UpdateBillingSettingsParams{Enabled: boolToInt32(v.Enabled), Mode: v.Mode, DefaultCurrency: v.DefaultCurrency, UpdatedBy: nullableString(nilIfEmpty(v.UpdatedBy))}))
}

func nilIfEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func (s *Store) ListPlanPrices(ctx context.Context, activeOnly bool) ([]model.PlanPrice, error) {
	out := []model.PlanPrice{}
	if s.driver == "sqlite" {
		if activeOnly {
			rows, err := s.sqlite.ListActivePlanPrices(ctx)
			if err != nil {
				return nil, err
			}
			for _, v := range rows {
				out = append(out, mapSQLiteActivePlanPrice(v))
			}
			return out, nil
		}
		rows, err := s.sqlite.ListPlanPrices(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range rows {
			out = append(out, mapSQLitePlanPrice(v))
		}
		return out, nil
	}
	if activeOnly {
		rows, err := s.postgres.ListActivePlanPrices(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range rows {
			out = append(out, mapPostgresActivePlanPrice(v))
		}
		return out, nil
	}
	rows, err := s.postgres.ListPlanPrices(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range rows {
		out = append(out, mapPostgresPlanPrice(v))
	}
	return out, nil
}

func (s *Store) GetPlanPrice(ctx context.Context, id string) (*model.PlanPrice, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetPlanPriceByID(ctx, id)
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLitePlanPriceByID(v), nil
	}
	v, err := s.postgres.GetPlanPriceByID(ctx, id)
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresPlanPriceByID(v), nil
}

func (s *Store) CreatePlanPrice(ctx context.Context, v model.PlanPrice) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.CreatePlanPrice(ctx, sqlitedb.CreatePlanPriceParams{ID: v.ID, PlanID: v.PlanID, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: boolToInt64(v.Active)}))
	}
	return normalize(s.postgres.CreatePlanPrice(ctx, postgresdb.CreatePlanPriceParams{ID: v.ID, PlanID: v.PlanID, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: boolToInt32(v.Active)}))
}

func (s *Store) UpdatePlanPrice(ctx context.Context, v model.PlanPrice) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.UpdatePlanPrice(ctx, sqlitedb.UpdatePlanPriceParams{Amount: v.Amount, Currency: v.Currency, SyncStatus: v.SyncStatus, SyncError: v.SyncError, ProviderProductID: v.ProviderProductID, Active: boolToInt64(v.Active), ID: v.ID})
	} else {
		n, err = s.postgres.UpdatePlanPrice(ctx, postgresdb.UpdatePlanPriceParams{Amount: v.Amount, Currency: v.Currency, SyncStatus: v.SyncStatus, SyncError: v.SyncError, ProviderProductID: v.ProviderProductID, Active: boolToInt32(v.Active), ID: v.ID})
	}
	return rowsAffected(n, err)
}

func (s *Store) GetSubscription(ctx context.Context, tenantID string) (*model.Subscription, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetSubscriptionByTenant(ctx, tenantID)
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLiteSubscription(v), nil
	}
	v, err := s.postgres.GetSubscriptionByTenant(ctx, tenantID)
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresSubscription(v), nil
}

func (s *Store) UpsertSubscription(ctx context.Context, v model.Subscription) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.UpsertSubscription(ctx, sqlitedb.UpsertSubscriptionParams{ID: v.ID, TenantID: v.TenantID, Provider: v.Provider, Mode: v.Mode, OrderID: v.OrderID, ProviderProductID: v.ProviderProductID, PlanPriceID: v.PlanPriceID, PlanID: v.PlanID, Status: v.Status, Currency: v.Currency, Amount: v.Amount, CurrentPeriodStart: nullableTime(v.CurrentPeriodStart), CurrentPeriodEnd: nullableTime(v.CurrentPeriodEnd), CancelAtPeriodEnd: boolToInt64(v.CancelAtPeriodEnd), LastEventAt: nullableTime(v.LastEventAt)}))
	}
	return normalize(s.postgres.UpsertSubscription(ctx, postgresdb.UpsertSubscriptionParams{ID: v.ID, TenantID: v.TenantID, Provider: v.Provider, Mode: v.Mode, OrderID: v.OrderID, ProviderProductID: v.ProviderProductID, PlanPriceID: v.PlanPriceID, PlanID: v.PlanID, Status: v.Status, Currency: v.Currency, Amount: v.Amount, CurrentPeriodStart: nullableTime(v.CurrentPeriodStart), CurrentPeriodEnd: nullableTime(v.CurrentPeriodEnd), CancelAtPeriodEnd: boolToInt32(v.CancelAtPeriodEnd), LastEventAt: nullableTime(v.LastEventAt)}))
}

func (s *Store) UpdateSubscriptionStatus(ctx context.Context, tenantID, status string, cancel bool, start, end, at *time.Time) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.UpdateSubscriptionStatus(ctx, sqlitedb.UpdateSubscriptionStatusParams{Status: status, CancelAtPeriodEnd: boolToInt64(cancel), CurrentPeriodStart: nullableTime(start), CurrentPeriodEnd: nullableTime(end), LastEventAt: nullableTime(at), TenantID: tenantID})
	} else {
		n, err = s.postgres.UpdateSubscriptionStatus(ctx, postgresdb.UpdateSubscriptionStatusParams{Status: status, CancelAtPeriodEnd: boolToInt32(cancel), CurrentPeriodStart: nullableTime(start), CurrentPeriodEnd: nullableTime(end), LastEventAt: nullableTime(at), TenantID: tenantID})
	}
	return rowsAffected(n, err)
}

func (s *Store) UpdateTenantSubscriptionQuota(ctx context.Context, tenantID, planID, subscriptionID string) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.UpdateTenantSubscriptionQuota(ctx, sqlitedb.UpdateTenantSubscriptionQuotaParams{PlanID: planID, SubscriptionID: nullableString(nilIfEmpty(subscriptionID)), TenantID: tenantID})
	} else {
		n, err = s.postgres.UpdateTenantSubscriptionQuota(ctx, postgresdb.UpdateTenantSubscriptionQuotaParams{PlanID: planID, SubscriptionID: nullableString(nilIfEmpty(subscriptionID)), TenantID: tenantID})
	}
	return rowsAffected(n, err)
}

// RestoreTenantQuotaSource 回收由某份订阅授予的套餐，恢复默认套餐。
// 影响 0 行是正常结果（管理员已接管套餐），不能当成 ErrNotFound——
// 那会让整个取消事件的事务回滚，Waffo 随后无休止地重试。
func (s *Store) RestoreTenantQuotaSource(ctx context.Context, tenantID, subscriptionID string) error {
	var err error
	if s.driver == "sqlite" {
		_, err = s.sqlite.RestoreTenantQuotaSource(ctx, sqlitedb.RestoreTenantQuotaSourceParams{TenantID: tenantID, SubscriptionID: nullableString(nilIfEmpty(subscriptionID))})
	} else {
		_, err = s.postgres.RestoreTenantQuotaSource(ctx, postgresdb.RestoreTenantQuotaSourceParams{TenantID: tenantID, SubscriptionID: nullableString(nilIfEmpty(subscriptionID))})
	}
	return normalize(err)
}

func (s *Store) CreateCheckout(ctx context.Context, v model.BillingCheckout) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.CreateCheckoutSession(ctx, sqlitedb.CreateCheckoutSessionParams{ID: v.ID, TenantID: v.TenantID, PlanPriceID: v.PlanPriceID, MerchantExternalID: v.MerchantExternalID, ProviderSessionID: v.ProviderSessionID, IdempotencyKey: v.IdempotencyKey, CheckoutUrl: v.CheckoutURL, Status: v.Status, ExpiresAt: nullableTime(v.ExpiresAt)}))
	}
	return normalize(s.postgres.CreateCheckoutSession(ctx, postgresdb.CreateCheckoutSessionParams{ID: v.ID, TenantID: v.TenantID, PlanPriceID: v.PlanPriceID, MerchantExternalID: v.MerchantExternalID, ProviderSessionID: v.ProviderSessionID, IdempotencyKey: v.IdempotencyKey, CheckoutUrl: v.CheckoutURL, Status: v.Status, ExpiresAt: nullableTime(v.ExpiresAt)}))
}

func (s *Store) GetCheckoutByIdempotency(ctx context.Context, tenantID, key string) (*model.BillingCheckout, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetCheckoutByIdempotency(ctx, sqlitedb.GetCheckoutByIdempotencyParams{TenantID: tenantID, IdempotencyKey: key})
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLiteCheckout(v), nil
	}
	v, err := s.postgres.GetCheckoutByIdempotency(ctx, postgresdb.GetCheckoutByIdempotencyParams{TenantID: tenantID, IdempotencyKey: key})
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresCheckout(v), nil
}

func (s *Store) GetCheckoutByMerchantExternalID(ctx context.Context, key string) (*model.BillingCheckout, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetCheckoutByMerchantExternalID(ctx, key)
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLiteCheckout(v), nil
	}
	v, err := s.postgres.GetCheckoutByMerchantExternalID(ctx, key)
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresCheckout(v), nil
}

func (s *Store) GetSubscriptionByOrderID(ctx context.Context, orderID string) (*model.Subscription, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetSubscriptionByOrderID(ctx, orderID)
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLiteSubscription(v), nil
	}
	v, err := s.postgres.GetSubscriptionByOrderID(ctx, orderID)
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresSubscription(v), nil
}

func (s *Store) UpdateCheckout(ctx context.Context, v model.BillingCheckout) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.UpdateCheckoutSession(ctx, sqlitedb.UpdateCheckoutSessionParams{ProviderSessionID: v.ProviderSessionID, CheckoutUrl: v.CheckoutURL, Status: v.Status, ExpiresAt: nullableTime(v.ExpiresAt), ID: v.ID})
	} else {
		n, err = s.postgres.UpdateCheckoutSession(ctx, postgresdb.UpdateCheckoutSessionParams{ProviderSessionID: v.ProviderSessionID, CheckoutUrl: v.CheckoutURL, Status: v.Status, ExpiresAt: nullableTime(v.ExpiresAt), ID: v.ID})
	}
	return rowsAffected(n, err)
}

// InsertWebhookEvent 登记一个事件，已登记过（同类型、同 eventId、同环境）时返回 false。
func (s *Store) InsertWebhookEvent(ctx context.Context, eventID, mode, eventType, storeID, hash string) (bool, error) {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.InsertWebhookEvent(ctx, sqlitedb.InsertWebhookEventParams{EventID: eventID, Mode: mode, EventType: eventType, StoreID: storeID, PayloadSha256: hash})
	} else {
		n, err = s.postgres.InsertWebhookEvent(ctx, postgresdb.InsertWebhookEventParams{EventID: eventID, Mode: mode, EventType: eventType, StoreID: storeID, PayloadSha256: hash})
	}
	return n > 0, normalize(err)
}

func (s *Store) UpdateWebhookEvent(ctx context.Context, eventType, eventID, mode, status, message string) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.UpdateWebhookEvent(ctx, sqlitedb.UpdateWebhookEventParams{Status: status, Error: message, EventType: eventType, EventID: eventID, Mode: mode})
	} else {
		n, err = s.postgres.UpdateWebhookEvent(ctx, postgresdb.UpdateWebhookEventParams{Status: status, Error: message, EventType: eventType, EventID: eventID, Mode: mode})
	}
	return rowsAffected(n, err)
}

func (s *Store) GetWebhookEventStatus(ctx context.Context, eventType, eventID, mode string) (string, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetWebhookEventStatus(ctx, sqlitedb.GetWebhookEventStatusParams{EventType: eventType, EventID: eventID, Mode: mode})
		return v, normalize(err)
	}
	v, err := s.postgres.GetWebhookEventStatus(ctx, postgresdb.GetWebhookEventStatusParams{EventType: eventType, EventID: eventID, Mode: mode})
	return v, normalize(err)
}

// ListPendingCheckouts 取租户在 since 之后创建、仍未确认的结账，最多 5 条，新的在前。
func (s *Store) ListPendingCheckouts(ctx context.Context, tenantID string, since time.Time) ([]model.BillingCheckout, error) {
	out := []model.BillingCheckout{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListPendingCheckouts(ctx, sqlitedb.ListPendingCheckoutsParams{TenantID: tenantID, CreatedAt: since.UTC()})
		if err != nil {
			return nil, normalize(err)
		}
		for _, r := range rows {
			out = append(out, *mapSQLiteCheckout(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListPendingCheckouts(ctx, postgresdb.ListPendingCheckoutsParams{TenantID: tenantID, CreatedAt: since.UTC()})
	if err != nil {
		return nil, normalize(err)
	}
	for _, r := range rows {
		out = append(out, *mapPostgresCheckout(r))
	}
	return out, nil
}

// MarkCheckoutCompleted 把产生了订阅的那次结账标为完成。只动 pending 的行。
func (s *Store) MarkCheckoutCompleted(ctx context.Context, merchantExternalID string) error {
	var err error
	if s.driver == "sqlite" {
		_, err = s.sqlite.MarkCheckoutCompleted(ctx, merchantExternalID)
	} else {
		_, err = s.postgres.MarkCheckoutCompleted(ctx, merchantExternalID)
	}
	return normalize(err)
}

func mapSQLitePlanPrice(v sqlitedb.ListPlanPricesRow) model.PlanPrice {
	return model.PlanPrice{ID: v.ID, PlanID: v.PlanID, PlanCode: v.PlanCode, PlanName: v.PlanName, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: v.Active != 0, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func mapPostgresPlanPrice(v postgresdb.ListPlanPricesRow) model.PlanPrice {
	return model.PlanPrice{ID: v.ID, PlanID: v.PlanID, PlanCode: v.PlanCode, PlanName: v.PlanName, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: v.Active != 0, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func mapSQLiteActivePlanPrice(v sqlitedb.ListActivePlanPricesRow) model.PlanPrice {
	return model.PlanPrice{ID: v.ID, PlanID: v.PlanID, PlanCode: v.PlanCode, PlanName: v.PlanName, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: v.Active != 0, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func mapPostgresActivePlanPrice(v postgresdb.ListActivePlanPricesRow) model.PlanPrice {
	return model.PlanPrice{ID: v.ID, PlanID: v.PlanID, PlanCode: v.PlanCode, PlanName: v.PlanName, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: v.Active != 0, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func mapSQLitePlanPriceByID(v sqlitedb.GetPlanPriceByIDRow) *model.PlanPrice {
	p := model.PlanPrice{ID: v.ID, PlanID: v.PlanID, PlanCode: v.PlanCode, PlanName: v.PlanName, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: v.Active != 0, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	return &p
}
func mapPostgresPlanPriceByID(v postgresdb.GetPlanPriceByIDRow) *model.PlanPrice {
	p := model.PlanPrice{ID: v.ID, PlanID: v.PlanID, PlanCode: v.PlanCode, PlanName: v.PlanName, BillingPeriod: v.BillingPeriod, Currency: v.Currency, Amount: v.Amount, ProviderProductID: v.ProviderProductID, SyncStatus: v.SyncStatus, SyncError: v.SyncError, Active: v.Active != 0, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
	return &p
}
func mapSQLiteSubscription(v sqlitedb.TenantSubscription) *model.Subscription {
	return &model.Subscription{ID: v.ID, TenantID: v.TenantID, Provider: v.Provider, Mode: v.Mode, OrderID: v.OrderID, ProviderProductID: v.ProviderProductID, PlanPriceID: v.PlanPriceID, PlanID: v.PlanID, Status: v.Status, Currency: v.Currency, Amount: v.Amount, CurrentPeriodStart: timePtr(v.CurrentPeriodStart), CurrentPeriodEnd: timePtr(v.CurrentPeriodEnd), CancelAtPeriodEnd: v.CancelAtPeriodEnd != 0, LastEventAt: timePtr(v.LastEventAt), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func mapPostgresSubscription(v postgresdb.TenantSubscription) *model.Subscription {
	return &model.Subscription{ID: v.ID, TenantID: v.TenantID, Provider: v.Provider, Mode: v.Mode, OrderID: v.OrderID, ProviderProductID: v.ProviderProductID, PlanPriceID: v.PlanPriceID, PlanID: v.PlanID, Status: v.Status, Currency: v.Currency, Amount: v.Amount, CurrentPeriodStart: timePtr(v.CurrentPeriodStart), CurrentPeriodEnd: timePtr(v.CurrentPeriodEnd), CancelAtPeriodEnd: v.CancelAtPeriodEnd != 0, LastEventAt: timePtr(v.LastEventAt), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func mapSQLiteCheckout(v sqlitedb.BillingCheckoutSession) *model.BillingCheckout {
	return &model.BillingCheckout{ID: v.ID, TenantID: v.TenantID, PlanPriceID: v.PlanPriceID, MerchantExternalID: v.MerchantExternalID, ProviderSessionID: v.ProviderSessionID, IdempotencyKey: v.IdempotencyKey, CheckoutURL: v.CheckoutUrl, Status: v.Status, ExpiresAt: timePtr(v.ExpiresAt), CreatedAt: v.CreatedAt}
}
func mapPostgresCheckout(v postgresdb.BillingCheckoutSession) *model.BillingCheckout {
	return &model.BillingCheckout{ID: v.ID, TenantID: v.TenantID, PlanPriceID: v.PlanPriceID, MerchantExternalID: v.MerchantExternalID, ProviderSessionID: v.ProviderSessionID, IdempotencyKey: v.IdempotencyKey, CheckoutURL: v.CheckoutUrl, Status: v.Status, ExpiresAt: timePtr(v.ExpiresAt), CreatedAt: v.CreatedAt}
}

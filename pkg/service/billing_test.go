package service_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"emailbox/configs"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

// fakeWaffo 是一个进程内的 Waffo：按路径回固定响应，并记下收到的请求体。
type fakeWaffo struct {
	mu       sync.Mutex
	requests map[string][]map[string]any
	// orders 是 GraphQL 能查到的订阅订单，键为 orderMerchantExternalId。
	orders map[string]map[string]string
}

func (f *fakeWaffo) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	f.mu.Lock()
	f.requests[r.URL.Path] = append(f.requests[r.URL.Path], parsed)
	f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/actions/subscription-product/create-product":
		_, _ = w.Write([]byte(`{"data":{"product":{"id":"PROD_` + strconv.Itoa(len(f.requests[r.URL.Path])) + `"}}}`))
	case "/v1/actions/checkout/create-session":
		_, _ = w.Write([]byte(`{"data":{"sessionId":"cs_1","checkoutUrl":"https://checkout.waffo.test/s/checkout/cs_1","expiresAt":"` +
			time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339) + `"}}`))
	case "/v1/graphql":
		vars, _ := parsed["variables"].(map[string]any)
		ref, _ := vars["ref"].(string)
		f.mu.Lock()
		order, ok := f.orders[ref]
		f.mu.Unlock()
		list := []map[string]string{}
		if ok {
			list = append(list, order)
		}
		out, _ := json.Marshal(map[string]any{"data": map[string]any{"subscriptionOrders": list}})
		_, _ = w.Write(out)
	case "/v1/actions/auth/issue-session-token":
		_, _ = w.Write([]byte(`{"data":{"token":"TOKEN-` + strconv.Itoa(len(f.requests[r.URL.Path])) + `"}}`))
	default:
		_, _ = w.Write([]byte(`{"data":{}}`))
	}
}

func (f *fakeWaffo) calls(path string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[path]
}

type billingFixture struct {
	store      *repo.Store
	svc        *service.BillingService
	waffo      *fakeWaffo
	webhookKey *rsa.PrivateKey
	tenantID   string
	userID     string
	freePlan   string
	proPlan    string
	proPrice   string
}

func pemKey(t *testing.T, typ string, der []byte) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

func newBillingFixture(t *testing.T) *billingFixture {
	t.Helper()
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	ctx := context.Background()
	store := testStore(t)
	result, _, err := service.NewAuthService(store).Register(ctx, model.RegisterRequest{
		Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	limits, err := store.GetEffectiveQuota(ctx, result.Tenants[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []model.Plan{
		{ID: "plan-pro", Code: "pro", Name: "专业版", MaxAccounts: 1000, MaxGroups: 100, DailyMailFetch: 50000},
		{ID: "plan-team", Code: "team", Name: "团队版", MaxAccounts: 5000, MaxGroups: 500, DailyMailFetch: 200000},
	} {
		if err := store.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	fake := &fakeWaffo{requests: map[string][]map[string]any{}, orders: map[string]map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(server.Close)
	merchantKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	webhookKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	pubDER, _ := x509.MarshalPKIXPublicKey(&webhookKey.PublicKey)
	svc, err := service.NewBillingService(store, configs.WaffoConfig{
		Environment: "test", APIBaseURL: server.URL, MerchantID: "MER_TEST", StoreID: "STO_TEST",
		PrivateKey:     pemKey(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(merchantKey)),
		WebhookTestKey: pemKey(t, "PUBLIC KEY", pubDER),
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &billingFixture{store: store, svc: svc, waffo: fake, webhookKey: webhookKey,
		tenantID: result.Tenants[0].ID, userID: result.User.ID, freePlan: limits.PlanCode, proPlan: "pro"}
	price, err := svc.CreatePrice(ctx, model.PlanPrice{PlanID: "plan-pro", BillingPeriod: "monthly", Currency: "USD", Amount: "9.90"})
	if err != nil {
		t.Fatal(err)
	}
	if price.SyncStatus != "active" || price.ProviderProductID != "PROD_1" {
		t.Fatalf("价格没有同步成功: %+v", price)
	}
	f.proPrice = price.ID
	return f
}

// send 构造、签名并投递一个 Webhook，走与 handler 相同的「验签 → 处理」两步。
func (f *billingFixture) send(t *testing.T, eventType, eventID string, at time.Time, data map[string]any) error {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"id": eventID, "eventId": eventID, "eventType": eventType, "storeId": "STO_TEST", "mode": "test",
		"timestamp": at.UTC().Format("2006-01-02T15:04:05.000Z"), "data": data,
	})
	if err := f.svc.VerifyWebhook(f.sign(t, body, time.Now()), body, time.Now()); err != nil {
		return err
	}
	return f.svc.ProcessWebhook(context.Background(), body)
}

func (f *billingFixture) sign(t *testing.T, body []byte, at time.Time) string {
	t.Helper()
	ts := strconv.FormatInt(at.UnixMilli(), 10)
	digest := sha256.Sum256([]byte(ts + "." + string(body)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.webhookKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return "t=" + ts + ",v1=" + base64.StdEncoding.EncodeToString(sig)
}

// subscriptionData 仿照 Waffo 文档里 subscription.activated 的 data：周期是纯日期。
func (f *billingFixture) subscriptionData(orderID, priceID string) map[string]any {
	return map[string]any{
		"orderId": orderID, "orderStatus": "active", "currency": "USD", "amount": "9.90",
		"merchantProvidedBuyerIdentity": f.tenantID,
		"orderMetadata":                 map[string]string{"tenant_id": f.tenantID, "plan_price_id": priceID},
		"productMetadata":               map[string]string{"plan_price_id": priceID},
		"planPrice":                     map[string]string{"total": "9.90"},
		"currentPeriodStart":            "2026-09-27", "currentPeriodEnd": "2026-10-27",
	}
}

func (f *billingFixture) planOf(t *testing.T) string {
	t.Helper()
	limits, err := f.store.GetEffectiveQuota(context.Background(), f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	return limits.PlanCode
}

func (f *billingFixture) subscription(t *testing.T) *model.Subscription {
	t.Helper()
	sub, err := f.svc.GetSubscription(context.Background(), f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

// 买家拿到的地址带顾客令牌，库里存的只有裸地址；重复提交同一个键复用会话、换新令牌。
func TestCheckoutKeepsBuyerTokenOutOfDatabase(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	settings, err := f.svc.UpdateSettings(ctx, model.BillingSettings{Enabled: true, DefaultCurrency: "USD"}, f.userID)
	if err != nil {
		t.Fatalf("打开支付失败: %v", err)
	}
	// 配置齐全时 missing 是空数组而不是 null：前端直接读 missing.length。
	if encoded, _ := json.Marshal(settings); !strings.Contains(string(encoded), `"missing":[]`) {
		t.Errorf("missing 没有序列化成空数组: %s", encoded)
	}

	first, err := f.svc.CreateCheckout(ctx, f.tenantID, f.userID, f.proPrice, "key-1", "https://mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(first.CheckoutURL, "?test=true#token=TOKEN-1") {
		t.Errorf("结账地址 = %s，期望带 ?test=true 与片段里的令牌", first.CheckoutURL)
	}
	stored, err := f.store.GetCheckoutByIdempotency(ctx, f.tenantID, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.CheckoutURL, "token") {
		t.Errorf("令牌被写进了数据库: %s", stored.CheckoutURL)
	}

	second, err := f.svc.CreateCheckout(ctx, f.tenantID, f.userID, f.proPrice, "key-1", "https://mail.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.waffo.calls("/v1/actions/checkout/create-session")); n != 1 {
		t.Errorf("同一个幂等键创建了 %d 个会话，期望复用 1 个", n)
	}
	if !strings.HasSuffix(second.CheckoutURL, "#token=TOKEN-2") {
		t.Errorf("复用会话时没有换新令牌: %s", second.CheckoutURL)
	}

	session := f.waffo.calls("/v1/actions/checkout/create-session")[0]
	meta, _ := session["metadata"].(map[string]any)
	if meta["tenant_id"] != f.tenantID || session["buyerEmail"] != "alice@example.com" {
		t.Errorf("会话参数不对: %v", session)
	}
	// 没配 WAFFO_SUCCESS_URL 时按请求域名推导；本地 http 开发不带（Waffo 付款时会拒绝非 https）。
	if session["successUrl"] != "https://mail.example.com/settings/usage?billing=success" {
		t.Errorf("successUrl = %v，期望按请求域名推导", session["successUrl"])
	}
	if _, err := f.svc.CreateCheckout(ctx, f.tenantID, f.userID, f.proPrice, "key-2", "http://localhost:5173"); err != nil {
		t.Fatal(err)
	}
	if local := f.waffo.calls("/v1/actions/checkout/create-session")[1]; local["successUrl"] != nil {
		t.Errorf("http 站点不该带 successUrl，实际 %v", local["successUrl"])
	}
	token := f.waffo.calls("/v1/actions/auth/issue-session-token")[0]
	if token["buyerIdentity"] != f.tenantID {
		t.Errorf("buyerIdentity = %v，期望租户 ID", token["buyerIdentity"])
	}
}

// 激活授予套餐、取消回收套餐；activated 与 payment_succeeded 共用 eventId 时两个都要处理。
func TestWebhookActivationAndCancellation(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	data := f.subscriptionData("ORD_A", f.proPrice)
	if err := f.send(t, "subscription.activated", "PAY_1", now, data); err != nil {
		t.Fatalf("处理激活事件失败: %v", err)
	}
	if got := f.planOf(t); got != f.proPlan {
		t.Fatalf("激活后套餐 = %s，期望 %s", got, f.proPlan)
	}
	sub := f.subscription(t)
	if sub.Status != "active" || sub.CurrentPeriodEnd == nil || sub.CurrentPeriodEnd.Format("2006-01-02") != "2026-10-27" {
		t.Errorf("订阅记录不对: %+v", sub)
	}

	if err := f.send(t, "subscription.payment_succeeded", "PAY_1", now, data); err != nil {
		t.Fatal(err)
	}
	status, err := f.store.GetWebhookEventStatus(context.Background(), "subscription.payment_succeeded", "PAY_1", "test")
	if err != nil || status != "processed" {
		t.Errorf("同 eventId 的 payment_succeeded 被当成重复丢掉了: %q %v", status, err)
	}

	if err := f.send(t, "subscription.canceled", "ORD_A-cancel", now.Add(time.Minute), data); err != nil {
		t.Fatal(err)
	}
	if got := f.planOf(t); got != f.freePlan {
		t.Errorf("取消后套餐 = %s，期望回到 %s", got, f.freePlan)
	}
	if f.subscription(t).Status != "canceled" {
		t.Error("订阅状态没有变成 canceled")
	}
}

// Waffo 的换套餐是「旧单取消 + 新单创建」，旧单的 canceled 可能最后才到。
// 它不能把刚付完新单的用户降回免费套餐；新单即使丢了 orderMetadata
// （结账页自愈重建会话），也要靠 buyerIdentity 与 productMetadata 找回归属。
func TestWebhookPlanChangeSurvivesOutOfOrderEvents(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	team, err := f.svc.CreatePrice(ctx, model.PlanPrice{PlanID: "plan-team", BillingPeriod: "monthly", Currency: "USD", Amount: "29"})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now().Add(-time.Hour)
	if err := f.send(t, "subscription.activated", "PAY_A", t0, f.subscriptionData("ORD_A", f.proPrice)); err != nil {
		t.Fatal(err)
	}
	healed := f.subscriptionData("ORD_B", team.ID)
	delete(healed, "orderMetadata")
	if err := f.send(t, "subscription.activated", "PAY_B", t0.Add(20*time.Minute), healed); err != nil {
		t.Fatal(err)
	}
	// 旧单迟到的 renewed 与 canceled：前者不能把新单顶掉，后者不能回收新单的套餐。
	if err := f.send(t, "subscription.renewed", "PAY_A2", t0.Add(10*time.Minute), f.subscriptionData("ORD_A", f.proPrice)); err != nil {
		t.Fatal(err)
	}
	if err := f.send(t, "subscription.canceled", "ORD_A-x", t0.Add(30*time.Minute), f.subscriptionData("ORD_A", f.proPrice)); err != nil {
		t.Fatal(err)
	}
	sub := f.subscription(t)
	if sub.OrderID != "ORD_B" || sub.Status != "active" {
		t.Errorf("订阅 = %s/%s，期望 ORD_B/active", sub.OrderID, sub.Status)
	}
	if got := f.planOf(t); got != "team" {
		t.Errorf("套餐 = %s，期望 team", got)
	}
}

// 同一订单里时间更早的事件是旧事件：先到的 activated 不能被迟到的 canceling 覆盖。
func TestWebhookIgnoresStaleEventForSameOrder(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	data := f.subscriptionData("ORD_A", f.proPrice)
	if err := f.send(t, "subscription.uncanceled", "E2", now, data); err != nil {
		t.Fatal(err)
	}
	if err := f.send(t, "subscription.canceling", "E1", now.Add(-time.Minute), data); err != nil {
		t.Fatal(err)
	}
	if got := f.subscription(t).Status; got != "active" {
		t.Errorf("状态 = %s，迟到的旧事件覆盖了新状态", got)
	}
}

// 管理员在订阅期间手工换的套餐，订阅取消时不能被回收成默认套餐。
func TestAdminPlanSurvivesSubscriptionCancellation(t *testing.T) {
	f := newBillingFixture(t)
	now := time.Now()
	data := f.subscriptionData("ORD_A", f.proPrice)
	if err := f.send(t, "subscription.activated", "PAY_1", now, data); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateTenantPlan(context.Background(), f.tenantID, "plan-team"); err != nil {
		t.Fatal(err)
	}
	if err := f.send(t, "subscription.canceled", "C1", now.Add(time.Minute), data); err != nil {
		t.Fatal(err)
	}
	if got := f.planOf(t); got != "team" {
		t.Errorf("套餐 = %s，管理员手工设的套餐被回收了", got)
	}
}

// 只接受本 Store、本环境、签名正确的事件。
func TestWebhookRejectsForeignOrForgedEvents(t *testing.T) {
	f := newBillingFixture(t)
	envelope := func(store, mode string) []byte {
		b, _ := json.Marshal(map[string]any{"eventId": "E", "eventType": "subscription.activated", "storeId": store, "mode": mode, "data": map[string]any{}})
		return b
	}
	now := time.Now()
	cases := map[string]error{
		"别的 Store": f.svc.VerifyWebhook(f.sign(t, envelope("STO_OTHER", "test"), now), envelope("STO_OTHER", "test"), now),
		"生产环境事件":   f.svc.VerifyWebhook(f.sign(t, envelope("STO_TEST", "prod"), now), envelope("STO_TEST", "prod"), now),
		"篡改后的正文":   f.svc.VerifyWebhook(f.sign(t, envelope("STO_TEST", "test"), now), envelope("STO_TEST", "test "), now),
	}
	for name, err := range cases {
		if !errors.Is(err, service.ErrWebhookRejected) {
			t.Errorf("%s: err = %v，期望 ErrWebhookRejected", name, err)
		}
	}
}

// 租户归属各路来源互相矛盾时拒绝处理，并且不能重试（回 2xx 让 Waffo 停手）。
func TestWebhookRejectsConflictingOwnership(t *testing.T) {
	f := newBillingFixture(t)
	data := f.subscriptionData("ORD_A", f.proPrice)
	data["merchantProvidedBuyerIdentity"] = "someone-else"
	err := f.send(t, "subscription.activated", "PAY_1", time.Now(), data)
	if !errors.Is(err, service.ErrWebhookUnprocessable) {
		t.Fatalf("err = %v，期望 ErrWebhookUnprocessable", err)
	}
	if got := f.planOf(t); got != f.freePlan {
		t.Errorf("归属冲突的事件改了套餐: %s", got)
	}
}

// 只改金额不能顺手下架；已同步的价格不能改币种；同步请求带上本地价格 ID。
func TestUpdatePriceRules(t *testing.T) {
	f := newBillingFixture(t)
	ctx := context.Background()
	updated, err := f.svc.UpdatePrice(ctx, f.proPrice, "12.00", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Active || updated.Amount != "12.00" || updated.SyncStatus != "active" {
		t.Errorf("改价结果不对: %+v", updated)
	}
	calls := f.waffo.calls("/v1/actions/subscription-product/update-product")
	if len(calls) != 1 {
		t.Fatalf("改价应同步一次，实际 %d 次", len(calls))
	}
	if meta, _ := calls[0]["metadata"].(map[string]any); meta["plan_price_id"] != f.proPrice {
		t.Errorf("同步请求缺少 plan_price_id: %v", calls[0])
	}
	if _, err := f.svc.UpdatePrice(ctx, f.proPrice, "12.00", "EUR", nil); !errors.Is(err, service.ErrInvalidPrice) {
		t.Errorf("改币种 err = %v，期望 ErrInvalidPrice", err)
	}
	for _, amount := range []string{"0", "1.234", "abc", "-1"} {
		if _, err := f.svc.UpdatePrice(ctx, f.proPrice, amount, "", nil); !errors.Is(err, service.ErrInvalidPrice) {
			t.Errorf("金额 %q 没有被拒绝: %v", amount, err)
		}
	}
}

// 凭据要么不配（支付不可用、服务照常启动，后台指明缺哪几个变量），要么必须能用：
// 私钥写坏了要在启动时失败，而不是等管理员在后台看到一句「缺少配置」。
func TestBillingServiceConfiguration(t *testing.T) {
	store := testStore(t)
	svc, err := service.NewBillingService(store, configs.WaffoConfig{Environment: "test"})
	if err != nil {
		t.Fatalf("未配置 Waffo 时不应启动失败: %v", err)
	}
	settings, err := svc.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Missing) == 0 || !strings.Contains(settings.Missing[0], "WAFFO_PRIVATE_KEY") {
		t.Errorf("缺失项没有指明环境变量: %v", settings.Missing)
	}
	_, err = service.NewBillingService(store, configs.WaffoConfig{
		Environment: "test", APIBaseURL: "https://api.waffo.ai", MerchantID: "MER_X", StoreID: "STO_X", PrivateKey: "not-a-key"})
	if err == nil {
		t.Error("私钥无法解析时应当启动失败")
	}
	if _, err := service.NewBillingService(store, configs.WaffoConfig{Environment: "test", SuccessURL: "http://localhost/ok"}); err == nil {
		t.Error("显式配置的非 https 回跳地址应当启动失败")
	}
}

// startPaidCheckout 发起一次结账，并让 Waffo 侧「付款成功」：GraphQL 能查到对应的生效订单。
func (f *billingFixture) startPaidCheckout(t *testing.T, orderID string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.UpdateSettings(ctx, model.BillingSettings{Enabled: true, DefaultCurrency: "USD"}, f.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateCheckout(ctx, f.tenantID, f.userID, f.proPrice, "k-"+orderID, "http://localhost:5173"); err != nil {
		t.Fatal(err)
	}
	checkout, err := f.store.GetCheckoutByIdempotency(ctx, f.tenantID, "k-"+orderID)
	if err != nil {
		t.Fatal(err)
	}
	f.waffo.mu.Lock()
	f.waffo.orders[checkout.MerchantExternalID] = map[string]string{
		"id": orderID, "status": "active", "orderMerchantExternalId": checkout.MerchantExternalID,
		"currentPeriodStart": "2026-09-27", "currentPeriodEnd": "2026-10-27",
		"createdAt": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	}
	f.waffo.mu.Unlock()
	return checkout.MerchantExternalID
}

// Webhook 到不了（本地开发、回调丢失）时，打开用量页的对账也要把已付款的订阅入账；
// 入账后结账标为完成，之后的对账不再请求 Waffo。
func TestSyncGrantsPlanWithoutWebhook(t *testing.T) {
	f := newBillingFixture(t)
	f.startPaidCheckout(t, "ORD_S")

	sub, changed, err := f.svc.SyncCheckouts(context.Background(), f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || sub == nil || sub.OrderID != "ORD_S" || sub.Status != "active" {
		t.Fatalf("对账结果 = %+v changed=%v，期望 ORD_S/active 且有变化", sub, changed)
	}
	if got := f.planOf(t); got != f.proPlan {
		t.Errorf("对账后套餐 = %s，期望 %s", got, f.proPlan)
	}

	queries := len(f.waffo.calls("/v1/graphql"))
	if _, changed, err := f.svc.SyncCheckouts(context.Background(), f.tenantID); err != nil || changed {
		t.Errorf("第二次对账 changed=%v err=%v，期望无变化", changed, err)
	}
	if n := len(f.waffo.calls("/v1/graphql")); n != queries {
		t.Errorf("结账已完成，第二次对账不该再请求 Waffo（多了 %d 次）", n-queries)
	}
}

// Webhook 先到、对账后到：对账报告「无变化」并把结账标为完成。
// 曾经的写法会把被乱序保护跳过的那次也算成变化，前端据此重取、再对账，转个不停。
func TestSyncAfterWebhookReportsNoChange(t *testing.T) {
	f := newBillingFixture(t)
	ref := f.startPaidCheckout(t, "ORD_W")
	data := f.subscriptionData("ORD_W", f.proPrice)
	delete(data, "orderMerchantExternalId")
	if err := f.send(t, "subscription.activated", "PAY_W", time.Now(), data); err != nil {
		t.Fatal(err)
	}

	_, changed, err := f.svc.SyncCheckouts(context.Background(), f.tenantID)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v，Webhook 已入账时对账不应报告变化", changed, err)
	}
	checkout, err := f.store.GetCheckoutByMerchantExternalID(context.Background(), ref)
	if err != nil || checkout.Status != "completed" {
		t.Errorf("结账状态 = %v %v，期望 completed", checkout, err)
	}
}

// Webhook 没到时，第二次购买前的对账要发现第一笔已经生效并拦下，不能让用户背两份订阅。
func TestCheckoutSyncsBeforeAllowingSecondPurchase(t *testing.T) {
	f := newBillingFixture(t)
	f.startPaidCheckout(t, "ORD_1")
	_, err := f.svc.CreateCheckout(context.Background(), f.tenantID, f.userID, f.proPrice, "second", "http://localhost:5173")
	if !errors.Is(err, service.ErrSubscriptionExists) {
		t.Fatalf("err = %v，期望 ErrSubscriptionExists", err)
	}
	if got := f.planOf(t); got != f.proPlan {
		t.Errorf("对账后套餐 = %s，期望 %s", got, f.proPlan)
	}
}

// 默认套餐不能标价：每个人注册即有，订阅取消后也回落到它。
func TestDefaultPlanCannotBePriced(t *testing.T) {
	f := newBillingFixture(t)
	plan, err := f.store.GetDefaultPlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreatePrice(context.Background(), model.PlanPrice{
		PlanID: plan.ID, BillingPeriod: "monthly", Currency: "USD", Amount: "2"})
	if !errors.Is(err, service.ErrInvalidPrice) {
		t.Errorf("err = %v，期望 ErrInvalidPrice", err)
	}
	// 之前已经标过价的也不能出现在可购列表里。
	if err := f.store.CreatePlanPrice(context.Background(), model.PlanPrice{ID: "legacy", PlanID: plan.ID,
		BillingPeriod: "monthly", Currency: "USD", Amount: "2", SyncStatus: "active", Active: true}); err != nil {
		t.Fatal(err)
	}
	prices, err := f.svc.ListPrices(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prices {
		if p.PlanID == plan.ID {
			t.Errorf("默认套餐的旧价格出现在可购列表里: %+v", p)
		}
	}
}

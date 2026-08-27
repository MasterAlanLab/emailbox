package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"emailbox/pkg/job"
	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

// stubRefresher 按邮箱前缀决定结果，用例因此能精确构造
// 「成功 / 认证失败 / 被封 / 代理故障」的任意组合，而不必真的打微软。
type stubRefresher struct{ account *model.MailAccount }

func newStubRefresher(a *model.MailAccount) service.TokenRefresher {
	return stubRefresher{account: a}
}

func (s stubRefresher) RefreshToken(ctx context.Context, cred mailer.Credential) error {
	switch {
	case strings.HasPrefix(cred.Email, "slow"):
		// 慢账号让「停止」有确定的时间窗可测。真实的一次令牌交换是几百毫秒到几秒，
		// 桩里瞬间返回的话，30 个账号会在停止请求落库之前就跑完。
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return nil
		}
	case strings.HasPrefix(cred.Email, "banned"):
		return mailer.NewError(mailer.ErrKindBanned, mailer.ChannelGraph, "账号已被封禁", nil)
	case strings.HasPrefix(cred.Email, "authfail"):
		return mailer.NewError(mailer.ErrKindAuthFailed, mailer.ChannelGraph, "refresh_token 已失效", nil)
	case strings.HasPrefix(cred.Email, "proxyfail"):
		return mailer.NewError(mailer.ErrKindProxyFailed, mailer.ChannelGraph, "代理不可用", nil)
	default:
		return nil
	}
}

// seedRefreshableAccounts 建 n 个带 refresh_token 的账号，邮箱前缀由调用方给。
func seedRefreshableAccounts(t *testing.T, e *echo.Echo, token, tenantID string, prefixes ...string) {
	t.Helper()
	for i, prefix := range prefixes {
		createAccount(t, e, token, tenantID, fmt.Sprintf(
			`{"email":"%s%02d@outlook.com","password":"pw","refresh_token":"M.token"}`, prefix, i))
	}
}

// waitForJob 轮询到任务终结为止，返回最终的任务体。
func waitForJob(t *testing.T, e *echo.Echo, token, tenantID, jobID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, body := do(t, e, http.MethodGet,
			mailPath(tenantID, "/jobs/"+jobID), token, "")
		if status != http.StatusOK {
			t.Fatalf("查询任务失败: %d %s", status, body)
		}
		var payload struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		if model.IsTerminalJobStatus(payload.Data["status"].(string)) {
			return payload.Data
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("任务 %s 在 30 秒内没有结束", jobID)
	return nil
}

// submitBatch 提交一个批量刷新任务并返回 job_id。
func submitBatch(t *testing.T, e *echo.Echo, token, tenantID string) string {
	t.Helper()
	body := `{"scope":"all"}`
	status, resp := do(t, e, http.MethodPost, mailPath(tenantID, "/jobs/token-refresh"), token, body)
	if status != http.StatusOK {
		t.Fatalf("提交任务失败: %d %s", status, resp)
	}
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.ID
}

// 批量刷新跑通：成功与失败各归各类，任务终态是 partial。
func TestBatchRefreshClassifiesResults(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "ok", "ok", "banned", "authfail", "proxyfail")

	jobID := submitBatch(t, e, token, tenantID)
	job := waitForJob(t, e, token, tenantID, jobID)

	if job["status"] != model.JobStatusPartial {
		t.Errorf("终态 = %v，期望 partial", job["status"])
	}
	if got := job["success_count"].(float64); got != 2 {
		t.Errorf("成功 %v 个，期望 2 个", got)
	}
	if got := job["failed_count"].(float64); got != 3 {
		t.Errorf("失败 %v 个，期望 3 个", got)
	}

	// 逐项结果里要能看出每个账号是因为什么失败的——
	// banned 要联系服务商、auth_failed 要重新授权、proxy_failed 要查代理配置，
	// 三者的处置完全不同，混成一个「失败」等于什么都没说。
	status, body := do(t, e, http.MethodGet,
		mailPath(tenantID, "/jobs/"+jobID+"/items?status=failed"), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询明细失败: %d %s", status, body)
	}
	for _, kind := range []string{"banned", "auth_failed", "proxy_failed"} {
		if !strings.Contains(body, `"error_kind":"`+kind+`"`) {
			t.Errorf("明细里缺少 error_kind=%s: %s", kind, body)
		}
	}
}

// 被识别为 banned 的账号要立刻置状态，后续请求在进协议层之前就被挡下。
func TestBannedAccountIsMarkedAfterRefresh(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "banned")

	jobID := submitBatch(t, e, token, tenantID)
	waitForJob(t, e, token, tenantID, jobID)

	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/accounts"), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询账号失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"status":"banned"`) {
		t.Errorf("被封账号应当已置为 banned: %s", body)
	}
}

// P4 验收：并发写 jobs 计数不能丢增量。
//
// 计数走的是相对 UPDATE（success_count = success_count + 1）而不是读改写，
// 后者在 N 个 worker 同时完成时会丢增量，而且丢多少取决于时序。
// 这条用例配合 -race 一起跑。
func TestJobCountersDoNotLoseIncrements(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	const total = 40
	prefixes := make([]string, 0, total)
	for i := range total {
		// 一半成功一半失败，两个计数器都参与并发累加
		if i%2 == 0 {
			prefixes = append(prefixes, "ok")
		} else {
			prefixes = append(prefixes, "authfail")
		}
	}
	seedRefreshableAccounts(t, e, token, tenantID, prefixes...)

	jobID := submitBatch(t, e, token, tenantID)
	job := waitForJob(t, e, token, tenantID, jobID)

	success, failed := job["success_count"].(float64), job["failed_count"].(float64)
	if success+failed != total {
		t.Errorf("成功 %v + 失败 %v = %v，期望 %d（说明并发累加丢了增量）",
			success, failed, success+failed, total)
	}
	if success != total/2 || failed != total/2 {
		t.Errorf("成功 %v / 失败 %v，期望各 %d", success, failed, total/2)
	}
}

// 任务只能被所属租户看到。job_items 上没有 tenant_id 列，
// 少了 service 层那道「先确认任务属于本租户」就等于知道 job_id 就能看别人的明细。
func TestJobsAreIsolatedAcrossTenants(t *testing.T) {
	e := newTestServer(t)
	aliceToken, aliceTenant := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, aliceToken, aliceTenant, "ok")
	jobID := submitBatch(t, e, aliceToken, aliceTenant)
	waitForJob(t, e, aliceToken, aliceTenant, jobID)

	bobToken, bobTenant := register(t, e, "bob", "bob@example.com")

	// 用自己的租户 ID 猜别人的 job_id
	for _, path := range []string{
		"/jobs/" + jobID,
		"/jobs/" + jobID + "/items",
		"/jobs/" + jobID + "/stream",
	} {
		status, _ := do(t, e, http.MethodGet, mailPath(bobTenant, path), bobToken, "")
		if status != http.StatusNotFound {
			t.Errorf("%s: 跨租户拿到 %d，期望 404", path, status)
		}
	}
	// 换成别人的租户 ID 则连租户成员校验都过不去
	status, _ := do(t, e, http.MethodGet, mailPath(aliceTenant, "/jobs/"+jobID), bobToken, "")
	if status != http.StatusForbidden {
		t.Errorf("非成员访问他人租户拿到 %d，期望 403", status)
	}
}

// P4 验收：强杀进程后重启，僵尸任务被标为 interrupted。
//
// 直接改库模拟「心跳停在很久以前」，再调 ReapStale——这正是 main.go 启动时做的事。
func TestStaleJobIsMarkedInterrupted(t *testing.T) {
	e, store, db := newTestServerWithStore(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "ok")

	jobID := submitBatch(t, e, token, tenantID)
	waitForJob(t, e, token, tenantID, jobID)

	// 把这条任务打回 running 并把心跳推回一小时前，等价于「进程在跑到一半时被杀」
	if _, err := db.Exec(
		`UPDATE jobs SET status = 'running', heartbeat_at = ?, finished_at = NULL WHERE id = ?`,
		time.Now().Add(-time.Hour), jobID); err != nil {
		t.Fatal(err)
	}

	reaped := reapStale(t, store)
	if reaped != 1 {
		t.Fatalf("回收了 %d 个僵尸任务，期望 1 个", reaped)
	}

	got := fetchJob(t, e, token, tenantID, jobID)
	if got["status"] != model.JobStatusInterrupted {
		t.Errorf("状态 = %v，期望 interrupted", got["status"])
	}
	if got["error_summary"] == "" {
		t.Error("interrupted 的任务应当说明原因")
	}
}

// SSE 要支持 last_event_id 续看：断线重连后只补发缺的那部分，而不是从头再来。
func TestJobStreamReplaysFromLastEventID(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "ok", "ok", "ok")

	jobID := submitBatch(t, e, token, tenantID)
	waitForJob(t, e, token, tenantID, jobID)

	// 任务已终结，流会把事件发完就关闭，因此这里不会挂住。
	full := doRaw(t, e, mailPath(tenantID, "/jobs/"+jobID+"/stream"), token).Body.String()
	if !strings.Contains(full, "event: started") || !strings.Contains(full, "event: finished") {
		t.Fatalf("完整流里应当同时有 started 与 finished:\n%s", full)
	}
	if !strings.Contains(full, "id: 1") {
		t.Errorf("事件应当带自增 id 供续看:\n%s", full)
	}

	// 从中间续看：seq 之前的事件不该再出现
	resumed := doRaw(t, e,
		mailPath(tenantID, "/jobs/"+jobID+"/stream?last_event_id=2"), token).Body.String()
	if strings.Contains(resumed, "id: 1\n") || strings.Contains(resumed, "id: 2\n") {
		t.Errorf("续看不该重发已确认的事件:\n%s", resumed)
	}
	if !strings.Contains(resumed, "event: finished") {
		t.Errorf("续看仍应收到 finished:\n%s", resumed)
	}
}

// 停止请求：正在处理的账号会跑完，还没轮到的标为 skipped，任务终态是 stopped。
func TestStoppedJobSkipsRemainingAccounts(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	// 每个账号 50ms、4 个 worker，30 个账号要 375ms 以上，
	// 停止请求有充足的时间窗落库。
	prefixes := make([]string, 0, 30)
	for range 30 {
		prefixes = append(prefixes, "slow")
	}
	seedRefreshableAccounts(t, e, token, tenantID, prefixes...)

	jobID := submitBatch(t, e, token, tenantID)
	if status, body := do(t, e, http.MethodPost,
		mailPath(tenantID, "/jobs/"+jobID+"/stop"), token, ""); status != http.StatusOK {
		t.Fatalf("停止失败: %d %s", status, body)
	}

	job := waitForJob(t, e, token, tenantID, jobID)
	if job["status"] != model.JobStatusStopped {
		t.Fatalf("终态 = %v，期望 stopped", job["status"])
	}
	if job["error_summary"] == "" {
		t.Error("stopped 的任务应当说明处理与跳过的数量")
	}

	// 还没轮到的那些要落成 skipped，而不是留在 pending——
	// 留在 pending 的话，任务已经结束了明细却显示「排队中」，没人看得懂。
	status, body := do(t, e, http.MethodGet,
		mailPath(tenantID, "/jobs/"+jobID+"/items?status=pending"), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询明细失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"total":0`) {
		t.Errorf("停止后不该还有 pending 的明细: %s", body)
	}

	status, body = do(t, e, http.MethodGet,
		mailPath(tenantID, "/jobs/"+jobID+"/items?status=skipped"), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询明细失败: %d %s", status, body)
	}
	if strings.Contains(body, `"total":0`) {
		t.Errorf("停止后应当有被跳过的明细: %s", body)
	}
}

// 刷新统计要按 error_kind 聚合，这是「一批账号被封」与「代理挂了」的区分依据。
func TestRefreshStatsGroupsByErrorKind(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "ok", "banned", "banned", "proxyfail")

	jobID := submitBatch(t, e, token, tenantID)
	waitForJob(t, e, token, tenantID, jobID)

	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/refresh/stats"), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询统计失败: %d %s", status, body)
	}
	var payload struct {
		Data struct {
			Total       int            `json:"total"`
			Success     int            `json:"success"`
			Failed      int            `json:"failed"`
			ByErrorKind map[string]int `json:"by_error_kind"`
			LastJob     *struct {
				ID string `json:"id"`
			} `json:"last_job"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Total != 4 {
		t.Errorf("总数 %d，期望 4", payload.Data.Total)
	}
	if payload.Data.Success != 1 || payload.Data.Failed != 3 {
		t.Errorf("成功 %d 失败 %d，期望 1 / 3", payload.Data.Success, payload.Data.Failed)
	}
	if payload.Data.ByErrorKind["banned"] != 2 {
		t.Errorf("banned 计数 = %d，期望 2（%v）", payload.Data.ByErrorKind["banned"], payload.Data.ByErrorKind)
	}
	if payload.Data.ByErrorKind["proxy_failed"] != 1 {
		t.Errorf("proxy_failed 计数 = %d，期望 1", payload.Data.ByErrorKind["proxy_failed"])
	}
	if payload.Data.LastJob == nil || payload.Data.LastJob.ID != jobID {
		t.Error("统计里应当带上最近一次任务")
	}
}

// 令牌刷新**没有额度**（000013 起）：它是「账号还能不能用」的前提，
// 卡住它等于让用户的账号批量失效。但用量仍然照记——用量页上那个数字是
// 「是不是有脚本在空转」的唯一线索。
func TestBatchRefreshHasNoQuotaButStillCounts(t *testing.T) {
	e, _, _ := newTestServerWithStore(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "ok", "ok", "ok")

	status, body := do(t, e, http.MethodPost,
		mailPath(tenantID, "/jobs/token-refresh"), token, `{"scope":"all"}`)
	if status != http.StatusOK {
		t.Fatalf("提交拿到 %d，期望 200（%s）", status, body)
	}

	// 3 个账号 → 用量 +3
	_, usage := do(t, e, http.MethodGet, "/api/v1/tenants/"+tenantID+"/quota", token, "")
	var payload struct {
		Data struct {
			Usage struct {
				TokenRefresh int `json:"token_refresh"`
			} `json:"usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(usage), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Usage.TokenRefresh != 3 {
		t.Errorf("刷新用量 = %d，期望 3（%s）", payload.Data.Usage.TokenRefresh, usage)
	}
}

// 单个刷新是同步的，直接返回结果。
func TestSingleAccountRefresh(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	accountID := createAccount(t, e, token, tenantID,
		`{"email":"ok@outlook.com","password":"pw","refresh_token":"M.token"}`)

	if status, body := do(t, e, http.MethodPost,
		mailPath(tenantID, "/accounts/"+accountID+"/token/refresh"), token, ""); status != http.StatusOK {
		t.Fatalf("单个刷新失败: %d %s", status, body)
	}

	failID := createAccount(t, e, token, tenantID,
		`{"email":"authfail@outlook.com","password":"pw","refresh_token":"M.token"}`)
	status, body := do(t, e, http.MethodPost,
		mailPath(tenantID, "/accounts/"+failID+"/token/refresh"), token, "")
	if status == http.StatusOK {
		t.Errorf("失效令牌不该返回成功: %s", body)
	}
	if !strings.Contains(body, "auth_failed") {
		t.Errorf("响应应当带上错误分类: %s", body)
	}
}

// 按分组刷新只覆盖该分组下的账号——令牌页上「刷新指定分组」这条路的全部意义
// 就在于不去动其它批次的账号（每个账号都是一次上游调用）。
func TestBatchRefreshByGroup(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	groupID := createGroup(t, e, token, tenantID, `{"name":"批次A"}`)
	for i := range 2 {
		createAccount(t, e, token, tenantID, fmt.Sprintf(
			`{"email":"in%02d@outlook.com","password":"pw","refresh_token":"M.token","group_id":"%s"}`,
			i, groupID))
	}
	// 落在默认分组里的账号，不该被这次任务带上。
	seedRefreshableAccounts(t, e, token, tenantID, "out", "out")

	status, resp := do(t, e, http.MethodPost, mailPath(tenantID, "/jobs/token-refresh"),
		token, fmt.Sprintf(`{"scope":"group","group_ids":["%s"]}`, groupID))
	if status != http.StatusOK {
		t.Fatalf("提交分组刷新拿到 %d，期望 200（%s）", status, resp)
	}
	var payload struct {
		Data struct {
			ID         string `json:"id"`
			TotalCount int    `json:"total_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.TotalCount != 2 {
		t.Fatalf("任务包含 %d 个账号，期望 2 个（只有分组内的）", payload.Data.TotalCount)
	}

	job := waitForJob(t, e, token, tenantID, payload.Data.ID)
	if got := job["success_count"].(float64); got != 2 {
		t.Errorf("成功 %v 个，期望 2 个", got)
	}
}

// 别人的（或者根本不存在的）分组 ID 不能用来提交任务：否则它就成了一个
// 「这个 ID 下有没有账号」的探测口。
func TestBatchRefreshRejectsForeignGroup(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	otherToken, otherTenantID := register(t, e, "bob", "bob@example.com")
	seedRefreshableAccounts(t, e, token, tenantID, "ok")

	foreign := createGroup(t, e, otherToken, otherTenantID, `{"name":"别人的分组"}`)
	status, body := do(t, e, http.MethodPost, mailPath(tenantID, "/jobs/token-refresh"),
		token, fmt.Sprintf(`{"scope":"group","group_ids":["%s"]}`, foreign))
	if status == http.StatusOK {
		t.Fatalf("用别的租户的分组提交不该成功: %s", body)
	}
}

func fetchJob(t *testing.T, e *echo.Echo, token, tenantID, jobID string) map[string]any {
	t.Helper()
	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/jobs/"+jobID), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询任务失败: %d %s", status, body)
	}
	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}

// reapStale 用一个独立的 Manager 跑一次僵尸回收，等价于 main.go 启动时那一步。
func reapStale(t *testing.T, store *repo.Store) int {
	t.Helper()
	manager := jobManagerForReap(store)
	n, err := manager.ReapStale(context.Background())
	if err != nil {
		t.Fatalf("回收僵尸任务失败: %v", err)
	}
	return n
}

// jobManagerForReap 造一个只用来做僵尸回收的 Manager。
// StaleAfter 压到 1 秒，用例才不用等真实的两分钟阈值。
func jobManagerForReap(store *repo.Store) *job.Manager {
	return job.New(store, job.Config{Workers: 1, StaleAfter: time.Second})
}

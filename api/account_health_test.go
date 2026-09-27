package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
)

type healthStats struct {
	Total         int            `json:"total"`
	OK            int            `json:"ok"`
	Invalid       int            `json:"invalid"`
	Error         int            `json:"error"`
	Unknown       int            `json:"unknown"`
	InvalidByKind map[string]int `json:"invalid_by_kind"`
}

func fetchHealth(t *testing.T, e *echo.Echo, token, tenantID string) healthStats {
	t.Helper()
	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/account-health"), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询健康分布失败: %d %s", status, body)
	}
	var payload struct {
		Data healthStats `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data
}

// submitCheck 提交一个检测全部账号的任务并返回 job_id。
func submitCheck(t *testing.T, e *echo.Echo, token, tenantID string) string {
	t.Helper()
	status, resp := do(t, e, http.MethodPost, mailPath(tenantID, "/jobs/account-check"), token, `{"scope":"all"}`)
	if status != http.StatusOK {
		t.Fatalf("提交检测任务失败: %d %s", status, resp)
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

func accountHealth(t *testing.T, e *echo.Echo, token, tenantID, accountID string) (string, string) {
	t.Helper()
	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/accounts/"+accountID), token, "")
	if status != http.StatusOK {
		t.Fatalf("查询账号失败: %d %s", status, body)
	}
	var payload struct {
		Data struct {
			HealthStatus    string `json:"health_status"`
			HealthErrorKind string `json:"health_error_kind"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.HealthStatus, payload.Data.HealthErrorKind
}

// seedHealthMix 建一组结果各不相同的账号：正常、邮箱拒绝连接、授权失效、网络不通、
// 已封禁、已停用。返回按这个顺序排列的账号 ID。
func seedHealthMix(t *testing.T, e *echo.Echo, token, tenantID string) []string {
	t.Helper()
	ids := make([]string, 0, 6)
	for i, prefix := range []string{"ok", unavailableEmailMarker, upstreamFailEmailMarker,
		networkFailEmailMarker, "was-banned", "was-disabled"} {
		ids = append(ids, createAccount(t, e, token, tenantID, fmt.Sprintf(
			`{"email":"%s%02d@outlook.com","refresh_token":"M.token"}`, prefix, i)))
	}
	for id, status := range map[string]string{ids[4]: "banned", ids[5]: "disabled"} {
		if code, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/accounts/"+id), token,
			`{"status":"`+status+`"}`); code != http.StatusOK {
			t.Fatalf("置为 %s 失败: %d %s", status, code, body)
		}
	}
	return ids
}

// 检测只把账号自身的失败判成 invalid：网络不通记 error，停用的账号不参与。
// 这条边界决定了「删除失效账号」会不会误删好账号。
func TestAccountCheckClassifiesHealth(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	ids := seedHealthMix(t, e, token, tenantID)

	job := waitForJob(t, e, token, tenantID, submitCheck(t, e, token, tenantID))
	if got := job["total_count"].(float64); got != 5 {
		t.Errorf("检测 %v 个账号，期望 5 个（停用的不参与）", got)
	}

	got := fetchHealth(t, e, token, tenantID)
	want := healthStats{Total: 6, OK: 1, Invalid: 3, Error: 1, Unknown: 1}
	if got.Total != want.Total || got.OK != want.OK || got.Invalid != want.Invalid ||
		got.Error != want.Error || got.Unknown != want.Unknown {
		t.Errorf("健康分布 = %+v，期望 %+v", got, want)
	}
	for _, kind := range []string{"account_unavailable", "auth_failed", "banned"} {
		if got.InvalidByKind[kind] != 1 {
			t.Errorf("失效原因 %s = %d，期望 1（%v）", kind, got.InvalidByKind[kind], got.InvalidByKind)
		}
	}
	if status, kind := accountHealth(t, e, token, tenantID, ids[3]); status != "error" || kind != "network" {
		t.Errorf("网络不通的账号 = %s/%s，期望 error/network", status, kind)
	}
}

// 线上事故的回归：令牌刷新永远成功、邮箱却拒绝连接。
// 刷新任务的成功绝不能把检测得出的 invalid 改回来。
func TestTokenRefreshDoesNotReviveInvalidAccount(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	id := createAccount(t, e, token, tenantID,
		`{"email":"`+unavailableEmailMarker+`@outlook.com","refresh_token":"M.token"}`)

	waitForJob(t, e, token, tenantID, submitCheck(t, e, token, tenantID))
	refreshJob := waitForJob(t, e, token, tenantID, submitBatch(t, e, token, tenantID))
	if refreshJob["success_count"].(float64) != 1 {
		t.Fatalf("前提不成立：令牌刷新应当成功，实际 %v", refreshJob)
	}
	if status, kind := accountHealth(t, e, token, tenantID, id); status != "invalid" || kind != "account_unavailable" {
		t.Errorf("令牌刷新后健康状态 = %s/%s，期望保持 invalid/account_unavailable", status, kind)
	}
}

// 收信本身就是一次检测：点开一个邮箱拒绝连接的账号，它就该进失效清单。
func TestMailAccessRecordsHealth(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	bad := createAccount(t, e, token, tenantID,
		`{"email":"`+unavailableEmailMarker+`@outlook.com","refresh_token":"M.token"}`)
	good := createAccount(t, e, token, tenantID, `{"email":"fine@outlook.com","refresh_token":"M.token"}`)

	do(t, e, http.MethodGet, messagePath(tenantID, bad, "?folder=inbox"), token, "")
	do(t, e, http.MethodGet, messagePath(tenantID, good, "?folder=inbox"), token, "")

	if status, _ := accountHealth(t, e, token, tenantID, bad); status != "invalid" {
		t.Errorf("拒绝连接的账号健康状态 = %s，期望 invalid", status)
	}
	if status, _ := accountHealth(t, e, token, tenantID, good); status != "ok" {
		t.Errorf("正常收信的账号健康状态 = %s，期望 ok", status)
	}
}

// 删除失效账号：数量对不上就拒绝；对上了只删 invalid，并且不越租户。
func TestDeleteInvalidAccounts(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	seedHealthMix(t, e, token, tenantID)
	waitForJob(t, e, token, tenantID, submitCheck(t, e, token, tenantID))

	// 另一个租户也有失效账号，Bob 的清理不能碰到 Alice 的。
	bobToken, bobTenant := register(t, e, "bob", "bob@example.com")
	createAccount(t, e, bobToken, bobTenant,
		`{"email":"`+unavailableEmailMarker+`-bob@outlook.com","refresh_token":"M.token"}`)
	waitForJob(t, e, bobToken, bobTenant, submitCheck(t, e, bobToken, bobTenant))

	path := mailPath(tenantID, "/accounts/batch/delete-invalid")
	if status, body := do(t, e, http.MethodPost, path, token, `{"expected":2}`); status != http.StatusConflict {
		t.Fatalf("数量对不上时状态 = %d，期望 409\n%s", status, body)
	}
	if got := fetchHealth(t, e, token, tenantID); got.Invalid != 3 {
		t.Fatalf("被拒绝的请求删掉了账号：剩余 invalid = %d", got.Invalid)
	}

	status, body := do(t, e, http.MethodPost, path, token, `{"expected":3}`)
	if status != http.StatusOK {
		t.Fatalf("删除失败: %d %s", status, body)
	}
	var payload struct {
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &payload)
	if payload.Data.Deleted != 3 {
		t.Errorf("删除 %d 个，期望 3 个", payload.Data.Deleted)
	}
	got := fetchHealth(t, e, token, tenantID)
	if got.Total != 3 || got.Invalid != 0 || got.Error != 1 {
		t.Errorf("删除后分布 = %+v，期望只剩正常、网络不通、停用各 1 个", got)
	}
	if got := fetchHealth(t, e, bobToken, bobTenant); got.Invalid != 1 {
		t.Errorf("Bob 的失效账号被波及：invalid = %d", got.Invalid)
	}
	if status, _ := do(t, e, http.MethodPost, mailPath(bobTenant, "/accounts/batch/delete-invalid"), token,
		`{"expected":1}`); status != http.StatusForbidden && status != http.StatusNotFound {
		t.Errorf("跨租户删除状态 = %d，期望 403/404", status)
	}
}

// 用户补好凭据之后旧结论作废，否则下一次清理会删掉一个刚修好的账号。
func TestNewCredentialsResetHealth(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	id := createAccount(t, e, token, tenantID,
		`{"email":"`+upstreamFailEmailMarker+`@outlook.com","refresh_token":"M.token"}`)
	waitForJob(t, e, token, tenantID, submitCheck(t, e, token, tenantID))

	if code, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/accounts/"+id), token,
		`{"remark":"只改备注"}`); code != http.StatusOK {
		t.Fatalf("改备注失败: %d %s", code, body)
	}
	if status, _ := accountHealth(t, e, token, tenantID, id); status != "invalid" {
		t.Errorf("只改备注不该清掉结论，实际 %s", status)
	}

	if code, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/accounts/"+id), token,
		`{"refresh_token":"M.new"}`); code != http.StatusOK {
		t.Fatalf("更新令牌失败: %d %s", code, body)
	}
	if status, _ := accountHealth(t, e, token, tenantID, id); status != "unknown" {
		t.Errorf("换了凭据后健康状态 = %s，期望 unknown", status)
	}
}

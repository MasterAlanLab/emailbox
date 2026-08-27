package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"emailbox/pkg/model"

	"github.com/labstack/echo/v5"
)

func createAccount(t *testing.T, e *echo.Echo, token, tenantID, body string) string {
	t.Helper()
	status, resp := do(t, e, http.MethodPost, mailPath(tenantID, "/accounts"), token, body)
	if status != http.StatusOK {
		t.Fatalf("创建账号失败: %d %s", status, resp)
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

func TestAccountCRUDOverHTTP(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	accountID := createAccount(t, e, token, tenantID,
		`{"email":"user@outlook.com","password":"hunter2","refresh_token":"M.token","remark":"备注"}`)

	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/accounts/"+accountID), token, "")
	if status != http.StatusOK {
		t.Fatalf("获取详情失败: %d %s", status, body)
	}
	// 明文凭据绝不能出现在详情响应里
	for _, secret := range []string{"hunter2", "M.token", "password_enc", "refresh_token_enc"} {
		if strings.Contains(body, secret) {
			t.Errorf("详情响应里出现了 %q:\n%s", secret, body)
		}
	}
	if !strings.Contains(body, `"has_password":true`) || !strings.Contains(body, `"has_refresh_token":true`) {
		t.Errorf("详情应标记凭据已设置:\n%s", body)
	}

	if status, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/accounts/"+accountID), token,
		`{"remark":"改过的备注"}`); status != http.StatusOK {
		t.Errorf("更新失败: %d %s", status, body)
	}

	status, body = do(t, e, http.MethodGet, mailPath(tenantID, "/accounts?q=USER&status=active"), token, "")
	if status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, body)
	}
	if !strings.Contains(body, accountID) {
		t.Errorf("筛选应命中该账号:\n%s", body)
	}
	if !strings.Contains(body, `"pagination"`) {
		t.Errorf("列表应带分页信息:\n%s", body)
	}

	if status, body := do(t, e, http.MethodDelete, mailPath(tenantID, "/accounts/"+accountID), token, ""); status != http.StatusOK {
		t.Errorf("删除失败: %d %s", status, body)
	}
	if status, _ := do(t, e, http.MethodGet, mailPath(tenantID, "/accounts/"+accountID), token, ""); status != http.StatusNotFound {
		t.Errorf("删除后应 404，实际 %d", status)
	}
}

func TestAccountImportOverHTTP(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	content := "a@outlook.com----pwd----24d9a0ed-1234-4abc-9def-0123456789ab----M.token\\nb@gmail.com----app-pwd"
	status, body := do(t, e, http.MethodPost, mailPath(tenantID, "/accounts/import"), token,
		`{"format":"auto","content":"`+content+`"}`)
	if status != http.StatusOK {
		t.Fatalf("导入失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"created":2`) {
		t.Errorf("应创建 2 个账号:\n%s", body)
	}
}

// 每个端点一条越权测试：拿自己的 tenantID 去操作别人的 accountID 必须落到 404，
// 批量接口则应一个也不命中。
func TestAccountTenantIsolationOverHTTP(t *testing.T) {
	e := newTestServer(t)
	aliceToken, aliceTenant := register(t, e, "alice", "alice@example.com")
	bobToken, bobTenant := register(t, e, "bobby", "bob@example.com")

	accountID := createAccount(t, e, aliceToken, aliceTenant, `{"email":"secret@outlook.com"}`)

	for _, tc := range []struct{ name, method, path, body string }{
		{"详情", http.MethodGet, mailPath(bobTenant, "/accounts/"+accountID), ""},
		{"更新", http.MethodPatch, mailPath(bobTenant, "/accounts/"+accountID), `{"remark":"越权"}`},
		{"删除", http.MethodDelete, mailPath(bobTenant, "/accounts/"+accountID), ""},
	} {
		if status, body := do(t, e, tc.method, tc.path, bobToken, tc.body); status != http.StatusNotFound {
			t.Errorf("%s 越权应 404，实际 %d %s", tc.name, status, body)
		}
	}

	// 批量接口：请求被受理，但一个也不该命中
	for _, tc := range []struct{ name, path, body string }{
		{"批量删除", "/accounts/batch/delete", `{"account_ids":["` + accountID + `"]}`},
		{"批量改状态", "/accounts/batch/status", `{"account_ids":["` + accountID + `"],"status":"disabled"}`},
		{"批量移动", "/accounts/batch/move", `{"account_ids":["` + accountID + `"],"group_id":""}`},
	} {
		status, body := do(t, e, http.MethodPost, mailPath(bobTenant, tc.path), bobToken, tc.body)
		if status != http.StatusOK {
			t.Errorf("%s 应被受理，实际 %d %s", tc.name, status, body)
			continue
		}
		if !strings.Contains(body, `"succeeded":0`) {
			t.Errorf("%s 跨租户不该命中任何账号:\n%s", tc.name, body)
		}
	}

	// A 的账号毫发无损
	if status, body := do(t, e, http.MethodGet, mailPath(aliceTenant, "/accounts/"+accountID), aliceToken, ""); status != http.StatusOK {
		t.Errorf("A 的账号应仍可访问，实际 %d %s", status, body)
	}
}

// 导入接口的请求体远超全局 64KB 上限，必须真的放行。
func TestAccountImportAcceptsLargeBody(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	var sb strings.Builder
	for i := range 3000 {
		sb.WriteString("user")
		sb.WriteString(strings.Repeat("0", 4))
		sb.WriteString(itoa(i))
		sb.WriteString("@gmail.com----app-password\\n")
	}
	body := `{"format":"imap","content":"` + sb.String() + `"}`
	if len(body) < 64*1024 {
		t.Fatalf("测试用的请求体只有 %d 字节，没有超过全局上限，这条用例就没意义了", len(body))
	}
	status, resp := do(t, e, http.MethodPost, mailPath(tenantID, "/accounts/import"), token, body)
	if status == http.StatusRequestEntityTooLarge {
		t.Fatalf("导入接口的大请求体被全局 BodyLimit 拦住了，检查 IsBulkPath 与 BulkBody 是否成对生效")
	}
	if status != http.StatusOK {
		t.Fatalf("导入失败: %d %s", status, resp[:min(len(resp), 300)])
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// 导出是全平台风险最高的接口，两件事必须钉死：导出的文件能被原样重新导入
// （07 文档 P1 验收的 round-trip 一项）、每一次成功的导出都留痕。
func TestAccountExportRoundTripsAndAudits(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	createAccount(t, e, token, tenantID,
		`{"email":"a@outlook.com","password":"pwd","client_id":"24d9a0ed-1234-4abc-9def-0123456789ab","refresh_token":"M.token"}`)
	createAccount(t, e, token, tenantID, `{"email":"b@gmail.com","imap_password":"app-pwd"}`)

	status, content := do(t, e, http.MethodPost, mailPath(tenantID, "/accounts/export"), token,
		`{"scope":"all"}`)
	if status != http.StatusOK {
		t.Fatalf("导出失败: %d %s", status, content)
	}
	for _, want := range []string{"M.token", "app-pwd"} {
		if !strings.Contains(content, want) {
			t.Errorf("导出内容缺少 %q:\n%s", want, content)
		}
	}

	// 换一个租户把导出的内容原样导回去，账号数必须一致
	bobToken, bobTenant := register(t, e, "bobby", "bob@example.com")
	escaped := strings.ReplaceAll(strings.TrimSpace(content), "\n", "\\n")
	status, body := do(t, e, http.MethodPost, mailPath(bobTenant, "/accounts/import"), bobToken,
		`{"format":"auto","content":"`+escaped+`"}`)
	if status != http.StatusOK {
		t.Fatalf("重新导入失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"created":2`) {
		t.Errorf("导出的文件应能重新导入出同样多的账号:\n%s", body)
	}

	// 审计里只该有成功那一次
	_, total, err := store.ListAuditLogs(context.Background(), model.AuditFilter{
		TenantID: tenantID, Action: model.AuditAccountExport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Errorf("导出的审计记录有 %d 条，期望 1 条", total)
	}
}

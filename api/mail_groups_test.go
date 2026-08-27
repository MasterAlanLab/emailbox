package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
)

// mailPath 拼出租户作用域下的邮箱接口路径。
func mailPath(tenantID, suffix string) string {
	return "/api/v1/tenants/" + tenantID + "/mail" + suffix
}

// createGroup 建一个分组并返回其 ID。
func createGroup(t *testing.T, e *echo.Echo, token, tenantID, body string) string {
	t.Helper()
	status, resp := do(t, e, http.MethodPost, mailPath(tenantID, "/groups"), token, body)
	if status != http.StatusOK {
		t.Fatalf("创建分组失败: %d %s", status, resp)
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

func TestGroupCRUDOverHTTP(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	// 注册时已创建默认分组，因此列表里一开始就有一个分组。
	status, body := do(t, e, http.MethodGet, mailPath(tenantID, "/groups"), token, "")
	if status != http.StatusOK {
		t.Fatalf("获取分组列表失败: %d %s", status, body)
	}
	var list struct {
		Data []struct {
			ID       string `json:"id"`
			IsSystem bool   `json:"is_system"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || !list.Data[0].IsSystem {
		t.Fatalf("应只有一个系统默认分组，实际 %s", body)
	}

	groupID := createGroup(t, e, token, tenantID, `{"name":"客户 A","color":"blue"}`)

	if status, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/groups/"+groupID), token,
		`{"name":"客户 B"}`); status != http.StatusOK {
		t.Errorf("改名失败: %d %s", status, body)
	}
	otherID := createGroup(t, e, token, tenantID, `{"name":"客户 C"}`)
	if status, body := do(t, e, http.MethodPost, mailPath(tenantID, "/groups/reorder"), token,
		`{"group_ids":["`+otherID+`","`+groupID+`"]}`); status != http.StatusOK {
		t.Errorf("排序失败: %d %s", status, body)
	}
	if status, body := do(t, e, http.MethodDelete, mailPath(tenantID, "/groups/"+groupID), token, ""); status != http.StatusOK {
		t.Errorf("删除失败: %d %s", status, body)
	}
	if status, _ := do(t, e, http.MethodPatch, mailPath(tenantID, "/groups/"+groupID), token,
		`{"name":"x"}`); status != http.StatusNotFound {
		t.Errorf("已删除的分组应 404，实际 %d", status)
	}
}

// 跨租户测试：拿着别人的 tenantID 请求必须被挡在 tenant.Member 中间件；
// 拿着自己的 tenantID 去操作别人的 groupID 必须落到 404。
func TestGroupTenantIsolationOverHTTP(t *testing.T) {
	e := newTestServer(t)
	aliceToken, aliceTenant := register(t, e, "alice", "alice@example.com")
	bobToken, bobTenant := register(t, e, "bobby", "bob@example.com")

	groupID := createGroup(t, e, aliceToken, aliceTenant, `{"name":"Alice 的分组"}`)

	// 直接用 A 的 tenantID：不是成员，403
	if status, body := do(t, e, http.MethodGet, mailPath(aliceTenant, "/groups"), bobToken, ""); status != http.StatusForbidden {
		t.Errorf("非成员访问他人租户应 403，实际 %d %s", status, body)
	}
	// 用自己的 tenantID 但传 A 的 groupID：SQL 的 WHERE 带 tenant_id，落到 404
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, mailPath(bobTenant, "/groups/"+groupID), `{"name":"越权改名"}`},
		{http.MethodDelete, mailPath(bobTenant, "/groups/"+groupID), ""},
	} {
		if status, body := do(t, e, tc.method, tc.path, bobToken, tc.body); status != http.StatusNotFound {
			t.Errorf("%s %s 越权应 404，实际 %d %s", tc.method, tc.path, status, body)
		}
	}
	// A 的分组没有被改动
	if status, _ := do(t, e, http.MethodGet, mailPath(aliceTenant, "/groups"), aliceToken, ""); status != http.StatusOK {
		t.Error("A 的分组列表应仍可访问")
	}
}

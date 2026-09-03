package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
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

// 分组代理是编辑表单的数据来源，这条钉住它的三个关键行为：
// 列表只给打码串、明文端点给原样的明文、PATCH 不带代理字段时原值不动。
//
// 第三条是最要紧的：前端在代理明文没读回来时会省掉这个字段，
// 若 PATCH 把「未提供」当成「清空」，用户改一次分组名就把代理洗没了，
// 那批账号从此走服务器公网 IP 直连——出问题时界面上完全看不出来。
func TestGroupProxyRoundTrip(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	const plain = "socks5://puser:psecret@proxy.example.com:1080"
	groupID := createGroup(t, e, token, tenantID,
		`{"name":"客户 A","proxy_url":"`+plain+`"}`)

	proxyPath := mailPath(tenantID, "/groups/"+groupID+"/proxy")
	readProxy := func() struct {
		ProxyURL string `json:"proxy_url"`
	} {
		t.Helper()
		status, body := do(t, e, http.MethodGet, proxyPath, token, "")
		if status != http.StatusOK {
			t.Fatalf("读取代理明文失败: %d %s", status, body)
		}
		var payload struct {
			Data struct {
				ProxyURL string `json:"proxy_url"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Data
	}

	// 列表接口不能带明文：一进 /mail 就把全部分组的代理口令发到浏览器，
	// 而绝大多数时候没人要看它们。
	_, listBody := do(t, e, http.MethodGet, mailPath(tenantID, "/groups"), token, "")
	if strings.Contains(listBody, "psecret") {
		t.Errorf("分组列表泄露了代理口令: %s", listBody)
	}
	if !strings.Contains(listBody, "socks5://puser:****@proxy.example.com:1080") {
		t.Errorf("分组列表应回打码后的代理: %s", listBody)
	}

	if got := readProxy(); got.ProxyURL != plain {
		t.Errorf("代理明文应原样回显，实际 %q", got.ProxyURL)
	}

	// 只改名字，不传代理字段 —— 代理必须原封不动。
	if status, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/groups/"+groupID), token,
		`{"name":"客户 B"}`); status != http.StatusOK {
		t.Fatalf("改名失败: %d %s", status, body)
	}
	if got := readProxy(); got.ProxyURL != plain {
		t.Errorf("只改名字不该动代理，实际 %+v", got)
	}

	// 显式传空串才是清空。
	if status, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/groups/"+groupID), token,
		`{"proxy_url":""}`); status != http.StatusOK {
		t.Fatalf("清空代理失败: %d %s", status, body)
	}
	if got := readProxy(); got.ProxyURL != "" {
		t.Errorf("显式传空串应清空代理，实际 %+v", got)
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
		// 代理明文端点走同一条路。它是分组这边唯一送出凭据明文的接口，
		// 借自己的 tenantID 去读别人分组的代理口令必须落到 404。
		{http.MethodGet, mailPath(bobTenant, "/groups/"+groupID+"/proxy"), ""},
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

// 定时刷新间隔的边界与回读。
//
// 越权改这个字段不用另测：PATCH 的租户隔离已由 TestGroupTenantIsolationOverHTTP
// 覆盖，而 UpdateMailGroupSchedule 的 WHERE 同样带 tenant_id，跨租户一样落 404。
// 这里只钉两件真会错的事：下限拦不拦得住，以及 0 有没有被当成「没传」丢掉。
func TestGroupRefreshIntervalOverHTTP(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	groupID := createGroup(t, e, token, tenantID, `{"name":"客户 A"}`)

	interval := func() (int, bool) {
		t.Helper()
		status, resp := do(t, e, http.MethodGet, mailPath(tenantID, "/groups"), token, "")
		if status != http.StatusOK {
			t.Fatalf("读分组列表失败: %d %s", status, resp)
		}
		var payload struct {
			Data []struct {
				ID                     string  `json:"id"`
				RefreshIntervalMinutes int     `json:"refresh_interval_minutes"`
				NextRefreshAt          *string `json:"next_refresh_at"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(resp), &payload); err != nil {
			t.Fatal(err)
		}
		for _, g := range payload.Data {
			if g.ID == groupID {
				return g.RefreshIntervalMinutes, g.NextRefreshAt != nil
			}
		}
		t.Fatalf("分组 %s 不在列表里", groupID)
		return 0, false
	}

	if got, hasNext := interval(); got != 0 || hasNext {
		t.Errorf("新建分组的定时刷新应当是关闭的，实际 interval=%d hasNext=%v", got, hasNext)
	}

	// 低于下限必须被拒。放过去的话，用户填个 5 分钟就能让平台每五分钟替他
	// 打一轮服务商，而风控的表现和令牌真的过期长得一模一样。
	patch := mailPath(tenantID, "/groups/"+groupID)
	if status, body := do(t, e, http.MethodPatch, patch, token, `{"refresh_interval_minutes":10079}`); status == http.StatusOK {
		t.Errorf("低于下限（7 天 = 10080 分钟）的间隔应被拒绝，实际 200 %s", body)
	}
	if got, _ := interval(); got != 0 {
		t.Errorf("被拒绝的请求不该改动任何东西，实际 interval=%d", got)
	}

	// 合法值：写进去并且算出了下次时刻。
	if status, body := do(t, e, http.MethodPatch, patch, token, `{"refresh_interval_minutes":10080}`); status != http.StatusOK {
		t.Fatalf("设置 7 天间隔失败: %d %s", status, body)
	}
	if got, hasNext := interval(); got != 10080 || !hasNext {
		t.Errorf("间隔应为 10080 且带下次时刻，实际 interval=%d hasNext=%v", got, hasNext)
	}

	// 关闭。0 在这个字段上是有含义的取值，一旦哪层把它当 falsy 过滤掉，
	// 界面会显示已关闭而实际还在按原周期刷。
	if status, body := do(t, e, http.MethodPatch, patch, token, `{"refresh_interval_minutes":0}`); status != http.StatusOK {
		t.Fatalf("关闭定时刷新失败: %d %s", status, body)
	}
	if got, hasNext := interval(); got != 0 || hasNext {
		t.Errorf("关闭后应当既没有间隔也没有下次时刻，实际 interval=%d hasNext=%v", got, hasNext)
	}

	// 不传这个字段时保持原值——PATCH 的语义，和代理字段一致。
	if status, body := do(t, e, http.MethodPatch, patch, token, `{"refresh_interval_minutes":20160}`); status != http.StatusOK {
		t.Fatalf("重新开启失败: %d %s", status, body)
	}
	if status, body := do(t, e, http.MethodPatch, patch, token, `{"name":"改个名"}`); status != http.StatusOK {
		t.Fatalf("只改名失败: %d %s", status, body)
	}
	if got, _ := interval(); got != 20160 {
		t.Errorf("只改名不应动到定时配置，实际 interval=%d", got)
	}
}

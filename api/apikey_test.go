package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"emailbox/pkg/handler"

	"github.com/labstack/echo/v5"
)

// doKey 发起一次带 API Key 的请求。与 do 的区别只在于身份走 Authorization 头
// 而不是 Cookie——这正是这批用例要验证的那条新通路。
func doKey(t *testing.T, e *echo.Echo, method, path, key string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if key != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// resetKey 生成租户的 API Key 并返回明文。
func resetKey(t *testing.T, e *echo.Echo, token, tenantID string) string {
	t.Helper()
	status, resp := do(t, e, http.MethodPost, "/api/v1/tenants/"+tenantID+"/api-key/reset", token, "")
	if status != http.StatusOK {
		t.Fatalf("生成 API Key 失败: %d %s", status, resp)
	}
	var payload struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(payload.Data.Token, "ebx_") {
		t.Fatalf("Key 应带 ebx_ 前缀，实际 %q", payload.Data.Token)
	}
	return payload.Data.Token
}

// API Key 是一个只读的虚拟角色，不是第二套接口：它走的是同一份 /mail 路由，
// 权限由 middleware.Require 收敛。这个用例把「能做什么、不能做什么」一次钉死——
// 少了任何一条，泄露的 Key 就可能拿走整租户的凭据明文。
func TestAPIKeyAccessMatrix(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	accountID := createAccount(t, e, token, tenantID,
		`{"email":"user@outlook.com","refresh_token":"M.token"}`)
	key := resetKey(t, e, token, tenantID)

	base := "/api/v1/tenants/" + tenantID + "/mail"

	t.Run("能取件", func(t *testing.T) {
		for _, path := range []string{
			base + "/groups",
			base + "/accounts",
			base + "/accounts/" + accountID + "/messages?folder=inbox&top=10",
			base + "/accounts/" + accountID + "/messages/msg-1",
		} {
			if status, body := doKey(t, e, http.MethodGet, path, key); status != http.StatusOK {
				t.Errorf("GET %s 应 200，实际 %d %s", path, status, body)
			}
		}
	})

	t.Run("写操作与导出一律拒绝", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			// 导出等于取走整租户的凭据明文，是全平台风险最高的接口。
			{http.MethodPost, base + "/accounts/export"},
			{http.MethodPost, base + "/accounts"},
			{http.MethodDelete, base + "/accounts/" + accountID},
			{http.MethodPost, base + "/groups"},
			{http.MethodPost, base + "/accounts/" + accountID + "/token/refresh"},
			// Key 不能读取或重置自己：泄露之后不能自我续命，也读不到别的信息。
			{http.MethodGet, "/api/v1/tenants/" + tenantID + "/api-key"},
			{http.MethodPost, "/api/v1/tenants/" + tenantID + "/api-key/reset"},
		} {
			if status, body := doKey(t, e, tc.method, tc.path, key); status != http.StatusForbidden {
				t.Errorf("%s %s 应 403，实际 %d %s", tc.method, tc.path, status, body)
			}
		}
	})

	t.Run("换个工作空间即拒绝", func(t *testing.T) {
		_, bobTenant := register(t, e, "bobby", "bob@example.com")
		path := "/api/v1/tenants/" + bobTenant + "/mail/accounts"
		if status, body := doKey(t, e, http.MethodGet, path, key); status != http.StatusForbidden {
			t.Errorf("跨工作空间应 403，实际 %d %s", status, body)
		}
	})

	// Key 不属于任何用户，那些以 user_id 为主语的端点对它没有意义。
	// 不挡住的话，handler 会拿着一个空 user_id 继续往下走。
	t.Run("工作空间之外的接口不认 Key", func(t *testing.T) {
		for _, path := range []string{"/api/v1/user/profile", "/api/v1/tenants", "/api/v1/auth/session"} {
			if status, body := doKey(t, e, http.MethodGet, path, key); status != http.StatusUnauthorized {
				t.Errorf("GET %s 应 401，实际 %d %s", path, status, body)
			}
		}
	})

	t.Run("无效或缺失的 Key 是 401", func(t *testing.T) {
		for _, k := range []string{"", "ebx_deadbeef", "not-even-prefixed"} {
			if status, _ := doKey(t, e, http.MethodGet, base+"/groups", k); status != http.StatusUnauthorized {
				t.Errorf("Key=%q 应 401，实际 %d", k, status)
			}
		}
	})

	// 重置的意义全在这里：旧 Key 必须当场失效，否则「我怀疑 Key 泄露了」这件事
	// 就没有任何补救手段。
	t.Run("重置后旧 Key 立即失效", func(t *testing.T) {
		newKey := resetKey(t, e, token, tenantID)
		if status, _ := doKey(t, e, http.MethodGet, base+"/groups", key); status != http.StatusUnauthorized {
			t.Errorf("旧 Key 应 401，实际 %d", status)
		}
		if status, body := doKey(t, e, http.MethodGet, base+"/groups", newKey); status != http.StatusOK {
			t.Errorf("新 Key 应 200，实际 %d %s", status, body)
		}
	})
}

// 页面要能回显 Key，因此明文必须能从库里还原（见 000012 迁移的说明）。
// 「生成之后再进页面就看不到了」是这个功能最容易在重构中丢掉的性质。
func TestAPIKeyIsReadableAfterCreation(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	// 还没生成时 data 为 null，页面据此显示「生成」而不是「重置」。
	status, body := do(t, e, http.MethodGet, "/api/v1/tenants/"+tenantID+"/api-key", token, "")
	if status != http.StatusOK || !strings.Contains(body, `"data":null`) {
		t.Fatalf("未生成时应回 data:null，实际 %d %s", status, body)
	}

	key := resetKey(t, e, token, tenantID)
	_, body = do(t, e, http.MethodGet, "/api/v1/tenants/"+tenantID+"/api-key", token, "")
	if !strings.Contains(body, key) {
		t.Errorf("回显的 Key 与生成的不一致:\n%s", body)
	}
}

// llms.txt 里写的每条路径都必须真的存在于路由表。
// 文档漂移比没有文档更糟——Agent 会照着一条不存在的路径反复重试。
func TestLLMsTxtMatchesRoutes(t *testing.T) {
	e := newTestServer(t)

	registered := map[string]bool{}
	for _, r := range e.Router().Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, endpoint := range handler.APIEndpoints {
		if !registered[endpoint.Method+" "+endpoint.Path] {
			t.Errorf("llms.txt 里的 %s %s 不在路由表里", endpoint.Method, endpoint.Path)
		}
	}

	rec := doRaw(t, e, "/llms.txt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("llms.txt 应公开可取，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, endpoint := range handler.APIEndpoints {
		// 文档里用 {tenantID}，路由表里是 :tenantID。
		public := endpoint.Path
		for _, seg := range strings.Split(endpoint.Path, "/") {
			if strings.HasPrefix(seg, ":") {
				public = strings.Replace(public, seg, "{"+seg[1:]+"}", 1)
			}
		}
		if !strings.Contains(body, public) {
			t.Errorf("llms.txt 没有写出 %s:\n%s", public, body)
		}
	}
	// 它是公开文件，绝不能包含任何 Key。
	if strings.Contains(body, "ebx_") && !strings.Contains(body, `KEY="ebx_..."`) {
		t.Error("llms.txt 里出现了疑似真实 Key")
	}
}

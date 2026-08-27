package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"emailbox/api"

	"github.com/labstack/echo/v5"
)

// 导入接口要收 8MB 的请求体，而全局 BodyLimit 只有 64KB。
//
// 这里钉死的是一个反直觉的组合：路由级 BodyLimit **不会**覆盖全局的那个，
// 两层都会生效且更严的说了算。所以放宽必须靠「全局 Skipper 放行 + 路由级重新限制」
// 这一对。任何一半失效的表现都是导入接口对稍大的文件直接返回 413，
// 而 413 完全不会让人联想到中间件配置。
func TestBulkBodyLimitOverridesGlobalLimit(t *testing.T) {
	const (
		normalPath = "/api/v1/tenants/t1/mail/accounts"
		bulkPath   = "/api/v1/tenants/t1/mail/accounts/import"
		batchPath  = "/api/v1/tenants/t1/mail/accounts/batch/delete"
	)
	drain := func(c *echo.Context) error {
		if _, err := io.Copy(io.Discard, c.Request().Body); err != nil {
			return err
		}
		return c.String(http.StatusOK, "ok")
	}
	e := echo.New()
	e.Use(api.GlobalBodyLimit())
	e.POST(normalPath, drain)
	e.POST(bulkPath, drain, api.BulkBody())
	e.POST(batchPath, drain, api.BulkBody())

	// 超过全局 64KB、但远小于 8MB 的请求体
	body := strings.Repeat("x", 256*1024)

	if status := postBody(t, e, normalPath, body); status != http.StatusRequestEntityTooLarge {
		t.Errorf("普通路由应受 64KB 全局限制，期望 413，实际 %d", status)
	}
	for _, path := range []string{bulkPath, batchPath} {
		if status := postBody(t, e, path, body); status != http.StatusOK {
			t.Errorf("%s 的大请求体放宽失效，期望 200，实际 %d", path, status)
		}
	}

	// 放宽不等于取消：8MB 仍然是硬上限，否则一个超大请求就能耗尽内存。
	if status := postBody(t, e, bulkPath, strings.Repeat("x", int(api.BulkBodyLimit)+1024)); status != http.StatusRequestEntityTooLarge {
		t.Errorf("超过 8MB 时导入路由也应拒绝，期望 413，实际 %d", status)
	}
}

func TestIsBulkPath(t *testing.T) {
	bulk := []string{
		"/api/v1/tenants/t1/mail/accounts/import",
		"/api/v1/tenants/t1/mail/accounts/batch/delete",
		"/api/v1/admin/tenants/t1/mail/accounts/batch/move",
	}
	normal := []string{
		"/api/v1/tenants/t1/mail/accounts",
		"/api/v1/auth/login",
		"/api/v1/tenants/t1/mail/accounts/importer", // 不能被前缀匹配误伤
	}
	for _, p := range bulk {
		if !api.IsBulkPath(p) {
			t.Errorf("%s 应被识别为大请求体接口", p)
		}
	}
	for _, p := range normal {
		if api.IsBulkPath(p) {
			t.Errorf("%s 不该被放宽请求体限制", p)
		}
	}
}

func postBody(t *testing.T, e *echo.Echo, path, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMETextPlain)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

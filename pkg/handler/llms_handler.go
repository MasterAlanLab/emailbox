package handler

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// APIEndpoint 是对外开放给 API Key 的一个端点。
//
// 这份清单是 llms.txt 的唯一来源，Path 写成 Echo 路由表里的形状（:tenantID），
// 好让 llms_test.go 能逐条去路由表里比对——接口改了却忘了改文档，
// 那个测试会直接失败。文档漂移比没有文档更糟：Agent 会照着不存在的路径反复重试。
type APIEndpoint struct {
	Method  string
	Path    string
	Summary string
	Params  string
}

// APIEndpoints 是 API Key 能调的全部端点。它们都是只读的——
// Key 的角色（model.TenantRoleAPI）只有 group:read / account:read / message:read。
var APIEndpoints = []APIEndpoint{
	{
		Method: http.MethodGet, Path: "/api/v1/tenants/:tenantID/mail/groups",
		Summary: "列出分组。data: [{id, name, description, color, account_count}]",
	},
	{
		Method: http.MethodGet, Path: "/api/v1/tenants/:tenantID/mail/accounts",
		Summary: "列出邮箱账号。data: {items: [{id, email, group_id, status, remark, last_refresh_status, ...}], pagination: {page, limit, total, pages}}",
		Params:  "group_id, q(匹配邮箱/备注), status=active|disabled|banned, refresh_status=never|success|failed, page=1, limit<=200(默认 20)",
	},
	{
		Method: http.MethodGet, Path: "/api/v1/tenants/:tenantID/mail/accounts/:accountID/messages",
		Summary: "列出邮件（每次调用扣 1 次 daily_mail_fetch 配额）。data: {items: [{id, id_mode, folder, subject, from, to, received_at, is_read, has_attachments, body_preview}], channel}",
		Params:  "folder=inbox|junk|all(默认 inbox), top<=50(默认 20), skip",
	},
	{
		Method: http.MethodGet, Path: "/api/v1/tenants/:tenantID/mail/accounts/:accountID/messages/:messageID",
		Summary: "读邮件正文（同样扣 1 次配额）。data: 列表里那些字段 + {body, body_type: text|html, attachments: [{id, name, content_type, size, is_inline}]}",
		Params:  "folder=inbox|junk(默认 inbox), id_mode=列表里同名字段原样回传",
	},
	{
		Method: http.MethodGet, Path: "/api/v1/tenants/:tenantID/mail/accounts/:accountID/messages/:messageID/attachments/:attachmentID",
		Summary: "下载附件，返回二进制流而不是 JSON",
		Params:  "folder=inbox|junk(默认 inbox), id_mode",
	},
}

// LLMs 输出 /llms.txt：给 Agent 看的接入说明。
//
// 公开、无鉴权、**不含任何 Key**——它描述的是「这个服务怎么调」，
// 而不是「你是谁」。Key 在网页上取，两者分开才能让这个文件可以被随便抓取。
func (h *APIKeyHandler) LLMs(c *echo.Context) error {
	return c.String(http.StatusOK, renderLLMs(baseURL(c)))
}

// baseURL 还原调用方看到的地址。反代后面 c.Scheme() 会读 X-Forwarded-Proto，
// 前提是 TrustProxy 打开——没打开时退回 http，文档里的示例仍然可用，
// 只是需要调用方自己改协议，好过写死一个猜出来的域名。
func baseURL(c *echo.Context) string {
	return c.Scheme() + "://" + c.Request().Host
}

func renderLLMs(base string) string {
	var b strings.Builder
	b.WriteString(`# emailbox 取件 API

> 多租户邮箱托管服务。用一把 API Key 读取你托管的邮箱账号与其中的邮件，
> 常见用途是取验证码。全部接口只读。

## 认证

Authorization: Bearer <API Key>

Key 在网页的「API」页面生成，一个工作空间（tenant）一把，重置后旧 Key 立即失效。
下面路径里的 {tenantID} 就是该工作空间的 ID，同一个页面上可以直接复制。
Key 只能访问自己所属的工作空间，换一个 tenantID 会得到 403。

## 约定

- 响应统一为 {"code": 0, "data": ..., "message": "..."}，code 非 0 即业务失败
- 时间为 RFC3339 字符串
- 账号列表分页从 page=1 开始；邮件列表用 top/skip，不用 page
- 邮件 ID 只在同一个 folder 下有意义：拿列表里的 id 去读正文时，folder 要一并带上
- 只读：任何写操作（新建、删除、导出、刷新令牌）一律 403，不必尝试

## 接口

`)
	for _, e := range APIEndpoints {
		b.WriteString(e.Method + " " + publicPath(e.Path) + "\n")
		b.WriteString("  " + e.Summary + "\n")
		if e.Params != "" {
			b.WriteString("  参数: " + e.Params + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(`## 取一封最新验证码的典型流程

1. 用邮箱地址找到账号 ID
2. 列出该账号最近的邮件（收件箱与垃圾箱一起查，验证码常被判成垃圾）
3. 按需读正文

` + "```bash\n" +
		`KEY="ebx_..."
BASE="` + base + `"
TENANT="{tenantID}"

ACCOUNT=$(curl -s -H "Authorization: Bearer $KEY" \
  "$BASE/api/v1/tenants/$TENANT/mail/accounts?q=user@example.com" \
  | jq -r '.data.items[0].id')

curl -s -H "Authorization: Bearer $KEY" \
  "$BASE/api/v1/tenants/$TENANT/mail/accounts/$ACCOUNT/messages?folder=all&top=5"
` + "```\n" + `
## 错误

- 401  Key 缺失或无效（含重置后仍在用旧 Key）
- 403  权限不足：写操作、或访问别的工作空间
- 403 且 code=1001  今日取件额度用尽，次日按工作空间时区重置
- 404  账号或邮件不存在
- 429  请求过于频繁
- 502  上游邮箱服务出错，data.error_kind 说明是要重新授权、换代理还是稍后再试

## 注意

- 列邮件与读正文各扣 1 次每日取件额度，额度见网页的「用量」页
- 单次请求会真的去连上游邮箱，耗时以秒计，客户端超时建议设到 60 秒以上
- 附件下载返回二进制流，不是 JSON
`)
	return b.String()
}

// publicPath 把 Echo 的 :param 换成文档里更常见的 {param}。
func publicPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"emailbox/pkg/handler"
	"emailbox/pkg/mailer"
)

// stubMailClient 是一个不联网的假通道，供路由与越权测试使用。
type stubMailClient struct{}

func (stubMailClient) Channel() string { return mailer.ChannelGraph }

func (stubMailClient) List(_ context.Context, _ mailer.Credential, opt mailer.ListOptions) ([]mailer.Message, error) {
	return []mailer.Message{{
		ID:         "msg-1",
		IDMode:     mailer.IDModeNone,
		Folder:     opt.Folder,
		Subject:    "测试邮件",
		From:       "sender@example.com",
		ReceivedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
	}}, nil
}

// noAttachmentMessageID 让 stub 走「协议层返回 nil Attachments」这一支。
// 真实实现里这是常态而非边角：绝大多数邮件都没有附件。
const noAttachmentMessageID = "no-attachment"

func (stubMailClient) Detail(_ context.Context, _ mailer.Credential, folder mailer.Folder, id, idMode string) (*mailer.Detail, error) {
	detail := &mailer.Detail{
		Message:  mailer.Message{ID: id, IDMode: idMode, Folder: folder, Subject: "测试邮件"},
		Body:     "<p>正文</p>",
		BodyType: "html",
		Attachments: []mailer.AttachmentMeta{
			{ID: "0", Name: "报告.pdf", ContentType: "application/pdf", Size: 3},
		},
	}
	if id == noAttachmentMessageID {
		detail.Attachments = nil
	}
	return detail, nil
}

func (stubMailClient) Attachment(_ context.Context, _ mailer.Credential, _ mailer.Folder, _, _, attID string) (*mailer.Attachment, error) {
	return &mailer.Attachment{
		AttachmentMeta: mailer.AttachmentMeta{ID: attID, Name: "报告.pdf", ContentType: "application/pdf", Size: 3},
		Content:        []byte("PDF"),
	}, nil
}

func (stubMailClient) MarkRead(_ context.Context, _ mailer.Credential, items []mailer.MessageRef) (mailer.BatchResult, error) {
	return mailer.BatchResult{Succeeded: len(items)}, nil
}

func (stubMailClient) Delete(_ context.Context, _ mailer.Credential, items []mailer.MessageRef) (mailer.BatchResult, error) {
	return mailer.BatchResult{Succeeded: len(items)}, nil
}

// failingMailClient 让上游一律以指定的 Kind 失败，用来测协议层错误的 HTTP 映射。
// 由 routes_test.go 的链工厂按账号邮箱选中（邮箱含 upstreamFailEmailMarker 时启用），
// 这样不用改动 newTestServer 的签名，其余用例完全不受影响。
type failingMailClient struct{ kind mailer.ErrKind }

// upstreamFailEmailMarker 是「请让这个账号的上游调用失败」的约定标记。
const upstreamFailEmailMarker = "upstream-fail"

func (c failingMailClient) fail() error {
	return &mailer.Error{Kind: c.kind, Channel: mailer.ChannelGraph, Message: "refresh_token 已失效，需要重新授权"}
}

func (failingMailClient) Channel() string { return mailer.ChannelGraph }

func (c failingMailClient) List(_ context.Context, _ mailer.Credential, _ mailer.ListOptions) ([]mailer.Message, error) {
	return nil, c.fail()
}

func (c failingMailClient) Detail(_ context.Context, _ mailer.Credential, _ mailer.Folder, _, _ string) (*mailer.Detail, error) {
	return nil, c.fail()
}

func (c failingMailClient) Attachment(_ context.Context, _ mailer.Credential, _ mailer.Folder, _, _, _ string) (*mailer.Attachment, error) {
	return nil, c.fail()
}

func (c failingMailClient) MarkRead(_ context.Context, _ mailer.Credential, _ []mailer.MessageRef) (mailer.BatchResult, error) {
	return mailer.BatchResult{}, c.fail()
}

func (c failingMailClient) Delete(_ context.Context, _ mailer.Credential, _ []mailer.MessageRef) (mailer.BatchResult, error) {
	return mailer.BatchResult{}, c.fail()
}

func messagePath(tenantID, accountID, suffix string) string {
	return mailPath(tenantID, "/accounts/"+accountID+"/messages"+suffix)
}

func TestMessageEndpoints(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	accountID := createAccount(t, e, token, tenantID,
		`{"email":"user@outlook.com","refresh_token":"M.token"}`)

	t.Run("列表", func(t *testing.T) {
		status, body := do(t, e, http.MethodGet, messagePath(tenantID, accountID, "?folder=inbox&top=10"), token, "")
		if status != http.StatusOK {
			t.Fatalf("%d %s", status, body)
		}
		if !strings.Contains(body, "测试邮件") {
			t.Errorf("响应里没有邮件:\n%s", body)
		}
	})

	t.Run("详情", func(t *testing.T) {
		status, body := do(t, e, http.MethodGet,
			messagePath(tenantID, accountID, "/msg-1?folder=inbox&id_mode="), token, "")
		if status != http.StatusOK {
			t.Fatalf("%d %s", status, body)
		}
		if !strings.Contains(body, "正文") {
			t.Errorf("详情缺正文:\n%s", body)
		}
	})

	t.Run("附件下载带 RFC 5987 文件名", func(t *testing.T) {
		rec := doRaw(t, e,
			messagePath(tenantID, accountID, "/msg-1/attachments/0?folder=inbox"), token)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		disposition := rec.Header().Get("Content-Disposition")
		// 中文名必须走 filename*，否则各家浏览器解出来的名字都不一样。
		if !strings.Contains(disposition, "filename*=UTF-8''") {
			t.Errorf("Content-Disposition = %q", disposition)
		}
		if strings.Contains(disposition, "报告") {
			t.Errorf("非 ASCII 不能直接进响应头: %q", disposition)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Error("附件响应缺 nosniff")
		}
		if rec.Body.String() != "PDF" {
			t.Errorf("附件内容 = %q", rec.Body.String())
		}
	})

	t.Run("附件打包", func(t *testing.T) {
		rec := doRaw(t, e,
			messagePath(tenantID, accountID, "/msg-1/attachments.zip?folder=inbox"), token)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
			t.Errorf("Content-Type = %q", ct)
		}
		if rec.Body.Len() == 0 {
			t.Error("ZIP 是空的")
		}
	})

	t.Run("批量标已读", func(t *testing.T) {
		status, body := do(t, e, http.MethodPost, messagePath(tenantID, accountID, "/read"), token,
			`{"items":[{"id":"msg-1","folder":"inbox","id_mode":""}]}`)
		if status != http.StatusOK {
			t.Fatalf("%d %s", status, body)
		}
		if !strings.Contains(body, `"succeeded":1`) {
			t.Errorf("结果 = %s", body)
		}
	})

	t.Run("批量删除", func(t *testing.T) {
		status, body := do(t, e, http.MethodPost, messagePath(tenantID, accountID, "/delete"), token,
			`{"items":[{"id":"msg-1","folder":"inbox","id_mode":""}]}`)
		if status != http.StatusOK {
			t.Fatalf("%d %s", status, body)
		}
	})

	t.Run("非法邮件夹被拒", func(t *testing.T) {
		status, _ := do(t, e, http.MethodGet, messagePath(tenantID, accountID, "?folder=nonsense"), token, "")
		if status != http.StatusBadRequest {
			t.Errorf("状态 = %d，期望 400", status)
		}
	})

	t.Run("空批量被拒", func(t *testing.T) {
		status, _ := do(t, e, http.MethodPost, messagePath(tenantID, accountID, "/read"), token, `{"items":[]}`)
		if status != http.StatusBadRequest {
			t.Errorf("状态 = %d，期望 400", status)
		}
	})

	t.Run("超量批量被拒", func(t *testing.T) {
		items := make([]string, 0, 300)
		for range 300 {
			items = append(items, `{"id":"1","folder":"inbox","id_mode":""}`)
		}
		body := `{"items":[` + strings.Join(items, ",") + `]}`
		status, _ := do(t, e, http.MethodPost, messagePath(tenantID, accountID, "/read"), token, body)
		if status != http.StatusBadRequest {
			t.Errorf("状态 = %d，期望 400", status)
		}
	})
}

// 跨租户：A 用户带 B 用户的 accountID 请求，每个端点都必须 404。
// 这是多租户 SaaS 里最常见也最致命的一类漏洞——泄露的是他人邮箱内容。
func TestMessageEndpointsRejectCrossTenant(t *testing.T) {
	e := newTestServer(t)
	aliceToken, aliceTenant := register(t, e, "alice", "alice@example.com")
	bobToken, bobTenant := register(t, e, "bob", "bob@example.com")

	aliceAccount := createAccount(t, e, aliceToken, aliceTenant,
		`{"email":"alice-mail@outlook.com","refresh_token":"M.token"}`)

	cases := []struct {
		name, method, path, body string
	}{
		{"列表", http.MethodGet, messagePath(bobTenant, aliceAccount, "?folder=inbox"), ""},
		{"详情", http.MethodGet, messagePath(bobTenant, aliceAccount, "/msg-1?folder=inbox"), ""},
		{"附件", http.MethodGet, messagePath(bobTenant, aliceAccount, "/msg-1/attachments/0?folder=inbox"), ""},
		{"附件打包", http.MethodGet, messagePath(bobTenant, aliceAccount, "/msg-1/attachments.zip?folder=inbox"), ""},
		{"标已读", http.MethodPost, messagePath(bobTenant, aliceAccount, "/read"),
			`{"items":[{"id":"msg-1","folder":"inbox","id_mode":""}]}`},
		{"删除", http.MethodPost, messagePath(bobTenant, aliceAccount, "/delete"),
			`{"items":[{"id":"msg-1","folder":"inbox","id_mode":""}]}`},
	}
	for _, c := range cases {
		status, body := do(t, e, c.method, c.path, bobToken, c.body)
		if status != http.StatusNotFound {
			t.Errorf("%s: bob 拿 alice 的账号 ID 请求返回 %d，期望 404\n%s", c.name, status, body)
		}
		if strings.Contains(body, "测试邮件") {
			t.Errorf("%s: 泄露了邮件内容:\n%s", c.name, body)
		}
	}

	// 反过来也要挡：bob 用自己的租户 ID 是上面那组，这里试 alice 的租户 ID。
	for _, c := range cases {
		path := strings.Replace(c.path, bobTenant, aliceTenant, 1)
		status, _ := do(t, e, c.method, path, bobToken, c.body)
		if status != http.StatusForbidden && status != http.StatusNotFound {
			t.Errorf("%s: bob 访问 alice 的租户返回 %d，期望 403/404", c.name, status)
		}
	}
}

// 被封的账号不该再打上游：每试一次都在加重风控。
func TestMessageEndpointsRejectBannedAccount(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	accountID := createAccount(t, e, token, tenantID,
		`{"email":"user@outlook.com","refresh_token":"M.token"}`)

	if status, body := do(t, e, http.MethodPatch, mailPath(tenantID, "/accounts/"+accountID), token,
		`{"status":"banned"}`); status != http.StatusOK {
		t.Fatalf("置为 banned 失败: %d %s", status, body)
	}

	status, body := do(t, e, http.MethodGet, messagePath(tenantID, accountID, "?folder=inbox"), token, "")
	if status != http.StatusConflict {
		t.Fatalf("状态 = %d，期望 409\n%s", status, body)
	}
	var payload struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal([]byte(body), &payload)
	if !strings.Contains(payload.Message, "封禁") {
		t.Errorf("提示应说明账号被封，实际 %q", payload.Message)
	}
}

// 托管邮箱的凭据失效，绝不能回 401。
//
// 401 说的是「本次请求的调用方没通过认证」。把上游的 auth_failed 也映射成 401，
// 前端拦截器就会把它当成会话过期：用户导入的一批账号里有一个 token 失效，
// 点开它，人就被踢回登录页了——而他自己的登录明明是好的。
// 对外 API 的调用方还会以为自己的令牌废了，跑去重新签发。
//
// 契约见 05 文档 §1.2：业务码 1005 对应 502，具体原因由 data.error_kind 承载。
// 没有附件时 attachments 必须是 []，不能是 null。
//
// Go 的 nil slice 会被 encoding/json 写成 `null`，而前端的类型声明是非空数组，
// 于是 AttachmentList 里那句 `message.attachments.filter(...)` 会抛 TypeError，
// 被 react-router 的 errorElement 接住——用户丢的是整个页面，不只是附件区。
// 绝大多数邮件都没有附件，所以这是打开邮件详情的**主**路径而不是边角情况。
// 断言落在序列化后的 JSON 文本上：改成 `omitempty` 或换 DTO 都会让字段重新消失，
// 而那两种写法在 Go 侧的类型检查里都是合法的。
func TestDetailAttachmentsAreNeverNull(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	accountID := createAccount(t, e, token, tenantID,
		`{"email":"alice@outlook.com","refresh_token":"M.token"}`)

	status, body := do(t, e, http.MethodGet,
		messagePath(tenantID, accountID, "/"+noAttachmentMessageID+"?folder=inbox"), token, "")
	if status != http.StatusOK {
		t.Fatalf("状态 = %d，期望 200\n%s", status, body)
	}
	if strings.Contains(body, `"attachments":null`) {
		t.Errorf("attachments 序列化成了 null，前端会崩：\n%s", body)
	}
	if !strings.Contains(body, `"attachments":[]`) {
		t.Errorf("attachments 不是空数组：\n%s", body)
	}
}

func TestUpstreamAuthFailureIsNotUnauthorized(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	// 邮箱里的标记让链工厂换上「一律 auth_failed」的假通道。
	accountID := createAccount(t, e, token, tenantID,
		`{"email":"`+upstreamFailEmailMarker+`@outlook.com","refresh_token":"M.token"}`)

	status, body := do(t, e, http.MethodGet, messagePath(tenantID, accountID, "?folder=inbox"), token, "")

	if status == http.StatusUnauthorized {
		t.Fatalf("上游认证失败回了 401，会把用户本人登出\n%s", body)
	}
	if status != http.StatusBadGateway {
		t.Fatalf("状态 = %d，期望 502\n%s", status, body)
	}

	var payload struct {
		Code int `json:"code"`
		Data struct {
			ErrorKind string `json:"error_kind"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("响应体解析失败: %v\n%s", err, body)
	}
	// 状态码退成 502 之后，「具体该怎么处置」全靠这两个字段，不能跟着一起丢。
	if payload.Code != handler.CodeUpstreamMailErr {
		t.Errorf("业务码 = %d，期望 %d", payload.Code, handler.CodeUpstreamMailErr)
	}
	if payload.Data.ErrorKind != string(mailer.ErrKindAuthFailed) {
		t.Errorf("error_kind = %q，期望 %q", payload.Data.ErrorKind, mailer.ErrKindAuthFailed)
	}
}

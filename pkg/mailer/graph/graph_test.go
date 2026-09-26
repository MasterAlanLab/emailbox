package graph

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emailbox/pkg/mailer"
)

// stub 是一个可编程的 Graph + token 端点桩。
type stub struct {
	mu sync.Mutex
	// tokenHandler 收到 scope，返回状态码与响应体。
	tokenHandler func(scope string) (int, string)
	// apiHandler 收到方法与「路径 + 查询串」，返回状态码、响应头与响应体。
	apiHandler func(method, path string) (int, http.Header, string)

	tokenScopes []string
	apiCalls    []string
	server      *httptest.Server
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		scope := form.Get("scope")

		s.mu.Lock()
		s.tokenScopes = append(s.tokenScopes, scope)
		handler := s.tokenHandler
		s.mu.Unlock()

		status, payload := 200, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`
		if handler != nil {
			status, payload = handler(scope)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, payload)
	})
	mux.HandleFunc("/v1.0/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.RequestURI(), "/v1.0")
		s.mu.Lock()
		s.apiCalls = append(s.apiCalls, r.Method+" "+path)
		handler := s.apiHandler
		s.mu.Unlock()

		if handler == nil {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		status, header, payload := handler(r.Method, path)
		for k, vs := range header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, payload)
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) client(cfg Config) *Client {
	cfg.BaseURL = s.server.URL + "/v1.0"
	cfg.TokenURL = s.server.URL + "/token"
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	return New(cfg)
}

func (s *stub) scopes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tokenScopes...)
}

func (s *stub) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.apiCalls...)
}

func testCred() mailer.Credential {
	return mailer.Credential{
		Email:        "user@outlook.com",
		AccountType:  mailer.AccountTypeOutlook,
		ClientID:     "client-id",
		RefreshToken: "refresh-token",
	}
}

func jsonHeader() http.Header {
	return http.Header{"Content-Type": []string{"application/json"}}
}

// scope 三级降级：同一个 client_id 下不同账号实际同意过的权限不一样，
// 拿一套写死的 scope 去换 token，只授权了只读的账号会直接失败。
func TestTokenScopeDegradation(t *testing.T) {
	for _, body := range []string{
		`{"error":"invalid_scope","error_description":"AADSTS70011: scope is not valid"}`,
		`{"error":"invalid_grant","error_codes":[65001]}`,
	} {
		t.Run(body, func(t *testing.T) {
			s := newStub(t)
			s.tokenHandler = func(scope string) (int, string) {
				if strings.Contains(scope, "Mail.ReadWrite") {
					return 400, body
				}
				return 200, `{"access_token":"at","expires_in":3600}`
			}
			s.apiHandler = func(string, string) (int, http.Header, string) {
				return 200, jsonHeader(), `{"value":[]}`
			}

			c := s.client(Config{})
			if _, err := c.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox}); err != nil {
				t.Fatalf("降级后应当成功：%v", err)
			}

			scopes := s.scopes()
			if len(scopes) != 2 {
				t.Fatalf("打了 %d 次 token 端点，期望 2 次（configured → read）：%v", len(scopes), scopes)
			}
			if !strings.Contains(scopes[0], "Mail.ReadWrite") {
				t.Errorf("第一个候选应当包含 Mail.ReadWrite，实际 %q", scopes[0])
			}
			if strings.Contains(scopes[1], "Mail.ReadWrite") {
				t.Errorf("第二个候选应当去掉 Mail.ReadWrite，实际 %q", scopes[1])
			}
		})
	}
}

// 三个候选全挂时要归到 consent_required，让用户知道该去重新授权，
// 而不是笼统的 provider_error。
func TestTokenScopeDegradationExhausted(t *testing.T) {
	s := newStub(t)
	s.tokenHandler = func(string) (int, string) {
		return 403, `{"error":"consent_required"}`
	}
	c := s.client(Config{})

	_, err := c.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindConsentRequired {
		t.Fatalf("错误分类 = %q，期望 consent_required（%v）", mailer.KindOf(err), err)
	}
	if got := len(s.scopes()); got != 3 {
		t.Errorf("试了 %d 个 scope，期望 3 个", got)
	}
}

// 被封与令牌失效必须当场停手：换 scope 是同样的结果，
// 每多试一次都在给已经触发风控的账号加码。
func TestTokenStopsOnTerminalErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want mailer.ErrKind
	}{
		{
			name: "被封",
			body: `{"error":"invalid_grant","error_description":"Application is in service abuse mode"}`,
			want: mailer.ErrKindBanned,
		},
		{
			name: "refresh_token 明确过期",
			body: `{"error":"invalid_grant","error_codes":[700082]}`,
			want: mailer.ErrKindAuthFailed,
		},
		{
			name: "未知 invalid_grant",
			body: `{"error":"invalid_grant"}`,
			want: mailer.ErrKindAuthFailed,
		},
		{
			name: "条件访问不因描述含 consent 而降级",
			body: `{"error":"invalid_grant","error_codes":[53003],"error_description":"consent PRIVATE-CREDENTIAL-MARKER"}`,
			want: mailer.ErrKindAuthFailed,
		},
		{
			name: "交互式登录",
			body: `{"error":"interaction_required"}`,
			want: mailer.ErrKindAuthFailed,
		},
	}
	for _, c := range cases {
		s := newStub(t)
		s.tokenHandler = func(string) (int, string) { return 400, c.body }
		client := s.client(Config{})

		_, err := client.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox})
		if mailer.KindOf(err) != c.want {
			t.Errorf("%s: 分类 = %q，期望 %q", c.name, mailer.KindOf(err), c.want)
		}
		if got := len(s.scopes()); got != 1 {
			t.Errorf("%s: 试了 %d 个 scope，期望停在第 1 个", c.name, got)
		}
		if mailer.Retriable(err) {
			t.Errorf("%s: 这类错误不该允许换通道重试", c.name)
		}
		if err != nil && strings.Contains(err.Error(), "PRIVATE-CREDENTIAL-MARKER") {
			t.Errorf("%s: Graph 错误包含上游凭据", c.name)
		}
	}
}

// 新值出现时通知持久化层，后续续期使用最新值，而不是依赖旧值的剩余寿命。
func TestTokenRotationIsReported(t *testing.T) {
	s := newStub(t)
	s.tokenHandler = func(string) (int, string) {
		return 200, `{"access_token":"at","refresh_token":"rotated","expires_in":3600}`
	}
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 200, jsonHeader(), `{"value":[]}`
	}

	var gotEmail, gotToken string
	c := s.client(Config{OnTokenRefresh: func(email, token string) {
		gotEmail, gotToken = email, token
	}})
	if _, err := c.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox}); err != nil {
		t.Fatal(err)
	}
	if gotEmail != "user@outlook.com" || gotToken != "rotated" {
		t.Fatalf("轮换回调收到 (%q, %q)，期望 (user@outlook.com, rotated)", gotEmail, gotToken)
	}
}

// 返回的 refresh_token 与原来相同时不该触发回调，否则每次拉信都要写一次库。
func TestTokenRotationSkippedWhenUnchanged(t *testing.T) {
	s := newStub(t)
	s.tokenHandler = func(string) (int, string) {
		return 200, `{"access_token":"at","refresh_token":"refresh-token"}`
	}
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 200, jsonHeader(), `{"value":[]}`
	}
	called := false
	c := s.client(Config{OnTokenRefresh: func(string, string) { called = true }})
	if _, err := c.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("refresh_token 没变时不该回调")
	}
}

func TestMissingRefreshTokenFailsFast(t *testing.T) {
	s := newStub(t)
	cred := testCred()
	cred.RefreshToken = "  "
	c := s.client(Config{})

	_, err := c.List(context.Background(), cred, mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindAuthFailed {
		t.Fatalf("分类 = %q，期望 auth_failed", mailer.KindOf(err))
	}
	if len(s.scopes()) != 0 {
		t.Error("没有 refresh_token 就不该打 token 端点")
	}
}

func TestListBuildsQueryAndMapsFields(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 200, jsonHeader(), `{"value":[{
			"id":"AAA",
			"subject":"你好",
			"from":{"emailAddress":{"name":"Sender","address":"s@example.com"}},
			"toRecipients":[{"emailAddress":{"address":"a@example.com"}},
			                {"emailAddress":{"name":"B","address":"b@example.com"}}],
			"receivedDateTime":"2026-08-20T10:00:00Z",
			"isRead":false,
			"hasAttachments":true,
			"bodyPreview":"预览"
		}]}`
	}
	c := s.client(Config{})

	msgs, err := c.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderJunk, Skip: 40, Top: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("返回 %d 条，期望 1 条", len(msgs))
	}
	m := msgs[0]
	if m.ID != "AAA" || m.Subject != "你好" {
		t.Errorf("字段映射错误：%+v", m)
	}
	if m.IDMode != mailer.IDModeNone {
		t.Errorf("Graph 的 id 没有 UID/序列号之分，IDMode 应为空，实际 %q", m.IDMode)
	}
	if m.Folder != mailer.FolderJunk {
		t.Errorf("Folder = %q，期望回填请求的邮件夹", m.Folder)
	}
	if m.From != "Sender <s@example.com>" {
		t.Errorf("From = %q", m.From)
	}
	if m.To != "a@example.com, B <b@example.com>" {
		t.Errorf("To = %q", m.To)
	}
	if !m.ReceivedAt.Equal(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("ReceivedAt = %v", m.ReceivedAt)
	}

	call := s.calls()[0]
	for _, want := range []string{
		"/me/mailFolders/junkemail/messages",
		"%24top=20",
		"%24skip=40",
		"receivedDateTime+desc",
	} {
		if !strings.Contains(call, want) {
			t.Errorf("请求 %q 里缺少 %q", call, want)
		}
	}
}

func TestFolderMapping(t *testing.T) {
	cases := map[mailer.Folder]string{
		mailer.FolderInbox:   "inbox",
		mailer.FolderJunk:    "junkemail",
		mailer.FolderDeleted: "deleteditems",
	}
	for folder, want := range cases {
		got, err := folderPath(folder)
		if err != nil || got != want {
			t.Errorf("folderPath(%q) = (%q, %v)，期望 %q", folder, got, err, want)
		}
	}
	// all 是上层聚合出来的，协议层收到它说明调用方漏了拆分这步。
	if _, err := folderPath(mailer.FolderAll); err == nil {
		t.Error("folder=all 应当明确报错，而不是悄悄只返回收件箱")
	}
	if _, err := folderPath("nonsense"); mailer.KindOf(err) != mailer.ErrKindFolderUnavailable {
		t.Errorf("未知邮件夹应当报 folder_unavailable，实际 %v", err)
	}
}

func TestDetailIncludesAttachmentsAndSurvivesAttachmentFailure(t *testing.T) {
	message := `{"id":"AAA","subject":"s","hasAttachments":true,
		"body":{"contentType":"HTML","content":"<p>hi</p>"}}`

	t.Run("附件列表正常", func(t *testing.T) {
		s := newStub(t)
		s.apiHandler = func(_, path string) (int, http.Header, string) {
			if strings.Contains(path, "/attachments") {
				return 200, jsonHeader(), `{"value":[{"id":"att1","name":"a.pdf",
					"contentType":"application/pdf","size":12,"isInline":false}]}`
			}
			return 200, jsonHeader(), message
		}
		d, err := s.client(Config{}).Detail(context.Background(), testCred(), mailer.FolderInbox, "AAA", "")
		if err != nil {
			t.Fatal(err)
		}
		if d.BodyType != "html" || d.Body != "<p>hi</p>" {
			t.Errorf("正文映射错误：%+v", d)
		}
		if len(d.Attachments) != 1 || d.Attachments[0].Name != "a.pdf" {
			t.Errorf("附件元信息错误：%+v", d.Attachments)
		}
	})

	t.Run("附件列表取不到也要能打开正文", func(t *testing.T) {
		s := newStub(t)
		s.apiHandler = func(_, path string) (int, http.Header, string) {
			if strings.Contains(path, "/attachments") {
				return 500, jsonHeader(), `{"error":{"code":"boom"}}`
			}
			return 200, jsonHeader(), message
		}
		d, err := s.client(Config{}).Detail(context.Background(), testCred(), mailer.FolderInbox, "AAA", "")
		if err != nil {
			t.Fatalf("附件失败不该让整封信打不开：%v", err)
		}
		if len(d.Attachments) != 0 {
			t.Errorf("附件区应当为空，实际 %+v", d.Attachments)
		}
	})

	t.Run("没有附件时不多打一次请求", func(t *testing.T) {
		s := newStub(t)
		s.apiHandler = func(string, string) (int, http.Header, string) {
			return 200, jsonHeader(), `{"id":"AAA","hasAttachments":false,"body":{"contentType":"text","content":"hi"}}`
		}
		if _, err := s.client(Config{}).Detail(context.Background(), testCred(), mailer.FolderInbox, "AAA", ""); err != nil {
			t.Fatal(err)
		}
		for _, call := range s.calls() {
			if strings.Contains(call, "/attachments") {
				t.Errorf("hasAttachments=false 时不该请求附件列表：%s", call)
			}
		}
	})
}

func TestAttachmentDecodesContent(t *testing.T) {
	content := []byte("hello attachment")
	s := newStub(t)
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 200, jsonHeader(), fmt.Sprintf(
			`{"id":"att1","name":"a.txt","contentType":"text/plain","contentBytes":%q}`,
			base64.StdEncoding.EncodeToString(content))
	}
	a, err := s.client(Config{}).Attachment(context.Background(), testCred(), mailer.FolderInbox, "AAA", mailer.IDModeNone, "att1")
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Content) != string(content) {
		t.Errorf("附件内容 = %q", a.Content)
	}
	// Graph 偶尔不给 size，用实际长度兜底，否则前端显示 0 字节。
	if a.Size != int64(len(content)) {
		t.Errorf("Size = %d，期望 %d", a.Size, len(content))
	}
}

// 429 带 Retry-After 时要退避重试。outlookEmail 完全没处理这个，
// 批量场景下不退避会让微软把限流窗口越拉越长。
func TestRetryAfterOn429(t *testing.T) {
	s := newStub(t)
	var hits int
	s.apiHandler = func(string, string) (int, http.Header, string) {
		hits++
		if hits == 1 {
			return 429, http.Header{"Retry-After": []string{"1"}}, `{"error":{"code":"tooManyRequests"}}`
		}
		return 200, jsonHeader(), `{"value":[]}`
	}

	var slept []time.Duration
	c := s.client(Config{})
	c.cfg.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}

	if _, err := c.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox}); err != nil {
		t.Fatalf("退避后应当成功：%v", err)
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Errorf("退避时长 = %v，期望一次 1s", slept)
	}
}

// 一直 429 时不能无限重试，最终要如实报 rate_limited 让上层去排队。
func TestRetryAfterGivesUp(t *testing.T) {
	s := newStub(t)
	var hits int
	s.apiHandler = func(string, string) (int, http.Header, string) {
		hits++
		return 429, http.Header{"Retry-After": []string{"1"}}, `{"error":{"code":"tooManyRequests"}}`
	}
	c := s.client(Config{})
	c.cfg.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := c.List(context.Background(), testCred(), mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindRateLimited {
		t.Fatalf("分类 = %q，期望 rate_limited", mailer.KindOf(err))
	}
	if hits != maxRetries+1 {
		t.Errorf("请求了 %d 次，期望 %d 次（首次 + %d 次重试）", hits, maxRetries+1, maxRetries)
	}
}

func TestRetryAfterIgnoredWhenTooLong(t *testing.T) {
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{"Retry-After": []string{"600"}},
	}
	if _, ok := retryAfter(resp); ok {
		t.Error("Retry-After 超过上限时应当直接失败，而不是把调用挂住 10 分钟")
	}
	resp.Header.Set("Retry-After", "Wed, 21 Oct 2026 07:28:00 GMT")
	if _, ok := retryAfter(resp); ok {
		t.Error("HTTP-date 形式解析不了时应当不重试")
	}
	resp.Header.Del("Retry-After")
	if _, ok := retryAfter(resp); ok {
		t.Error("没有 Retry-After 时不该重试")
	}
	resp.StatusCode = 500
	resp.Header.Set("Retry-After", "1")
	if _, ok := retryAfter(resp); ok {
		t.Error("500 不带 Retry-After 语义，不该在这里重试")
	}
}

func TestClassifyAPIError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   mailer.ErrKind
	}{
		{"被封", 403, `{"error":{"code":"ErrorAccessDenied","message":"service abuse mode"}}`, mailer.ErrKindBanned},
		{"限流", 429, `{"error":{"code":"tooManyRequests"}}`, mailer.ErrKindRateLimited},
		{"令牌无效", 401, `{"error":{"code":"InvalidAuthenticationToken"}}`, mailer.ErrKindAuthFailed},
		{"权限不足", 403, `{"error":{"code":"ErrorAccessDenied"}}`, mailer.ErrKindConsentRequired},
		{"资源不存在", 404, `{"error":{"code":"ErrorItemNotFound"}}`, mailer.ErrKindFolderUnavailable},
		{"服务商故障", 503, `{"error":{"code":"ServiceUnavailable"}}`, mailer.ErrKindProviderError},
		{"响应体不是 JSON 也不能崩", 400, `<html>bad gateway</html>`, mailer.ErrKindProviderError},
	}
	for _, c := range cases {
		err := classifyAPIError(c.status, []byte(c.body))
		if mailer.KindOf(err) != c.want {
			t.Errorf("%s: 分类 = %q，期望 %q", c.name, mailer.KindOf(err), c.want)
		}
	}
}

func TestMarkReadUsesBatch(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(_, path string) (int, http.Header, string) {
		if path != "/$batch" {
			return 404, nil, ""
		}
		// 故意打乱顺序：Graph 不保证 responses 与 requests 同序。
		return 200, jsonHeader(), `{"responses":[
			{"id":"1","status":200},
			{"id":"0","status":200},
			{"id":"2","status":404,"body":{"error":{"code":"ErrorItemNotFound"}}}
		]}`
	}

	items := []mailer.MessageRef{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	result, err := s.client(Config{}).MarkRead(context.Background(), testCred(), items)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 2 || result.Failed != 1 {
		t.Fatalf("成功 %d 失败 %d，期望 2/1", result.Succeeded, result.Failed)
	}
	// responses 的顺序与 requests 不保证一致，必须按 id 对回去。
	if !result.Items[0].OK || !result.Items[1].OK || result.Items[2].OK {
		t.Errorf("结果没有按 id 对回原始顺序：%+v", result.Items)
	}
	if result.Items[2].Ref.ID != "c" {
		t.Errorf("失败项指向 %q，期望 c", result.Items[2].Ref.ID)
	}
	if len(s.calls()) != 1 {
		t.Errorf("发了 %d 个请求，$batch 应当只要 1 个：%v", len(s.calls()), s.calls())
	}
}

// 超过 20 条要自动分批，一整批发过去会被 Graph 整批拒绝。
func TestBatchSplitsAtLimit(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(_, _ string) (int, http.Header, string) {
		return 200, jsonHeader(), `{"responses":[]}`
	}

	items := make([]mailer.MessageRef, 45)
	for i := range items {
		items[i] = mailer.MessageRef{ID: fmt.Sprint(i)}
	}
	result, err := s.client(Config{}).Delete(context.Background(), testCred(), items)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.calls()) != 3 {
		t.Fatalf("发了 %d 个 $batch，期望 3 个（20 + 20 + 5）", len(s.calls()))
	}
	// 桩没有返回任何 response，每条都应当被记成失败而不是悄悄消失。
	if result.Failed != 45 {
		t.Errorf("失败 %d 条，期望 45 条", result.Failed)
	}
}

// $batch 本身不可用时回退逐条，别让这个优化反而降低可用性。
func TestBatchFallsBackToOneByOne(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(method, path string) (int, http.Header, string) {
		if path == "/$batch" {
			return 400, jsonHeader(), `{"error":{"code":"BadRequest","message":"batch not supported"}}`
		}
		if method == "PATCH" {
			return 200, jsonHeader(), `{}`
		}
		return 405, jsonHeader(), `{"error":{"code":"MethodNotAllowed"}}`
	}

	items := []mailer.MessageRef{{ID: "a"}, {ID: "b"}}
	result, err := s.client(Config{}).MarkRead(context.Background(), testCred(), items)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 2 {
		t.Fatalf("回退逐条后应当全部成功，实际 %+v", result)
	}
	if len(s.calls()) != 3 {
		t.Errorf("请求序列 = %v，期望 1 次 $batch + 2 次逐条", s.calls())
	}
}

// 令牌失效这类错误逐条重试也是一样的结果，应当直接抛给回退链。
func TestBatchDoesNotFallBackOnAuthError(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 401, jsonHeader(), `{"error":{"code":"InvalidAuthenticationToken"}}`
	}
	_, err := s.client(Config{}).Delete(context.Background(), testCred(), []mailer.MessageRef{{ID: "a"}})
	if mailer.KindOf(err) != mailer.ErrKindAuthFailed {
		t.Fatalf("分类 = %q，期望 auth_failed", mailer.KindOf(err))
	}
	if len(s.calls()) != 1 {
		t.Errorf("请求了 %d 次，令牌问题下不该逐条重试", len(s.calls()))
	}
}

func TestEmptyBatchIsNoop(t *testing.T) {
	s := newStub(t)
	result, err := s.client(Config{}).MarkRead(context.Background(), testCred(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 0 || result.Failed != 0 || len(s.calls()) != 0 || len(s.scopes()) != 0 {
		t.Errorf("空列表不该产生任何请求：%+v / %v", result, s.calls())
	}
}

func TestContextCancellation(t *testing.T) {
	s := newStub(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.client(Config{}).List(ctx, testCred(), mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindCanceled {
		t.Fatalf("分类 = %q，期望 canceled（%v）", mailer.KindOf(err), err)
	}
}

// 响应头已经到达后取消 context，错误也应归到 canceled；若把 body 读取错误记成
// network，任务页会误导用户检查网络，并且与 IMAP 令牌端点的分类不一致。
func TestTokenResponseReadCancellation(t *testing.T) {
	headersSent := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(headersSent)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-headersSent
		cancel()
	}()
	err := New(Config{TokenURL: server.URL, Timeout: 2 * time.Second}).RefreshToken(ctx, testCred())
	if mailer.KindOf(err) != mailer.ErrKindCanceled {
		t.Fatalf("分类 = %q，期望 canceled（%v）", mailer.KindOf(err), err)
	}
}

// 代理配置错误要归到 proxy_failed 并带上打码后的代理地址，
// 否则用户只看到「请求失败」，根本不知道是代理的问题。
func TestProxyFailureIsAttributedAndMasked(t *testing.T) {
	s := newStub(t)
	cred := testCred()
	cred.Proxy = mailer.ProxyConfig{URL: "ftp://user:secret@proxy:21"}

	_, err := s.client(Config{}).List(context.Background(), cred, mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindProxyFailed {
		t.Fatalf("分类 = %q，期望 proxy_failed", mailer.KindOf(err))
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("错误信息里泄露了代理口令：%v", err)
	}
}

func TestProxyAuthenticationFailureDoesNotFallBackToDirect(t *testing.T) {
	var directCalls, proxyCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		fmt.Fprint(w, `{"access_token":"access"}`)
	}))
	t.Cleanup(tokenServer.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyCalls.Add(1)
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		// reason phrase 故意不是标准文本；分类必须读状态码而不是匹配英文字符串。
		_, _ = fmt.Fprint(rw, "HTTP/1.1 407 Authentication Needed\r\nContent-Length: 0\r\n\r\n")
		_ = rw.Flush()
	}))
	t.Cleanup(proxy.Close)

	cred := testCred()
	cred.Proxy = mailer.ProxyConfig{URL: strings.Replace(proxy.URL, "http://", "http://user:bad-password@", 1)}
	// HTTPS 会让 net/http 先向代理发送 CONNECT；407 此时经 Do 的 error 返回，
	// 而不是一份可交给 OAuth 分类器的普通 HTTP Response。
	tokenURL := strings.Replace(tokenServer.URL, "http://", "https://", 1)
	err := New(Config{TokenURL: tokenURL, Timeout: time.Second}).RefreshToken(context.Background(), cred)
	if mailer.KindOf(err) != mailer.ErrKindProxyFailed {
		t.Fatalf("分类 = %q，期望 proxy_failed（%v）", mailer.KindOf(err), err)
	}
	var upstream *mailer.Error
	if !errors.As(err, &upstream) || len(upstream.Attempts) != 1 {
		t.Fatalf("CONNECT 407 后不应继续备用或直连：%+v", err)
	}
	if proxyCalls.Load() != 1 || directCalls.Load() != 0 {
		t.Fatalf("代理认证失败后仍切换了候选：代理=%d，直连=%d", proxyCalls.Load(), directCalls.Load())
	}
	if strings.Contains(err.Error(), "bad-password") {
		t.Fatal("错误信息泄露了代理口令")
	}
}

func TestGraphAPIProxyAuthenticationFailureDoesNotFallBackToDirect(t *testing.T) {
	var directTokenCalls, proxyCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directTokenCalls.Add(1)
		fmt.Fprint(w, `{"access_token":"direct-access"}`)
	}))
	t.Cleanup(tokenServer.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		// 第一个 HTTP token 请求由代理直接桩成功；随后的 HTTPS Graph 请求走 CONNECT。
		fmt.Fprint(w, `{"access_token":"proxy-access"}`)
	}))
	t.Cleanup(proxy.Close)

	cred := testCred()
	cred.Proxy = mailer.ProxyConfig{URL: strings.Replace(proxy.URL, "http://", "http://user:bad-password@", 1)}
	c := New(Config{
		TokenURL: tokenServer.URL,
		BaseURL:  "https://graph.invalid/v1.0",
		Timeout:  time.Second,
	})
	_, err := c.List(context.Background(), cred, mailer.ListOptions{Folder: mailer.FolderInbox})
	var upstream *mailer.Error
	if !errors.As(err, &upstream) || upstream.Kind != mailer.ErrKindProxyFailed || len(upstream.Attempts) != 1 {
		t.Fatalf("Graph CONNECT 407 后不应继续备用或直连：%+v", err)
	}
	if proxyCalls.Load() != 2 || directTokenCalls.Load() != 0 {
		t.Fatalf("Graph 代理认证失败后仍切换了候选：代理=%d，直连令牌=%d",
			proxyCalls.Load(), directTokenCalls.Load())
	}
	if strings.Contains(err.Error(), "bad-password") {
		t.Fatal("错误信息泄露了代理口令")
	}
}

// 代理连不上时要顺着候选链继续试，最后落到直连。
func TestProxyFailoverFallsBackToDirect(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 200, jsonHeader(), `{"value":[]}`
	}
	cred := testCred()
	// 保留地址段：SOCKS5 握手必定失败，且不会真的打到外网。
	cred.Proxy = mailer.ProxyConfig{URL: "socks5://192.0.2.1:1080"}

	c := s.client(Config{Timeout: 300 * time.Millisecond})
	if _, err := c.List(context.Background(), cred, mailer.ListOptions{Folder: mailer.FolderInbox}); err != nil {
		t.Fatalf("代理不可用时应当回落到直连：%v", err)
	}
}

func TestScopeCandidatesAreDeduped(t *testing.T) {
	got := scopeCandidates()
	if len(got) != 3 {
		t.Fatalf("候选 = %v，期望 3 个", got)
	}
	seen := map[string]bool{}
	for _, s := range got {
		if seen[s] {
			t.Errorf("候选重复：%q", s)
		}
		seen[s] = true
	}
	if !strings.Contains(got[0], "offline_access") {
		t.Errorf("第一个候选必须带 offline_access，否则拿不到新的 refresh_token：%q", got[0])
	}
	if got[2] != mailer.ScopeGraphDefault {
		t.Errorf("最后一个候选 = %q，期望 %q", got[2], mailer.ScopeGraphDefault)
	}
}

func TestShouldDegradeScope(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"一般请求错误不降级", 400, "AADSTS90023: invalid request", false},
		{"一般授权错误不降级", 400, "AADSTS70000: invalid grant", false},
		{"AADSTS70011", 400, "aadsts70011", true},
		{"invalid_scope", 400, `{"error":"invalid_scope"}`, true},
		{"no applicable permissions", 403, "No applicable permissions found", true},
		{"结构化 no applicable permissions", 403, `{"error":"invalid_grant","error_description":"No applicable permissions found"}`, true},
		{"unauthorized or expired", 401, "the permissions requested are unauthorized or expired", true},
		{"结构化 unauthorized or expired", 401, `{"error":"invalid_grant","error_description":"The permissions requested are unauthorized or expired"}`, true},
		{"invalid_grant 不是 scope 问题", 400, `{"error":"invalid_grant"}`, false},
		{"明确权限诊断才降级", 400, `{"error":"invalid_grant","error_codes":[65001]}`, true},
		{"交互登录不降级", 400, `{"error":"interaction_required"}`, false},
		{"客户端配置错误不降级", 400, `{"error":"invalid_client","error_description":"consent invalid scope"}`, false},
		{"5xx 不降级", 500, "invalid_scope", false},
		{"429 不降级", 429, "consent", false},
	}
	for _, c := range cases {
		if got := shouldDegradeScope(c.status, []byte(c.body)); got != c.want {
			t.Errorf("%s: = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestChannelName(t *testing.T) {
	if got := New(Config{}).Channel(); got != mailer.ChannelGraph {
		t.Errorf("Channel = %q，期望 %q", got, mailer.ChannelGraph)
	}
}

// 邮件 id 里出现空格、+、/ 时必须转义，否则拼出来的 URL 指向另一封信。
func TestMessageIDIsEscapedInURL(t *testing.T) {
	s := newStub(t)
	s.apiHandler = func(string, string) (int, http.Header, string) {
		return 200, jsonHeader(), `{"id":"x","body":{"contentType":"text","content":""}}`
	}
	if _, err := s.client(Config{}).Detail(
		context.Background(), testCred(), mailer.FolderInbox, "a b/c+d", ""); err != nil {
		t.Fatal(err)
	}
	call := s.calls()[0]
	if strings.Contains(call, "a b/c+d") {
		t.Fatalf("邮件 id 未转义：%s", call)
	}
	if !strings.Contains(call, url.PathEscape("a b/c+d")) {
		t.Fatalf("请求路径 = %s", call)
	}
}

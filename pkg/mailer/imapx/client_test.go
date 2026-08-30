package imapx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"emailbox/pkg/mailer"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

const (
	testEmail    = "user@example.com"
	testPassword = "app-password"
)

// testServer 是一个进程内的真 IMAP 服务器。
//
// 用真服务器而不是手写的协议桩：SELECT / FETCH / STORE / EXPUNGE 的交互细节
// （UID 与序号、EXPUNGE 后序号重排）用桩很难模拟对，而那恰恰是最容易出错的地方。
type testServer struct {
	addr string
	user *imapmemserver.User
}

func newTestServer(t *testing.T, mailboxes map[string][]string) *testServer {
	t.Helper()

	memServer := imapmemserver.New()
	user := imapmemserver.NewUser(testEmail, testPassword)
	memServer.AddUser(user)

	for name, messages := range mailboxes {
		if err := user.Create(name, nil); err != nil {
			t.Fatalf("创建邮箱 %q 失败: %v", name, err)
		}
		for _, raw := range messages {
			appendMessage(t, user, name, raw)
		}
	}

	caps := imap.CapSet{
		imap.CapIMAP4rev1: {},
		imap.CapUIDPlus:   {},
		imap.CapID:        {},
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(user), nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
		Logger:       discardLogger{},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})
	return &testServer{addr: ln.Addr().String(), user: user}
}

type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}

func appendMessage(t *testing.T, user *imapmemserver.User, mailbox, raw string) {
	t.Helper()
	if _, err := user.Append(mailbox, &literalReader{s: raw}, &imap.AppendOptions{
		Time: time.Now(),
	}); err != nil {
		t.Fatalf("投递邮件到 %q 失败: %v", mailbox, err)
	}
}

type literalReader struct {
	s   string
	pos int
}

func (l *literalReader) Read(p []byte) (int, error) {
	if l.pos >= len(l.s) {
		return 0, io.EOF
	}
	n := copy(p, l.s[l.pos:])
	l.pos += n
	return n, nil
}

func (l *literalReader) Size() int64 { return int64(len(l.s)) }

// client 返回一个连到测试服务器的 IMAP 通道。用明文连接，跳过 TLS。
func (ts *testServer) client() *Client {
	return New(Config{
		Channel: mailer.ChannelIMAP,
		Timeout: 5 * time.Second,
		DialFunc: func(ctx context.Context, _ string, _ int, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", ts.addr)
		},
	})
}

func testCred() mailer.Credential {
	return mailer.Credential{
		Email:        testEmail,
		Provider:     mailer.ProviderCustom,
		AccountType:  mailer.AccountTypeIMAP,
		IMAPHost:     "imap.example.com",
		IMAPPort:     993,
		IMAPPassword: testPassword,
	}
}

func makeMessage(subject, body string) string {
	return "From: Sender <sender@example.com>\r\n" +
		"To: " + testEmail + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: Thu, 20 Aug 2026 10:00:00 +0800\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body + "\r\n"
}

func TestListReturnsNewestFirst(t *testing.T) {
	msgs := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		msgs = append(msgs, makeMessage(fmt.Sprintf("第 %d 封", i), "body"))
	}
	ts := newTestServer(t, map[string][]string{"INBOX": msgs})

	got, err := ts.client().List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("返回 %d 条，期望 5 条", len(got))
	}
	// IMAP 的序号是从旧到新的，列表必须倒过来。
	if got[0].Subject != "第 5 封" || got[4].Subject != "第 1 封" {
		t.Errorf("顺序不对：%q … %q", got[0].Subject, got[4].Subject)
	}
	if got[0].IDMode != mailer.IDModeUID {
		t.Errorf("IDMode = %q，期望 uid", got[0].IDMode)
	}
	if got[0].Folder != mailer.FolderInbox {
		t.Errorf("Folder = %q", got[0].Folder)
	}
	if got[0].From != "Sender <sender@example.com>" {
		t.Errorf("From = %q", got[0].From)
	}
}

// 分页要无重叠无遗漏。这是 P2 验收表第 14 项。
func TestListPagination(t *testing.T) {
	msgs := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		msgs = append(msgs, makeMessage(fmt.Sprintf("m%02d", i), "body"))
	}
	ts := newTestServer(t, map[string][]string{"INBOX": msgs})
	client := ts.client()

	seen := map[string]bool{}
	for skip := 0; skip < 10; skip += 3 {
		page, err := client.List(context.Background(), testCred(),
			mailer.ListOptions{Folder: mailer.FolderInbox, Skip: skip, Top: 3})
		if err != nil {
			t.Fatalf("skip=%d: %v", skip, err)
		}
		for _, m := range page {
			if seen[m.Subject] {
				t.Errorf("skip=%d: %q 重复出现", skip, m.Subject)
			}
			seen[m.Subject] = true
		}
	}
	if len(seen) != 10 {
		t.Errorf("翻完所有页共看到 %d 封，期望 10 封（有遗漏）", len(seen))
	}

	// skip 超过总数是空页，不是错误。
	page, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Skip: 100, Top: 10})
	if err != nil {
		t.Fatalf("越界分页不该报错：%v", err)
	}
	if len(page) != 0 {
		t.Errorf("越界分页返回了 %d 条", len(page))
	}
}

func TestPageRange(t *testing.T) {
	cases := []struct {
		total      uint32
		skip, top  int
		start, end uint32
		ok         bool
	}{
		{total: 10, skip: 0, top: 3, start: 8, end: 10, ok: true},
		{total: 10, skip: 3, top: 3, start: 5, end: 7, ok: true},
		{total: 10, skip: 9, top: 3, start: 1, end: 1, ok: true},
		{total: 10, skip: 8, top: 5, start: 1, end: 2, ok: true},
		{total: 10, skip: 10, top: 3, ok: false},
		{total: 0, skip: 0, top: 3, ok: false},
		{total: 3, skip: 0, top: 100, start: 1, end: 3, ok: true},
	}
	for _, c := range cases {
		start, end, ok := pageRange(c.total, c.skip, c.top)
		if ok != c.ok {
			t.Errorf("pageRange(%d,%d,%d) ok = %v，期望 %v", c.total, c.skip, c.top, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if start != c.start || end != c.end {
			t.Errorf("pageRange(%d,%d,%d) = %d..%d，期望 %d..%d",
				c.total, c.skip, c.top, start, end, c.start, c.end)
		}
	}
}

func TestListEmptyMailbox(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": nil})
	got, err := ts.client().List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("空邮箱返回了 %d 条", len(got))
	}
}

func TestDetailAndAttachment(t *testing.T) {
	raw := multipartMessage
	ts := newTestServer(t, map[string][]string{"INBOX": {raw}})
	client := ts.client()

	list, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("列表返回 %d 条", len(list))
	}
	if !list[0].HasAttachments {
		t.Error("BODYSTRUCTURE 里有附件，列表却说没有")
	}

	detail, err := client.Detail(context.Background(), testCred(),
		mailer.FolderInbox, list[0].ID, list[0].IDMode)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Subject != "测试" {
		t.Errorf("Subject = %q", detail.Subject)
	}
	if detail.BodyType != "html" || !strings.Contains(detail.Body, "HTML正文") {
		t.Errorf("Body = (%q, %q)", detail.Body, detail.BodyType)
	}
	if len(detail.Attachments) != 1 || detail.Attachments[0].Name != "evil.pdf" {
		t.Fatalf("附件元信息 = %+v", detail.Attachments)
	}

	att, err := client.Attachment(context.Background(), testCred(),
		mailer.FolderInbox, list[0].ID, list[0].IDMode, detail.Attachments[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(att.Content) != "PDF-CONTENT" {
		t.Errorf("附件内容 = %q", att.Content)
	}
}

// BODY.PEEK 不能把邮件标成已读。少了 PEEK 的话，用户点开列表就等于全标已读。
func TestDetailDoesNotMarkAsRead(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": {makeMessage("hi", "body")}})
	client := ts.client()

	list, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Detail(context.Background(), testCred(),
		mailer.FolderInbox, list[0].ID, list[0].IDMode); err != nil {
		t.Fatal(err)
	}

	after, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	if after[0].IsRead {
		t.Error("读详情把邮件标成了已读，BODY.PEEK 没生效")
	}
}

func TestMarkRead(t *testing.T) {
	ts := newTestServer(t, map[string][]string{
		"INBOX": {makeMessage("a", "1"), makeMessage("b", "2")},
	})
	client := ts.client()

	list, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	refs := []mailer.MessageRef{
		{ID: list[0].ID, IDMode: list[0].IDMode, Folder: mailer.FolderInbox},
	}
	result, err := client.MarkRead(context.Background(), testCred(), refs)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("结果 = %+v", result)
	}

	after, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range after {
		want := m.ID == list[0].ID
		if m.IsRead != want {
			t.Errorf("%q 的已读状态 = %v，期望 %v", m.Subject, m.IsRead, want)
		}
	}
}

// 删除必须是「打 \Deleted + EXPUNGE」两步。只打标记的话邮件还在，
// 用户会以为没删掉。
func TestDeleteExpunges(t *testing.T) {
	ts := newTestServer(t, map[string][]string{
		"INBOX": {makeMessage("keep", "1"), makeMessage("remove", "2")},
	})
	client := ts.client()

	list, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	var target mailer.Message
	for _, m := range list {
		if m.Subject == "remove" {
			target = m
		}
	}
	result, err := client.Delete(context.Background(), testCred(), []mailer.MessageRef{
		{ID: target.ID, IDMode: target.IDMode, Folder: mailer.FolderInbox},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 1 {
		t.Fatalf("结果 = %+v", result)
	}

	after, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Subject != "keep" {
		t.Errorf("删除后剩下 %d 封：%+v", len(after), after)
	}
}

// 批量操作跨邮件夹时要按邮件夹分组：IMAP 一次只能选中一个邮箱，
// 逐封 SELECT 的话删 100 封要 SELECT 100 次。
func TestBatchGroupsByFolder(t *testing.T) {
	ts := newTestServer(t, map[string][]string{
		"INBOX": {makeMessage("inbox-1", "a")},
		"Junk":  {makeMessage("junk-1", "b")},
	})
	client := ts.client()

	inbox, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if err != nil {
		t.Fatal(err)
	}
	junk, err := client.List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderJunk, Top: 10})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.MarkRead(context.Background(), testCred(), []mailer.MessageRef{
		{ID: inbox[0].ID, IDMode: inbox[0].IDMode, Folder: mailer.FolderInbox},
		{ID: junk[0].ID, IDMode: junk[0].IDMode, Folder: mailer.FolderJunk},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 2 {
		t.Fatalf("跨邮件夹批量结果 = %+v", result)
	}
}

// UID 与序列号不能混进同一个 NumSet——那正是「操作到错误邮件」的来源。
func TestSplitByIDMode(t *testing.T) {
	items := []mailer.MessageRef{
		{ID: "10", IDMode: mailer.IDModeUID},
		{ID: "3", IDMode: mailer.IDModeSequence},
		{ID: "11", IDMode: mailer.IDModeUID},
		{ID: "abc", IDMode: mailer.IDModeUID},
		{ID: "0", IDMode: mailer.IDModeUID},
		{ID: "", IDMode: mailer.IDModeSequence},
	}
	uids, seqs, bad := splitByIDMode(items)
	if len(uids.refs) != 2 {
		t.Errorf("UID 组 %d 条，期望 2 条", len(uids.refs))
	}
	if len(seqs.refs) != 1 {
		t.Errorf("序号组 %d 条，期望 1 条", len(seqs.refs))
	}
	if len(bad) != 3 {
		t.Errorf("非法 id %d 条，期望 3 条", len(bad))
	}
	if got := uids.set.String(); got != "10:11" {
		t.Errorf("UID 集合 = %q", got)
	}
	if got := seqs.set.String(); got != "3" {
		t.Errorf("序号集合 = %q", got)
	}
}

func TestNumSetForRespectsIDMode(t *testing.T) {
	uidSet, err := numSetFor("42", mailer.IDModeUID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := uidSet.(imap.UIDSet); !ok {
		t.Errorf("idMode=uid 应当产出 UIDSet，实际 %T", uidSet)
	}
	seqSet, err := numSetFor("42", mailer.IDModeSequence)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := seqSet.(imap.SeqSet); !ok {
		t.Errorf("idMode=sequence 应当产出 SeqSet，实际 %T", seqSet)
	}
}

// 候选表没命中时要 LIST 出所有邮箱按别名重试。
func TestSelectFallsBackToListing(t *testing.T) {
	ts := newTestServer(t, map[string][]string{
		"INBOX":       {makeMessage("a", "1")},
		"My Spam Box": {makeMessage("junk", "2")},
	})
	got, err := ts.client().List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderJunk, Top: 10})
	if err != nil {
		t.Fatalf("候选表没命中时应当靠 LIST 兜底：%v", err)
	}
	if len(got) != 1 || got[0].Subject != "junk" {
		t.Errorf("匹配到了错误的邮箱：%+v", got)
	}
}

// 两轮都找不到时要报 folder_unavailable，并带上试过哪些名字——
// 没有这个诊断信息，「垃圾箱打不开」的工单无从下手。
func TestSelectReportsDiagnostics(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": {makeMessage("a", "1")}})
	_, err := ts.client().List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderJunk, Top: 10})
	if mailer.KindOf(err) != mailer.ErrKindFolderUnavailable {
		t.Fatalf("分类 = %q，期望 folder_unavailable（%v）", mailer.KindOf(err), err)
	}
	var e *mailer.Error
	if !asMailerError(err, &e) {
		t.Fatal("期望结构化错误")
	}
	if !strings.Contains(e.Detail, "Junk") {
		t.Errorf("诊断信息里应当列出试过的名字，实际 %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "INBOX") {
		t.Errorf("诊断信息里应当列出服务器上的邮箱，实际 %q", e.Detail)
	}
}

// folder=all 要由上层拆分，协议层收到它说明调用方漏了这步。
func TestFolderAllIsRejected(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": nil})
	_, err := ts.client().List(context.Background(), testCred(),
		mailer.ListOptions{Folder: mailer.FolderAll, Top: 10})
	if err == nil {
		t.Fatal("folder=all 应当明确报错，而不是悄悄只返回收件箱")
	}
}

func TestWrongPasswordIsClassified(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": nil})
	cred := testCred()
	cred.IMAPPassword = "wrong"

	_, err := ts.client().List(context.Background(), cred,
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if mailer.KindOf(err) != mailer.ErrKindAuthFailed {
		t.Fatalf("分类 = %q，期望 auth_failed（%v）", mailer.KindOf(err), err)
	}
	// 鉴权失败换通道也是一样的结果。
	if mailer.Retriable(err) {
		t.Error("鉴权失败不该允许换通道重试")
	}
}

func TestMissingPasswordFailsFast(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": nil})
	cred := testCred()
	cred.IMAPPassword = ""
	cred.Password = ""

	_, err := ts.client().List(context.Background(), cred,
		mailer.ListOptions{Folder: mailer.FolderInbox, Top: 10})
	if mailer.KindOf(err) != mailer.ErrKindAuthFailed {
		t.Fatalf("分类 = %q，期望 auth_failed", mailer.KindOf(err))
	}
}

func TestEmptyBatchIsNoop(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": nil})
	result, err := ts.client().MarkRead(context.Background(), testCred(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 0 || result.Failed != 0 {
		t.Errorf("空列表结果 = %+v", result)
	}
}

func TestChannelName(t *testing.T) {
	cases := map[string]string{
		"":                    mailer.ChannelIMAP,
		mailer.ChannelIMAPNew: mailer.ChannelIMAPNew,
		mailer.ChannelIMAPOld: mailer.ChannelIMAPOld,
	}
	for in, want := range cases {
		if got := New(Config{Channel: in}).Channel(); got != want {
			t.Errorf("Channel(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// login.live.com 的请求体不能带 scope，带了反而会失败。
func TestTokenEndpointPerChannel(t *testing.T) {
	endpoint, scope := tokenEndpoint(mailer.ChannelIMAPOld)
	if endpoint != mailer.TokenURLLive {
		t.Errorf("旧版通道端点 = %q", endpoint)
	}
	if scope != "" {
		t.Errorf("旧版通道不能带 scope，实际 %q", scope)
	}
	endpoint, scope = tokenEndpoint(mailer.ChannelIMAPNew)
	if endpoint != mailer.TokenURLIMAP {
		t.Errorf("新版通道端点 = %q", endpoint)
	}
	if scope != mailer.ScopeIMAP {
		t.Errorf("新版通道 scope = %q", scope)
	}
}

func TestRefreshTokenExchangesOAuthWithoutIMAP(t *testing.T) {
	for _, channel := range []string{mailer.ChannelIMAPNew, mailer.ChannelIMAPOld} {
		t.Run(channel, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := requests.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.Form.Get("grant_type") != "refresh_token" ||
					r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" ||
					r.Form.Get("refresh_token") != "old-token" {
					t.Errorf("令牌请求参数错误：%s %v", r.Method, r.Form)
				}
				if channel == mailer.ChannelIMAPOld && r.Form.Has("scope") {
					t.Error("旧版 IMAP 的令牌请求携带了 scope")
				}
				if channel == mailer.ChannelIMAPNew && r.Form.Get("scope") != mailer.ScopeIMAP {
					t.Errorf("新版 IMAP 的 scope = %q", r.Form.Get("scope"))
				}
				w.Header().Set("Content-Type", "application/json")
				if request == 1 {
					fmt.Fprint(w, `{"access_token":"access","refresh_token":"new-token"}`)
					return
				}
				// 交换成功也可能没有轮换值，成功状态不应依赖 token 字符串发生变化。
				fmt.Fprint(w, `{"access_token":"access"}`)
			}))
			defer srv.Close()
			var dials, rotations int
			c := New(Config{
				Channel: channel, TokenURL: srv.URL, Timeout: time.Second,
				DialFunc: func(context.Context, string, int, string) (net.Conn, error) {
					dials++
					return nil, errors.New("纯令牌刷新调用了 IMAP 拨号")
				},
				OnTokenRefresh: func(email, token string) {
					rotations++
					if email != testEmail || token != "new-token" {
						t.Errorf("轮换回调 = %q %q", email, token)
					}
				},
			})
			cred := mailer.Credential{
				Email: testEmail, ClientID: "client", ClientSecret: "secret", RefreshToken: "old-token",
			}
			for range 2 {
				if err := c.RefreshToken(context.Background(), cred); err != nil {
					t.Fatal(err)
				}
			}
			if requests.Load() != 2 || rotations != 1 || dials != 0 {
				t.Fatalf("令牌请求/轮换/IMAP拨号 = %d/%d/%d", requests.Load(), rotations, dials)
			}
		})
	}
}

func TestRefreshTokenRejectsPasswordChannel(t *testing.T) {
	c := New(Config{Channel: mailer.ChannelIMAP})
	err := c.RefreshToken(context.Background(), testCred())
	if mailer.KindOf(err) != mailer.ErrKindProviderError || !strings.Contains(err.Error(), "不支持令牌刷新") {
		t.Fatalf("密码通道应明确说明刷新不适用，实际 %v", err)
	}
}

func TestRefreshTokenProxyFailures(t *testing.T) {
	for _, mode := range []string{"config", "network", "auth", "proxy_auth", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			var directCalls, proxyCalls atomic.Int32
			tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				directCalls.Add(1)
				fmt.Fprint(w, `{"access_token":"access"}`)
			}))
			defer tokenServer.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyCalls.Add(1)
				io.Copy(io.Discard, r.Body)
				if mode == "timeout" {
					<-r.Context().Done()
					return
				}
				if mode == "proxy_auth" {
					w.WriteHeader(http.StatusProxyAuthRequired)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
			}))
			defer proxy.Close()
			proxyURL := strings.Replace(proxy.URL, "http://", "http://user:proxy-password@", 1)
			if mode == "config" {
				proxyURL = "ftp://user:proxy-password@127.0.0.1:21"
			}
			if mode == "network" {
				proxy.Close()
			}
			tokenURL := tokenServer.URL
			if mode == "proxy_auth" {
				// 生产 token 端点是 HTTPS，407 发生在 CONNECT 阶段并经 Do 的 error 返回。
				tokenURL = strings.Replace(tokenURL, "http://", "https://", 1)
			}
			c := New(Config{Channel: mailer.ChannelIMAPNew, TokenURL: tokenURL, Timeout: 100 * time.Millisecond})
			cred := mailer.Credential{Email: testEmail, RefreshToken: "token", Proxy: mailer.ProxyConfig{URL: proxyURL}}
			err := c.RefreshToken(context.Background(), cred)
			if mode == "network" || mode == "timeout" {
				if err != nil || directCalls.Load() != 1 {
					t.Fatalf("连接失败应切到直连：err=%v，直连次数=%d", err, directCalls.Load())
				}
				return
			}
			wantKind, wantProxyCalls := mailer.ErrKindProxyFailed, int32(0)
			switch mode {
			case "auth":
				wantKind, wantProxyCalls = mailer.ErrKindAuthFailed, 1
			case "proxy_auth":
				wantKind, wantProxyCalls = mailer.ErrKindProxyFailed, 1
			}
			var e *mailer.Error
			if !errors.As(err, &e) || e.Kind != wantKind || len(e.Attempts) != 1 {
				t.Fatalf("错误分类/尝试记录错误：%+v", err)
			}
			if directCalls.Load() != 0 || proxyCalls.Load() != wantProxyCalls {
				t.Errorf("配置/认证错误发生后继续了代理候选：直连=%d，代理=%d", directCalls.Load(), proxyCalls.Load())
			}
			if strings.Contains(fmt.Sprint(e, e.Attempts), "proxy-password") {
				t.Error("失败记录泄露了代理口令")
			}
		})
	}
}

type proxyAuthDialer struct{}

func (proxyAuthDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("username/password authentication failed")
}

func TestIMAPProxyAuthenticationFailureDoesNotFallBackToDirect(t *testing.T) {
	c := New(Config{Channel: mailer.ChannelIMAPNew})
	cred := mailer.Credential{
		Email: testEmail,
		Proxy: mailer.ProxyConfig{URL: "socks5://user:bad-password@proxy.invalid:1080"},
	}
	var calls int
	_, err := withProxy(context.Background(), c, cred, func(string) (struct{}, error) {
		calls++
		_, err := mailer.DialTLS(context.Background(), proxyAuthDialer{}, "imap.example.com", 993)
		return struct{}{}, err
	})
	var upstream *mailer.Error
	if !errors.As(err, &upstream) || upstream.Kind != mailer.ErrKindProxyFailed ||
		len(upstream.Attempts) != 1 || calls != 1 {
		t.Fatalf("SOCKS 认证失败后不应继续备用或直连：calls=%d err=%+v", calls, err)
	}
	if strings.Contains(fmt.Sprint(upstream, upstream.Attempts), "bad-password") {
		t.Fatal("错误信息泄露了代理口令")
	}
}

func TestRefreshTokenCancellationStopsProxyFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var directCalls atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		fmt.Fprint(w, `{"access_token":"access"}`)
	}))
	defer tokenServer.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	}))
	defer proxy.Close()
	c := New(Config{Channel: mailer.ChannelIMAPNew, TokenURL: tokenServer.URL, Timeout: time.Second})
	cred := mailer.Credential{Email: testEmail, RefreshToken: "token", Proxy: mailer.ProxyConfig{URL: proxy.URL}}
	err := c.RefreshToken(ctx, cred)
	if mailer.KindOf(err) != mailer.ErrKindCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("调用方取消应保留 canceled 和原始 cause，实际 %v", err)
	}
	if directCalls.Load() != 0 {
		t.Error("调用方取消后仍尝试了直连")
	}
}

func TestTokenCancellationWhileReadingResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, "{")
		http.NewResponseController(w).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	hc := &http.Client{Transport: cancelOnReadTransport{RoundTripper: transport, cancel: cancel}, Timeout: time.Second}
	c := New(Config{Channel: mailer.ChannelIMAPNew, TokenURL: srv.URL})
	_, err := c.fetchAccessToken(ctx, hc, mailer.Credential{Email: testEmail, RefreshToken: "token"})
	if mailer.KindOf(err) != mailer.ErrKindCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("读响应期间取消应保留 canceled，而不是 network：%v", err)
	}
}

// 在响应头已返回、开始读正文时取消，避免依赖 sleep 猜测当前请求阶段。
type cancelOnReadTransport struct {
	http.RoundTripper
	cancel context.CancelFunc
}

func (t cancelOnReadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.RoundTripper.RoundTrip(req)
	if err == nil {
		resp.Body = cancelOnReadBody{ReadCloser: resp.Body, cancel: t.cancel}
	}
	return resp, err
}

type cancelOnReadBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelOnReadBody) Read(p []byte) (int, error) {
	b.cancel()
	return b.ReadCloser.Read(p)
}

func TestRefreshTokenRejectsMalformedSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"refresh_token":"new-token"}`)
	}))
	defer srv.Close()
	var rotated bool
	c := New(Config{
		Channel: mailer.ChannelIMAPNew, TokenURL: srv.URL, Timeout: time.Second,
		OnTokenRefresh: func(string, string) { rotated = true },
	})
	err := c.RefreshToken(context.Background(), mailer.Credential{Email: testEmail, RefreshToken: "old-token"})
	if mailer.KindOf(err) != mailer.ErrKindProviderError || rotated {
		t.Fatalf("缺少 access_token 应报上游响应错误且不轮换，实际 err=%v，轮换=%v", err, rotated)
	}
}

func TestIMAPServerPerChannel(t *testing.T) {
	cred := mailer.Credential{Email: "u@outlook.com"}
	if host, _ := imapServer(mailer.ChannelIMAPNew, cred); host != mailer.IMAPServerNew {
		t.Errorf("新版通道主机 = %q", host)
	}
	if host, _ := imapServer(mailer.ChannelIMAPOld, cred); host != mailer.IMAPServerOld {
		t.Errorf("旧版通道主机 = %q", host)
	}
	// 密码通道：先用账号自己填的，没填才按域名推断。
	explicit := mailer.Credential{Email: "u@gmail.com", IMAPHost: "custom.example.com"}
	if host, _ := imapServer(mailer.ChannelIMAP, explicit); host != "custom.example.com" {
		t.Errorf("显式主机被忽略了：%q", host)
	}
	inferred := mailer.Credential{Email: "u@gmail.com"}
	if host, _ := imapServer(mailer.ChannelIMAP, inferred); host != "imap.gmail.com" {
		t.Errorf("按域名推断失败：%q", host)
	}
	if _, port := imapServer(mailer.ChannelIMAP, inferred); port != mailer.DefaultIMAPPort {
		t.Errorf("端口没有回落到默认值")
	}
}

// XOAUTH2 失败时服务器先发一个带错误详情的挑战，客户端必须回一个空串才能结束。
// 不回的话连接会挂住——批量刷新时表现为「卡死」，比失败难查得多。
func TestXOAUTH2AnswersFailureChallenge(t *testing.T) {
	c := &xoauth2Client{username: testEmail, token: "tok"}
	mech, ir, err := c.Start()
	if err != nil {
		t.Fatal(err)
	}
	if mech != "XOAUTH2" {
		t.Errorf("机制 = %q", mech)
	}
	want := "user=" + testEmail + "\x01auth=Bearer tok\x01\x01"
	if string(ir) != want {
		t.Errorf("初始响应 = %q，期望 %q", ir, want)
	}
	resp, err := c.Next([]byte(`{"status":"400"}`))
	if err != nil {
		t.Fatalf("失败挑战必须回一个空串：%v", err)
	}
	if len(resp) != 0 {
		t.Errorf("回复 = %q，期望空串", resp)
	}
	if _, err := c.Next([]byte("again")); err == nil {
		t.Error("第二轮挑战应当报错")
	}
}

func TestContextCancellation(t *testing.T) {
	ts := newTestServer(t, map[string][]string{"INBOX": nil})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ts.client().List(ctx, testCred(), mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindCanceled {
		t.Fatalf("分类 = %q，期望 canceled（%v）", mailer.KindOf(err), err)
	}
}

// 代理配置写错时不能顺着候选链滑到直连——用户以为流量走代理，实际是裸连。
func TestProxyConfigErrorDoesNotFallBackToDirect(t *testing.T) {
	c := New(Config{Channel: mailer.ChannelIMAP, Timeout: time.Second})
	cred := testCred()
	cred.Proxy = mailer.ProxyConfig{URL: "http://user:secret@proxy:8080"}

	_, err := c.List(context.Background(), cred, mailer.ListOptions{Folder: mailer.FolderInbox})
	if mailer.KindOf(err) != mailer.ErrKindProxyFailed {
		t.Fatalf("分类 = %q，期望 proxy_failed（%v）", mailer.KindOf(err), err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("错误信息里泄露了代理口令：%v", err)
	}
}

func TestGroupByFolderPreservesOrder(t *testing.T) {
	items := []mailer.MessageRef{
		{ID: "1", Folder: mailer.FolderJunk},
		{ID: "2", Folder: mailer.FolderInbox},
		{ID: "3", Folder: mailer.FolderJunk},
	}
	groups := groupByFolder(items)
	if len(groups) != 2 {
		t.Fatalf("分了 %d 组，期望 2 组", len(groups))
	}
	if groups[0].folder != mailer.FolderJunk || len(groups[0].items) != 2 {
		t.Errorf("第一组 = %+v", groups[0])
	}
	if groups[1].folder != mailer.FolderInbox {
		t.Errorf("第二组 = %+v", groups[1])
	}
}

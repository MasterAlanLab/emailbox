package mailer

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fakeClient 是一个可编程的通道实现：每次调用返回预设结果，并记录被调过。
type fakeClient struct {
	channel string
	err     error
	calls   int
}

func (f *fakeClient) Channel() string { return f.channel }

func (f *fakeClient) List(context.Context, Credential, ListOptions) ([]Message, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []Message{{ID: f.channel}}, nil
}

func (f *fakeClient) Detail(context.Context, Credential, Folder, string, string) (*Detail, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &Detail{Message: Message{ID: f.channel}}, nil
}

func (f *fakeClient) Attachment(context.Context, Credential, Folder, string, string, string) (*Attachment, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &Attachment{}, nil
}

func (f *fakeClient) MarkRead(context.Context, Credential, []MessageRef) (BatchResult, error) {
	f.calls++
	if f.err != nil {
		return BatchResult{}, f.err
	}
	return BatchResult{Succeeded: 1}, nil
}

func (f *fakeClient) Delete(context.Context, Credential, []MessageRef) (BatchResult, error) {
	f.calls++
	if f.err != nil {
		return BatchResult{}, f.err
	}
	return BatchResult{Succeeded: 1}, nil
}

func outlookCred(lastChannel string) Credential {
	return Credential{Email: "user@outlook.com", AccountType: AccountTypeOutlook, AuthChannel: lastChannel}
}

// auth_channel 决定先试哪条通道。这是「大部分账号不必每次都从 Graph 重试一遍」的
// 全部依据，顺序错了就是每个账号每次都多付两次失败的代价。
func TestChannelOrder(t *testing.T) {
	cases := []struct {
		name string
		cred Credential
		want []string
	}{
		{
			name: "密码鉴权账号只有一条通道",
			cred: Credential{Email: "u@gmail.com", AccountType: AccountTypeIMAP},
			want: []string{ChannelIMAP},
		},
		{
			name: "Outlook 未记录通道时按默认顺序",
			cred: outlookCred(""),
			want: []string{ChannelGraph, ChannelIMAPNew, ChannelIMAPOld},
		},
		{
			name: "上次是 Graph 时顺序不变",
			cred: outlookCred(ChannelGraph),
			want: []string{ChannelGraph, ChannelIMAPNew, ChannelIMAPOld},
		},
		{
			name: "上次是新版 IMAP 时把它提到最前",
			cred: outlookCred(ChannelIMAPNew),
			want: []string{ChannelIMAPNew, ChannelGraph, ChannelIMAPOld},
		},
		{
			name: "上次是旧版 IMAP 时把它提到最前",
			cred: outlookCred(ChannelIMAPOld),
			want: []string{ChannelIMAPOld, ChannelGraph, ChannelIMAPNew},
		},
		{
			// 账号类型被改过时库里可能残留 imap 这种不在候选表里的值。
			name: "记录的通道不在候选表里则忽略它",
			cred: outlookCred(ChannelIMAP),
			want: []string{ChannelGraph, ChannelIMAPNew, ChannelIMAPOld},
		},
	}
	for _, c := range cases {
		if got := ChannelOrder(c.cred); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: 顺序 = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// 可回退的错误应当继续往下试，且成功后要把整条失败路径交给 OnSuccess——
// 主代理长期故障这类问题只有在「最终成功」的调用里才看得见。
func TestChainFallsBackAndReportsAttempts(t *testing.T) {
	graph := &fakeClient{channel: ChannelGraph, err: newError(ErrKindNetwork, ChannelGraph, "连接超时", nil)}
	imapNew := &fakeClient{channel: ChannelIMAPNew}
	imapOld := &fakeClient{channel: ChannelIMAPOld}
	chain := NewChain(map[string]Client{
		ChannelGraph:   graph,
		ChannelIMAPNew: imapNew,
		ChannelIMAPOld: imapOld,
	})

	var got ChannelSuccess
	chain.OnSuccess = func(_ Credential, result ChannelSuccess) { got = result }

	msgs, err := chain.List(context.Background(), outlookCred(""), ListOptions{Folder: FolderInbox})
	if err != nil {
		t.Fatalf("回退后应当成功：%v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != ChannelIMAPNew {
		t.Fatalf("返回的是 %v，期望来自 %s 的结果", msgs, ChannelIMAPNew)
	}
	if imapOld.calls != 0 {
		t.Error("已经成功过就不该再试第三条通道")
	}
	if got.Channel != ChannelIMAPNew {
		t.Errorf("回执通道 = %q，期望 %q", got.Channel, ChannelIMAPNew)
	}
	if len(got.Attempts) != 1 || got.Attempts[0].Kind != ErrKindNetwork {
		t.Errorf("失败记录 = %+v，期望一条 network", got.Attempts)
	}
}

// 不可回退的错误必须当场停手：换通道用的是同一个 token / 同一个被封账号，
// 继续试只会拖长响应并加重上游风控。
func TestChainStopsOnNonRetriableError(t *testing.T) {
	// auth_failed 不在此列：Graph 的授权失败要继续回退，见 TestChainFallsBackOnGraphAuthFailure。
	for _, kind := range []ErrKind{ErrKindBanned, ErrKindConsentRequired} {
		graph := &fakeClient{channel: ChannelGraph, err: newError(kind, ChannelGraph, "拒绝", nil)}
		imapNew := &fakeClient{channel: ChannelIMAPNew}
		chain := NewChain(map[string]Client{ChannelGraph: graph, ChannelIMAPNew: imapNew})

		called := false
		chain.OnSuccess = func(Credential, ChannelSuccess) { called = true }

		_, err := chain.List(context.Background(), outlookCred(""), ListOptions{})
		if KindOf(err) != kind {
			t.Errorf("%s: 错误分类 = %q，期望原样透出", kind, KindOf(err))
		}
		if imapNew.calls != 0 {
			t.Errorf("%s: 不可回退的错误后仍试了下一条通道", kind)
		}
		if called {
			t.Errorf("%s: 失败时不该触发 OnSuccess", kind)
		}
		var e *Error
		if !errors.As(err, &e) || len(e.Attempts) != 1 {
			t.Errorf("%s: 失败记录应当附在错误上，实际 %+v", kind, err)
		}
	}
}

// Graph 与 IMAP 拿同一个 refresh_token 申请的是不同 scope，微软可以只拒其中一套。
// 因此 Graph 的 auth_failed 必须继续回退——否则「Graph 无权、IMAP 可用」的账号
// 会在第一步就被判死，尽管它的信完全拉得到。
func TestChainFallsBackOnGraphAuthFailure(t *testing.T) {
	graph := &fakeClient{channel: ChannelGraph, err: newError(ErrKindAuthFailed, ChannelGraph, "令牌失效", nil)}
	imapNew := &fakeClient{channel: ChannelIMAPNew}
	chain := NewChain(map[string]Client{ChannelGraph: graph, ChannelIMAPNew: imapNew})

	if _, err := chain.List(context.Background(), outlookCred(""), ListOptions{}); err != nil {
		t.Fatalf("Graph 授权失败后应回退到 IMAP，实际 %v", err)
	}
	if imapNew.calls != 1 {
		t.Errorf("IMAP 通道调用次数 = %d，期望 1", imapNew.calls)
	}
}

// 放宽只针对 Graph：IMAP 侧的 auth_failed 是账号自身没开 IMAP 或授权码写错，
// 换另一条 IMAP 通道结果一样，继续试只是白白多打一次上游。
func TestChainStopsOnIMAPAuthFailure(t *testing.T) {
	imapNew := &fakeClient{channel: ChannelIMAPNew, err: newError(ErrKindAuthFailed, ChannelIMAPNew, "未开启 IMAP", nil)}
	imapOld := &fakeClient{channel: ChannelIMAPOld}
	chain := NewChain(map[string]Client{ChannelIMAPNew: imapNew, ChannelIMAPOld: imapOld})

	_, err := chain.List(context.Background(), outlookCred(""), ListOptions{})
	if KindOf(err) != ErrKindAuthFailed {
		t.Errorf("错误分类 = %q，期望 auth_failed", KindOf(err))
	}
	if imapOld.calls != 0 {
		t.Error("IMAP 授权失败后不该再试下一条 IMAP 通道")
	}
}

// 全部通道失败时要保留整条路径，否则排障只看得到最后一条通道的报错。
func TestChainReportsAllAttemptsWhenEveryChannelFails(t *testing.T) {
	failing := func(channel string) Client {
		return &fakeClient{channel: channel, err: newError(ErrKindNetwork, channel, "连接超时", nil)}
	}
	chain := NewChain(map[string]Client{
		ChannelGraph:   failing(ChannelGraph),
		ChannelIMAPNew: failing(ChannelIMAPNew),
		ChannelIMAPOld: failing(ChannelIMAPOld),
	})
	_, err := chain.List(context.Background(), outlookCred(""), ListOptions{})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望结构化错误，实际 %v", err)
	}
	if len(e.Attempts) != 3 {
		t.Fatalf("失败记录 = %d 条，期望 3 条", len(e.Attempts))
	}
	for i, want := range []string{ChannelGraph, ChannelIMAPNew, ChannelIMAPOld} {
		if e.Attempts[i].Channel != want {
			t.Errorf("第 %d 条记录的通道 = %q，期望 %q", i, e.Attempts[i].Channel, want)
		}
	}
}

// 调用方取消或整体超时后不该再消耗剩余通道。
func TestChainStopsWhenContextCanceled(t *testing.T) {
	graph := &fakeClient{channel: ChannelGraph}
	chain := NewChain(map[string]Client{ChannelGraph: graph})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := chain.List(ctx, outlookCred(""), ListOptions{})
	if KindOf(err) != ErrKindCanceled {
		t.Errorf("错误分类 = %q，期望 canceled", KindOf(err))
	}
	if graph.calls != 0 {
		t.Error("context 已取消却仍然发起了请求")
	}
}

// 五个接口方法共享同一套回退逻辑，逐个确认没有哪个方法漏接。
func TestChainFallbackAppliesToEveryMethod(t *testing.T) {
	cred := outlookCred("")
	cases := []struct {
		name string
		call func(*Chain) error
	}{
		{"List", func(c *Chain) error { _, err := c.List(context.Background(), cred, ListOptions{}); return err }},
		{"Detail", func(c *Chain) error {
			_, err := c.Detail(context.Background(), cred, FolderInbox, "1", IDModeNone)
			return err
		}},
		{"Attachment", func(c *Chain) error {
			_, err := c.Attachment(context.Background(), cred, FolderInbox, "1", IDModeUID, "a1")
			return err
		}},
		{"MarkRead", func(c *Chain) error { _, err := c.MarkRead(context.Background(), cred, nil); return err }},
		{"Delete", func(c *Chain) error { _, err := c.Delete(context.Background(), cred, nil); return err }},
	}
	for _, c := range cases {
		graph := &fakeClient{channel: ChannelGraph, err: newError(ErrKindNetwork, ChannelGraph, "连接超时", nil)}
		imapNew := &fakeClient{channel: ChannelIMAPNew}
		chain := NewChain(map[string]Client{ChannelGraph: graph, ChannelIMAPNew: imapNew})
		if err := c.call(chain); err != nil {
			t.Errorf("%s: 应当回退成功，实际 %v", c.name, err)
		}
		if imapNew.calls != 1 {
			t.Errorf("%s: 第二条通道被调用 %d 次，期望 1 次", c.name, imapNew.calls)
		}
	}
}

// 日志与错误详情里绝不能出现完整邮箱。
func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"user@outlook.com": "u***@outlook.com",
		"a@b.com":          "a***@b.com",
		"@outlook.com":     "***",
		"nodomain":         "***",
		"":                 "***",
	}
	for in, want := range cases {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q，期望 %q", in, got, want)
		}
	}
}

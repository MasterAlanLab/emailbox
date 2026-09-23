package mailer

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeClient struct {
	channel      string
	err          error
	calls        int
	refreshCalls int
}

func (f *fakeClient) Channel() string { return f.channel }
func (f *fakeClient) RefreshToken(context.Context, Credential) error {
	f.calls++
	f.refreshCalls++
	return f.err
}
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

func outlookCred(last string) Credential {
	return Credential{Email: "user@outlook.com", Provider: "outlook", AccountType: AccountTypeOutlook, RefreshToken: "refresh", AuthChannel: last}
}

func TestChannelOrder(t *testing.T) {
	cases := []struct {
		name string
		cred Credential
		want []string
	}{
		{"密码鉴权账号只有一条通道", Credential{Email: "u@qq.com", Provider: "qq", AccountType: AccountTypeIMAP}, []string{ChannelIMAP}},
		{"Outlook 未记录通道", outlookCred(""), []string{ChannelIMAPNew, ChannelIMAPOld}},
		{"Outlook 上次新版优先", outlookCred(ChannelIMAPNew), []string{ChannelIMAPNew, ChannelIMAPOld}},
		{"Outlook 上次旧版优先", outlookCred(ChannelIMAPOld), []string{ChannelIMAPOld, ChannelIMAPNew}},
		{"Gmail OAuth 使用 Gmail IMAP", Credential{Email: "u@gmail.com", Provider: "gmail", AccountType: AccountTypeIMAP, RefreshToken: "refresh"}, []string{ChannelIMAPGmail}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ChannelOrder(c.cred); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("顺序 = %v，期望 %v", got, c.want)
			}
		})
	}
}

func TestChainFallsBackFromNewToOldIMAP(t *testing.T) {
	newClient := &fakeClient{channel: ChannelIMAPNew, err: newError(ErrKindNetwork, ChannelIMAPNew, "连接超时", nil)}
	oldClient := &fakeClient{channel: ChannelIMAPOld}
	chain := NewChain(map[string]Client{ChannelIMAPNew: newClient, ChannelIMAPOld: oldClient})
	var success ChannelSuccess
	chain.OnSuccess = func(_ Credential, got ChannelSuccess) { success = got }
	msgs, err := chain.List(context.Background(), outlookCred(""), ListOptions{Folder: FolderInbox})
	if err != nil || len(msgs) != 1 || msgs[0].ID != ChannelIMAPOld {
		t.Fatalf("回退结果 = %v, %v", msgs, err)
	}
	if success.Channel != ChannelIMAPOld || len(success.Attempts) != 1 {
		t.Fatalf("成功回执 = %+v", success)
	}
}

func TestChainStopsOnNonRetriableError(t *testing.T) {
	first := &fakeClient{channel: ChannelIMAPNew, err: newError(ErrKindAuthFailed, ChannelIMAPNew, "登录失败", nil)}
	second := &fakeClient{channel: ChannelIMAPOld}
	chain := NewChain(map[string]Client{ChannelIMAPNew: first, ChannelIMAPOld: second})
	_, err := chain.List(context.Background(), outlookCred(""), ListOptions{})
	if KindOf(err) != ErrKindAuthFailed || second.calls != 0 {
		t.Fatalf("错误 = %v，第二通道调用 = %d", err, second.calls)
	}
}

func TestChainFallbackAppliesToEveryMethod(t *testing.T) {
	cred := outlookCred("")
	calls := []struct {
		name string
		call func(*Chain) error
	}{
		{"List", func(c *Chain) error { _, err := c.List(context.Background(), cred, ListOptions{}); return err }},
		{"Detail", func(c *Chain) error {
			_, err := c.Detail(context.Background(), cred, FolderInbox, "1", IDModeUID)
			return err
		}},
		{"Attachment", func(c *Chain) error {
			_, err := c.Attachment(context.Background(), cred, FolderInbox, "1", IDModeUID, "a")
			return err
		}},
		{"MarkRead", func(c *Chain) error { _, err := c.MarkRead(context.Background(), cred, nil); return err }},
		{"Delete", func(c *Chain) error { _, err := c.Delete(context.Background(), cred, nil); return err }},
		{"RefreshToken", func(c *Chain) error { return c.RefreshToken(context.Background(), cred) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			newClient := &fakeClient{channel: ChannelIMAPNew, err: newError(ErrKindNetwork, ChannelIMAPNew, "连接超时", nil)}
			oldClient := &fakeClient{channel: ChannelIMAPOld}
			chain := NewChain(map[string]Client{ChannelIMAPNew: newClient, ChannelIMAPOld: oldClient})
			if err := tc.call(chain); err != nil {
				t.Fatal(err)
			}
			if oldClient.calls != 1 {
				t.Fatalf("旧版通道调用次数 = %d", oldClient.calls)
			}
		})
	}
}

func TestTokenRefreshUsesPreferredChannel(t *testing.T) {
	oldClient := &fakeClient{channel: ChannelIMAPOld}
	newClient := &fakeClient{channel: ChannelIMAPNew}
	chain := NewChain(map[string]Client{ChannelIMAPNew: newClient, ChannelIMAPOld: oldClient})
	var success ChannelSuccess
	chain.OnSuccess = func(_ Credential, got ChannelSuccess) { success = got }
	if err := chain.RefreshToken(context.Background(), outlookCred(ChannelIMAPOld)); err != nil {
		t.Fatal(err)
	}
	if oldClient.refreshCalls != 1 || newClient.calls != 0 || success.Channel != ChannelIMAPOld {
		t.Fatalf("刷新通道错误: %+v", success)
	}
}

func TestChainStopsWhenContextCanceled(t *testing.T) {
	client := &fakeClient{channel: ChannelIMAPNew}
	chain := NewChain(map[string]Client{ChannelIMAPNew: client})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := chain.List(ctx, outlookCred(""), ListOptions{})
	if KindOf(err) != ErrKindCanceled || client.calls != 0 {
		t.Fatalf("错误 = %v, calls=%d", err, client.calls)
	}
}

func TestWithAttemptsKeepsCause(t *testing.T) {
	cause := errors.New("cause")
	err := withAttempts(cause, []Attempt{{Channel: ChannelIMAPNew}})
	if !errors.Is(err, cause) {
		t.Fatal("cause 未保留")
	}
}

package imapx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emailbox/pkg/mailer"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// TestConcurrentListsAreActuallyParallel 是 P2 验收表第 13 项的常驻版本。
//
// 这一项是本方案相对 outlookEmail 的核心价值主张：Python 版为了让 IMAP 走代理，
// 全局 monkeypatch 了 socket.socket，只能用进程锁把并发压成串行；
// 这里改成每连接一个独立 Dialer，20 个账号应当真正同时在跑。
// 用真账号跑 20 条并发会撞服务商风控，所以对着进程内 IMAP 服务器验证。
//
// 断言方式是「20 次拨号必须在同一时刻同时在场」：任何一处全局锁都会让到场数
// 停在 1，屏障等不满就超时失败，而不是靠比较耗时——后者在 CI 上必然是 flaky 的。
func TestConcurrentListsAreActuallyParallel(t *testing.T) {
	const workers = 20

	msgs := []string{makeMessage("并发", "body")}
	ts := newTestServer(t, map[string][]string{"INBOX": msgs})

	var arrived atomic.Int32
	allArrived := make(chan struct{})
	giveUp := make(chan struct{})
	var giveUpOnce sync.Once
	errNotParallel := errors.New("20 次拨号没能同时在场，协议层存在全局串行点")

	// dial 在真正建连之前先卡住，直到 20 个调用全部到齐。
	//
	// 第一个等超时的人负责关掉 giveUp，让其余还在排队的立刻一起返回：
	// 真出问题时它们是串行的，各等各的超时会把失败用例拖到几分钟。
	dial := func(ctx context.Context, _ string, _ int, _ string) (net.Conn, error) {
		if arrived.Add(1) == workers {
			close(allArrived)
		}
		select {
		case <-allArrived:
		case <-giveUp:
			return nil, errNotParallel
		case <-time.After(5 * time.Second):
			giveUpOnce.Do(func() { close(giveUp) })
			return nil, errNotParallel
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ts.addr)
	}

	var wg sync.WaitGroup
	errs := make([]error, workers)
	counts := make([]int, workers)

	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个 goroutine 一个独立的 Client，对应「20 个不同账号」。
			client := New(Config{
				Channel:  mailer.ChannelIMAP,
				Timeout:  30 * time.Second,
				DialFunc: dial,
			})
			got, err := client.List(context.Background(), testCred(),
				mailer.ListOptions{Folder: mailer.FolderInbox, Top: 5})
			errs[i], counts[i] = err, len(got)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			if errors.Is(err, errNotParallel) {
				t.Fatalf("第 %d 个账号: %v", i, err)
			}
			t.Fatalf("第 %d 个账号拉信失败: %v", i, err)
		}
		if counts[i] != 1 {
			t.Errorf("第 %d 个账号拿到 %d 封，期望 1 封", i, counts[i])
		}
	}
	if n := arrived.Load(); n != workers {
		t.Fatalf("到场 %d 次拨号，期望 %d 次", n, workers)
	}
}

// TestProxyIdentityIsPerConnection 守住上面那条结论的前提，同时是 P2 验收表第 12 项
// （`{mail}` 模板代理，不同账号使用不同代理身份）的常驻版本。
//
// 八个账号共用同一份代理配置字符串，`{mail}` 必须在每条连接上各自展开成自己的身份。
// 一旦有人图省事把展开结果或 Dialer 提成包级变量，账号之间的代理身份就会互相串——
// 批量拉信时表现为「一部分账号莫名其妙走了别人的出口」，而这恰恰最容易触发服务商风控。
func TestProxyIdentityIsPerConnection(t *testing.T) {
	const workers = 8

	emails := make([]string, workers)
	for i := range emails {
		emails[i] = fmt.Sprintf("user%02d@example.com", i)
	}
	addr := newMultiUserServer(t, emails, makeMessage("x", "body"))

	var mu sync.Mutex
	seen := map[string]int{}

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			email := emails[i]
			client := New(Config{
				Channel: mailer.ChannelIMAP,
				Timeout: 30 * time.Second,
				// 第三个参数是这条连接自己的代理 URL，记下来看有没有串号。
				DialFunc: func(ctx context.Context, _ string, _ int, proxyURL string) (net.Conn, error) {
					mu.Lock()
					seen[proxyURL]++
					mu.Unlock()
					var d net.Dialer
					return d.DialContext(ctx, "tcp", addr)
				},
			})
			cred := testCred()
			cred.Email = email
			// 八个账号填的是同一个模板串，区别只能来自 {mail} 的展开。
			cred.Proxy = mailer.ProxyConfig{URL: "socks5://" + mailer.MailPlaceholder + "@127.0.0.1:1080"}
			if _, err := client.List(context.Background(), cred,
				mailer.ListOptions{Folder: mailer.FolderInbox, Top: 1}); err != nil {
				t.Errorf("%s 拉信失败: %v", email, err)
			}
		}()
	}
	wg.Wait()

	if len(seen) != workers {
		t.Fatalf("拨号看到 %d 种代理配置，期望 %d 种各用各的", len(seen), workers)
	}
	for proxy, n := range seen {
		if n != 1 {
			t.Errorf("代理 %q 被用了 %d 次，期望 1 次", proxy, n)
		}
	}
}

// newMultiUserServer 起一个多用户的进程内 IMAP 服务器，返回监听地址。
// client_test.go 里的 newTestServer 是单用户的（每条连接固定绑同一个 UserSession），
// 而这里要的正是「不同账号各自登录」，所以走 imapmemserver 自己的 NewSession。
func newMultiUserServer(t *testing.T, emails []string, raw string) string {
	t.Helper()

	memServer := imapmemserver.New()
	for _, email := range emails {
		user := imapmemserver.NewUser(email, testPassword)
		if err := user.Create("INBOX", nil); err != nil {
			t.Fatalf("创建 %s 的收件箱失败: %v", email, err)
		}
		appendMessage(t, user, "INBOX", raw)
		memServer.AddUser(user)
	}

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}, imap.CapID: {}},
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
	return ln.Addr().String()
}

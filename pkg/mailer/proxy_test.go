package mailer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProxyAuthenticationErrorsAreRecognized(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("transport: %w", errProxyAuthentication),
		errors.New("Proxy Authentication Required"),
		fmt.Errorf("proxyconnect tcp: %w", errors.New("username/password authentication failed")),
		errors.New("socks connect: no acceptable authentication methods"),
	} {
		if !IsProxyAuthenticationError(err) {
			t.Errorf("未识别代理认证错误：%v", err)
		}
	}
	if IsProxyAuthenticationError(errors.New("connection refused")) {
		t.Error("普通网络故障被误判为代理认证失败")
	}
}

type failingDialer struct{ err error }

func (d failingDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, d.err
}

func TestDialTLSKeepsProxyAuthenticationFailure(t *testing.T) {
	_, err := DialTLS(context.Background(), failingDialer{
		err: errors.New("username/password authentication failed"),
	}, "imap.example.com", 993)
	if KindOf(err) != ErrKindProxyFailed {
		t.Fatalf("分类 = %q，期望 proxy_failed（%v）", KindOf(err), err)
	}
}

// 代理配置必须整组一起取：账号填了主代理就连它自己的两个备用一起用。
// 混搭（主用账号的、备用用分组的）会让 failover 跑到一个完全无关的出口上，
// 出问题时几乎不可能靠日志理解。
func TestResolveProxyTakesWholeGroup(t *testing.T) {
	account := ProxyConfig{URL: "socks5://acct:1080", Fallback1: "socks5://acct-b:1080"}
	group := ProxyConfig{URL: "socks5://grp:1080", Fallback1: "socks5://grp-b:1080", Fallback2: "socks5://grp-c:1080"}
	parent := ProxyConfig{URL: "socks5://parent:1080"}

	cases := []struct {
		name    string
		account ProxyConfig
		chain   []ProxyConfig
		want    ProxyConfig
	}{
		{"账号自己有配置时整组用账号的", account, []ProxyConfig{group, parent}, account},
		{"账号没配时用最近的分组", ProxyConfig{}, []ProxyConfig{group, parent}, group},
		{"分组没配时继续往父分组找", ProxyConfig{}, []ProxyConfig{{}, parent}, parent},
		{"都没配则直连", ProxyConfig{}, []ProxyConfig{{}, {}}, ProxyConfig{}},
		{"没有分组链时也不能崩", ProxyConfig{}, nil, ProxyConfig{}},
		// 只填了备用而没填主代理的配置视为未配置：备用是主代理的兜底，
		// 单独存在没有意义，继续往上找才符合用户预期。
		{"只有备用没有主代理时视为未配置", ProxyConfig{Fallback1: "socks5://x:1080"}, []ProxyConfig{group}, group},
		{"空白字符不算配置", ProxyConfig{URL: "   "}, []ProxyConfig{group}, group},
	}
	for _, c := range cases {
		if got := ResolveProxy(c.account, c.chain); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: = %+v，期望 %+v", c.name, got, c.want)
		}
	}
}

func TestExpandMailPlaceholder(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		email string
		want  string
	}{
		{
			name:  "去掉点和加号并转小写",
			url:   "socks5://User.Name+tag@outlook.com:pwd@proxy:1080",
			email: "User.Name+tag@outlook.com",
			want:  "socks5://User.Name+tag@outlook.com:pwd@proxy:1080",
		},
		{
			name:  "展开占位符",
			url:   "socks5://" + MailPlaceholder + ":pwd@proxy:1080",
			email: "User.Name+tag@outlook.com",
			want:  "socks5://usernametag:pwd@proxy:1080",
		},
		{
			name:  "同一个 URL 里出现多次都要展开",
			url:   MailPlaceholder + "-" + MailPlaceholder,
			email: "ab@x.com",
			want:  "ab-ab",
		},
		{
			name:  "没有占位符时原样返回",
			url:   "socks5://proxy:1080",
			email: "a@b.com",
			want:  "socks5://proxy:1080",
		},
		{
			name:  "邮箱没有 @ 时按整串处理",
			url:   MailPlaceholder,
			email: "Weird_Name",
			want:  "weirdname",
		},
		{
			name:  "中文等非字母数字一律丢弃",
			url:   MailPlaceholder,
			email: "用户a1@x.com",
			want:  "a1",
		},
	}
	for _, c := range cases {
		if got := ExpandMailPlaceholder(c.url, c.email); got != c.want {
			t.Errorf("%s: = %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestProxyCandidates(t *testing.T) {
	cases := []struct {
		name  string
		cfg   ProxyConfig
		email string
		want  []string
	}{
		{
			name: "三个都填则依次尝试，末尾补直连",
			cfg:  ProxyConfig{URL: "socks5://a:1080", Fallback1: "socks5://b:1080", Fallback2: "socks5://c:1080"},
			want: []string{"socks5://a:1080", "socks5://b:1080", "socks5://c:1080", DirectProxy},
		},
		{
			name: "空串与重复项被剔除",
			cfg:  ProxyConfig{URL: "socks5://a:1080", Fallback1: "  ", Fallback2: "socks5://a:1080"},
			want: []string{"socks5://a:1080", DirectProxy},
		},
		{
			name: "完全没配时只有直连",
			cfg:  ProxyConfig{},
			want: []string{DirectProxy},
		},
		{
			name:  "占位符展开后才去重：不同账号是不同出口，不能合并",
			cfg:   ProxyConfig{URL: "socks5://" + MailPlaceholder + "@p:1080", Fallback1: "socks5://" + MailPlaceholder + "@p:1080"},
			email: "user@x.com",
			want:  []string{"socks5://user@p:1080", DirectProxy},
		},
	}
	for _, c := range cases {
		if got := ProxyCandidates(c.cfg, c.email); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: = %v，期望 %v", c.name, got, c.want)
		}
	}
}

// 代理串常带认证口令。任何输出路径（日志、错误详情、前端回显）都必须先过这里，
// 而且解析失败的分支同样不能漏——那恰恰是最容易把口令原样吐出去的地方。
func TestMaskProxyNeverLeaksPassword(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"直连", DirectProxy, "direct"},
		{"带用户名口令", "socks5://user:secret@proxy.example.com:1080", "socks5://user:****@proxy.example.com:1080"},
		{"只有用户名", "socks5://user@proxy.example.com:1080", "socks5://user@proxy.example.com:1080"},
		{"没有认证信息", "socks5://proxy.example.com:1080", "socks5://proxy.example.com:1080"},
		{"解析失败但带 @ 时也要打码", "socks5://us er:sec ret@proxy:1080", "socks5://****@proxy:1080"},
		{"解析失败且不带 @ 时原样返回", "not a url", "not a url"},
	}
	for _, c := range cases {
		got := MaskProxy(c.raw)
		if got != c.want {
			t.Errorf("%s: MaskProxy(%q) = %q，期望 %q", c.name, c.raw, got, c.want)
		}
		if strings.Contains(got, "secret") || strings.Contains(got, "sec ret") {
			t.Errorf("%s: 打码后仍然含有口令：%q", c.name, got)
		}
	}
}

func TestNewDialer(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"直连", DirectProxy, false},
		{"socks5", "socks5://proxy.example.com:1080", false},
		{"socks5h 等同 socks5", "socks5h://proxy.example.com:1080", false},
		{"带认证的 socks5", "socks5://u:p@proxy.example.com:1080", false},
		// IMAP 经 HTTP 代理需要 CONNECT 隧道，暂不支持；必须明确报错，
		// 否则用户配了 http 代理却看到「连接超时」，根本猜不到原因。
		{"HTTP 代理不支持", "http://proxy.example.com:8080", true},
		{"未知协议", "ftp://proxy.example.com:21", true},
		{"非法地址", "://", true},
	}
	for _, c := range cases {
		d, err := NewDialer(c.url, time.Second)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: 期望报错，实际成功", c.name)
				continue
			}
			if KindOf(err) != ErrKindProxyFailed {
				t.Errorf("%s: 错误分类 = %q，期望 proxy_failed", c.name, KindOf(err))
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
		} else if d == nil {
			t.Errorf("%s: dialer 为 nil", c.name)
		}
	}
}

// 每次调用都要拿到独立的 Transport，否则不同代理身份会共用连接池——
// {mail} 模板代理「每个账号一个出口」的前提就没了。
func TestNewHTTPClientIsolatesTransport(t *testing.T) {
	a, err := NewHTTPClient("socks5://proxy.example.com:1080", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewHTTPClient("socks5://other.example.com:1080", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Transport == b.Transport {
		t.Fatal("两个客户端共用了同一个 Transport")
	}

	if _, err := NewHTTPClient("http://proxy.example.com:8080", time.Second); err != nil {
		t.Errorf("HTTP 代理对 Graph 通道是支持的：%v", err)
	}
	if _, err := NewHTTPClient("ftp://proxy:21", time.Second); KindOf(err) != ErrKindProxyFailed {
		t.Errorf("未知协议应当报 proxy_failed，实际 %v", err)
	}
}

// 拨号失败要归到 network 而不是漏成裸错误，否则统计里这一类会消失。
func TestDialTLSClassifiesDialFailure(t *testing.T) {
	dialer, err := NewDialer(DirectProxy, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// 保留地址段，连接必定失败且不会真的打到外网。
	_, err = DialTLS(context.Background(), dialer, "192.0.2.1", 993)
	if KindOf(err) != ErrKindNetwork {
		t.Fatalf("错误分类 = %q，期望 network（原始错误：%v）", KindOf(err), err)
	}
}

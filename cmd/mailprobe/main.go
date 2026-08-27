// Command mailprobe 是协议层的联调工具：拿一个真实账号，逐通道跑一遍并打印诊断。
//
// 它存在的理由是 P2 的验收只能用真实账号完成，而在 Web 界面里排查
// 「为什么这个账号拉不到信」既慢又看不到中间过程。mailprobe 不碰数据库、
// 不需要起服务，凭据从命令行或环境变量来，直接对着上游打。
//
//	go run ./cmd/mailprobe -email a@outlook.com -refresh-token "M.C123..." -folder inbox
//	go run ./cmd/mailprobe -email a@gmail.com -imap-password "xxxx" -provider gmail
//
// 凭据不要写进 shell 历史：优先用 MAILPROBE_REFRESH_TOKEN / MAILPROBE_PASSWORD。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"emailbox/pkg/mailer"
	"emailbox/pkg/mailer/graph"
	"emailbox/pkg/mailer/imapx"
)

type options struct {
	email        string
	provider     string
	clientID     string
	refreshToken string
	password     string
	imapHost     string
	imapPort     int
	proxy        string
	fallback1    string
	fallback2    string
	folder       string
	top          int
	skip         int
	detail       bool
	timeout      time.Duration
}

func main() {
	opt := parseFlags()
	if opt.email == "" {
		fmt.Fprintln(os.Stderr, "缺少 -email")
		flag.Usage()
		os.Exit(2)
	}

	cred, err := buildCredential(opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}

	os.Exit(run(cred, opt))
}

// run 与 main 分开，是为了让 defer cancel() 真的跑到：
// 在 main 里 os.Exit 会跳过所有 defer。
func run(cred mailer.Credential, opt options) int {
	ctx, cancel := context.WithTimeout(context.Background(), opt.timeout)
	defer cancel()

	printHeader(cred, opt)
	if failed := probeChannels(ctx, cred, opt); failed {
		return 1
	}
	return 0
}

func parseFlags() options {
	var opt options
	flag.StringVar(&opt.email, "email", "", "邮箱地址（必填）")
	flag.StringVar(&opt.provider, "provider", "", "服务商 code，留空按域名推断")
	flag.StringVar(&opt.clientID, "client-id", "", "OAuth client_id，留空用公共 ID")
	flag.StringVar(&opt.refreshToken, "refresh-token", "", "OAuth refresh_token（或用 MAILPROBE_REFRESH_TOKEN）")
	flag.StringVar(&opt.password, "imap-password", "", "IMAP 密码/授权码（或用 MAILPROBE_PASSWORD）")
	flag.StringVar(&opt.imapHost, "imap-host", "", "IMAP 主机，留空按服务商推断")
	flag.IntVar(&opt.imapPort, "imap-port", mailer.DefaultIMAPPort, "IMAP 端口")
	flag.StringVar(&opt.proxy, "proxy", "", "代理 URL，支持 {mail} 占位符")
	flag.StringVar(&opt.fallback1, "proxy-fallback1", "", "备用代理 1")
	flag.StringVar(&opt.fallback2, "proxy-fallback2", "", "备用代理 2")
	flag.StringVar(&opt.folder, "folder", "inbox", "邮件夹 inbox|junkemail|deleteditems")
	flag.IntVar(&opt.top, "top", 5, "拉取条数")
	flag.IntVar(&opt.skip, "skip", 0, "跳过条数")
	flag.BoolVar(&opt.detail, "detail", false, "额外拉取第一封的详情与附件列表")
	flag.DurationVar(&opt.timeout, "timeout", 2*time.Minute, "整体超时")
	flag.Parse()

	// 凭据优先从环境变量取，避免进 shell 历史。
	if v := os.Getenv("MAILPROBE_REFRESH_TOKEN"); v != "" && opt.refreshToken == "" {
		opt.refreshToken = v
	}
	if v := os.Getenv("MAILPROBE_PASSWORD"); v != "" && opt.password == "" {
		opt.password = v
	}
	return opt
}

func buildCredential(opt options) (mailer.Credential, error) {
	provider := mailer.ProviderForEmail(opt.email)
	if opt.provider != "" {
		provider = mailer.ProviderByCode(opt.provider)
	}
	host, port := opt.imapHost, opt.imapPort
	if host == "" {
		host = provider.IMAPHost
	}
	if port <= 0 {
		port = mailer.DefaultIMAPPort
	}

	cred := mailer.Credential{
		Email:        opt.email,
		Provider:     provider.Code,
		AccountType:  provider.Type,
		ClientID:     opt.clientID,
		RefreshToken: opt.refreshToken,
		IMAPHost:     host,
		IMAPPort:     port,
		IMAPPassword: opt.password,
		Password:     opt.password,
		Proxy: mailer.ProxyConfig{
			URL:       opt.proxy,
			Fallback1: opt.fallback1,
			Fallback2: opt.fallback2,
		},
	}
	if cred.AccountType == mailer.AccountTypeOutlook && cred.RefreshToken == "" {
		return cred, errors.New("outlook 账号需要 -refresh-token")
	}
	if cred.AccountType == mailer.AccountTypeIMAP && cred.IMAPPassword == "" {
		return cred, errors.New("IMAP 账号需要 -imap-password")
	}
	if cred.AccountType == mailer.AccountTypeIMAP && host == "" {
		return cred, errors.New("未知服务商，请用 -imap-host 指定 IMAP 主机")
	}
	return cred, nil
}

func printHeader(cred mailer.Credential, opt options) {
	fmt.Printf("账号     %s\n", mailer.MaskEmail(cred.Email))
	fmt.Printf("服务商   %s (%s)\n", cred.Provider, cred.AccountType)
	if cred.AccountType == mailer.AccountTypeIMAP {
		fmt.Printf("IMAP     %s:%d\n", cred.IMAPHost, cred.IMAPPort)
	}
	candidates := mailer.ProxyCandidates(cred.Proxy, cred.Email)
	masked := make([]string, 0, len(candidates))
	for _, p := range candidates {
		masked = append(masked, mailer.MaskProxy(p))
	}
	fmt.Printf("代理链   %s\n", strings.Join(masked, " → "))
	fmt.Printf("邮件夹   %s（skip=%d top=%d）\n\n", opt.folder, opt.skip, opt.top)
}

// probeChannels 逐条通道单独试，而不是走回退链。
//
// 回退链会在第一条成功时停下，那正好掩盖掉「另外两条其实是坏的」——
// 排障时想看的恰恰是每条通道各自的结果。
func probeChannels(ctx context.Context, cred mailer.Credential, opt options) bool {
	timeout := opt.timeout / 3
	channels := channelsFor(cred, timeout)

	anySucceeded := false
	for _, ch := range channels {
		fmt.Printf("── %s ", ch.name)
		fmt.Println(strings.Repeat("─", max(0, 60-len(ch.name))))

		start := time.Now()
		messages, err := ch.client.List(ctx, cred, mailer.ListOptions{
			Folder: mailer.Folder(opt.folder),
			Skip:   opt.skip,
			Top:    opt.top,
		})
		elapsed := time.Since(start).Round(time.Millisecond)

		if err != nil {
			reportFailure(err, elapsed)
			continue
		}
		anySucceeded = true
		fmt.Printf("   成功 %s，%d 封\n", elapsed, len(messages))
		for _, m := range messages {
			fmt.Printf("   · [%s] %s — %s%s\n",
				m.ReceivedAt.Local().Format("01-02 15:04"),
				truncate(m.Subject, 40), truncate(m.From, 30), attachmentMark(m))
		}
		if opt.detail && len(messages) > 0 {
			probeDetail(ctx, ch.client, cred, messages[0], opt)
		}
		fmt.Println()
	}
	return !anySucceeded
}

type namedChannel struct {
	name   string
	client mailer.Client
}

func channelsFor(cred mailer.Credential, timeout time.Duration) []namedChannel {
	if cred.AccountType != mailer.AccountTypeOutlook {
		return []namedChannel{{
			name:   "IMAP（密码鉴权）",
			client: imapx.New(imapx.Config{Channel: mailer.ChannelIMAP, Timeout: timeout}),
		}}
	}
	return []namedChannel{
		{name: "Graph", client: graph.New(graph.Config{Timeout: timeout})},
		{
			name:   "IMAP 新版（outlook.live.com）",
			client: imapx.New(imapx.Config{Channel: mailer.ChannelIMAPNew, Timeout: timeout}),
		},
		{
			name:   "IMAP 旧版（outlook.office365.com）",
			client: imapx.New(imapx.Config{Channel: mailer.ChannelIMAPOld, Timeout: timeout}),
		},
	}
}

func reportFailure(err error, elapsed time.Duration) {
	fmt.Printf("   失败 %s — [%s] %s\n", elapsed, mailer.KindOf(err), err)
	var e *mailer.Error
	if !errors.As(err, &e) {
		return
	}
	if e.Detail != "" {
		fmt.Printf("     详情: %s\n", truncate(e.Detail, 200))
	}
	for _, a := range e.Attempts {
		fmt.Printf("     尝试 proxy=%s kind=%s\n", a.Proxy, a.Kind)
	}
	if !mailer.RetriableFrom(e.Channel, err) {
		fmt.Println("     这类错误换通道也是同样的结果，回退链会在此停手")
	}
}

func probeDetail(
	ctx context.Context, client mailer.Client, cred mailer.Credential,
	msg mailer.Message, opt options,
) {
	detail, err := client.Detail(ctx, cred, mailer.Folder(opt.folder), msg.ID, msg.IDMode)
	if err != nil {
		fmt.Printf("   详情失败 — [%s] %s\n", mailer.KindOf(err), err)
		return
	}
	fmt.Printf("   详情: %s 正文 %d 字节，附件 %d 个\n",
		detail.BodyType, len(detail.Body), len(detail.Attachments))
	for _, a := range detail.Attachments {
		fmt.Printf("     附件 %s (%s, %d 字节, inline=%v)\n", a.Name, a.ContentType, a.Size, a.IsInline)
	}
}

func attachmentMark(m mailer.Message) string {
	if m.HasAttachments {
		return " 📎"
	}
	return ""
}

func truncate(s string, n int) string {
	runes := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}

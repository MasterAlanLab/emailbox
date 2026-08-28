package service

import (
	"context"
	"time"

	"emailbox/pkg/mailer"
	"emailbox/pkg/mailer/graph"
	"emailbox/pkg/mailer/imapx"
	"emailbox/pkg/model"
)

// defaultChainTimeout 是单次远端调用的超时。
//
// 整条链的总时长要由调用方的 context 控制：三条通道各自超时会累计到两分钟以上，
// 只在单次调用上设超时是不够的。
const defaultChainTimeout = 60 * time.Second

// ChainOptions 是回退链的构造参数。
type ChainOptions struct {
	Timeout           time.Duration
	OAuthClientID     string
	OAuthClientSecret string
}

// defaultChainFactory 返回「按账号构造回退链」的函数。
//
// 每个账号一套新客户端，不复用：客户端里带着该账号的代理配置与连接池，
// 复用就等于让不同账号共用出口身份，{mail} 模板代理的意义就没了。
// 构造本身很轻，真正的开销在建连接上。
func defaultChainFactory(s *MessageService, opt ChainOptions) func(*model.MailAccount) mailer.Client {
	if opt.Timeout <= 0 {
		opt.Timeout = defaultChainTimeout
	}
	return func(account *model.MailAccount) mailer.Client {
		// 两个回调都脱离请求的 context：服务端已经把 token 换过了，
		// 这时若因请求取消而不落库，账号下次刷新就会失效。
		hookCtx := func() context.Context { return context.WithoutCancel(context.Background()) }
		onRotate := func(_, refreshToken string) {
			s.OnTokenRotated(hookCtx(), account.TenantID, account.ID, refreshToken)
		}

		chain := mailer.NewChain(map[string]mailer.Client{
			mailer.ChannelGraph: graph.New(graph.Config{
				Timeout:        opt.Timeout,
				OnTokenRefresh: onRotate,
			}),
			mailer.ChannelIMAPNew: imapx.New(imapx.Config{
				Channel:        mailer.ChannelIMAPNew,
				Timeout:        opt.Timeout,
				OnTokenRefresh: onRotate,
			}),
			mailer.ChannelIMAPOld: imapx.New(imapx.Config{
				Channel:        mailer.ChannelIMAPOld,
				Timeout:        opt.Timeout,
				OnTokenRefresh: onRotate,
			}),
			mailer.ChannelIMAP: imapx.New(imapx.Config{
				Channel: mailer.ChannelIMAP,
				Timeout: opt.Timeout,
			}),
		})
		previous := account.AuthChannel
		chain.OnSuccess = func(_ mailer.Credential, result mailer.ChannelSuccess) {
			s.OnChannelSuccess(hookCtx(), account.TenantID, account.ID, previous, result.Channel)
		}
		return chain
	}
}

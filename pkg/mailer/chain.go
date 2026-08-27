package mailer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// ChannelOrder 返回该账号要依次尝试的通道名。
//
// 密码鉴权的普通 IMAP 账号只有一条通道；Outlook OAuth 账号有三条：
// Graph → 新版 IMAP → 旧版 IMAP。若账号上记着 auth_channel（上次成功的通道），
// 把它提到最前面——这能省掉大部分账号每次都从 Graph 重试一遍的开销，
// 也是本方案把 auth_channel 存到具体通道（而非只存 graph|imap）的意义。
func ChannelOrder(cred Credential) []string {
	if cred.AccountType != AccountTypeOutlook {
		return []string{ChannelIMAP}
	}
	order := []string{ChannelGraph, ChannelIMAPNew, ChannelIMAPOld}
	last := strings.TrimSpace(cred.AuthChannel)
	if last == "" || last == ChannelGraph {
		return order
	}
	out := make([]string, 0, len(order))
	for _, c := range order {
		if c == last {
			out = append([]string{c}, out...)
			continue
		}
		out = append(out, c)
	}
	// 上次成功的通道若不在候选表里（比如账号类型被改过），忽略它按默认顺序走。
	if out[0] != last {
		return order
	}
	return out
}

// ChannelSuccess 是回退链成功时的回执，供 service 层把结果写回账号。
type ChannelSuccess struct {
	// Channel 是本次成功的通道；与账号上记的不同时应写回。
	Channel string
	// Attempts 是成功之前的失败记录，全部已脱敏。即使最终成功也值得留下：
	// 主代理持续故障时这里能看出来。
	Attempts []Attempt
}

// Chain 按通道顺序依次尝试，直到某条成功。
type Chain struct {
	// clients 按通道名索引可用的实现。
	clients map[string]Client
	// OnSuccess 在某条通道成功后调用，供上层写回 auth_channel。
	// 允许为 nil（例如 cmd/mailprobe 这类不落库的调用方）。
	OnSuccess func(cred Credential, result ChannelSuccess)
}

// NewChain 组装回退链。
func NewChain(clients map[string]Client) *Chain {
	return &Chain{clients: clients}
}

// Channel 让 Chain 自身也满足 Client 接口。它没有固定通道，返回空串。
func (c *Chain) Channel() string { return "" }

// run 是回退链的核心：按顺序试通道，遇到不可回退的错误立即停手。
//
// 泛型化是为了让五个接口方法共享同一套回退与记账逻辑——
// 早期把逻辑复制五份的话，「哪些错误不该回退」这种判断迟早会在某一份里改漏。
func runChain[T any](
	ctx context.Context, c *Chain, cred Credential,
	call func(client Client) (T, error),
) (T, error) {
	var zero T
	order := ChannelOrder(cred)
	attempts := make([]Attempt, 0, len(order))
	var lastErr error

	for _, channel := range order {
		client, ok := c.clients[channel]
		if !ok {
			continue
		}
		// 调用方取消或整体超时后不该再试下一条通道。
		if err := ctx.Err(); err != nil {
			return zero, newError(ErrKindCanceled, channel, "请求已取消", err)
		}

		result, err := call(client)
		if err == nil {
			if c.OnSuccess != nil {
				c.OnSuccess(cred, ChannelSuccess{Channel: channel, Attempts: attempts})
			}
			return result, nil
		}

		lastErr = err
		attempts = append(attempts, Attempt{
			Channel: channel,
			Kind:    KindOf(err),
			Message: err.Error(),
		})

		if !RetriableFrom(channel, err) {
			// 账号被封这类错误换通道也是同样的结果，
			// 继续试只会浪费三倍时间并加重上游风控。
			slog.Debug("通道失败且不可回退，停止尝试",
				"channel", channel, "kind", KindOf(err), "email", MaskEmail(cred.Email))
			return zero, withAttempts(err, attempts)
		}
		slog.Debug("通道失败，尝试下一条",
			"channel", channel, "kind", KindOf(err), "email", MaskEmail(cred.Email))
	}

	if lastErr == nil {
		return zero, newError(ErrKindProviderError, "", "没有可用的通道", nil)
	}
	return zero, withAttempts(lastErr, attempts)
}

// withAttempts 把尝试记录附到错误上。非本包的错误先包一层，
// 否则整条回退路径会丢失，排障时只能看到最后一条通道的报错。
func withAttempts(err error, attempts []Attempt) error {
	var e *Error
	if errors.As(err, &e) {
		e.Attempts = attempts
		return e
	}
	wrapped := newError(KindOf(err), "", err.Error(), err)
	wrapped.Attempts = attempts
	return wrapped
}

func (c *Chain) List(ctx context.Context, cred Credential, opt ListOptions) ([]Message, error) {
	return runChain(ctx, c, cred, func(client Client) ([]Message, error) {
		return client.List(ctx, cred, opt)
	})
}

func (c *Chain) Detail(ctx context.Context, cred Credential, folder Folder, id, idMode string) (*Detail, error) {
	return runChain(ctx, c, cred, func(client Client) (*Detail, error) {
		return client.Detail(ctx, cred, folder, id, idMode)
	})
}

func (c *Chain) Attachment(ctx context.Context, cred Credential, folder Folder, msgID, idMode, attID string) (*Attachment, error) {
	return runChain(ctx, c, cred, func(client Client) (*Attachment, error) {
		return client.Attachment(ctx, cred, folder, msgID, idMode, attID)
	})
}

func (c *Chain) MarkRead(ctx context.Context, cred Credential, items []MessageRef) (BatchResult, error) {
	return runChain(ctx, c, cred, func(client Client) (BatchResult, error) {
		return client.MarkRead(ctx, cred, items)
	})
}

func (c *Chain) Delete(ctx context.Context, cred Credential, items []MessageRef) (BatchResult, error) {
	return runChain(ctx, c, cred, func(client Client) (BatchResult, error) {
		return client.Delete(ctx, cred, items)
	})
}

// MaskEmail 把邮箱打码后写日志：u***@outlook.com。
// 协议层的日志字段里绝不出现完整邮箱、令牌或代理口令。
func MaskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

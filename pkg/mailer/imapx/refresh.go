package imapx

import (
	"context"

	"emailbox/pkg/mailer"
)

var _ mailer.TokenRefresher = (*Client)(nil)

// RefreshToken 只交换 IMAP OAuth 令牌，不建立 IMAP 连接，也不读取邮件。
// 成功表示该通道签发了 access_token；若同时轮换 refresh_token，复用收信时的写回回调。
func (c *Client) RefreshToken(ctx context.Context, cred mailer.Credential) error {
	if c.cfg.Channel != mailer.ChannelIMAPNew && c.cfg.Channel != mailer.ChannelIMAPOld {
		return newError(c.cfg.Channel, mailer.ErrKindProviderError,
			"当前通道不支持令牌刷新，请使用 Outlook OAuth 通道", nil)
	}
	_, err := withProxy(ctx, c, cred, func(proxyURL string) (string, error) {
		hc, err := mailer.NewHTTPClient(proxyURL, c.cfg.Timeout)
		if err != nil {
			return "", err
		}
		defer hc.CloseIdleConnections()
		return c.fetchAccessToken(ctx, hc, cred)
	})
	return err
}

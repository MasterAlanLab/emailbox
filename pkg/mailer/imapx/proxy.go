package imapx

import (
	"context"

	"emailbox/pkg/mailer"
)

// withProxy 让收信与纯令牌刷新共用代理候选和失败边界，避免两条路径的出站策略漂移。
func withProxy[T any](
	ctx context.Context, c *Client, cred mailer.Credential,
	fn func(proxyURL string) (T, error),
) (T, error) {
	var zero T
	candidates := mailer.ProxyCandidates(cred.Proxy, cred.Email)
	attempts := make([]mailer.Attempt, 0, len(candidates))
	var lastErr error

	for _, proxyURL := range candidates {
		if err := ctx.Err(); err != nil {
			return zero, newError(c.cfg.Channel, mailer.ErrKindCanceled, "请求已取消", err)
		}

		result, err := fn(proxyURL)
		if err == nil {
			return result, nil
		}
		lastErr = err
		attempts = append(attempts, mailer.Attempt{
			Channel: c.cfg.Channel,
			Proxy:   mailer.MaskProxy(proxyURL),
			Kind:    mailer.KindOf(err),
			Message: err.Error(),
		})
		// 只有连接失败才切代理。构造阶段就报错意味着配置非法，
		// 顺着候选走到直连会让用户误以为流量仍经过自己的代理。
		if !mailer.RetriableWithAnotherProxy(err) || isProxyConfigError(err) {
			return zero, attachAttempts(err, attempts)
		}
	}

	if lastErr == nil {
		return zero, newError(c.cfg.Channel, mailer.ErrKindProxyFailed, "没有可用的出站通道", nil)
	}
	if mailer.KindOf(lastErr) == mailer.ErrKindNetwork && len(candidates) > 1 {
		wrapped := newError(c.cfg.Channel, mailer.ErrKindProxyFailed, "全部代理候选均不可用", lastErr)
		wrapped.Attempts = attempts
		return zero, wrapped
	}
	return zero, attachAttempts(lastErr, attempts)
}

func isProxyConfigError(err error) bool {
	var e *mailer.Error
	if !asMailerError(err, &e) {
		return false
	}
	return e.Kind == mailer.ErrKindProxyFailed
}

func asMailerError(err error, target **mailer.Error) bool {
	for err != nil {
		if e, ok := err.(*mailer.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func attachAttempts(err error, attempts []mailer.Attempt) error {
	var e *mailer.Error
	if asMailerError(err, &e) {
		e.Attempts = attempts
		return e
	}
	wrapped := mailer.NewError(mailer.KindOf(err), "", err.Error(), err)
	wrapped.Attempts = attempts
	return wrapped
}

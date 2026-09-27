package waffo

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidWebhook = errors.New("waffo Webhook 签名无效")

// 时间窗与官方 SDK 一致：过去 45 分钟、未来 1 分钟。
// 过去一侧必须宽：Waffo 重试时原样重放最初的签名头，t 不会更新；
// 未来一侧没有这个理由，放宽只会让重放攻击多一个窗口。
const (
	webhookMaxAge    = 45 * time.Minute
	webhookMaxFuture = time.Minute
)

// VerifyWebhook 校验 `X-Waffo-Signature: t=<毫秒>,v1=<base64>`。
// 签名输入是 t + "." + 原始请求体，body 必须是未经任何解析、重排的原始字节。
func VerifyWebhook(signature string, pub *rsa.PublicKey, body []byte, now time.Time) error {
	if pub == nil {
		return fmt.Errorf("%w: 未配置该环境的公钥", ErrInvalidWebhook)
	}
	parts := map[string]string{}
	for _, item := range strings.Split(signature, ",") {
		if key, value, ok := strings.Cut(strings.TrimSpace(item), "="); ok {
			parts[key] = value
		}
	}
	ts, err := strconv.ParseInt(parts["t"], 10, 64)
	if err != nil || parts["v1"] == "" {
		return ErrInvalidWebhook
	}
	age := now.Sub(time.UnixMilli(ts))
	if age > webhookMaxAge || age < -webhookMaxFuture {
		return fmt.Errorf("%w: 时间戳超出允许范围", ErrInvalidWebhook)
	}
	sig, err := base64.StdEncoding.DecodeString(parts["v1"])
	if err != nil {
		return ErrInvalidWebhook
	}
	digest := sha256.Sum256([]byte(parts["t"] + "." + string(body)))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		return ErrInvalidWebhook
	}
	return nil
}

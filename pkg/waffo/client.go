// Package waffo 是计费服务直连 Waffo Pancake HTTP API 的薄适配层。
//
// 签名、供应商字段名与错误信封都收在这里，handler 与 service 只看到项目自己的结构。
// 接口契约以官方文档与 @waffo/pancake-ts 的实际行为为准（docs.waffo.ai）。
package waffo

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("waffo 配置未就绪")

// maxResponseBytes 限制读入的响应体大小。正常响应只有几 KB，
// 上限防的是代理或网关回一个巨大错误页把内存吃掉。
const maxResponseBytes = 2 << 20

type Config struct {
	BaseURL    string
	MerchantID string
	PrivateKey string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	merchantID string
	privateKey *rsa.PrivateKey
	httpClient *http.Client
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.MerchantID) == "" || strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, ErrNotConfigured
	}
	key, err := ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("解析 Waffo 私钥失败: %w", err)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(cfg.BaseURL, "/"), merchantID: cfg.MerchantID, privateKey: key, httpClient: hc}, nil
}

// keyDER 把环境变量里常见的几种写法统一成 DER：
// 多行 PEM、写成一行且换行被转义成字面 `\n` 的 PEM、去掉头尾的纯 Base64，
// 以及整段 PEM 再 Base64 一次（WAFFO_PRIVATE_KEY_BASE64 的写法）。
// 与官方 SDK 的 normalizePrivateKey / normalizePublicKey 接受同一组输入。
func keyDER(raw string) ([]byte, error) {
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, `\n`, "\n"), "\r\n", "\n"))
	if text == "" {
		return nil, errors.New("密钥为空")
	}
	if !strings.Contains(text, "-----BEGIN") {
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if err != nil {
			return nil, errors.New("密钥既不是 PEM 也不是 Base64")
		}
		if !bytes.Contains(decoded, []byte("-----BEGIN")) {
			return decoded, nil
		}
		text = string(decoded)
	}
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("PEM 格式无效")
	}
	return block.Bytes, nil
}

func ParsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	der, err := keyDER(raw)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("仅支持 RSA PKCS#1 或 PKCS#8 私钥")
}

func ParsePublicKey(raw string) (*rsa.PublicKey, error) {
	der, err := keyDER(raw)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKIXPublicKey(der); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("公钥不是 RSA")
	}
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("仅支持 RSA SPKI 或 PKCS#1 公钥")
}

// APIError 是 Waffo 返回的失败。Message 取自错误信封的 errors[0].message：
// 它说的是请求哪里不对（「Store is not active」之类），不含凭据，可以给管理员看。
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Waffo API 返回 HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("Waffo API 返回 HTTP %d：%s", e.StatusCode, e.Message)
}

// Retryable 表示换个时间再试可能成功。文档的约定是 4xx 永不重试、5xx 退避重试。
func (e *APIError) Retryable() bool { return e.StatusCode >= 500 }

type envelope struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// call 发一个 POST 并解开 {data, errors} 信封。
//
// 2xx 也要看 errors：官方 SDK 的 unwrapAction 就是这么判的，只看状态码会把
// 一个带错误的 200 当成功，接着在解析 data 时得到一堆零值。
func (c *Client) call(ctx context.Context, path string, body any, idempotencyKey, bearer, environment string) (json.RawMessage, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		// 顾客会话令牌：网关要求同时带环境头（见 SDK 的 BearerHttpClient）。
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Environment", environment)
	} else {
		if err := c.sign(req, path, raw); err != nil {
			return nil, err
		}
	}
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	var env envelope
	parseErr := json.Unmarshal(data, &env)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || len(env.Errors) > 0 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		if len(env.Errors) > 0 {
			apiErr.Message = truncate(env.Errors[0].Message, 200)
		}
		return nil, apiErr
	}
	if parseErr != nil {
		return nil, fmt.Errorf("waffo 响应不是合法 JSON: %w", parseErr)
	}
	return env.Data, nil
}

// sign 按 API Key 规范签名：METHOD\nPATH\nTIMESTAMP\nSHA256_BASE64(BODY)，
// 时间戳是 Unix 秒。body 必须是随后真正发出去的那份字节。
func (c *Client) sign(req *http.Request, path string, body []byte) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	bodyDigest := sha256.Sum256(body)
	canonical := "POST\n" + path + "\n" + timestamp + "\n" + base64.StdEncoding.EncodeToString(bodyDigest[:])
	digest := sha256.Sum256([]byte(canonical))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return err
	}
	req.Header.Set("X-Merchant-Id", c.merchantID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(sig))
	return nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// PriceInfo 是某个币种下的价格。amount 是展示格式字符串（"9.90"），不用浮点。
type PriceInfo struct {
	Amount      string `json:"amount"`
	TaxIncluded bool   `json:"taxIncluded"`
	TaxCategory string `json:"taxCategory"`
}

type SubscriptionProductRequest struct {
	StoreID       string               `json:"storeId"`
	Name          string               `json:"name"`
	BillingPeriod string               `json:"billingPeriod"`
	Prices        map[string]PriceInfo `json:"prices"`
	// Metadata 会随 productMetadata 出现在每个订阅 Webhook 里。
	// 用它带上本地价格 ID：结账页「自愈」重建会话时 orderMetadata 会丢，这份不会。
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (c *Client) CreateSubscriptionProduct(ctx context.Context, req SubscriptionProductRequest, idempotencyKey string) (string, error) {
	data, err := c.call(ctx, "/v1/actions/subscription-product/create-product", req, idempotencyKey, "", "")
	if err != nil {
		return "", err
	}
	var out struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.Product.ID == "" {
		return "", errors.New("waffo 产品响应缺少 id")
	}
	return out.Product.ID, nil
}

// UpdateSubscriptionProductRequest 更新产品会生成新的不可变版本；
// 已有订阅继续用旧版本，新结账用新版本。内容没变时 Waffo 不会新建版本。
type UpdateSubscriptionProductRequest struct {
	ID            string               `json:"id"`
	Name          string               `json:"name,omitempty"`
	BillingPeriod string               `json:"billingPeriod,omitempty"`
	Prices        map[string]PriceInfo `json:"prices,omitempty"`
	Metadata      map[string]string    `json:"metadata,omitempty"`
}

func (c *Client) UpdateSubscriptionProduct(ctx context.Context, req UpdateSubscriptionProductRequest, idempotencyKey string) error {
	_, err := c.call(ctx, "/v1/actions/subscription-product/update-product", req, idempotencyKey, "", "")
	return err
}

type CheckoutRequest struct {
	ProductID               string            `json:"productId"`
	Currency                string            `json:"currency"`
	BuyerEmail              string            `json:"buyerEmail,omitempty"`
	SuccessURL              string            `json:"successUrl,omitempty"`
	ExpiresInSeconds        int               `json:"expiresInSeconds,omitempty"`
	Language                string            `json:"language,omitempty"`
	OrderMerchantExternalID string            `json:"orderMerchantExternalId,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
}

type CheckoutSession struct {
	SessionID   string    `json:"sessionId"`
	CheckoutURL string    `json:"checkoutUrl"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func (c *Client) CreateCheckout(ctx context.Context, req CheckoutRequest, idempotencyKey string) (*CheckoutSession, error) {
	data, err := c.call(ctx, "/v1/actions/checkout/create-session", req, idempotencyKey, "", "")
	if err != nil {
		return nil, err
	}
	var out CheckoutSession
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out.CheckoutURL == "" {
		return nil, errors.New("waffo 结账响应缺少 checkoutUrl")
	}
	return &out, nil
}

// SessionTokenRequest 签发顾客会话令牌。buyerIdentity 会编进令牌，
// 随后以 merchantProvidedBuyerIdentity 出现在该顾客所有订单的 Webhook 里。
type SessionTokenRequest struct {
	BuyerIdentity string `json:"buyerIdentity"`
	StoreID       string `json:"storeId,omitempty"`
	ProductID     string `json:"productId,omitempty"`
}

// IssueSessionToken 不带幂等键：重复签发无害，而一个键不能同时用于两个端点
// （官方 SDK 的 authenticated checkout 同样如此）。
func (c *Client) IssueSessionToken(ctx context.Context, req SessionTokenRequest) (string, error) {
	data, err := c.call(ctx, "/v1/actions/auth/issue-session-token", req, "", "", "")
	if err != nil {
		return "", err
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("waffo 会话令牌响应为空")
	}
	return out.Token, nil
}

var orderIDPattern = regexp.MustCompile(`^ORD_[0-9A-Za-z]{22}$`)

// CancelSubscription 以商户身份请求取消：active 的订阅会进入 canceling，周期末结束。
func (c *Client) CancelSubscription(ctx context.Context, orderID, idempotencyKey string) error {
	if !orderIDPattern.MatchString(orderID) {
		return fmt.Errorf("订单号格式无效")
	}
	_, err := c.call(ctx, "/v1/actions/subscription-order/cancel-order", map[string]string{"orderId": orderID}, idempotencyKey, "", "")
	return err
}

// ReactivateSubscription 撤销周期末取消。Waffo 只开放了顾客侧入口，
// 因此先以该顾客的 buyerIdentity 签发会话令牌，再以令牌调用。
func (c *Client) ReactivateSubscription(ctx context.Context, orderID, token, environment string) error {
	if !orderIDPattern.MatchString(orderID) {
		return fmt.Errorf("订单号格式无效")
	}
	_, err := c.call(ctx, "/v1/actions/subscription-order/reactivate-order", map[string]string{"orderId": orderID}, "", token, environment)
	return err
}

// SubscriptionOrder 是 GraphQL 查到的订阅订单。日期字段按字符串保留，
// 与 Webhook 一样可能是纯日期，由调用方解析。
type SubscriptionOrder struct {
	ID                      string `json:"id"`
	Status                  string `json:"status"`
	OrderMerchantExternalID string `json:"orderMerchantExternalId"`
	CurrentPeriodStart      string `json:"currentPeriodStart"`
	CurrentPeriodEnd        string `json:"currentPeriodEnd"`
	CreatedAt               string `json:"createdAt"`
}

const subscriptionOrderQuery = `query($storeId: String!, $ref: String!) {
  subscriptionOrders(storeId: $storeId, filter: { orderMerchantExternalId: { eq: $ref } }) {
    id status orderMerchantExternalId currentPeriodStart currentPeriodEnd createdAt
  }
}`

// FindSubscriptionOrder 按结账时写入的 orderMerchantExternalId 查订阅订单，没有则返回 nil。
// 走只读的 GraphQL 端点，同样用 API Key 签名；环境由 API Key 决定。
func (c *Client) FindSubscriptionOrder(ctx context.Context, storeID, merchantExternalID string) (*SubscriptionOrder, error) {
	data, err := c.call(ctx, "/v1/graphql", map[string]any{
		"query":     subscriptionOrderQuery,
		"variables": map[string]string{"storeId": storeID, "ref": merchantExternalID},
	}, "", "", "")
	if err != nil {
		return nil, err
	}
	var out struct {
		SubscriptionOrders []SubscriptionOrder `json:"subscriptionOrders"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	for i := range out.SubscriptionOrders {
		// 过滤条件已经按它筛过；再核对一遍，避免过滤写法变化后误认别人的订单。
		if out.SubscriptionOrders[i].OrderMerchantExternalID == merchantExternalID {
			return &out.SubscriptionOrders[i], nil
		}
	}
	return nil, nil
}

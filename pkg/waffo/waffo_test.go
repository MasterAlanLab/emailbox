package waffo

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func signWebhook(t *testing.T, key *rsa.PrivateKey, at time.Time, body []byte) string {
	t.Helper()
	ts := strconv.FormatInt(at.UnixMilli(), 10)
	digest := sha256.Sum256([]byte(ts + "." + string(body)))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return "t=" + ts + ",v1=" + base64.StdEncoding.EncodeToString(sig)
}

// 时间戳是毫秒；签名覆盖原始 body，改一个字节就要失败。
func TestVerifyWebhookUsesMillisecondsAndRawBody(t *testing.T) {
	key := testKey(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	body := []byte(`{"eventId":"evt_1","mode":"test"}`)
	header := signWebhook(t, key, now, body)
	if err := VerifyWebhook(header, &key.PublicKey, body, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyWebhook(header, &key.PublicKey, []byte(`{"eventId":"evt_2","mode":"test"}`), now); err == nil {
		t.Fatal("篡改过的 body 通过了验签")
	}
}

// Waffo 重试时原样重放签名头，44 分钟前的 t 必须仍然有效；
// 未来一侧只容忍 1 分钟时钟偏差，超出即视为伪造。
func TestVerifyWebhookTimeWindow(t *testing.T) {
	key := testKey(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	body := []byte(`{}`)
	cases := []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"重试窗口内", now.Add(-44 * time.Minute), true},
		{"超过 45 分钟", now.Add(-46 * time.Minute), false},
		{"轻微超前", now.Add(30 * time.Second), true},
		{"超前 2 分钟", now.Add(2 * time.Minute), false},
	}
	for _, c := range cases {
		err := VerifyWebhook(signWebhook(t, key, c.at, body), &key.PublicKey, body, now)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v，期望通过 = %v", c.name, err, c.ok)
		}
	}
}

// 环境变量里的密钥有好几种写法，都要认得；认不出来必须报错，不能静默当成没配。
func TestParseKeysAcceptEnvFormats(t *testing.T) {
	key := testKey(t)
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	pkcs8DER, _ := x509.MarshalPKCS8PrivateKey(key)
	pubDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	privateInputs := map[string]string{
		"多行 PEM":          pkcs1,
		"转义换行的单行 PEM":     strings.ReplaceAll(pkcs1, "\n", `\n`),
		"去掉头尾的 Base64":    base64.StdEncoding.EncodeToString(pkcs8DER),
		"整段 PEM 再 Base64": base64.StdEncoding.EncodeToString([]byte(pkcs1)),
	}
	for name, raw := range privateInputs {
		got, err := ParsePrivateKey(raw)
		if err != nil || got.N.Cmp(key.N) != 0 {
			t.Errorf("私钥 %s: %v", name, err)
		}
	}
	if got, err := ParsePublicKey(strings.ReplaceAll(pub, "\n", `\n`)); err != nil || got.N.Cmp(key.N) != 0 {
		t.Errorf("单行公钥: %v", err)
	}
	if _, err := ParsePrivateKey("not a key"); err == nil {
		t.Error("非法私钥没有报错")
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	key := testKey(t)
	client, err := New(Config{BaseURL: server.URL, MerchantID: "MER_TEST",
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// 签名覆盖的必须是真正发出去的那份 body，并带上幂等键。
func TestCallSignsExactBodyAndAddsIdempotency(t *testing.T) {
	var got *http.Request
	var gotBody []byte
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"data":{"checkoutUrl":"https://checkout.example/s","sessionId":"cs_1","expiresAt":"2026-01-22T10:30:00.000Z"}}`))
	})
	session, err := client.CreateCheckout(t.Context(), CheckoutRequest{ProductID: "PROD_TEST", Currency: "USD"}, "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID != "cs_1" || session.ExpiresAt.IsZero() {
		t.Fatalf("会话解析不完整: %+v", session)
	}
	if got.Header.Get("X-Idempotency-Key") != "idem-1" || got.Header.Get("X-Merchant-Id") != "MER_TEST" {
		t.Fatal("缺少幂等键或商户头")
	}
	ts := got.Header.Get("X-Timestamp")
	bodyDigest := sha256.Sum256(gotBody)
	canonical := "POST\n/v1/actions/checkout/create-session\n" + ts + "\n" + base64.StdEncoding.EncodeToString(bodyDigest[:])
	digest := sha256.Sum256([]byte(canonical))
	sig, _ := base64.StdEncoding.DecodeString(got.Header.Get("X-Signature"))
	if err := rsa.VerifyPKCS1v15(&client.privateKey.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("签名与实际发送的 body 对不上: %v", err)
	}
}

// 200 里带 errors 也是失败；错误文案要带出来给管理员看。
func TestCallTreatsErrorEnvelopeAsFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"Store is not active","layer":"store"}]}`))
	})
	_, err := client.CreateSubscriptionProduct(t.Context(), SubscriptionProductRequest{StoreID: "STO_x"}, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Error(), "Store is not active") {
		t.Fatalf("err = %v，期望带出供应商错误文案的 APIError", err)
	}
}

// 内置的平台公钥必须能解析，否则所有 Webhook 都会被当成伪造拒掉。
func TestPlatformPublicKeysParse(t *testing.T) {
	for _, mode := range []string{"test", "prod"} {
		if _, err := ParsePublicKey(PlatformPublicKey(mode)); err != nil {
			t.Errorf("%s 平台公钥无法解析: %v", mode, err)
		}
	}
	if PlatformPublicKey("test") == PlatformPublicKey("prod") {
		t.Error("测试与生产公钥不应相同")
	}
}

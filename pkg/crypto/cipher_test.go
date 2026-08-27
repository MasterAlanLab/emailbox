package crypto

import (
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef" // 恰好 32 字节

func newTestCipher(t *testing.T) Cipher {
	t.Helper()
	c, err := New(testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	for _, plaintext := range []string{"hunter2", "含中文的密码 🔐", strings.Repeat("x", 4096)} {
		enc, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if !c.IsEncrypted(enc) {
			t.Errorf("密文缺少 %q 前缀: %q", Prefix, enc)
		}
		if strings.Contains(enc, plaintext) {
			t.Errorf("密文里出现了明文: %q", enc)
		}
		got, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plaintext {
			t.Errorf("往返结果不一致: 期望 %q，得到 %q", plaintext, got)
		}
	}
}

// 每次加密都要用新的随机 nonce，否则 GCM 的安全性会被破坏。
func TestEncryptUsesRandomNonce(t *testing.T) {
	c := newTestCipher(t)
	first, err := c.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := c.Encrypt("same-plaintext")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if first == second {
		t.Error("同一明文两次加密得到了相同密文，nonce 没有随机化")
	}
}

func TestEncryptEmptyStringStaysEmpty(t *testing.T) {
	c := newTestCipher(t)
	enc, err := c.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc != "" {
		t.Errorf("空串加密后应仍为空串，得到 %q", enc)
	}
}

// 用错误的密钥解密必须报错，而不是静默返回空串——
// 静默失败会让批量刷新把上万个账号误标为「令牌无效」。
func TestDecryptWithWrongKeyReturnsError(t *testing.T) {
	c := newTestCipher(t)
	enc, err := c.Encrypt("refresh-token")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	other, err := New("fedcba9876543210fedcba9876543210")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := other.Decrypt(enc)
	if err == nil {
		t.Fatalf("用错误密钥解密应报错，却返回了 %q", got)
	}
	if got != "" {
		t.Errorf("报错时不应返回内容，得到 %q", got)
	}
}

func TestDecryptRejectsCorruptedCiphertext(t *testing.T) {
	c := newTestCipher(t)
	cases := map[string]string{
		"非法 base64": Prefix + "!!!not-base64!!!",
		"长度不足":      Prefix + "AAAA",
		"篡改内容":      "",
	}
	enc, err := c.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// 翻转最后一个字符来模拟篡改
	tampered := []byte(enc)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	cases["篡改内容"] = string(tampered)

	for name, input := range cases {
		if _, err := c.Decrypt(input); err == nil {
			t.Errorf("%s：应报错却成功了", name)
		}
	}
}

// 开发模式下可能残留明文数据，解密时原样返回而不是报错。
func TestDecryptPassesThroughPlaintext(t *testing.T) {
	c := newTestCipher(t)
	got, err := c.Decrypt("legacy-plaintext")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "legacy-plaintext" {
		t.Errorf("期望原样返回，得到 %q", got)
	}
}

func TestParseKeyAcceptsHexBase64AndRaw(t *testing.T) {
	raw := strings.Repeat("k", KeySize)
	generated, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	valid := []string{
		raw,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", // hex
		generated, // base64
	}
	for _, key := range valid {
		got, err := ParseKey(key)
		if err != nil {
			t.Errorf("ParseKey(%q): %v", key, err)
			continue
		}
		if len(got) != KeySize {
			t.Errorf("ParseKey(%q) 长度为 %d，期望 %d", key, len(got), KeySize)
		}
	}
}

func TestParseKeyRejectsBadInput(t *testing.T) {
	for _, key := range []string{"", "   ", "too-short", strings.Repeat("k", 31)} {
		if _, err := ParseKey(key); err == nil {
			t.Errorf("ParseKey(%q) 应报错", key)
		}
	}
}

func TestPlaintextCipher(t *testing.T) {
	c := NewPlaintext()
	enc, err := c.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc != "secret" {
		t.Errorf("明文实现不应改写内容，得到 %q", enc)
	}
	// 读到真密文时必须报错，提示密钥丢失而不是把密文当明文用。
	if _, err := c.Decrypt(Prefix + "AAAA"); err == nil {
		t.Error("明文实现读到密文时应报错")
	}
}

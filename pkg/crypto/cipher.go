// Package crypto 提供敏感字段的对称加密与查表哈希。
//
// 邮箱账号的登录密码、OAuth refresh_token、代理地址等凭据必须密文落库，
// 且解密失败要能明确报错——静默返回空串会让批量刷新把上万个账号误判为
// 「令牌无效」，属于灾难性误判。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Prefix 是密文的固定前缀，带版本号是为将来的密钥轮换留出分派空间：
// v2 可以用新密钥加密，解密时按前缀选择对应的密钥。
const Prefix = "enc:v1:"

// KeySize 是 AES-256 要求的密钥长度。
const KeySize = 32

// ErrNoKey 表示未配置 ENCRYPTION_KEY，无法完成加解密。
var ErrNoKey = errors.New("未配置 ENCRYPTION_KEY，无法加解密敏感字段")

// Cipher 是敏感字段的加解密接口。
type Cipher interface {
	// Encrypt 把明文加密为带 Prefix 的密文串。
	Encrypt(plaintext string) (string, error)
	// Decrypt 解密密文串；传入非密文格式的字符串时原样返回，
	// 以便兼容开发模式下已有的明文数据。
	Decrypt(ciphertext string) (string, error)
	// IsEncrypted 判断字符串是否为本包产生的密文。
	IsEncrypted(s string) bool
}

// IsEncrypted 判断字符串是否为本包产生的密文。
func IsEncrypted(s string) bool { return strings.HasPrefix(s, Prefix) }

// HashToken 返回字符串的 SHA-256 十六进制哈希，
// 用于 API Key、分享链接 token 的查表比对（与 sessions.token_hash 的做法一致）。
func HashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// aesCipher 是基于 AES-256-GCM 的实现，每条记录使用独立随机 nonce。
type aesCipher struct {
	aead cipher.AEAD
}

// New 用原始密钥串构造 Cipher。密钥可以是 32 字节的 base64（标准或 URL 变体）
// 或 64 个字符的 hex；也接受恰好 32 字节的原始字符串。
func New(rawKey string) (Cipher, error) {
	key, err := ParseKey(rawKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("构造 AES 分组密码失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("构造 GCM 失败: %w", err)
	}
	return &aesCipher{aead: aead}, nil
}

// ParseKey 把配置里的密钥串解析为 32 字节密钥。
func ParseKey(rawKey string) ([]byte, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, ErrNoKey
	}
	if key, err := hex.DecodeString(rawKey); err == nil && len(key) == KeySize {
		return key, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(rawKey); err == nil && len(key) == KeySize {
			return key, nil
		}
	}
	if len(rawKey) == KeySize {
		return []byte(rawKey), nil
	}
	return nil, fmt.Errorf("ENCRYPTION_KEY 必须是 %d 字节密钥的 base64 或 hex 编码", KeySize)
}

// GenerateKey 随机生成一个 base64 编码的新密钥，供 `make gen-key` 之类的工具使用。
func GenerateKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("生成随机密钥失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

func (c *aesCipher) IsEncrypted(s string) bool { return IsEncrypted(s) }

func (c *aesCipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成 nonce 失败: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *aesCipher) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	// 开发模式下可能存在未加密的历史数据，原样返回而不是报错。
	if !IsEncrypted(ciphertext) {
		return ciphertext, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, Prefix))
	if err != nil {
		return "", fmt.Errorf("密文 base64 解码失败: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize+c.aead.Overhead() {
		return "", errors.New("密文长度不足，数据可能已损坏")
	}
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		// 这里绝不能吞掉错误：密钥变更或数据损坏必须让调用方感知。
		return "", fmt.Errorf("解密失败，密钥可能已变更或数据已损坏: %w", err)
	}
	return string(plaintext), nil
}

// plaintextCipher 是开发模式下未配置密钥时的降级实现：明文存储，便于本地调试。
// 生产模式不会走到这里——configs 层会在缺少密钥时直接启动失败。
type plaintextCipher struct{}

// NewPlaintext 返回不加密的降级实现，仅用于本地开发。
func NewPlaintext() Cipher { return plaintextCipher{} }

func (plaintextCipher) IsEncrypted(s string) bool { return IsEncrypted(s) }

func (plaintextCipher) Encrypt(plaintext string) (string, error) { return plaintext, nil }

func (plaintextCipher) Decrypt(ciphertext string) (string, error) {
	// 曾经配置过密钥、后来又被移除的场景：明文实现读不了旧密文，必须报错。
	if IsEncrypted(ciphertext) {
		return "", ErrNoKey
	}
	return ciphertext, nil
}

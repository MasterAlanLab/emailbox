package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"emailbox/pkg/crypto"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
)

// ErrAPIKeyInvalid 表示 Authorization 头里的 Key 不存在。handler 层映射为 401。
var ErrAPIKeyInvalid = errors.New("API Key 无效")

// APIKeyService 管理租户的对外取件凭据。
//
// Key 不是第二套接口，而是一个只读的虚拟角色（model.TenantRoleAPI）：
// 鉴权通过后照常走 /mail/** 那一份路由，权限由 middleware.Require 收敛。
type APIKeyService struct {
	store  *repo.Store
	cipher crypto.Cipher
}

func NewAPIKeyService(store *repo.Store, cipher crypto.Cipher) *APIKeyService {
	return &APIKeyService{store: store, cipher: cipher}
}

// Get 返回租户当前的 Key 明文。还没生成时返回 nil，由 handler 回 data:null。
func (s *APIKeyService) Get(ctx context.Context, tenantID string) (*model.APIKeyView, error) {
	key, err := s.store.GetAPIKeyByTenant(ctx, tenantID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	token, err := s.cipher.Decrypt(key.TokenEnc)
	if err != nil {
		// 密文解不开通常意味着 ENCRYPTION_KEY 换过。这时候 Key 本身仍然可用
		// （鉴权只看摘要），但没人能再看到它——只能重置。
		return nil, errors.New("无法解密该 Key，请重置后重新配置调用方")
	}
	return &model.APIKeyView{Token: token, CreatedAt: key.CreatedAt, UpdatedAt: key.UpdatedAt}, nil
}

// Reset 生成一把新 Key 并覆盖旧的。旧 Key 当场失效。
func (s *APIKeyService) Reset(ctx context.Context, tenantID string) (*model.APIKeyView, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("生成 API Key 失败: %w", err)
	}
	token := model.APIKeyPrefix + hex.EncodeToString(raw)
	enc, err := s.cipher.Encrypt(token)
	if err != nil {
		return nil, fmt.Errorf("加密 API Key 失败: %w", err)
	}
	if err := s.store.UpsertAPIKey(ctx, &model.TenantAPIKey{
		TenantID: tenantID, TokenHash: TokenHash(token), TokenEnc: enc,
	}); err != nil {
		return nil, err
	}
	// 回读一次拿准确的时间戳：created_at 由数据库的 CURRENT_TIMESTAMP 决定。
	return s.Get(ctx, tenantID)
}

// Authenticate 校验 Authorization 头里的 Key，返回它所属的租户。
//
// 只查摘要，不解密：解密是回显路径，鉴权路径上多一次 AES 运算没有意义，
// 而且失败模式（密钥换过）会把「Key 是对的」误判成「Key 是错的」。
func (s *APIKeyService) Authenticate(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrAPIKeyInvalid
	}
	key, err := s.store.GetAPIKeyByHash(ctx, TokenHash(token))
	if err != nil {
		return "", ErrAPIKeyInvalid
	}
	return key.TenantID, nil
}

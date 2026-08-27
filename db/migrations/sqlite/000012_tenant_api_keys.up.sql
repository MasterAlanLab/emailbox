-- 对外取件用的 API Key。一个租户一把，够用且没有「这把是哪个脚本的」这类问题。
--
-- 两列存两份不是冗余：
--   token_hash 供中间件按 O(1) 命中（UNIQUE 自带索引），和 sessions 一样只存摘要；
--   token_enc  是 AES-GCM 密文，只为了「回到页面还能看到自己的 Key」——
--              密文每次 nonce 不同，没法用它查，所以两列缺一不可。
--
-- 代价要说清楚：拿到库 + ENCRYPTION_KEY 就能还原 Key。这个库里本来就躺着
-- 同样可解密的 refresh_token 与邮箱密码，Key 的敏感度不高于它们，
-- 不新增风险类别。若某天要改成「只在创建时显示一次」，删掉 token_enc 即可。
CREATE TABLE tenant_api_keys (
    tenant_id  TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    token_enc  TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

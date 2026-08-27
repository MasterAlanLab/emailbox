-- 删表即吊销所有 Key。重新执行 up 之后每个租户都要重新生成。
DROP TABLE IF EXISTS tenant_api_keys;

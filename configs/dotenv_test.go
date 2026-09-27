package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// .env 存在却解析失败时必须报错，而且报错里不能带原文——
// godotenv 的错误会把出错位置之后的整段内容（常常是私钥）带出来。
// 曾经的处理是记一句「未找到 .env」继续启动，整份配置静默退回默认值。
func TestLoadDotEnvFailsLoudlyWithoutEchoingSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "A=1\nKEY=-----BEGIN PUBLIC KEY-----\nMIIBsecretline\n-----END PUBLIC KEY-----\nSECRET=\"topsecret\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	err := loadDotEnv(path)
	if err == nil {
		t.Fatal("未加引号的多行值应当报错")
	}
	if !strings.Contains(err.Error(), "第 3 行") {
		t.Errorf("报错没有指出出错的行: %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "MIIB") {
		t.Errorf("报错里带出了原文: %v", err)
	}
}

func TestLoadDotEnvAcceptsQuotedMultilineAndMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Errorf("没有 .env 是正常情况: %v", err)
	}
	path := filepath.Join(t.TempDir(), ".env")
	content := "EBX_TEST_PEM=\"-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("EBX_TEST_PEM") })
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(os.Getenv("EBX_TEST_PEM"), "MIIB") {
		t.Error("引号括起的多行值没有加载")
	}
}

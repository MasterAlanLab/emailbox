package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 密钥是解开本机全部邮箱凭据的唯一凭证：第二次启动必须拿到同一把，
// 拿不到就等于所有已存账号集体解密失败。
func TestLoadOrCreateEncryptionKeyIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrCreateEncryptionKey(dir)
	if err != nil {
		t.Fatalf("首次生成密钥失败: %v", err)
	}
	if first == "" {
		t.Fatal("生成的密钥为空")
	}
	second, err := loadOrCreateEncryptionKey(dir)
	if err != nil {
		t.Fatalf("复用密钥失败: %v", err)
	}
	if first != second {
		t.Fatal("第二次启动拿到了不同的密钥，已存凭据将无法解密")
	}
}

// 密钥文件必须只有本人可读：同一台机器上的其他用户读到它就等于拿到全部凭据。
func TestLoadOrCreateEncryptionKeyFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不使用 POSIX 权限位")
	}
	dir := t.TempDir()
	if _, err := loadOrCreateEncryptionKey(dir); err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, desktopKeyFile))
	if err != nil {
		t.Fatalf("读取密钥文件失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("密钥文件权限为 %o，应为 600", perm)
	}
}

// 密钥文件被改坏时必须报错而不是回落到明文存储：
// 静默回落会让邮箱密码从此以明文入库，而用户完全无从察觉。
func TestLoadOrCreateEncryptionKeyRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, desktopKeyFile), []byte("not-a-valid-key"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	if _, err := loadOrCreateEncryptionKey(dir); err == nil {
		t.Fatal("密钥非法时应当报错，不能静默生成新密钥或回落到明文")
	}
}

// 静态资源路径不能被 ".." 带出嵌入的文件系统。
func TestStaticFilePathBlocksTraversal(t *testing.T) {
	cases := map[string]string{
		"/index.html":              "index.html",
		"/assets/app-abc123.js":    "assets/app-abc123.js",
		"/../../etc/passwd":        "etc/passwd",
		"/assets/../../../secrets": "secrets",
		"/":                        "",
	}
	for in, want := range cases {
		if got := staticFilePath(in); got != want {
			t.Errorf("staticFilePath(%q) = %q，期望 %q", in, got, want)
		}
	}
}

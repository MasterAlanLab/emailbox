package model

import (
	"os"
	"regexp"
	"testing"
)

// 审计页把动作显示成中文，映射表在前端手工维护。后端每加一个 Audit* 常量，
// 前端就必须给它起名字——漏掉的话管理员在页面上看到的又是 account.delete 这样的代码。
func TestEveryAuditActionHasChineseLabel(t *testing.T) {
	src, err := os.ReadFile("audit.go")
	if err != nil {
		t.Fatal(err)
	}
	labels, err := os.ReadFile("../../web/src/lib/auditActions.ts")
	if err != nil {
		t.Fatal(err)
	}
	actions := regexp.MustCompile(`Audit[A-Za-z]+\s*=\s*"([a-z_.]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(actions) == 0 {
		t.Fatal("没有从 audit.go 解析出任何审计动作")
	}
	for _, m := range actions {
		if !regexp.MustCompile(`"` + regexp.QuoteMeta(m[1]) + `":`).Match(labels) {
			t.Errorf("审计动作 %s 在 web/src/lib/auditActions.ts 里没有中文名", m[1])
		}
	}
}

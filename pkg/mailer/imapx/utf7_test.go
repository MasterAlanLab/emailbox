package imapx

import "testing"

// golden case 取自 outlookEmail 的常量表与各服务商实际返回的邮箱名。
func TestDecodeUTF7(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"INBOX", "INBOX"},
		{"Deleted Messages", "Deleted Messages"},
		{"&V4NXPpCuTvY-", "垃圾邮件"},
		{"&V4NXPnux-", "垃圾箱"},
		{"&XfJSIJZkkK5O9g-", "已删除邮件"},
		{"&XfJSIJZk-", "已删除"},
		{"&XfJT0ZABkK5O9g-", "已发送邮件"},
		// &- 是字面量 & 的转义。
		{"AT&-T", "AT&T"},
		{"&-", "&"},
		// 编码段与 ASCII 混排。
		{"[Gmail]/&V4NXPpCuTvY-", "[Gmail]/垃圾邮件"},
		{"a&V4NXPnux-b", "a垃圾箱b"},
		{"", ""},
	}
	for _, c := range cases {
		got, err := DecodeUTF7(c.in)
		if err != nil {
			t.Errorf("DecodeUTF7(%q) 报错：%v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("DecodeUTF7(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// 畸形输入要报错而不是返回半截结果：半截的邮箱名拿去 SELECT
// 会命中另一个邮件夹，或者静默失败。
func TestDecodeUTF7RejectsMalformed(t *testing.T) {
	for _, in := range []string{
		"&V4NXPpCuTvY",   // 缺结束符
		"&",              // 只有转义符
		"&@@@-",          // 非法 base64
		"&V4NXPpCuTvYA-", // UTF-16 码元不完整（奇数字节）
	} {
		if got, err := DecodeUTF7(in); err == nil {
			t.Errorf("DecodeUTF7(%q) = %q，期望报错", in, got)
		}
	}
}

func TestEncodeUTF7(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"INBOX", "INBOX"},
		{"垃圾邮件", "&V4NXPpCuTvY-"},
		{"垃圾箱", "&V4NXPnux-"},
		{"已删除邮件", "&XfJSIJZkkK5O9g-"},
		{"AT&T", "AT&-T"},
		{"&", "&-"},
		{"[Gmail]/垃圾邮件", "[Gmail]/&V4NXPpCuTvY-"},
		{"", ""},
	}
	for _, c := range cases {
		if got := EncodeUTF7(c.in); got != c.want {
			t.Errorf("EncodeUTF7(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestUTF7RoundTrip(t *testing.T) {
	for _, s := range []string{
		"INBOX", "垃圾邮件", "已删除邮件", "已发送邮件", "收件箱",
		"AT&T", "a&b&c", "[Gmail]/所有邮件", "混合 mixed 文本",
		// 基本多文种平面之外的字符走代理对，编解码都要正确处理。
		"emoji📮箱",
	} {
		encoded := EncodeUTF7(s)
		got, err := DecodeUTF7(encoded)
		if err != nil {
			t.Errorf("往返 %q 失败（编码为 %q）：%v", s, encoded, err)
			continue
		}
		if got != s {
			t.Errorf("往返 %q → %q → %q", s, encoded, got)
		}
	}
}

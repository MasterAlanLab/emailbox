package mailer

import "testing"

func TestProviderForEmail(t *testing.T) {
	cases := map[string]struct {
		code string
		typ  AccountType
	}{
		"a@outlook.com":     {"outlook", AccountTypeOutlook},
		"a@hotmail.com":     {"outlook", AccountTypeOutlook},
		"a@live.com":        {"outlook", AccountTypeOutlook},
		"a@live.cn":         {"outlook", AccountTypeOutlook},
		"a@gmail.com":       {"gmail", AccountTypeIMAP},
		"a@googlemail.com":  {"gmail", AccountTypeIMAP},
		"a@qq.com":          {"qq", AccountTypeIMAP},
		"a@foxmail.com":     {"qq", AccountTypeIMAP},
		"a@163.com":         {"163", AccountTypeIMAP},
		"a@126.com":         {"126", AccountTypeIMAP},
		"a@yahoo.co.jp":     {"yahoo", AccountTypeIMAP},
		"a@aliyun.com":      {"aliyun", AccountTypeIMAP},
		"a@2925.com":        {"2925", AccountTypeIMAP},
		"A@OutLook.COM":     {"outlook", AccountTypeOutlook},
		"a@ mail.163.com  ": {ProviderCustom, AccountTypeIMAP},
		"a@unknown.tld":     {ProviderCustom, AccountTypeIMAP},
		// 带 + 别名的地址要按 @ 之后的域名判断，不能被前面的内容带偏。
		"a+tag@outlook.com": {"outlook", AccountTypeOutlook},
		// 地址里有多个 @ 时以最后一个为准。
		"a@b@gmail.com": {"gmail", AccountTypeIMAP},
		"nodomain":      {ProviderCustom, AccountTypeIMAP},
		"":              {ProviderCustom, AccountTypeIMAP},
	}
	for email, want := range cases {
		got := ProviderForEmail(email)
		if got.Code != want.code {
			t.Errorf("ProviderForEmail(%q).Code = %q，期望 %q", email, got.Code, want.code)
		}
		if got.Type != want.typ {
			t.Errorf("ProviderForEmail(%q).Type = %q，期望 %q", email, got.Type, want.typ)
		}
	}
}

// 只有 Outlook 是 OAuth 账号，也只有它有三条通道。
// 这个判断错了会让回退链形态整个跑偏。
func TestOnlyOutlookIsOAuthAccount(t *testing.T) {
	for code, p := range Providers {
		wantOAuth := code == "outlook"
		if (p.Type == AccountTypeOutlook) != wantOAuth {
			t.Errorf("服务商 %q 的类型 = %q，与预期不符", code, p.Type)
		}
	}
}

func TestProviderByCode(t *testing.T) {
	if got := ProviderByCode("  GMAIL "); got.Code != "gmail" {
		t.Errorf("大小写与空白应当被规范化，实际 %q", got.Code)
	}
	if got := ProviderByCode("nope"); got.Code != ProviderCustom {
		t.Errorf("未知 code 应当回落到 custom，实际 %q", got.Code)
	}
}

// 前端下拉框直接用这个列表，缺项就是「用户选不到某个服务商」。
func TestKnownProvidersCoversEveryProvider(t *testing.T) {
	list := KnownProviders()
	if len(list) != len(Providers) {
		t.Fatalf("列表 %d 项，服务商表 %d 项", len(list), len(Providers))
	}
	seen := map[string]bool{}
	for _, p := range list {
		if p.Code == "" {
			t.Fatalf("列表里出现了空项，说明 order 里写了服务商表中不存在的 code：%+v", list)
		}
		if seen[p.Code] {
			t.Errorf("%q 重复出现", p.Code)
		}
		seen[p.Code] = true
		if p.Label == "" {
			t.Errorf("%q 缺少展示名", p.Code)
		}
		if p.Code != ProviderCustom && p.IMAPHost == "" {
			t.Errorf("%q 缺少 IMAP 主机", p.Code)
		}
		if p.IMAPPort == 0 {
			t.Errorf("%q 缺少 IMAP 端口", p.Code)
		}
	}
}

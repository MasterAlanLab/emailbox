package mailer

import "testing"

const (
	uuidA = "24d9a0ed-1234-4abc-9def-0123456789ab"
	uuidB = "9e5f94bc-e8a4-4e73-b8be-63364c29d753"
)

// 4 段格式里 client_id 与 refresh_token 的顺序在野外两种都有。
// 判错的后果是刷新令牌静默失败、错误信息毫无指向性，所以这里逐种情形钉死。
func TestParseOutlookOAuthResolvesFieldOrder(t *testing.T) {
	cases := []struct {
		name             string
		line             string
		clientIDFirst    bool
		wantClientID     string
		wantRefreshToken string
	}{
		{
			name:             "client_id 在前（形态可判）",
			line:             "u@outlook.com----pwd----" + uuidA + "----M.C123_BAY.0.U.longtokenvalue",
			wantClientID:     uuidA,
			wantRefreshToken: "M.C123_BAY.0.U.longtokenvalue",
		},
		{
			name:             "client_id 在后（形态可判，顺序反了也能纠正）",
			line:             "u@outlook.com----pwd----M.C123_BAY.0.U.longtokenvalue----" + uuidA,
			wantClientID:     uuidA,
			wantRefreshToken: "M.C123_BAY.0.U.longtokenvalue",
		},
		{
			name:             "两段都像 UUID，按 ClientIDFirst=true 取前者",
			line:             "u@outlook.com----pwd----" + uuidA + "----" + uuidB,
			clientIDFirst:    true,
			wantClientID:     uuidA,
			wantRefreshToken: uuidB,
		},
		{
			name:             "两段都像 UUID，按 ClientIDFirst=false 取后者",
			line:             "u@outlook.com----pwd----" + uuidA + "----" + uuidB,
			clientIDFirst:    false,
			wantClientID:     uuidB,
			wantRefreshToken: uuidA,
		},
	}
	for _, c := range cases {
		got, err := ParseLine(c.line, ImportOptions{Format: FormatOutlookOAuth, ClientIDFirst: c.clientIDFirst})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.ClientID != c.wantClientID {
			t.Errorf("%s: client_id = %q，期望 %q", c.name, got.ClientID, c.wantClientID)
		}
		if got.RefreshToken != c.wantRefreshToken {
			t.Errorf("%s: refresh_token = %q，期望 %q", c.name, got.RefreshToken, c.wantRefreshToken)
		}
		if got.AccountType != AccountTypeOutlook {
			t.Errorf("%s: account_type = %q，期望 outlook", c.name, got.AccountType)
		}
	}
}

func TestParseIMAPInfersProviderFromDomain(t *testing.T) {
	cases := map[string]struct {
		provider string
		host     string
	}{
		"a@gmail.com":   {"gmail", "imap.gmail.com"},
		"a@foxmail.com": {"qq", "imap.qq.com"},
		"a@163.com":     {"163", "imap.163.com"},
		"a@yahoo.co.jp": {"yahoo", "imap.mail.yahoo.com"},
		"a@aliyun.com":  {"aliyun", "imap.aliyun.com"},
	}
	for email, want := range cases {
		got, err := ParseLine(email+"----app-password", ImportOptions{Format: FormatIMAP})
		if err != nil {
			t.Errorf("%s: %v", email, err)
			continue
		}
		if got.Provider != want.provider || got.IMAPHost != want.host {
			t.Errorf("%s: 得到 %s/%s，期望 %s/%s", email, got.Provider, got.IMAPHost, want.provider, want.host)
		}
		if got.IMAPPort != DefaultIMAPPort {
			t.Errorf("%s: 端口 %d，期望 %d", email, got.IMAPPort, DefaultIMAPPort)
		}
		if got.Password != "app-password" {
			t.Errorf("%s: 密码 %q", email, got.Password)
		}
	}
}

// 未知域名无法推断服务商，必须报错而不是悄悄落到一个错误的 IMAP 服务器上。
func TestParseIMAPRejectsUnknownDomain(t *testing.T) {
	if _, err := ParseLine("a@self-hosted.example----pwd", ImportOptions{Format: FormatIMAP}); err == nil {
		t.Error("未知域名应报错并提示改用自定义 IMAP 格式")
	}
}

func TestParseCustomIMAP(t *testing.T) {
	got, err := ParseLine("a@corp.example----pwd----mail.corp.example----1993", ImportOptions{Format: FormatCustomIMAP})
	if err != nil {
		t.Fatal(err)
	}
	if got.IMAPHost != "mail.corp.example" || got.IMAPPort != 1993 {
		t.Errorf("得到 %s:%d", got.IMAPHost, got.IMAPPort)
	}
	if got.Provider != ProviderCustom || got.AccountType != AccountTypeIMAP {
		t.Errorf("provider=%s type=%s", got.Provider, got.AccountType)
	}

	// 行内未给出时用统一默认值
	got, err = ParseLine("a@corp.example----pwd", ImportOptions{
		Format: FormatCustomIMAP, IMAPHost: "mail.default.example", IMAPPort: 993,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.IMAPHost != "mail.default.example" {
		t.Errorf("应回落到统一默认主机，实际 %s", got.IMAPHost)
	}

	if _, err := ParseLine("a@corp.example----pwd----host.example----notaport", ImportOptions{Format: FormatCustomIMAP}); err == nil {
		t.Error("非法端口应报错")
	}
	if _, err := ParseLine("a@corp.example----pwd----host.example----70000", ImportOptions{Format: FormatCustomIMAP}); err == nil {
		t.Error("超出范围的端口应报错")
	}
}

// 自动识别要能在三种格式间正确分流，否则批量导入会把整批账号存成错误类型。
func TestDetectFormat(t *testing.T) {
	cases := []struct {
		line string
		want ImportFormat
	}{
		{"a@gmail.com----pwd", FormatIMAP},
		{"a@corp.example----pwd----mail.corp.example----993", FormatCustomIMAP},
		{"a@outlook.com----pwd----" + uuidA + "----token", FormatOutlookOAuth},
		// 第 3 段既不像 UUID 也不像主机名（没有点）→ 按 OAuth 处理
		{"a@outlook.com----pwd----notahost----token", FormatOutlookOAuth},
	}
	for _, c := range cases {
		if got := detectFormat(splitFields(c.line)); got != c.want {
			t.Errorf("%q: 识别为 %s，期望 %s", c.line, got, c.want)
		}
	}
}

func TestParseLineRejectsMalformedInput(t *testing.T) {
	for _, line := range []string{"", "   ", "notanemail----pwd", "a@example.com"} {
		if _, err := ParseLine(line, ImportOptions{Format: FormatAuto}); err == nil {
			t.Errorf("%q 应被拒绝", line)
		}
	}
}

// 行尾多余的分隔符与空白在真实导出文件里很常见，不该让整行失败。
func TestParseLineToleratesTrailingSeparators(t *testing.T) {
	got, err := ParseLine("  a@gmail.com----pwd----  ", ImportOptions{Format: FormatAuto})
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "a@gmail.com" || got.Password != "pwd" {
		t.Errorf("解析结果 %+v", got)
	}
}

// 行尾分隔符的清理不能用 strings.Trim(line, "----")——那个 cutset 是**字符集合**，
// 会把密码末尾合法的 "-" 一起吃掉，属于静默篡改凭据。
func TestParseLinePreservesTrailingHyphenInPassword(t *testing.T) {
	got, err := ParseLine("a@gmail.com----secret-", ImportOptions{Format: FormatIMAP})
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "secret-" {
		t.Errorf("密码被篡改：得到 %q，期望 %q", got.Password, "secret-")
	}
}

func TestIsProbableClientID(t *testing.T) {
	valid := []string{uuidA, uuidB, "24D9A0ED-1234-4ABC-9DEF-0123456789AB"}
	invalid := []string{
		"", "24d9a0ed12344abc9def0123456789ab", // 无连字符
		"24d9a0ed-1234-4abc-9def-0123456789a",  // 少一位
		"24d9a0ed-1234-4abc-9def-0123456789ag", // 含非十六进制字符
		"M.C123_BAY.0.U.longtokenvalue",
	}
	for _, s := range valid {
		if !isProbableClientID(s) {
			t.Errorf("%q 应被识别为 client_id", s)
		}
	}
	for _, s := range invalid {
		if isProbableClientID(s) {
			t.Errorf("%q 不应被识别为 client_id", s)
		}
	}
}

// 服务商识别本身的用例在 provider_test.go。

// 导出的唯一硬性要求是能被重新导入且解析回同样的凭据。
// FormatLine 的分支必须与 detectFormat 的判据对偶，这里逐种形态跑一遍往返。
func TestFormatLineRoundTripsThroughParseLine(t *testing.T) {
	cases := []ParsedAccount{
		{
			Email: "u@outlook.com", Password: "pwd", ClientID: uuidA,
			RefreshToken: "M.C123_BAY.0.U.longtokenvalue",
			IMAPHost:     IMAPServerNew, IMAPPort: DefaultIMAPPort,
		},
		// 密码为空的 OAuth 账号：中间那段是空的，切分后不能少一段错位
		{
			Email: "u@outlook.com", ClientID: uuidA, RefreshToken: "M.token",
			IMAPHost: IMAPServerNew, IMAPPort: DefaultIMAPPort,
		},
		{
			Email: "u@gmail.com", Password: "app-password",
			IMAPHost: "imap.gmail.com", IMAPPort: DefaultIMAPPort,
		},
		{
			Email: "u@corp.example.com", Password: "pwd",
			IMAPHost: "mail.corp.example.com", IMAPPort: 1993,
		},
	}
	for _, want := range cases {
		line, err := FormatLine(want)
		if err != nil {
			t.Fatalf("导出 %s 失败: %v", want.Email, err)
		}
		got, err := ParseLine(line, ImportOptions{ClientIDFirst: true})
		if err != nil {
			t.Fatalf("重新导入 %q 失败: %v", line, err)
		}
		if got.Email != want.Email || got.Password != want.Password ||
			got.RefreshToken != want.RefreshToken || got.ClientID != want.ClientID ||
			got.IMAPHost != want.IMAPHost || got.IMAPPort != want.IMAPPort {
			t.Errorf("往返后凭据不一致\n导出: %q\n期望: %+v\n实际: %+v", line, want, *got)
		}
	}
}

// 分隔符出现在凭据里会让重新导入切出完全不同的字段——这是静默的数据损坏，
// 宁可导不出来。
func TestFormatLineRejectsSeparatorInCredential(t *testing.T) {
	_, err := FormatLine(ParsedAccount{
		Email: "u@gmail.com", Password: "bad" + Separator + "password",
		IMAPHost: "imap.gmail.com", IMAPPort: DefaultIMAPPort,
	})
	if err == nil {
		t.Fatal("凭据含分隔符时应报错")
	}
}

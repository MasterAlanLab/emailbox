package mailer

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatLine 把一个账号还原成一行导入文本，是 ParseLine 的逆运算。
//
// 导出文件的唯一硬性要求是「能被本平台重新导入且账号数一致」，因此这里的分支
// 必须与 detectFormat 的判据严格对偶：
//   - 有 refresh_token → 4 段 outlook 形态（第 3 段是 UUID 形态的 client_id）
//   - 域名能推断出服务商且服务器仍是默认值 → 2 段形态，重新导入时由域名推断
//   - 其余 → 4 段自定义 IMAP 形态（第 3 段像主机名，detectFormat 据此分流）
func FormatLine(a ParsedAccount) (string, error) {
	fields, err := exportFields(a)
	if err != nil {
		return "", err
	}
	for _, f := range fields {
		// 凭据里出现分隔符会让这一行在重新导入时被切成完全不同的字段，
		// 那是静默的数据损坏——宁可导不出来，也不能导出一份错的。
		if strings.Contains(f, Separator) {
			return "", fmt.Errorf("凭据中包含分隔符 %q，无法导出", Separator)
		}
	}
	return strings.Join(fields, Separator), nil
}

func exportFields(a ParsedAccount) ([]string, error) {
	email := strings.TrimSpace(a.Email)
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("邮箱 %q 不合法", a.Email)
	}
	if a.RefreshToken != "" {
		clientID := a.ClientID
		if clientID == "" {
			clientID = DefaultOAuthClientID
		}
		return []string{email, a.Password, clientID, a.RefreshToken}, nil
	}
	if a.Password == "" {
		return nil, fmt.Errorf("账号 %s 没有可导出的凭据", email)
	}
	if p := ProviderForEmail(email); p.Code != ProviderCustom &&
		p.IMAPHost == a.IMAPHost && p.IMAPPort == a.IMAPPort {
		return []string{email, a.Password}, nil
	}
	if a.IMAPHost == "" {
		return nil, fmt.Errorf("账号 %s 既无 refresh_token 也无 IMAP 服务器地址", email)
	}
	port := a.IMAPPort
	if port == 0 {
		port = DefaultIMAPPort
	}
	return []string{email, a.Password, a.IMAPHost, strconv.Itoa(port)}, nil
}

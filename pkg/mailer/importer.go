package mailer

import (
	"fmt"
	"strconv"
	"strings"
)

// Separator 是导入文本的字段分隔符，与 outlookEmail 的导出格式一致。
const Separator = "----"

// ImportFormat 是导入内容的格式。
type ImportFormat string

const (
	// FormatOutlookOAuth 是 4 段：邮箱----密码----A----B，A/B 为 client_id 与 refresh_token。
	FormatOutlookOAuth ImportFormat = "outlook_oauth"
	// FormatIMAP 是 2 段：邮箱----授权码，服务商由域名推断。
	FormatIMAP ImportFormat = "imap"
	// FormatCustomIMAP 是 4 段：邮箱----密码----imap_host----imap_port。
	FormatCustomIMAP ImportFormat = "custom_imap"
	// FormatAuto 让解析器按段数与形态自行判断。
	FormatAuto ImportFormat = "auto"
)

// ParsedAccount 是一行导入内容解析后的结果。
type ParsedAccount struct {
	Email        string
	Password     string
	ClientID     string
	RefreshToken string
	IMAPHost     string
	IMAPPort     int
	Provider     string
	AccountType  AccountType
}

// ImportOptions 调整解析行为。
type ImportOptions struct {
	Format ImportFormat
	// ClientIDFirst 决定 4 段格式里两段都像/都不像 client_id 时的取用顺序。
	// 与 outlookEmail 的 account_format 参数对应，默认 client_id 在前。
	ClientIDFirst bool
	// IMAPHost/IMAPPort 在 custom_imap 格式下作为统一默认值，
	// 行内显式给出的值优先。
	IMAPHost string
	IMAPPort int
}

// ParseLine 解析一行导入内容。
//
// 4 段 Outlook 格式最麻烦的地方是 A/B 两段哪个是 client_id、哪个是 refresh_token：
// 导出源不统一，两种顺序都在野外存在。判据是 client_id 恒为 8-4-4-4-12 的 UUID 形态，
// 而 refresh_token 是长随机串。两段都像或都不像时才回退到 ClientIDFirst 指定的顺序。
// 判错的后果是刷新令牌时静默失败，且错误信息毫无指向性，所以这里宁可多花几行判断。
func ParseLine(line string, opts ImportOptions) (*ParsedAccount, error) {
	parts := splitFields(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("字段不足，至少需要「邮箱%s凭据」两段", Separator)
	}
	email := strings.ToLower(strings.TrimSpace(parts[0]))
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("第 1 段 %q 不是合法邮箱", parts[0])
	}

	format := opts.Format
	if format == "" || format == FormatAuto {
		format = detectFormat(parts)
	}
	switch format {
	case FormatIMAP:
		return parseIMAP(email, parts)
	case FormatCustomIMAP:
		return parseCustomIMAP(email, parts, opts)
	case FormatOutlookOAuth:
		return parseOutlookOAuth(email, parts, opts)
	default:
		return nil, fmt.Errorf("未知的导入格式 %q", format)
	}
}

// detectFormat 按段数与第 3 段的形态推断格式。
func detectFormat(parts []string) ImportFormat {
	if len(parts) < 4 {
		return FormatIMAP
	}
	// 第 3 段不像 UUID 却像域名 → 自定义 IMAP（邮箱----密码----host----port）
	third := strings.TrimSpace(parts[2])
	if !isProbableClientID(third) && looksLikeHost(third) {
		return FormatCustomIMAP
	}
	return FormatOutlookOAuth
}

func parseIMAP(email string, parts []string) (*ParsedAccount, error) {
	password := strings.TrimSpace(parts[1])
	if password == "" {
		return nil, fmt.Errorf("第 2 段（授权码）为空")
	}
	p := ProviderForEmail(email)
	if p.Code == ProviderCustom {
		return nil, fmt.Errorf("无法从域名推断服务商，请改用自定义 IMAP 格式并指定服务器")
	}
	return &ParsedAccount{
		Email: email, Password: password,
		IMAPHost: p.IMAPHost, IMAPPort: p.IMAPPort,
		Provider: p.Code, AccountType: p.Type,
	}, nil
}

func parseCustomIMAP(email string, parts []string, opts ImportOptions) (*ParsedAccount, error) {
	password := strings.TrimSpace(parts[1])
	if password == "" {
		return nil, fmt.Errorf("第 2 段（密码）为空")
	}
	host := opts.IMAPHost
	port := opts.IMAPPort
	if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
		host = strings.TrimSpace(parts[2])
	}
	if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
		v, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil || v <= 0 || v > 65535 {
			return nil, fmt.Errorf("第 4 段 %q 不是合法端口", parts[3])
		}
		port = v
	}
	if host == "" {
		return nil, fmt.Errorf("缺少 IMAP 服务器地址")
	}
	if port == 0 {
		port = DefaultIMAPPort
	}
	return &ParsedAccount{
		Email: email, Password: password, IMAPHost: host, IMAPPort: port,
		Provider: ProviderCustom, AccountType: AccountTypeIMAP,
	}, nil
}

func parseOutlookOAuth(email string, parts []string, opts ImportOptions) (*ParsedAccount, error) {
	if len(parts) < 4 {
		return nil, fmt.Errorf("outlook 格式需要 4 段：邮箱%s密码%sclient_id%srefresh_token",
			Separator, Separator, Separator)
	}
	password := strings.TrimSpace(parts[1])
	a, b := strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])

	clientID, refreshToken := a, b
	switch {
	case isProbableClientID(a) && !isProbableClientID(b):
		clientID, refreshToken = a, b
	case isProbableClientID(b) && !isProbableClientID(a):
		clientID, refreshToken = b, a
	default:
		// 两段都像 UUID 或都不像，无从判断形态，只能听配置的。
		if !opts.ClientIDFirst {
			clientID, refreshToken = b, a
		}
	}
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh_token 为空")
	}
	if clientID == "" {
		clientID = DefaultOAuthClientID
	}
	p := ProviderForEmail(email)
	// 非 outlook 域名也可能走 OAuth（企业自建域），此时保留推断出的服务商标签，
	// 但连接参数按 Outlook 处理。
	host, port := IMAPServerNew, DefaultIMAPPort
	provider := p.Code
	if provider == ProviderCustom {
		provider = "outlook"
	}
	return &ParsedAccount{
		Email: email, Password: password, ClientID: clientID, RefreshToken: refreshToken,
		IMAPHost: host, IMAPPort: port, Provider: provider, AccountType: AccountTypeOutlook,
	}, nil
}

// splitFields 按 ---- 切分，并容忍行尾的空白与多余分隔符。
//
// 注意不能用 strings.Trim(line, Separator) 去掉行尾分隔符：Trim 的第二个参数是
// **字符集合**而非后缀，那样会把密码末尾合法的 "-" 一并吃掉。
// 这里改为先切分、再丢弃尾部的空字段。
func splitFields(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.Split(line, Separator)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// isProbableClientID 判断字符串是否为 8-4-4-4-12 的 UUID 形态。
// 这是区分 client_id 与 refresh_token 的唯一可靠特征。
func isProbableClientID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
			if !isHex {
				return false
			}
		}
	}
	return true
}

// looksLikeHost 判断字符串是否像主机名：含点、不含 @ 与空格。
func looksLikeHost(s string) bool {
	if s == "" || strings.ContainsAny(s, "@ \t") {
		return false
	}
	if !strings.Contains(s, ".") {
		return false
	}
	// 纯数字加点的是 IP，也算合法主机
	for _, r := range s {
		ok := r == '.' || r == '-' || r >= '0' && r <= '9' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !ok {
			return false
		}
	}
	return true
}

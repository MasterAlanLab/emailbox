package service

import (
	"net/url"
	"strings"
)

// MaskProxyURL 把代理地址里的口令替换成 ****，用于日志与接口回显。
//
// 代理串常见形态是 socks5h://user:pass@host:port，其中 user 部分可能还带
// {mail} 模板。整串是加密存储的敏感字段，任何出库路径都必须先过这里——
// 一次疏忽就会把上千个代理账号的口令写进日志文件。
func MaskProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	// 解析失败时不能原样返回：那正是最容易漏掉口令的分支。
	if err != nil || u.User == nil {
		if !strings.Contains(raw, "@") {
			return raw
		}
		at := strings.LastIndex(raw, "@")
		scheme := ""
		if i := strings.Index(raw, "://"); i >= 0 {
			scheme = raw[:i+3]
		}
		return scheme + "****" + raw[at:]
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return u.String()
	}
	u.User = url.UserPassword(u.User.Username(), "****")
	// url.String() 会把 **** 转义，这里换回可读形式。
	return strings.ReplaceAll(u.String(), "%2A%2A%2A%2A", "****")
}

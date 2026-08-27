// Package imapx 是 IMAP 通道。本文件实现 IMAP modified UTF-7（RFC 3501 §5.1.3）。
//
// Go 标准库没有这个编码，而中文邮箱的文件夹名几乎全是它编码的
// （QQ 的垃圾箱就是 &V4NXPpCuTvY-）。不实现它就只能靠硬编码的候选串去猜，
// LIST 回来的名字也没法读。
package imapx

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf16"
)

// modified UTF-7 用 , 代替 base64 的 /，且不带 = 填充。
// 用 , 是因为 / 在 IMAP 的邮箱名里是层级分隔符。
var utf7Base64 = base64.NewEncoding(
	"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+,",
).WithPadding(base64.NoPadding)

// DecodeUTF7 把 IMAP modified UTF-7 的邮箱名解成 Go 字符串。
func DecodeUTF7(s string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			out.WriteByte(s[i])
			i++
			continue
		}
		rest := s[i+1:]
		end := strings.IndexByte(rest, '-')
		if end < 0 {
			return "", fmt.Errorf("imapx: %q 里的 & 没有对应的结束符 -", s)
		}
		chunk := rest[:end]
		i += 1 + end + 1

		// &- 是字面量 & 的转义。
		if chunk == "" {
			out.WriteByte('&')
			continue
		}
		decoded, err := decodeChunk(chunk)
		if err != nil {
			return "", fmt.Errorf("imapx: 解码 %q 失败: %w", s, err)
		}
		out.WriteString(decoded)
	}
	return out.String(), nil
}

func decodeChunk(chunk string) (string, error) {
	raw, err := utf7Base64.DecodeString(chunk)
	if err != nil {
		return "", err
	}
	if len(raw)%2 != 0 {
		return "", fmt.Errorf("UTF-16 码元不完整（%d 字节）", len(raw))
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
	}
	return string(utf16.Decode(units)), nil
}

// EncodeUTF7 把 Go 字符串编成 IMAP modified UTF-7 的邮箱名。
func EncodeUTF7(s string) string {
	var out strings.Builder
	var pending []rune

	flush := func() {
		if len(pending) == 0 {
			return
		}
		units := utf16.Encode(pending)
		raw := make([]byte, 0, len(units)*2)
		for _, u := range units {
			raw = append(raw, byte(u>>8), byte(u))
		}
		out.WriteByte('&')
		out.WriteString(utf7Base64.EncodeToString(raw))
		out.WriteByte('-')
		pending = pending[:0]
	}

	for _, r := range s {
		switch {
		case r == '&':
			flush()
			out.WriteString("&-")
		// 可打印 ASCII 直接原样输出。
		case r >= 0x20 && r <= 0x7e:
			flush()
			out.WriteRune(r)
		default:
			pending = append(pending, r)
		}
	}
	flush()
	return out.String()
}

package imapx

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
)

// newLenientBase64Reader 解码 base64 传输编码，并容忍尾部损坏。
//
// 野外的 base64 正文经常是坏的：分页抓取截断、发件端换行处理有 bug、
// 中转 MTA 改写过内容。标准库的 decoder 一遇到坏字节就整个失败，
// 那样用户看到的是一封完全空白的信。这里保留能解出来的前缀——
// 一封少了结尾的信仍然是可读的。
func newLenientBase64Reader(r io.Reader) io.Reader {
	raw, err := io.ReadAll(io.LimitReader(r, maxAttachmentBytes))
	if len(raw) == 0 && err != nil {
		return bytes.NewReader(nil)
	}

	cleaned := make([]byte, 0, len(raw))
	for _, b := range raw {
		switch b {
		case '\r', '\n', ' ', '\t':
			// base64 正文按 76 字符折行，空白一律丢弃。
		default:
			cleaned = append(cleaned, b)
		}
	}
	// 用 RawStdEncoding 自己控制填充，才能在任意位置截断后继续解。
	trimmed := bytes.TrimRight(cleaned, "=")

	for len(trimmed) > 0 {
		// 长度 %4==1 是不可能出现的合法形态，砍掉一个字节。
		if len(trimmed)%4 == 1 {
			trimmed = trimmed[:len(trimmed)-1]
			continue
		}
		decoded, err := base64.RawStdEncoding.DecodeString(string(trimmed))
		if err == nil {
			return bytes.NewReader(decoded)
		}
		var corrupt base64.CorruptInputError
		if errors.As(err, &corrupt) && int(corrupt) < len(trimmed) {
			trimmed = trimmed[:int(corrupt)]
			continue
		}
		break
	}
	return bytes.NewReader(nil)
}

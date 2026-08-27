package imapx

import (
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// 中文邮箱的头部常用 GBK/GB2312/Big5 编码。Python 的 decode_header 默认能处理，
// Go 不显式接 x/text 的话主题会变成乱码——而且只在特定服务商上出现，
// 用 Outlook 账号测是发现不了的。
func TestDecodeHeaderHandlesChineseCharsets(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().String("测试主题")
	if err != nil {
		t.Fatal(err)
	}
	big5, err := traditionalchinese.Big5.NewEncoder().String("測試主題")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "UTF-8 base64",
			in:   "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("你好")) + "?=",
			want: "你好",
		},
		{
			name: "GBK base64",
			in:   "=?GBK?B?" + base64.StdEncoding.EncodeToString([]byte(gbk)) + "?=",
			want: "测试主题",
		},
		{
			name: "GB2312 base64",
			in:   "=?GB2312?B?" + base64.StdEncoding.EncodeToString([]byte(gbk)) + "?=",
			want: "测试主题",
		},
		{
			name: "Big5 base64",
			in:   "=?Big5?B?" + base64.StdEncoding.EncodeToString([]byte(big5)) + "?=",
			want: "測試主題",
		},
		{
			name: "quoted-printable",
			in:   "=?UTF-8?Q?Hello=20World?=",
			want: "Hello World",
		},
		{
			name: "编码字与纯文本混排",
			in:   "Re: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("你好")) + "?= (fwd)",
			want: "Re: 你好 (fwd)",
		},
		{
			name: "没有编码字时原样返回",
			in:   "Plain Subject",
			want: "Plain Subject",
		},
		{
			name: "空串",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		if got := DecodeHeader(c.in); got != c.want {
			t.Errorf("%s: DecodeHeader(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}
}

// 解不出来时返回原文。空主题会让用户以为邮件损坏了，乱码至少还能认出是哪封。
func TestDecodeHeaderFallsBackToRaw(t *testing.T) {
	in := "=?NOSUCHCHARSET?B?QUJD?="
	if got := DecodeHeader(in); got != in {
		t.Errorf("DecodeHeader(%q) = %q，期望原样返回", in, got)
	}
}

// 附件名完全由发件人控制。不清理的话 ../../ 这种名字会在下载或打包 ZIP 时
// 写到目录外——这是附件功能最直接的一个攻击面。
func TestSanitizeAttachmentFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32\config`, "config"},
		{"/absolute/path/file.txt", "file.txt"},
		{"C:\\Users\\victim\\file.txt", "file.txt"},
		{"a/b/c/../../../evil.sh", "evil.sh"},
		{"..", "attachment"},
		{".", "attachment"},
		{"", "attachment"},
		{"   ", "attachment"},
		{"file\x00name.txt", "file_name.txt"},
		{"file\nname.txt", "file_name.txt"},
		{"a<b>c:d\"e|f?g*h.txt", "a_b_c_d_e_f_g_h.txt"},
		{"中文附件.docx", "中文附件.docx"},
		{"  spaced.pdf  ", "spaced.pdf"},
		// 尾部的点在 Windows 上会被吞掉，顺带清理。
		{"trailing...", "trailing"},
	}
	for _, c := range cases {
		if got := SanitizeAttachmentFilename(c.in); got != c.want {
			t.Errorf("SanitizeAttachmentFilename(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// 超长名字要按 rune 截断，不能切出半个 UTF-8 字符。
func TestSanitizeAttachmentFilenameTruncates(t *testing.T) {
	got := SanitizeAttachmentFilename(strings.Repeat("中", 300) + ".pdf")
	if len(got) > 200 {
		t.Errorf("长度 %d 字节，期望不超过 200", len(got))
	}
	if !strings.ContainsRune(got, '中') {
		t.Errorf("截断后内容异常：%q", got)
	}
	for _, r := range got {
		if r == '\uFFFD' {
			t.Fatalf("截断切出了半个字符：%q", got)
		}
	}
}

var multipartMessage = "From: =?UTF-8?B?5byg5LiJ?= <zhang@example.com>\r\n" +
	"To: a@example.com, B <b@example.com>\r\n" +
	"Cc: c@example.com\r\n" +
	"Subject: =?UTF-8?B?5rWL6K+V?=\r\n" +
	"Date: Thu, 20 Aug 2026 10:00:00 +0800\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"OUTER\"\r\n" +
	"\r\n" +
	"--OUTER\r\n" +
	"Content-Type: multipart/alternative; boundary=\"INNER\"\r\n" +
	"\r\n" +
	"--INNER\r\n" +
	"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
	"\r\n" +
	"纯文本正文\r\n" +
	"--INNER\r\n" +
	"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"<p>HTML=E6=AD=A3=E6=96=87</p>\r\n" +
	"--INNER--\r\n" +
	"--OUTER\r\n" +
	"Content-Type: application/pdf; name=\"report.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"../../evil.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	base64.StdEncoding.EncodeToString([]byte("PDF-CONTENT")) + "\r\n" +
	"--OUTER--\r\n"

func TestParseMessage(t *testing.T) {
	msg, err := ParseMessage([]byte(multipartMessage))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "测试" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	if msg.From != "张三 <zhang@example.com>" {
		t.Errorf("From = %q", msg.From)
	}
	if msg.To != "a@example.com, B <b@example.com>" {
		t.Errorf("To = %q", msg.To)
	}
	if msg.Cc != "c@example.com" {
		t.Errorf("Cc = %q", msg.Cc)
	}
	if msg.Date.IsZero() {
		t.Error("Date 没有解析出来")
	}
	if msg.TextBody != "纯文本正文" {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if msg.HTMLBody != "<p>HTML正文</p>" {
		t.Errorf("HTMLBody = %q", msg.HTMLBody)
	}

	// 优先 HTML。
	body, bodyType := msg.Body()
	if bodyType != "html" || body != "<p>HTML正文</p>" {
		t.Errorf("Body() = (%q, %q)", body, bodyType)
	}

	if len(msg.Attachments) != 1 {
		t.Fatalf("附件 %d 个，期望 1 个", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Name != "evil.pdf" {
		t.Errorf("附件名 = %q，期望已清理成 evil.pdf", att.Name)
	}
	if string(att.Content) != "PDF-CONTENT" {
		t.Errorf("附件内容 = %q", att.Content)
	}
	if att.ContentType != "application/pdf" {
		t.Errorf("附件类型 = %q", att.ContentType)
	}
	if att.IsInline {
		t.Error("attachment 不该被标成内嵌")
	}
}

// 只有纯文本时回退到 text。
func TestParseMessagePlainTextOnly(t *testing.T) {
	raw := "Subject: hi\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n只有纯文本"
	msg, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, bodyType := msg.Body()
	if bodyType != "text" || body != "只有纯文本" {
		t.Errorf("Body() = (%q, %q)", body, bodyType)
	}
}

// GBK 编码的正文同样要解出来，不能只处理头部。
func TestParseMessageGBKBody(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().String("中文正文")
	if err != nil {
		t.Fatal(err)
	}
	raw := "Subject: hi\r\nContent-Type: text/plain; charset=GBK\r\n\r\n" + gbk
	msg, parseErr := ParseMessage([]byte(raw))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if msg.TextBody != "中文正文" {
		t.Errorf("TextBody = %q，期望 中文正文", msg.TextBody)
	}
}

// 内嵌图片（Content-Disposition: inline + Content-Id）要标成 inline，
// 前端据此决定是显示在附件列表还是内联到正文。
func TestParseMessageInlineAttachment(t *testing.T) {
	raw := "Subject: hi\r\n" +
		"Content-Type: multipart/related; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/html\r\n\r\n<img src=\"cid:img1\">\r\n" +
		"--B\r\n" +
		"Content-Type: image/png; name=\"logo.png\"\r\n" +
		"Content-Disposition: inline; filename=\"logo.png\"\r\n" +
		"Content-Id: <img1>\r\n\r\n" +
		"binary\r\n" +
		"--B--\r\n"
	msg, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("附件 %d 个", len(msg.Attachments))
	}
	if !msg.Attachments[0].IsInline {
		t.Error("应当标为内嵌")
	}
	if msg.Attachments[0].ContentID != "img1" {
		t.Errorf("ContentID = %q，期望去掉尖括号后的 img1", msg.Attachments[0].ContentID)
	}
}

// 截断的 multipart 很常见。已经解出来的部分要照样返回，不能整封信作废。
func TestParseMessageTruncatedMultipart(t *testing.T) {
	raw := "Subject: hi\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\n前半段正文\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\n后面被截断了"
	msg, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("截断不该让整封信解析失败：%v", err)
	}
	if !strings.Contains(msg.TextBody, "前半段正文") {
		t.Errorf("已解出的部分丢了：%q", msg.TextBody)
	}
}

// 恶意构造的深层嵌套不能把解析器拖死。
func TestParseMessageBoundedNesting(t *testing.T) {
	var b strings.Builder
	b.WriteString("Subject: bomb\r\nContent-Type: multipart/mixed; boundary=\"B0\"\r\n\r\n")
	depth := 40
	for i := range depth {
		b.WriteString("--B" + string(rune('0'+i%10)) + "\r\n")
		b.WriteString("Content-Type: multipart/mixed; boundary=\"B" + string(rune('0'+(i+1)%10)) + "\"\r\n\r\n")
	}
	if _, err := ParseMessage([]byte(b.String())); err != nil {
		t.Fatalf("不该报错，应当在深度上限处停下：%v", err)
	}
}

// 尾部损坏的 base64 要保留能解出来的前缀：一封少了结尾的信仍然可读，
// 整个丢掉就是一片空白。
func TestLenientBase64(t *testing.T) {
	const plain = "hello world, this is the body"
	valid := base64.StdEncoding.EncodeToString([]byte(plain))

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"完整", valid, plain},
		{"带折行", valid[:8] + "\r\n" + valid[8:], plain},
		{"缺填充", strings.TrimRight(valid, "="), plain},
		// 垃圾在有效数据之后，前面的内容应当全部保住。
		{"尾部追加垃圾", valid + "!!!", plain},
		{"全是垃圾", "!!!!", ""},
		{"空", "", ""},
	}
	for _, c := range cases {
		got, err := io.ReadAll(newLenientBase64Reader(strings.NewReader(c.in)))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s: 解出 %q，期望 %q", c.name, got, c.want)
		}
	}

	// 中途损坏：截断点之前的内容要保住，之后的丢掉。
	corrupted := valid[:20] + "!" + valid[21:]
	got, err := io.ReadAll(newLenientBase64Reader(strings.NewReader(corrupted)))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("中途损坏时整封信都空了，应当保留前缀")
	}
	if !strings.HasPrefix(plain, string(got)) {
		t.Errorf("解出的 %q 不是原文的前缀", got)
	}
	if string(got) == plain {
		t.Error("损坏点之后的内容不该还能解出来")
	}
}

func TestHTMLToPreview(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"去标签", "<p>hello <b>world</b></p>", 0, "hello world"},
		{"折叠空白", "<p>a</p>\n\n<p>   b   </p>", 0, "a b"},
		{"实体转义", "<p>a &amp; b &lt;c&gt;</p>", 0, "a & b <c>"},
		{"script 内容不进摘要", "<script>alert(1)</script>正文", 0, "正文"},
		{"style 内容不进摘要", "<style>p{color:red}</style>正文", 0, "正文"},
		{"按 rune 截断", "<p>一二三四五六</p>", 3, "一二三…"},
		{"纯文本原样", "no tags here", 0, "no tags here"},
	}
	for _, c := range cases {
		if got := HTMLToPreview(c.in, c.limit); got != c.want {
			t.Errorf("%s: HTMLToPreview(%q) = %q，期望 %q", c.name, c.in, got, c.want)
		}
	}
}

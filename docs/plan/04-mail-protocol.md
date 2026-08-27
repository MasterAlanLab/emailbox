# 04 · 邮件协议层设计（`pkg/mailer`）

本文档是整个方案里**必须严格照搬 outlookEmail 实战经验**的部分。
这些常量、端点、scope、回退顺序、文件夹名，都是靠大量真实账号试出来的，
凭常识重新设计几乎一定会踩坑。来源标注为 `outlookEmail/outlook_web/segments/*.py`。

## 1. 领域接口

```go
// pkg/mailer/mailer.go
package mailer

type Folder string
const (
    FolderInbox    Folder = "inbox"
    FolderJunk     Folder = "junkemail"
    FolderDeleted  Folder = "deleteditems"
    FolderAll      Folder = "all"      // 聚合 inbox + junk，按时间倒序
)

type Credential struct {
    Email        string
    Provider     string   // outlook/gmail/qq/...
    AccountType  string   // outlook | imap
    ClientID     string
    RefreshToken string   // 已解密
    Password     string   // 已解密
    IMAPHost     string
    IMAPPort     int
    IMAPPassword string   // 已解密
    AuthChannel  string   // 上次成功的通道，优先尝试
    Proxy        ProxyConfig
}

type Message struct {
    ID             string    // provider message id
    IDMode         string    // uid | sequence | ""（Graph）
    Folder         Folder
    Subject        string
    From           string
    To             string
    Cc             string
    ReceivedAt     time.Time
    IsRead         bool
    HasAttachments bool
    BodyPreview    string
}

type Detail struct {
    Message
    Body        string
    BodyType    string   // text | html
    Attachments []AttachmentMeta
}

type ListOptions struct{ Folder Folder; Skip, Top int }

type Client interface {
    List(ctx context.Context, cred Credential, opt ListOptions) ([]Message, error)
    Detail(ctx context.Context, cred Credential, folder Folder, id, idMode string) (*Detail, error)
    Attachment(ctx context.Context, cred Credential, folder Folder, msgID, attID string) (*Attachment, error)
    MarkRead(ctx context.Context, cred Credential, items []MessageRef) (BatchResult, error)
    Delete(ctx context.Context, cred Credential, items []MessageRef) (BatchResult, error)
}

// 通道级实现：graph.Client / imapx.Client(new) / imapx.Client(old) / imapx.Client(password)
// chain.Client 组合它们并实现回退，对外只暴露一个 Client。
```

**所有方法都接受 `context.Context`**，超时由 service 层用
`context.WithTimeout(ctx, cfg.Mail.OverallTimeout)` 统一控制。
这是相对 Python 版的实质改进——Python 只在每个 HTTP/IMAP 调用上设超时，
整条回退链（3 个通道 × 各自超时）可能累计到 2 分钟以上。

## 2. 常量（照抄，勿改）

来源：`01_bootstrap.py:166-175`

```go
const (
    TokenURLLive  = "https://login.live.com/oauth20_token.srf"                          // 旧版 IMAP
    TokenURLGraph = "https://login.microsoftonline.com/common/oauth2/v2.0/token"        // Graph
    TokenURLIMAP  = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"     // 新版 IMAP

    IMAPServerOld = "outlook.office365.com"
    IMAPServerNew = "outlook.live.com"
    IMAPPort      = 993

    ScopeIMAP  = "https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
    ScopeGraphDefault = "https://graph.microsoft.com/.default"

    DefaultOAuthClientID    = "9e5f94bc-e8a4-4e73-b8be-63364c29d753"  // 公共客户端 ID
    DefaultOAuthRedirectURI = "http://localhost:8080"
)

var OAuthGraphScopes = []string{
    "https://graph.microsoft.com/Mail.Read",
    "https://graph.microsoft.com/Mail.ReadWrite",
    "https://graph.microsoft.com/User.Read",
}
// 手动授权链接的 scope（含 offline_access）；
// 注意：Graph 与 IMAP 的 scope 不能放进同一次授权，否则微软报 AADSTS70011。
var OAuthAuthorizeScopes = append([]string{"offline_access"}, OAuthGraphScopes...)
```

> **`login.live.com` 端点特殊性**：请求体里**不带 scope**，只有
> `client_id / grant_type=refresh_token / refresh_token`。这是旧版 IMAP 通道专用。
> 来源 `outlook_mail_reader.py:113-119`。

### 2.1 Provider 表

来源：`01_bootstrap.py:570-644`

```go
var Providers = map[string]Provider{
    "outlook": {Label: "Outlook",     IMAPHost: IMAPServerNew,        Port: 993, Type: "outlook"},
    "gmail":   {Label: "Gmail",       IMAPHost: "imap.gmail.com",     Port: 993, Type: "imap"},
    "qq":      {Label: "QQ邮箱",      IMAPHost: "imap.qq.com",        Port: 993, Type: "imap"},
    "163":     {Label: "163邮箱",     IMAPHost: "imap.163.com",       Port: 993, Type: "imap"},
    "126":     {Label: "126邮箱",     IMAPHost: "imap.126.com",       Port: 993, Type: "imap"},
    "yahoo":   {Label: "Yahoo",       IMAPHost: "imap.mail.yahoo.com",Port: 993, Type: "imap"},
    "aliyun":  {Label: "阿里邮箱",    IMAPHost: "imap.aliyun.com",    Port: 993, Type: "imap"},
    "2925":    {Label: "2925邮箱",    IMAPHost: "imap.2925.com",      Port: 993, Type: "imap"},
    "custom":  {Label: "自定义 IMAP", IMAPHost: "",                   Port: 993, Type: "imap"},
}

var DomainProvider = map[string]string{
    "outlook.com": "outlook", "hotmail.com": "outlook", "live.com": "outlook", "live.cn": "outlook",
    "gmail.com": "gmail", "googlemail.com": "gmail",
    "qq.com": "qq", "foxmail.com": "qq",
    "163.com": "163", "126.com": "126",
    "yahoo.com": "yahoo", "yahoo.co.jp": "yahoo", "yahoo.co.uk": "yahoo",
    "aliyun.com": "aliyun", "alimail.com": "aliyun",
    "2925.com": "2925",
}
```

### 2.2 IMAP 文件夹候选表

来源：`01_bootstrap.py:646-688`。**这些 `&V4NXPpCuTvY-` 是 IMAP modified UTF-7 编码的中文名。**

> **⚠️ 本表原先有一处错**：QQ/163/126 与 2925 的 `FolderDeleted` 候选原本写的是
> `&XfJT0ZABkK5O9g-` 与 `&XfJT0ZAB-`，解码出来是**「已发送邮件」/「已发送」**，不是已删除。
> 照抄的话，用户点「已删除」会 SELECT 到发件箱——看到的是自己发出去的信，
> 在那里执行删除就是真的删发件箱。正确值是 `&XfJSIJZkkK5O9g-` / `&XfJSIJZk-`，下表已修正。
>
> 编码名靠肉眼 review 看不出来，`imapx` 里有 `TestEncodedFolderNamesDecodeToWhatTheyClaim`
> 逐条解码校验这张表，新增服务商时它会拦住同类错误。

```go
var ProviderFolders = map[string]map[Folder][]string{
    "gmail": {
        FolderInbox:   {"INBOX", "Inbox"},
        FolderJunk:    {"[Gmail]/Spam", "[Gmail]/垃圾邮件"},
        FolderDeleted: {"[Gmail]/Trash", "[Gmail]/已删除邮件"},
    },
    // &V4NXPpCuTvY- = 垃圾邮件；&XfJSIJZkkK5O9g- = 已删除邮件
    "qq":  {FolderInbox: {"INBOX","Inbox"}, FolderJunk: {"Junk","&V4NXPpCuTvY-"}, FolderDeleted: {"Deleted Messages","&XfJSIJZkkK5O9g-"}},
    "163": {同 qq},
    "126": {同 qq},
    "yahoo": {FolderInbox: {"INBOX","Inbox"}, FolderJunk: {"Bulk Mail","Spam"}, FolderDeleted: {"Trash"}},
    // &V4NXPnux- = 垃圾箱；&XfJSIJZk- = 已删除
    "2925": {FolderInbox: {"INBOX","Inbox"},
             FolderJunk:    {"&V4NXPnux-","Junk","Junk Email","Spam","SPAM"},
             FolderDeleted: {"&XfJSIJZk-","Trash","Deleted","Deleted Items","Deleted Messages"}},
    "_default": {
        FolderInbox:   {"INBOX", "Inbox"},
        FolderJunk:    {"Junk", "Junk Email", "Spam", "SPAM", "Bulk Mail"},
        FolderDeleted: {"Trash", "Deleted", "Deleted Items", "Deleted Messages"},
    },
}

// LIST 结果打分匹配用的别名集合（小写比较）
var FolderMatchAliases = map[Folder][]string{
    FolderInbox:   {"inbox", "收件箱"},
    FolderJunk:    {"junk", "junk email", "spam", "bulk mail", "垃圾邮件", "垃圾箱"},
    FolderDeleted: {"trash", "deleted", "deleted items", "deleted messages", "已删除邮件", "垃圾箱"},
}
```

## 3. 通道回退链（`pkg/mailer/chain.go`）

对应 `03_mail_helpers.py` 各 `*_result` 函数与调用方的组合逻辑。

```
账号 account_type == "imap"（Gmail/QQ/163/...）
    → 单通道：IMAP + 密码鉴权（LOGIN/PLAIN）

账号 account_type == "outlook"（OAuth）
    → 通道顺序（若 auth_channel 有值则把它提到最前）：
        1. graph      Graph API             （token: TokenURLGraph，scope 降级链）
        2. imap_new   outlook.live.com      （token: TokenURLIMAP，  scope=ScopeIMAP）
        3. imap_old   outlook.office365.com （token: TokenURLLive，  无 scope）
    → 任一通道成功：把该通道写回 accounts.auth_channel，本次结果返回
    → 全部失败：返回最后一个通道的结构化错误 + 各通道失败摘要
```

**不该继续回退的情形**（否则白白浪费 3 倍时间并放大风控）：

- 响应体含 `User account is found to be in service abuse mode` → **账号被封**，
  直接返回 `ErrKindBanned`，不再试下一通道，并把 `accounts.status` 置为 `banned`。
  来源：`outlook_mail_reader.py:124`、`03_mail_helpers.py` 多处。
- **IMAP 通道上**的 `invalid_grant` / 授权码错误 → `ErrKindAuthFailed`，不再回退。
  那是账号自身的配置问题（授权码不对、未开 IMAP 服务），换另一条 IMAP 通道结果一样。
- `context.Canceled` / `DeadlineExceeded` → 直接返回。

值得回退的：网络错误、代理错误、单通道 5xx、Graph 权限不足（`AADSTS*`）、IMAP SELECT 失败，
**以及 Graph 通道上的 `auth_failed`**。

> **Graph 的 `auth_failed` 要继续回退**（`mailer.RetriableFrom`，2026-08-27）。
> 这里一度写着「三条通道用的是同一个 refresh_token，一条认证失败则三条都会失败」。
> token 确实是同一个，但**申请的 scope 不是**：Graph 要 `https://graph.microsoft.com/...`，
> 两条 IMAP 要 `https://outlook.office.com/IMAP.AccessAsUser.All`（`provider.go` 的 `ScopeIMAP`）。
> 微软完全可能拒掉其中一套而照常签发另一套——管理员回收了 Graph 权限、应用注册变更、
> 或该账号本就只被授予过 IMAP scope，都会打到这个组合上。按旧逻辑这类账号在 Graph 一步就被判死，
> 尽管两条 IMAP 通道拿同一个 token 拉信完全正常。实测确认：mailprobe 逐通道试时
> Graph 报 `auth_failed`，而两条 IMAP 都成功拉到了邮件。
>
> 放宽**只针对 Graph**：`banned` 仍然立即停手（那才是越试越严的一类），IMAP 侧的
> `auth_failed` 也仍然立即停手。`mailer.Retriable` 保留原语义，回退链改用 `RetriableFrom`。

## 4. Graph 通道（`pkg/mailer/graph/`）

### 4.1 Token 获取与 scope 降级

来源：`03_mail_helpers.py:355-444`。**这是最容易被低估的一段逻辑。**

```
候选 scope 依次为：
  1. "configured" = Mail.Read + Mail.ReadWrite + User.Read + offline_access
  2. "read"       = 去掉 Mail.ReadWrite（有些账号只授权了只读）
  3. "default"    = "https://graph.microsoft.com/.default"
（去重后按序尝试；每个候选自身还要走代理 failover）

判定「是否降级重试下一个 scope」：
  HTTP 状态码 ∈ {400, 401, 403}  且  响应体命中下列任一：
    error ∈ {invalid_scope, consent_required, interaction_required}
    正文含 aadsts90023 / aadsts70000 / aadsts70011
    正文含 "no applicable permissions" / "requested are unauthorized or expired"
             / "consent" / "invalid scope"
  否则：直接返回该响应（不再降级）
```

Go 实现要点：把响应体读一次存 `[]byte`，既用于 JSON 解析也用于小写子串匹配
（Python 用 `json.dumps(details).lower()`，Go 直接 `bytes.ToLower` 即可）。

### 4.2 API 调用

来源：`03_mail_helpers.py:567-985`

| 操作 | 请求 |
|---|---|
| 列表 | `GET /v1.0/me/mailFolders/{folder}/messages`<br>`$top`, `$skip`, `$orderby=receivedDateTime desc`<br>`$select=id,subject,from,toRecipients,receivedDateTime,isRead,hasAttachments,bodyPreview`<br>Header `Prefer: outlook.body-content-type='text'` |
| 详情 | `GET /v1.0/me/messages/{id}`，`$select=...,ccRecipients,body,bodyPreview`<br>Header `Prefer: outlook.body-content-type='html'` |
| 原始 MIME | `GET /v1.0/me/messages/{id}/$value` |
| 附件列表 | `GET /v1.0/me/messages/{id}/attachments` |
| 附件下载 | `GET /v1.0/me/messages/{id}/attachments/{attID}/$value` |
| 标已读 | `PATCH /v1.0/me/messages/{id}`，body `{"isRead": true}` |
| 删除 | `DELETE /v1.0/me/messages/{id}` |

folder 映射：`inbox→inbox`、`junkemail→junkemail`、`deleteditems→deleteditems`、
`trash→deleteditems`（well-known folder names）。

批量标已读/删除在 Graph 上没有原生批量接口。Python 是逐条循环。
**Go 侧应改为 `$batch`**（`POST /v1.0/$batch`，每批最多 20 个请求）——
批量场景下这是 20 倍的往返差距，是本方案对 outlookEmail 的一处明确优化。
若 `$batch` 遇到限制则回退逐条。

### 4.3 限流

Graph 会返回 `429` 并带 `Retry-After`。Python 版没有处理这个。
Go 侧实现：遇到 429/503 且带 `Retry-After` 时，最多重试 2 次，按其值 sleep（上限 30s）。

## 5. IMAP 通道（`pkg/mailer/imapx/`）

### 5.1 鉴权

**OAuth 账号（XOAUTH2）** —— 来源 `outlook_mail_reader.py:157,263`：

```
auth string = "user=" + email + "\x01auth=Bearer " + accessToken + "\x01\x01"
```
go-imap v2 + go-sasl：`sasl.NewXoauth2Client(email, token)`，或自行实现 `sasl.Client`
返回上述字节串（注意是 base64 编码后作为 `AUTHENTICATE XOAUTH2` 的初始响应）。

**密码账号**：`LOGIN` / `AUTHENTICATE PLAIN`。
错误信息要归一化（`normalize_imap_auth_error`）：QQ/163 等返回的是自家提示语，
要翻译成「授权码错误或未开启 IMAP 服务」这类可操作文案。

### 5.2 IMAP ID 扩展（网易系必需）

来源：`03_mail_helpers.py:1630-1673`。163/126 强制要求客户端发送 `ID` 命令，
否则报 `Unsafe Login`。发送内容：

```
ID ("name" "emailbox" "version" "<app version>" "vendor" "emailbox" "support-email" "")
```

go-imap v2 有 `imapclient.Client.ID()`；若使用 v1 需要自己发原始命令。

### 5.3 文件夹解析

来源：`03_mail_helpers.py:1674-1789`

```
1. 取 ProviderFolders[provider][folder] 候选列表
2. 逐个尝试 SELECT，名称要试多种引号形式（裸名 / "名" / 已编码名）
3. 全失败 → LIST "" "*" 拿到全部邮箱名
   → 每个名字做 UTF-7 解码 + 小写归一
   → 用 FolderMatchAliases[folder] 打分排序
   → 按分数从高到低再试 SELECT
4. 仍失败 → 返回 ErrKindFolderUnavailable，附带诊断信息（试过哪些名字、各自的错误）
```

IMAP modified UTF-7 编解码需要自己实现（Go 标准库没有）：
基于 RFC 2152 的变体，`&` 是转义符，`,` 替代 base64 的 `/`，`&-` 表示字面量 `&`。
`03_mail_helpers.py` 的 `decode_imap_utf7` 可直接作为参照与测试用例来源。

### 5.4 UID vs 序列号

来源：README「Outlook/Hotmail OAuth 的 IMAP 回退链路默认按 UID 读取详情和附件」。

- 列表返回的 `Message.IDMode` 必须如实标注（`uid` 或 `sequence`）
- 详情/附件请求必须带回同样的 `id_mode`，服务端据此决定用 `UID FETCH` 还是 `FETCH`
- **混用会取到错误的邮件**——这是 outlookEmail 修过的真实 bug，不要重蹈

Go 侧统一优先 UID（`UID FETCH` / `UID SEARCH` / `UID STORE`），
仅在服务器不支持时回落到序列号，并在 `IDMode` 中标明。

### 5.5 分页

IMAP 没有原生分页。Python 的做法（`get_emails_imap_with_server:1122-1131`）：
`SEARCH ALL` 拿全部 ID → 按 `total - skip - top : total - skip` 切片 → 反转（新→旧）。

对邮件数很多的邮箱这会拉回巨大的 ID 列表。Go 侧优化：
先 `SELECT` 取 `EXISTS` 计数，直接按序号区间 `FETCH`，避免 `SEARCH ALL`；
只有在需要过滤（按发件人/主题）时才 `SEARCH`。

抓取时用 `FETCH (UID INTERNALDATE ENVELOPE BODYSTRUCTURE BODY.PEEK[HEADER])`
而不是 Python 的 `RFC822`（拉全文）——**列表页不需要正文全文**，
这是批量场景下最大的带宽优化点。`BODY.PEEK` 还能避免意外把邮件标记为已读。

### 5.6 MIME 解析

来源：`03_mail_helpers.py:1456-1558`

- 头部解码：`=?UTF-8?B?...?=` 等编码字（Go: `mime.WordDecoder`，
  需设置 `CharsetReader` 以支持 GBK/GB2312/Big5——**中文邮箱常见**，
  Python 的 `decode_header` 默认能处理，Go 必须显式接 `golang.org/x/text/encoding`）
- 正文提取：优先 `text/html`，回退 `text/plain`；HTML 转纯文本用于 `body_preview`
- 附件枚举：`Content-Disposition: attachment` 或有 `filename` 参数的 part
- 附件文件名净化（防路径穿越）：对应 `sanitize_attachment_filename`

## 6. 代理（`pkg/mailer/proxy.go`）

### 6.1 解析优先级

来源：`02_groups_accounts.py:1173-1299`

```
账号自身 proxy_url 非空  → 用账号的（含它自己的 2 个 fallback）
否则                     → 用分组的；分组为空则向上逐级找父分组
仍为空                   → 直连
```

### 6.2 `{mail}` 模板

来源：`02_groups_accounts.py:1144-1163`，README「代理与 Resin `{mail}` 模板」

```go
// 把 URL 中字面量 {mail} 替换为：邮箱 @ 前的 local-part，去掉所有非字母数字字符，转小写
// user.name+tag@outlook.com  →  usernametag
// 配置原样入库，编辑回显不展开，只在出站时替换
func expandMailPlaceholder(rawURL, email string) string
```

示例：`socks5h://outlook.{mail}:TOKEN@127.0.0.1:2260`

### 6.3 failover 候选

来源：`03_mail_helpers.py:100-141`

```
候选序列 = [primary, fallback1, fallback2, DIRECT]（去空、去重）
逐个尝试；仅当错误"值得换代理重试"时才继续（连接被拒/超时/SOCKS 握手失败/代理认证失败）
认证失败、4xx 业务错误不换代理
成功时若 index > 0，记 WARN 日志（说明主代理有问题）
```

### 6.4 Go 实现（相对 Python 的关键改进）

```go
// HTTP：每个请求构造独立 http.Client（或缓存 per-proxy 的 Transport）
transport := &http.Transport{Proxy: http.ProxyURL(u)}                 // http/https 代理
// 或
dialer, _ := proxy.SOCKS5("tcp", host, auth, proxy.Direct)            // socks5 / socks5h
transport := &http.Transport{DialContext: dialer.(proxy.ContextDialer).DialContext}

// IMAP：直接用 dialer 建 TCP，再 tls.Client 包装，最后交给 imapclient
conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
c := imapclient.New(tlsConn, opts)
```

> **这是本方案相对 outlookEmail 最重要的性能改进。**
> Python 靠 `socks.set_default_proxy()` 全局改 `socket.socket`，并用进程锁保护，
> 导致**所有 IMAP 连接被强制串行**。Go 的每连接 Dialer 没有全局状态，
> 可以在 worker pool 里真正并发拉信，且不同账号可以用不同代理同时出站。

### 6.5 日志脱敏

代理 URL 含口令。日志输出前必须打码（对应 `format_proxy_for_log`）：
`socks5h://outlook.abc:****@127.0.0.1:2260`。

## 7. 账号导入解析（`pkg/mailer/importer.go` 或 service 层）

来源：`02_groups_accounts.py:3649-3800`

```
Outlook OAuth（4 段）：邮箱----密码----A----B
    A/B 中形如 UUID 的那个是 client_id，另一个是 refresh_token
    （is_probable_client_id：8-4-4-4-12 的十六进制形态）
    两者都像/都不像 → 按 account_format 参数指定的顺序，默认 client_id 在前

标准 IMAP（2 段）：邮箱----授权码
    provider 由域名推断，imap_host/port 查 Providers 表

自定义 IMAP（4 段）：邮箱----密码----imap_host----imap_port
    第 3 段不像 UUID 且像域名 → 判定为自定义 IMAP
```

导入接口的返回必须是**逐行结果**，而不是全部成功/全部失败：

```json
{ "total": 5000, "created": 4812, "updated": 100, "skipped": 60, "failed": 28,
  "errors": [{ "line": 137, "email": "x@y.com", "reason": "邮箱已存在于其他分组" }] }
```

`errors` 数组要截断（如最多 200 条）并给出总数，否则响应体会失控。

## 8. 错误分类（`pkg/mailer/errors.go`）

```go
type ErrKind string
const (
    ErrKindAuthFailed        ErrKind = "auth_failed"        // refresh_token 失效、密码错误
    ErrKindBanned            ErrKind = "banned"             // service abuse mode
    ErrKindConsentRequired   ErrKind = "consent_required"   // scope/权限不足，需重新授权
    ErrKindProxyFailed       ErrKind = "proxy_failed"       // 全部代理候选失败
    ErrKindNetwork           ErrKind = "network"            // 超时、连接重置
    ErrKindRateLimited       ErrKind = "rate_limited"       // 429
    ErrKindFolderUnavailable ErrKind = "folder_unavailable" // SELECT 失败
    ErrKindProviderError     ErrKind = "provider_error"     // 其它 4xx/5xx
)

type Error struct {
    Kind        ErrKind
    Channel     string   // graph | imap_new | imap_old | imap
    Message     string   // 面向用户的中文文案
    StatusCode  int
    Detail      string   // 已脱敏，供排障
    Attempts    []Attempt // 各通道/各代理的尝试记录
}
```

`ErrKind` 直接落到 `job_items.error_kind` 与 `mail_refresh_logs.error_kind`，
前端据此做筛选与聚合统计（「本次刷新失败 312 个，其中被封 47 个、令牌失效 210 个、代理故障 55 个」）——
这是 outlookEmail 只有自由文本 error_message 时做不到的。

## 9. 测试策略

| 层次 | 方法 |
|---|---|
| 纯函数 | provider 推断、`{mail}` 展开、导入解析、UTF-7 编解码、邮箱候选生成、scope 降级判定 → 普通单测，用 outlookEmail 的常量表做 golden case |
| Graph 客户端 | `httptest.Server` 模拟 token 端点与 Graph 端点，覆盖：scope 三级降级、abuse mode、429 Retry-After、$batch |
| IMAP 客户端 | 起一个最小 IMAP 服务器桩（或用 `go-imap` 自带的 server 包）覆盖：XOAUTH2、ID、文件夹解析回退、UID/序列号、分页边界 |
| 回退链 | 用可编程的假通道组合，断言：banned 不回退、network 回退、成功后写回 auth_channel |
| 代理 | 起本地 SOCKS5 桩，验证 failover 顺序与"是否值得重试"判定 |
| 端到端 | 手工，用真实账号跑一份 checklist（阶段 2 验收） |

协议层不依赖数据库，全部可以纯单测覆盖 —— 这正是把 `mailer` 独立成包的收益。

package imapx

import (
	"sort"
	"strings"

	"emailbox/pkg/mailer"
)

// ProviderFolders 是各服务商的邮件夹候选名，按尝试顺序排列。
//
// 那些 &...- 是 IMAP modified UTF-7 编码的中文名，**不要凭肉眼抄**：
// 04 文档原表里 QQ/163/126 与 2925 的「已删除」候选写的是 &XfJT0ZABkK5O9g- 与
// &XfJT0ZAB-，实际解码出来是「已发送邮件」和「已发送」。照抄的话，用户点「垃圾箱/已删除」
// 会 SELECT 到发件箱——看到的是自己发出去的信，而在那里执行删除就是真的删发件箱。
// 已改为正确的 &XfJSIJZkkK5O9g- / &XfJSIJZk-，并加了 TestEncodedFolderNamesDecodeToWhatTheyClaim
// 逐条校验这张表里每个编码名的实际含义。
var ProviderFolders = map[string]map[mailer.Folder][]string{
	"gmail": {
		mailer.FolderInbox:   {"INBOX", "Inbox"},
		mailer.FolderJunk:    {"[Gmail]/Spam", "[Gmail]/垃圾邮件"},
		mailer.FolderDeleted: {"[Gmail]/Trash", "[Gmail]/已删除邮件"},
	},
	"qq":  chineseProviderFolders(),
	"163": chineseProviderFolders(),
	"126": chineseProviderFolders(),
	"yahoo": {
		mailer.FolderInbox:   {"INBOX", "Inbox"},
		mailer.FolderJunk:    {"Bulk Mail", "Spam"},
		mailer.FolderDeleted: {"Trash"},
	},
	"2925": {
		mailer.FolderInbox: {"INBOX", "Inbox"},
		// &V4NXPnux- = 垃圾箱
		mailer.FolderJunk: {"&V4NXPnux-", "Junk", "Junk Email", "Spam", "SPAM"},
		// &XfJSIJZk- = 已删除
		mailer.FolderDeleted: {"&XfJSIJZk-", "Trash", "Deleted", "Deleted Items", "Deleted Messages"},
	},
	defaultProvider: {
		mailer.FolderInbox:   {"INBOX", "Inbox"},
		mailer.FolderJunk:    {"Junk", "Junk Email", "Spam", "SPAM", "Bulk Mail"},
		mailer.FolderDeleted: {"Trash", "Deleted", "Deleted Items", "Deleted Messages"},
	},
}

const defaultProvider = "_default"

func chineseProviderFolders() map[mailer.Folder][]string {
	return map[mailer.Folder][]string{
		mailer.FolderInbox: {"INBOX", "Inbox"},
		// &V4NXPpCuTvY- = 垃圾邮件
		mailer.FolderJunk: {"Junk", "&V4NXPpCuTvY-"},
		// &XfJSIJZkkK5O9g- = 已删除邮件
		mailer.FolderDeleted: {"Deleted Messages", "&XfJSIJZkkK5O9g-"},
	}
}

// FolderMatchAliases 是 LIST 结果打分匹配用的别名，全部小写。
var FolderMatchAliases = map[mailer.Folder][]string{
	mailer.FolderInbox:   {"inbox", "收件箱"},
	mailer.FolderJunk:    {"junk", "junk email", "spam", "bulk mail", "垃圾邮件", "垃圾箱"},
	mailer.FolderDeleted: {"trash", "deleted", "deleted items", "deleted messages", "已删除邮件", "已删除", "垃圾箱"},
}

// neverMatch 是任何情况下都不能被当成收件箱/垃圾箱/已删除的邮箱名。
//
// 这是上面那个「已发送被当成已删除」问题的兜底：候选表可以再写错一次，
// 但只要发件箱、草稿箱这些名字在这里，打分匹配就不会把它们选出来。
// 误选的后果不是「看不到邮件」，而是「看到并可能删掉本不该动的邮件」。
var neverMatch = []string{
	"sent", "sent items", "sent messages", "sent mail",
	"drafts", "draft", "outbox", "archive", "junk-clean",
	"已发送", "已发送邮件", "草稿箱", "草稿", "发件箱", "归档",
}

// FolderCandidates 返回该服务商该邮件夹要依次尝试 SELECT 的名字。
// 服务商专属候选排在前面，通用候选补在后面（去重保序）。
func FolderCandidates(provider string, folder mailer.Folder) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	add := func(names []string) {
		for _, n := range names {
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	if byFolder, ok := ProviderFolders[strings.ToLower(strings.TrimSpace(provider))]; ok {
		add(byFolder[folder])
	}
	add(ProviderFolders[defaultProvider][folder])
	return out
}

// FolderMatch 是一个 LIST 结果的匹配结果。
type FolderMatch struct {
	// Name 是原始的（可能是 UTF-7 编码的）邮箱名，SELECT 时要用它。
	Name string
	// Display 是解码后的名字，用于日志与诊断。
	Display string
	Score   int
}

// MatchFolders 从 LIST 回来的邮箱名里挑出最像目标邮件夹的那些，按分数从高到低。
//
// 候选表全试失败后才走到这里：服务商可能改了名字，或者用户把邮箱语言设成了别的。
// 分数为 0 的不返回——宁可报「找不到该邮件夹」，也不要随便 SELECT 一个凑数的，
// 那会让用户在「垃圾箱」里看到完全无关的邮件。
func MatchFolders(folder mailer.Folder, mailboxes []string) []FolderMatch {
	aliases := FolderMatchAliases[folder]
	if len(aliases) == 0 {
		return nil
	}
	out := make([]FolderMatch, 0, len(mailboxes))
	for _, raw := range mailboxes {
		display := raw
		if decoded, err := DecodeUTF7(raw); err == nil {
			display = decoded
		}
		if score := scoreMailbox(display, folder, aliases); score > 0 {
			out = append(out, FolderMatch{Name: raw, Display: display, Score: score})
		}
	}
	// 同分时按名字排序，保证结果稳定：否则同一个账号每次拉信可能命中不同的邮件夹。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func scoreMailbox(display string, folder mailer.Folder, aliases []string) int {
	norm := strings.ToLower(strings.TrimSpace(display))
	leaf := norm
	// 层级分隔符各家不同（Gmail 用 /，有的服务器用 .），两种都剥一次。
	if i := strings.LastIndexAny(leaf, "/."); i >= 0 && i+1 < len(leaf) {
		leaf = leaf[i+1:]
	}

	if matchesAny(norm, leaf, neverMatch) {
		return 0
	}
	if matchesAny(norm, leaf, aliases) {
		return 100
	}
	// 精确命中了别的邮件夹的别名，就不该再算作本邮件夹的模糊匹配。
	for other, otherAliases := range FolderMatchAliases {
		if other != folder && matchesAny(norm, leaf, otherAliases) {
			return 0
		}
	}
	for _, alias := range aliases {
		if strings.Contains(leaf, alias) {
			return 60
		}
		if strings.Contains(norm, alias) {
			return 40
		}
	}
	return 0
}

func matchesAny(norm, leaf string, candidates []string) bool {
	for _, c := range candidates {
		if norm == c || leaf == c {
			return true
		}
	}
	return false
}

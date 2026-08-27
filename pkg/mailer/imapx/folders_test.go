package imapx

import (
	"strings"
	"testing"

	"emailbox/pkg/mailer"
)

// 这张表里每个 &...- 的实际含义都必须与它所在的位置一致。
//
// 这条用例的由来：04 文档把「已发送邮件」的编码名（&XfJT0ZABkK5O9g-）写在了
// FolderDeleted 下。照抄的话，用户打开「已删除」会 SELECT 到发件箱——
// 看到的是自己发出去的信，在那里执行删除就是真的删发件箱。
// 编码名靠肉眼 review 是看不出来的，只能让机器解码后逐条核对。
func TestEncodedFolderNamesDecodeToWhatTheyClaim(t *testing.T) {
	// 每个邮件夹允许出现的中文名。不在表里的中文名一律视为写错了。
	allowed := map[mailer.Folder][]string{
		mailer.FolderInbox:   {"收件箱"},
		mailer.FolderJunk:    {"垃圾邮件", "垃圾箱"},
		mailer.FolderDeleted: {"已删除邮件", "已删除", "废件箱"},
	}
	for provider, byFolder := range ProviderFolders {
		for folder, names := range byFolder {
			for _, name := range names {
				if !strings.Contains(name, "&") {
					continue
				}
				decoded, err := DecodeUTF7(name)
				if err != nil {
					t.Errorf("%s/%s: %q 不是合法的 modified UTF-7：%v", provider, folder, name, err)
					continue
				}
				// 可能带 [Gmail]/ 这样的前缀，只看最后一段。
				leaf := decoded
				if i := strings.LastIndex(leaf, "/"); i >= 0 {
					leaf = leaf[i+1:]
				}
				ok := false
				for _, want := range allowed[folder] {
					if leaf == want {
						ok = true
						break
					}
				}
				if !ok {
					t.Errorf("%s 的 %s 候选里有 %q，实际是 %q —— 不属于该邮件夹",
						provider, folder, name, decoded)
				}
			}
		}
	}
}

// 发件箱/草稿箱绝不能被当成垃圾箱或已删除。误选的后果不是「看不到邮件」，
// 而是「看到并可能删掉本不该动的邮件」。
func TestSentAndDraftsAreNeverMatched(t *testing.T) {
	mailboxes := []string{
		"Sent", "Sent Items", "Sent Messages", "Drafts", "Outbox", "Archive",
		"&XfJT0ZABkK5O9g-", // 已发送邮件
		"&XfJT0ZAB-",       // 已发送
	}
	for _, folder := range []mailer.Folder{mailer.FolderInbox, mailer.FolderJunk, mailer.FolderDeleted} {
		if got := MatchFolders(folder, mailboxes); len(got) != 0 {
			t.Errorf("%s 匹配到了不该匹配的邮箱：%+v", folder, got)
		}
	}
}

func TestMatchFolders(t *testing.T) {
	cases := []struct {
		name      string
		folder    mailer.Folder
		mailboxes []string
		wantFirst string
		wantNone  bool
	}{
		{
			name:      "精确命中英文名",
			folder:    mailer.FolderJunk,
			mailboxes: []string{"INBOX", "Junk", "Sent"},
			wantFirst: "Junk",
		},
		{
			name:      "精确命中 UTF-7 编码的中文名",
			folder:    mailer.FolderJunk,
			mailboxes: []string{"INBOX", "&V4NXPpCuTvY-"},
			wantFirst: "&V4NXPpCuTvY-",
		},
		{
			name:      "带层级前缀时取最后一段比较",
			folder:    mailer.FolderJunk,
			mailboxes: []string{"[Gmail]/Spam", "INBOX"},
			wantFirst: "[Gmail]/Spam",
		},
		{
			name:      "点号也是层级分隔符",
			folder:    mailer.FolderDeleted,
			mailboxes: []string{"INBOX.Trash"},
			wantFirst: "INBOX.Trash",
		},
		{
			name:      "精确匹配优先于包含匹配",
			folder:    mailer.FolderJunk,
			mailboxes: []string{"My Junk Folder", "Junk"},
			wantFirst: "Junk",
		},
		{
			name:      "一个都不像时宁可返回空",
			folder:    mailer.FolderJunk,
			mailboxes: []string{"INBOX", "Work", "Newsletters"},
			wantNone:  true,
		},
		{
			// Trash 是已删除的别名，不能因为「垃圾」两个字就被当成垃圾箱。
			name:      "别的邮件夹的精确别名不算模糊匹配",
			folder:    mailer.FolderJunk,
			mailboxes: []string{"Trash"},
			wantNone:  true,
		},
	}
	for _, c := range cases {
		got := MatchFolders(c.folder, c.mailboxes)
		if c.wantNone {
			if len(got) != 0 {
				t.Errorf("%s: 期望无匹配，实际 %+v", c.name, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s: 没有匹配到任何邮箱", c.name)
			continue
		}
		if got[0].Name != c.wantFirst {
			t.Errorf("%s: 首选 = %q，期望 %q（完整结果 %+v）", c.name, got[0].Name, c.wantFirst, got)
		}
	}
}

// 解码后的名字要带出来，排障时才知道 &V4NXPpCuTvY- 到底是什么。
func TestMatchFoldersReportsDisplayName(t *testing.T) {
	got := MatchFolders(mailer.FolderJunk, []string{"&V4NXPpCuTvY-"})
	if len(got) != 1 {
		t.Fatalf("匹配结果 = %+v", got)
	}
	if got[0].Display != "垃圾邮件" {
		t.Errorf("Display = %q，期望 垃圾邮件", got[0].Display)
	}
	if got[0].Name != "&V4NXPpCuTvY-" {
		t.Errorf("Name 必须保留原始编码名供 SELECT 使用，实际 %q", got[0].Name)
	}
}

func TestFolderCandidates(t *testing.T) {
	// 服务商专属候选在前，通用候选补在后面。
	got := FolderCandidates("qq", mailer.FolderJunk)
	if len(got) == 0 || got[0] != "Junk" {
		t.Fatalf("qq 的垃圾箱候选 = %v", got)
	}
	if !contains(got, "&V4NXPpCuTvY-") {
		t.Errorf("缺少 QQ 的中文垃圾箱名：%v", got)
	}
	if !contains(got, "Spam") {
		t.Errorf("通用候选没有补进来：%v", got)
	}

	// 去重：Junk 同时在专属表与通用表里，只能出现一次。
	seen := map[string]int{}
	for _, n := range got {
		seen[n]++
		if seen[n] > 1 {
			t.Errorf("候选 %q 重复出现：%v", n, got)
		}
	}

	// 未知服务商回落到通用候选。
	unknown := FolderCandidates("nonesuch", mailer.FolderDeleted)
	if !contains(unknown, "Trash") {
		t.Errorf("未知服务商应当拿到通用候选，实际 %v", unknown)
	}

	if got := FolderCandidates("qq", mailer.FolderAll); len(got) != 0 {
		t.Errorf("folder=all 没有对应的 IMAP 邮件夹，应当返回空，实际 %v", got)
	}
}

// 三个邮件夹每个服务商都要有候选，漏一个就是「这家的垃圾箱永远打不开」。
func TestEveryProviderCoversEveryFolder(t *testing.T) {
	for provider, byFolder := range ProviderFolders {
		for _, folder := range []mailer.Folder{
			mailer.FolderInbox, mailer.FolderJunk, mailer.FolderDeleted,
		} {
			if len(byFolder[folder]) == 0 {
				t.Errorf("%s 缺少 %s 的候选", provider, folder)
			}
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

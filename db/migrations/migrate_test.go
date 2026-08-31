package migrations

import (
	"context"
	"database/sql"
	"path"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // 注册 SQLite 驱动供本测试使用。
)

// 迁移文件里的 SQL 错误只有真正执行才会暴露。这里把全部迁移跑一遍，
// 并断言注册流程依赖的默认套餐已就位——缺了它，新用户注册会直接失败。
func TestUpAppliesAllMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/migrate.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := Up(ctx, db, "sqlite"); err != nil {
		t.Fatalf("首次迁移失败: %v", err)
	}
	// 迁移必须幂等：重复执行不应报错，也不该重复插入种子数据。
	if err := Up(ctx, db, "sqlite"); err != nil {
		t.Fatalf("重复迁移失败: %v", err)
	}

	var code string
	if err := db.QueryRowContext(ctx, `SELECT code FROM plans WHERE is_default = 1`).Scan(&code); err != nil {
		t.Fatalf("查询默认套餐失败: %v", err)
	}
	if code != "free" {
		t.Fatalf("默认套餐应为 free，实际 %q", code)
	}
}

// 全新库上跑通的迁移，在**已有数据的库**上未必跑得通——000008 要整表重建
// users（SQLite 去不掉写在列上的 UNIQUE），而 tenants / tenant_members /
// sessions / audit_logs 四张表都有外键指着它。空库上重建当然不会出问题，
// 有数据时才会撞上外键，正是这类迁移最容易翻车的地方。
//
// 这里模拟真实升级：先跑到某个中间版本、灌进数据，再跑完剩下的迁移，
// 断言行不丢、列不串、外键仍然成立。
func TestUpOnPopulatedDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/populated.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 先建到 000007 为止，模拟一个已经在跑的旧库。
	all, err := loadMigrations("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	const upTo = 7
	for _, m := range all {
		if m.version > upTo {
			break
		}
		if err := apply(ctx, db, "sqlite", m); err != nil {
			t.Fatalf("预置迁移 %s 失败: %v", m.name, err)
		}
	}

	for _, s := range []string{
		`INSERT INTO users(id,username,email,password_hash,avatar_url,status,platform_role)
		 VALUES ('u1','alan','alan@x.com','HASH1','av1','active','admin'),
		        ('u2','bob','bob@x.com','HASH2','','disabled','user')`,
		`INSERT INTO tenants(id,name,slug,created_by,kind) VALUES ('t1','T','t-1','u1','team')`,
		`INSERT INTO tenant_members(id,tenant_id,user_id,role) VALUES ('m1','t1','u1','owner')`,
		`INSERT INTO sessions(id,user_id,token_hash,active_tenant_id,expires_at)
		 VALUES ('s1','u1','TOKHASH','t1',datetime('now','+1 day'))`,
		`INSERT INTO audit_logs(id,tenant_id,actor_user_id,actor_email,actor_kind,action,resource_type)
		 VALUES ('a1','t1','u1','alan@x.com','admin','account.create','account')`,
	} {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("灌数据失败: %v\n%s", err, s)
		}
	}

	if err := Up(ctx, db, "sqlite"); err != nil {
		t.Fatalf("在已有数据的库上升级失败: %v", err)
	}

	// 行不能丢，列不能串——整表重建最典型的两种事故。
	// avatar_url 灌进去时还在（000014 之前），迁移跑完之后它已经被删掉，
	// 所以这里只核对留下来的列。
	var username, email, hash, status, role string
	err = db.QueryRowContext(ctx,
		`SELECT username, email, password_hash, status, platform_role FROM users WHERE id = 'u1'`,
	).Scan(&username, &email, &hash, &status, &role)
	if err != nil {
		t.Fatalf("查 u1 失败: %v", err)
	}
	got := []string{username, email, hash, status, role}
	want := []string{"alan", "alan@x.com", "HASH1", "active", "admin"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("users 第 %d 列 = %q，期望 %q（整行 %v）", i, got[i], want[i], got)
		}
	}

	// 外键指向的是 users 这个名字，重建后必须仍然成立。
	for _, table := range []string{"tenants", "tenant_members", "sessions", "audit_logs"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("%s 的行在 users 重建后丢了：%d != 1", table, n)
		}
	}
	var violations int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Errorf("重建后存在 %d 条外键违规", violations)
	}

	// 000008 的目的：多个用户可以都不填邮箱。旧结构下 UNIQUE 只允许一个空串。
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users(id,username,password_hash) VALUES ('u3','carol','H3'),('u4','dave','H4')`); err != nil {
		t.Fatalf("两个无邮箱用户应能共存: %v", err)
	}
	// 但填了邮箱的仍然必须唯一，否则等于把校验整个丢了。
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users(id,username,email,password_hash) VALUES ('u5','eve','alan@x.com','H5')`); err == nil {
		t.Error("重复的非空邮箱应被部分唯一索引拒绝")
	}

	// 000009 的改名。
	var actorName string
	if err := db.QueryRowContext(ctx, `SELECT actor_name FROM audit_logs WHERE id = 'a1'`).Scan(&actorName); err != nil {
		t.Fatalf("actor_email 应已改名为 actor_name: %v", err)
	}
}

// 000010 + 000011 把分组树压平成一层。压平最容易丢的不是行，而是**继承**：
// 子分组原来没配代理时走父分组的，父子关系一没，它就会悄悄变成直连。
// 这里造一棵三层的树，断言压平之后账号还在原分组上、代理也还是原来那一个。
func TestFlattenGroupsKeepsAccountsAndInheritedProxy(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/groups.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	all, err := loadMigrations("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	// 建到 000009 为止：此时 mail_groups 还是带 parent_id / level 的树。
	const upTo = 9
	for _, m := range all {
		if m.version > upTo {
			break
		}
		if err := apply(ctx, db, "sqlite", m); err != nil {
			t.Fatalf("预置迁移 %s 失败: %v", m.name, err)
		}
	}

	for _, s := range []string{
		`INSERT INTO users(id,username,email,password_hash) VALUES ('u1','alan','alan@x.com','H1')`,
		`INSERT INTO tenants(id,name,slug,created_by,kind) VALUES ('t1','T','t-1','u1','team')`,
		`INSERT INTO mail_groups(id,tenant_id,parent_id,name,level,is_system,proxy_url,fallback_proxy_url_1)
		 VALUES ('g1','t1',NULL,'default',1,1,'',''),
		        ('g2','t1',NULL,'root',1,0,'ENC_ROOT','ENC_ROOT_FB'),
		        ('g3','t1','g2','child',2,0,'',''),
		        ('g4','t1','g3','grandchild',3,0,'',''),
		        ('g5','t1','g2','child-own-proxy',2,0,'ENC_OWN','')`,
		`INSERT INTO mail_accounts(id,tenant_id,group_id,email,email_normalized)
		 VALUES ('a1','t1','g4','a@x.com','a@x.com')`,
	} {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("灌数据失败: %v\n%s", err, s)
		}
	}

	if err := Up(ctx, db, "sqlite"); err != nil {
		t.Fatalf("压平分组失败: %v", err)
	}

	// 层级列没了，分组一个不少。
	if _, err := db.ExecContext(ctx, `SELECT parent_id FROM mail_groups`); err == nil {
		t.Error("parent_id 应已随分组压平消失")
	}
	// 000016 去掉了两个备用代理位，分组只留一个 proxy_url。
	if _, err := db.ExecContext(ctx, `SELECT fallback_proxy_url_1 FROM mail_groups`); err == nil {
		t.Error("fallback_proxy_url_1 应已随 000016 删除")
	}
	var groups int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mail_groups WHERE tenant_id = 't1'`).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if groups != 5 {
		t.Errorf("分组数应不变（5），实际 %d", groups)
	}

	// 账号仍挂在原来的分组上，没有被父分组的删除或重建带走。
	var groupID string
	if err := db.QueryRowContext(ctx, `SELECT group_id FROM mail_accounts WHERE id = 'a1'`).Scan(&groupID); err != nil {
		t.Fatalf("账号丢了: %v", err)
	}
	if groupID != "g4" {
		t.Errorf("账号应仍在 g4，实际 %s", groupID)
	}

	// 继承来的代理已经落到子分组自己身上。
	for _, tc := range []struct{ id, url string }{
		{"g3", "ENC_ROOT"},
		{"g4", "ENC_ROOT"},
		{"g5", "ENC_OWN"}, // 自己配了就不继承
		{"g1", ""},        // 本来就没有可继承的
	} {
		var url string
		if err := db.QueryRowContext(ctx,
			`SELECT proxy_url FROM mail_groups WHERE id = ?`, tc.id).Scan(&url); err != nil {
			t.Fatal(err)
		}
		if url != tc.url {
			t.Errorf("%s 的代理 = %q，期望 %q", tc.id, url, tc.url)
		}
	}

	var violations int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Errorf("压平后存在 %d 条外键违规", violations)
	}
}

// 用了 migrate:no-transaction 的文件自己管事务，版本号是在文件跑完之后才记的：
// 两者之间崩溃，下次启动会把该文件**再跑一遍**。所以它必须可重复执行，
// 否则一次意外断电就会让服务再也起不来。
func TestNoTxMigrationsAreRepeatable(t *testing.T) {
	all, err := loadMigrations("sqlite")
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, m := range all {
		content, err := files.ReadFile(path.Join("sqlite", m.name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(content)), NoTxDirective) {
			continue
		}
		checked++

		// 关键是**在它当时会遇到的 schema 上**重跑，而不是在跑完全部迁移的库上：
		// 迁移是有序的，000008 只可能在版本 7 的库上执行。拿一个已经跑到最新的库
		// 去重跑它，测的是一个永远不会发生的场景——000014 删掉 avatar_url 之后，
		// 000008 的整表重建当然会因为找不到那一列而失败。
		t.Run(m.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/repeat.db?_pragma=foreign_keys(1)")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			if _, err := db.ExecContext(ctx,
				`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
				t.Fatal(err)
			}
			for _, prev := range all {
				if prev.version > m.version {
					break
				}
				if err := apply(ctx, db, "sqlite", prev); err != nil {
					t.Fatalf("预置迁移 %s 失败: %v", prev.name, err)
				}
			}
			if _, err := db.ExecContext(ctx,
				`INSERT INTO users(id,username,password_hash) VALUES ('u1','alan','H1')`); err != nil {
				t.Fatal(err)
			}

			// 抹掉版本记录再跑一次，正是「文件跑完、版本号还没记上」时崩溃的那个窗口。
			if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ?`, m.version); err != nil {
				t.Fatal(err)
			}
			if err := Up(ctx, db, "sqlite"); err != nil {
				t.Fatalf("重复执行失败（它声明了 %s，必须可重跑）: %v", NoTxDirective, err)
			}

			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = 'u1'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("重跑后 users 行数变了: %d", n)
			}
			var violations int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil {
				t.Fatal(err)
			}
			if violations != 0 {
				t.Errorf("重跑后存在 %d 条外键违规", violations)
			}
		})
	}
	if checked == 0 {
		t.Skip("当前没有使用 no-transaction 的迁移")
	}
}

// 旧实现把文件名和版本号写死为 000001，新增迁移不会被执行。
// 这里直接校验加载逻辑：目录里有几个 .up.sql 就该识别出几个，且按版本升序。
func TestLoadMigrationsSortedByVersion(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		got, err := loadMigrations(dialect)
		if err != nil {
			t.Fatalf("%s: %v", dialect, err)
		}
		if len(got) == 0 {
			t.Fatalf("%s: 未找到任何迁移文件", dialect)
		}
		for i := 1; i < len(got); i++ {
			if got[i].version <= got[i-1].version {
				t.Fatalf("%s: 迁移未按版本升序: %v", dialect, got)
			}
		}
		if got[0].version != 1 {
			t.Fatalf("%s: 首个迁移版本应为 1，实际 %d", dialect, got[0].version)
		}
	}
}

func TestParseVersion(t *testing.T) {
	valid := map[string]int{
		"000001_init.up.sql":         1,
		"000002_add_projects.up.sql": 2,
		"000010_x.up.sql":            10,
	}
	for name, want := range valid {
		got, err := parseVersion(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Fatalf("%s: 期望版本 %d，实际 %d", name, want, got)
		}
	}
	for _, name := range []string{"init.up.sql", "abc_init.up.sql", "000000_init.up.sql"} {
		if _, err := parseVersion(name); err == nil {
			t.Fatalf("%s: 期望报错，实际通过", name)
		}
	}
}

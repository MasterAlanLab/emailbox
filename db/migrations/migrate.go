// Package migrations 按版本号顺序执行内嵌的 SQL 迁移。
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed sqlite/*.up.sql postgres/*.up.sql
var files embed.FS

// migration 是一个待执行的迁移文件。
type migration struct {
	version int
	name    string
}

// Up 执行所有尚未应用的迁移。新增迁移只需在对应方言目录下放入
// 形如 000002_add_xxx.up.sql 的文件，无需改动这里的代码。
func Up(ctx context.Context, db *sql.DB, driver string) error {
	dialect := driver
	if dialect == "postgresql" {
		dialect = "postgres"
	}
	if dialect != "sqlite" && dialect != "postgres" {
		return fmt.Errorf("不支持的数据库驱动: %s", driver)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}
	pending, err := loadMigrations(dialect)
	if err != nil {
		return err
	}
	for _, m := range pending {
		if applied[m.version] {
			continue
		}
		if err := apply(ctx, db, dialect, m); err != nil {
			return err
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// loadMigrations 读取某个方言目录下的迁移文件并按版本号升序排列。
func loadMigrations(dialect string) ([]migration, error) {
	entries, err := fs.ReadDir(files, dialect)
	if err != nil {
		return nil, err
	}
	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version, err := parseVersion(name)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration{version: version, name: name})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version == migrations[i-1].version {
			return nil, fmt.Errorf("迁移版本号重复: %d (%s, %s)", migrations[i].version, migrations[i-1].name, migrations[i].name)
		}
	}
	return migrations, nil
}

// parseVersion 从 000002_add_xxx.up.sql 这样的文件名中取出版本号 2。
func parseVersion(name string) (int, error) {
	prefix, _, found := strings.Cut(name, "_")
	if !found {
		return 0, fmt.Errorf("迁移文件名缺少版本号前缀: %s", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("迁移文件名版本号非法: %s", name)
	}
	if version <= 0 {
		return 0, fmt.Errorf("迁移版本号必须为正数: %s", name)
	}
	return version, nil
}

// NoTxDirective 让一个迁移文件跳过执行器的事务包装，自己管事务。
//
// 只为一种情况存在：SQLite 改不了建表时写在列上的约束（比如把
// `email TEXT NOT NULL UNIQUE` 的 UNIQUE 去掉），只能整表重建，
// 而重建要先 DROP 掉旧表——一旦有别的表用外键指着它就会被拒绝。
// 官方给的做法是先 `PRAGMA foreign_keys=OFF`，但**该 PRAGMA 在事务内是空操作**，
// 于是包在事务里的迁移根本没法做表重建。`defer_foreign_keys` 也不行：
// 它只是把检查推迟到 COMMIT，而 DROP 记下的违规计数不会因为把表建回来而清零。
//
// 用了这个指令的文件必须自己写 BEGIN/COMMIT，并且**必须可重复执行**：
// 版本号是在文件执行完之后才记的，两者之间崩溃会导致下次启动重跑一遍。
const NoTxDirective = "-- migrate:no-transaction"

// apply 执行一个迁移文件并记录其版本号。默认整个文件跑在一个事务里；
// 文件首行是 NoTxDirective 时，事务由文件自己负责。
func apply(ctx context.Context, db *sql.DB, dialect string, m migration) error {
	content, err := files.ReadFile(path.Join(dialect, m.name))
	if err != nil {
		return err
	}
	if strings.HasPrefix(strings.TrimSpace(string(content)), NoTxDirective) {
		return applyWithoutTx(ctx, db, m, string(content))
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return errors.Join(cause, rollbackErr)
		}
		return cause
	}
	for _, statement := range strings.Split(string(content), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return rollback(fmt.Errorf("执行迁移 %s 失败: %w", m.name, err))
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (`+strconv.Itoa(m.version)+`)`); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

// applyWithoutTx 逐条执行迁移文件，不加事务包装。见 NoTxDirective 的说明。
//
// 语句直接打在 db 上而不是某个连出来的连接上：`database/sql` 是连接池，
// 同一个 *sql.DB 上的两条语句可能落在不同连接上，而 PRAGMA 与 BEGIN 都是
// **连接级**的。所以这里必须先 Conn() 拿一条独占连接，把整个文件跑在它上面，
// 否则 `PRAGMA foreign_keys=OFF` 可能设在 A 连接、重建却发生在 B 连接。
func applyWithoutTx(ctx context.Context, db *sql.DB, m migration, content string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	for _, statement := range strings.Split(content, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("执行迁移 %s 失败: %w", m.name, err)
		}
	}
	// 版本号只能在文件跑完之后记：文件自己 COMMIT 过了，没法把这条 INSERT
	// 塞进它的事务里。这中间崩溃会让下次启动重跑一遍该文件——
	// 这正是 NoTxDirective 要求文件可重复执行的原因。
	_, err = conn.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES (`+strconv.Itoa(m.version)+`)`)
	return err
}

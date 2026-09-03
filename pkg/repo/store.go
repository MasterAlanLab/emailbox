package repo

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
)

type Store struct {
	db       *sql.DB
	driver   string
	sqlite   *sqlitedb.Queries
	postgres *postgresdb.Queries
	// inTx 标记这个 Store 是不是某个事务的句柄，供 WithTx 判断嵌套。
	inTx bool
}

func NewStore(db *sql.DB, driver string) *Store {
	s := &Store{db: db, driver: driver}
	if driver == "sqlite" {
		s.sqlite = sqlitedb.New(db)
	} else {
		s.postgres = postgresdb.New(db)
	}
	return s
}

// WithTx 在一个事务里执行 fn。
//
// 已经在事务里时直接复用当前句柄，不再开一层。这不是优化而是必须：
// 有些 repo 方法自己就带事务（DeleteTenant 要把软删和清理会话绑在一起），
// service 把它组合进更大的事务时就会嵌套。而 SQLite 的生产配置只有一个连接
// （见 pkg/database），内层 BeginTx 会去等一把外层自己攥着、永远不会释放的锁——
// 表现不是报错，是整个进程静默挂死。PostgreSQL 那边则是拿另一条连接，
// 两个事务互相看不见对方的未提交数据，症状更隐蔽。
//
// 复用的语义是「内层跟着外层一起成败」，这正是调用方要的：
// 删用户时清邮箱、删租户、删会话必须是一个原子操作。
func (s *Store) WithTx(ctx context.Context, fn func(*Store) error) error {
	if s.inTx {
		return fn(s)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	child := &Store{db: s.db, driver: s.driver, inTx: true}
	if s.driver == "sqlite" {
		child.sqlite = s.sqlite.WithTx(tx)
	} else {
		child.postgres = s.postgres.WithTx(tx)
	}
	// fn panic 时必须回滚，否则连接不会归还；
	// SQLite 只有一个连接（MaxOpenConns=1），泄漏一次即全局死锁。
	defer func() {
		if r := recover(); r != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Error("panic 后事务回滚失败", "error", rbErr)
			}
			panic(r)
		}
	}()
	if err := fn(child); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}
func normalize(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate")) {
		return ErrConflict
	}
	return err
}
func nullableString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}
func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}
func timePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	x := v.Time
	return &x
}

// nullableTime 把可空时刻转成驱动参数，并统一归一到 UTC。
//
// UTC 不是可选的。SQLite 把 time.Time 存成字符串，于是时间列上的 `<=` 实际是
// **字符串比较**：一条带 +08:00 写入的记录和一个 UTC 的查询参数比起来，
// 结果只取决于两串文本的字典序。表现是「明明到期了却扫不出来」，
// 而且不会报任何错——TestGroupRefreshScheduleParity 就是先红后绿钉住这一点的。
func nullableTime(v *time.Time) sql.NullTime {
	if v == nil {
		return sql.NullTime{}
	}
	return utcNullTime(*v)
}

// utcNullTime 同 nullableTime，供非指针的必填时刻使用。
func utcNullTime(v time.Time) sql.NullTime {
	return sql.NullTime{Time: v.UTC(), Valid: true}
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func boolToInt32(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

// rowsAffected 把「更新/删除影响 0 行」翻译成 ErrNotFound。
// 由于所有写语句的 WHERE 都带 tenant_id，跨租户操作会自然落到这里，
// 表现为 404 而不是「静默成功」。
func rowsAffected(n int64, err error) error {
	if err != nil {
		return normalize(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullInt64(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func nullInt32(v *int) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*v), Valid: true}
}

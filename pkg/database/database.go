// Package database manages SQL database connections.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"emailbox/configs"

	_ "github.com/jackc/pgx/v5/stdlib" // 注册 PostgreSQL database/sql 驱动。
	_ "modernc.org/sqlite"             // 注册 SQLite database/sql 驱动。
)

var DB *sql.DB

func Init() error {
	if configs.AppConfig == nil {
		return fmt.Errorf("配置未初始化")
	}
	driver, dsn, err := connectionConfig(configs.AppConfig.Database.Driver, configs.AppConfig.GetDatabaseDSN())
	if err != nil {
		return err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(30 * time.Minute)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("数据库连接失败: %w", err)
	}
	DB = db
	return nil
}

func connectionConfig(driver, dsn string) (string, string, error) {
	switch driver {
	case "sqlite":
		return "sqlite", dsn, nil
	case "postgres", "postgresql":
		return "pgx", dsn, nil
	default:
		return "", "", fmt.Errorf("不支持的数据库驱动: %s", driver)
	}
}
func GetDB() *sql.DB { return DB }
func Close() error {
	if DB == nil {
		return nil
	}
	return DB.Close()
}

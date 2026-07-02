// Package gormdb 提供基于 GORM 的 PostgreSQL 连接工厂。
//
// 只暴露 New 一个入口：读取 config.DBConfig，返回配置好连接池参数的 *gorm.DB。
package gormdb

import (
	"fmt"
	"time"

	"my_project/pkg/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// New 打开 PostgreSQL 连接并按配置设置连接池上限。
// 调用方负责在退出时通过 (*gorm.DB).DB() 拿到 *sql.DB 并 Close。
func New(cfg config.DBConfig) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("gormdb: open: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("gormdb: sql handle: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// Package models 存放 GORM 持久化实体，仅在 persistence 层内部使用。
//
// 与 domain 层的聚合根一一对应，但字段结构服务于 DB schema，
// domain <-> model 的映射由各 Repository 实现负责。
package models

import (
	"time"
)

// Task 对应 ai_tasks 表。字段与 1_create_ai_tasks.up.sql 保持一致。
// Messages 存原始 JSONB 字节，编解码由 repo 完成，避免依赖 datatypes 包。
type Task struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	UserID    string    `gorm:"column:user_id;not null;default:''"`
	Model     string    `gorm:"column:model;not null;default:''"`
	ModelOpts []byte    `gorm:"column:model_opts;type:jsonb;not null;default:'{}'"`
	Messages  []byte    `gorm:"column:messages;type:jsonb;not null;default:'{}'"`
	Status    int16     `gorm:"column:status;not null;default:1;index"`
	Content   string    `gorm:"column:content;not null;default:''"`
	ErrorMsg  string    `gorm:"column:error_message;not null;default:''"`
	CreatedAt time.Time `gorm:"column:created_at;not null;index:,sort:desc"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

// TableName 指定物理表名，避免 GORM 复数化规则将 Task 映射为 tasks。
func (Task) TableName() string { return "ai_tasks" }

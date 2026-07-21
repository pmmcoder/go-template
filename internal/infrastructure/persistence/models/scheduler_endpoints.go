// Package models 存放 GORM 持久化实体，仅在 persistence 层内部使用。
//
// 与 domain 层的聚合根一一对应，但字段结构服务于 DB schema，
// domain <-> model 的映射由各 Repository 实现负责。
package models

import (
	"encoding/json"
	"time"
)

// SchedulerEndpointModel 调度端点配置
type SchedulerEndpointModel struct {
	ID            int64           `gorm:"column:id;primaryKey;autoIncrement"`
	Platform      string          `gorm:"column:platform;uniqueIndex:uq_scheduler_endpoint,priority:1"`
	Account       string          `gorm:"column:account;uniqueIndex:uq_scheduler_endpoint,priority:2"`
	Capability    string          `gorm:"column:capability;uniqueIndex:uq_scheduler_endpoint,priority:3;index:idx_scheduler_endpoint_cap"`
	Weight        int             `gorm:"column:weight;default:1"`
	MaxInflight   int             `gorm:"column:max_inflight;default:0"`
	InflightScope int16           `gorm:"column:inflight_scope;type:smallint;default:0"`
	Enabled       bool            `gorm:"column:enabled;default:true"`
	ExtraParams   json.RawMessage `gorm:"column:extra_params;type:jsonb;default:'{}'"`
	CreatedAt     time.Time       `gorm:"column:created_at"`
	UpdatedAt     time.Time       `gorm:"column:updated_at"`
}

func (SchedulerEndpointModel) TableName() string { return "public.scheduler_endpoints" }

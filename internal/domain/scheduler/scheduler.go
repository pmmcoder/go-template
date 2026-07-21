package scheduler

import (
	"encoding/json"
	"time"
)

type SchedulerEndpoint struct {
	id            int64
	platform      string
	account       string
	capability    string
	weight        int
	maxInflight   int
	inflightScope int16
	enabled       bool
	extraParams   json.RawMessage
	createdAt     time.Time
	updatedAt     time.Time
}

func (t *SchedulerEndpoint) ID() int64 { return t.id }

type State struct {
	ID            int64
	Platform      string
	Account       string
	Capability    string
	Weight        int
	MaxInflight   int
	InflightScope int16
	Enabled       bool
	ExtraParams   json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Snapshot 导出当前状态供 repo 写库。
func (t *SchedulerEndpoint) Snapshot() State {
	return State{
		ID:            t.id,
		Platform:      t.platform,
		Account:       t.account,
		Capability:    t.capability,
		Weight:        t.weight,
		MaxInflight:   t.maxInflight,
		InflightScope: t.inflightScope,
		Enabled:       t.enabled,
		ExtraParams:   t.extraParams,
		CreatedAt:     t.createdAt,
		UpdatedAt:     t.updatedAt,
	}
}

// Restore 由数据库行重建聚合，仅 repo 调用。
func Restore(s State) *SchedulerEndpoint {
	return &SchedulerEndpoint{
		id:            s.ID,
		platform:      s.Platform,
		account:       s.Account,
		capability:    s.Capability,
		weight:        s.Weight,
		maxInflight:   s.MaxInflight,
		inflightScope: s.InflightScope,
		enabled:       s.Enabled,
		extraParams:   s.ExtraParams,
		createdAt:     s.CreatedAt,
		updatedAt:     s.UpdatedAt,
	}
}

// SetID 仅供 repo 在 INSERT RETURNING 之后回填，业务层勿调。
func (t *SchedulerEndpoint) SetID(id int64) { t.id = id }

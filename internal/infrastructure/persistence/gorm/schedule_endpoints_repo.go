package gormdb

import (
	"context"
	"my_project/internal/domain/scheduler"
	"my_project/internal/infrastructure/persistence/models"

	"gorm.io/gorm"
)

type SchedulerEndpointRepo struct {
	db *gorm.DB
}

func NewSchedulerEndpointRepo(db *gorm.DB) *SchedulerEndpointRepo {
	return &SchedulerEndpointRepo{db: db}
}

func (dao *SchedulerEndpointRepo) ListEnabled(ctx context.Context) ([]scheduler.State, error) {
	var rows []models.SchedulerEndpointModel
	err := dao.db.WithContext(ctx).Where("enabled = ? AND weight > ?", true, 0).Find(&rows).Error

	if err != nil {
		return nil, err
	}

	result := make([]scheduler.State, len(rows))
	for i, row := range rows {
		result[i] = scheduler.State{
			ID:            row.ID,
			Platform:      row.Platform,
			Account:       row.Account,
			Capability:    row.Capability,
			Weight:        row.Weight,
			MaxInflight:   row.MaxInflight,
			InflightScope: row.InflightScope,
			Enabled:       row.Enabled,
			ExtraParams:   row.ExtraParams,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
		}
	}

	return result, err
}

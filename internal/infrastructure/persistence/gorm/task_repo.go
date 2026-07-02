// Package persistence 提供 domain 层 Repository 接口的 PostgreSQL 实现。
package gormdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"my_project/internal/domain/task"
	"my_project/internal/infrastructure/persistence/models"

	"gorm.io/gorm"
)

// TaskRepo 基于 GORM 实现 task.Repository。
type TaskRepo struct {
	db *gorm.DB
}

// NewTaskRepo 注入 *gorm.DB，返回任务仓储实例。
func NewTaskRepo(db *gorm.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

// Save 新建或更新。id==0 时 INSERT 并回填 id；否则 UPDATE 全字段（不含 created_at）。
func (r *TaskRepo) Save(ctx context.Context, t *task.Task) error {
	s := t.Snapshot()
	msgs, err := json.Marshal(s.Messages)
	if err != nil {
		return fmt.Errorf("task repo: marshal messages: %w", err)
	}

	m := models.Task{
		ID:          s.ID,
		Model:       s.Model,
		Messages:    msgs,
		Status:      int16(s.Status),
		Content:     s.Content,
		TotalTokens: s.TotalTokens,
		ErrorMsg:    s.ErrorMsg,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.UpdatedAt,
	}

	if s.ID == 0 {
		if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
			return fmt.Errorf("task repo: insert: %w", err)
		}
		t.SetID(m.ID)
		return nil
	}

	res := r.db.WithContext(ctx).
		Model(&models.Task{}).
		Where("id = ?", s.ID).
		Updates(map[string]any{
			"model":         m.Model,
			"messages":      m.Messages,
			"status":        m.Status,
			"content":       m.Content,
			"total_tokens":  m.TotalTokens,
			"error_message": m.ErrorMsg,
			"updated_at":    m.UpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("task repo: update id=%d: %w", s.ID, res.Error)
	}
	if res.RowsAffected == 0 {
		return task.ErrNotFound
	}
	return nil
}

// FindByID 按主键加载。不存在返回 task.ErrNotFound。
func (r *TaskRepo) FindByID(ctx context.Context, id int64) (*task.Task, error) {
	var m models.Task
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, task.ErrNotFound
		}
		return nil, fmt.Errorf("task repo: find id=%d: %w", id, err)
	}

	s := task.State{
		ID:          m.ID,
		Model:       m.Model,
		Status:      task.Status(m.Status),
		Content:     m.Content,
		TotalTokens: m.TotalTokens,
		ErrorMsg:    m.ErrorMsg,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if err := json.Unmarshal(m.Messages, &s.Messages); err != nil {
		return nil, fmt.Errorf("task repo: unmarshal messages id=%d: %w", id, err)
	}
	return task.Restore(s), nil
}

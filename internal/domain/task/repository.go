package task

import (
	"context"
	"errors"
)

// ErrNotFound 任务不存在。repo 在 FindByID 找不到记录时返回。
var ErrNotFound = errors.New("task: not found")

// Repository 聚合仓储端口。实现位于 internal/infrastructure/persistence/。
type Repository interface {
	// Save 新建（id==0，INSERT RETURNING + SetID）或更新（id!=0，UPDATE）。
	Save(ctx context.Context, t *Task) error
	// FindByID 按主键加载。不存在返回 ErrNotFound。
	FindByID(ctx context.Context, id int64) (*Task, error)
}

package scheduler

import (
	"context"
)

// Repository 聚合仓储端口。实现位于 internal/infrastructure/persistence/。
type Repository interface {
	ListEnabled(ctx context.Context) ([]State, error)
}

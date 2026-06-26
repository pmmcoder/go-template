package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// TaskQueue 通用任务队列名称（即 river job kind）。
// 所有业务任务复用同一 kind，通过 TaskType 分发到不同处理器，
// 以简化对外 API：调用方只需注册 handler 并按 taskType 提交。
const TaskQueue = "ai"

// TaskJobArgs 通用任务参数。
type TaskJobArgs struct {
	TaskType string            `json:"task_type"`          // 任务类型，用于分发到对应 handler
	Payload  string            `json:"payload"`            // 任务负载（通常为 JSON 字符串）
	Metadata map[string]string `json:"metadata,omitempty"` // 扩展元数据
}

// Kind 实现 river.JobArgs 接口。
func (TaskJobArgs) Kind() string { return TaskQueue }

func (TaskJobArgs) Hooks() []rivertype.Hook {
	// Order is significant. See output below.
	return []rivertype.Hook{
		&JobHook{},
	}
}

// TaskHandler 任务处理函数：处理 payload，返回非 nil 错误将触发重试。
type TaskHandler func(ctx context.Context, payload string) error

// TaskWorker 通用任务处理器，按 TaskType 分发到已注册的 handler。
type TaskWorker struct {
	river.WorkerDefaults[TaskJobArgs]
	logger *slog.Logger

	mu       sync.RWMutex
	handlers map[string]TaskHandler
}

// NewTaskWorker 创建通用 task worker。
func NewTaskWorker(logger *slog.Logger) *TaskWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &TaskWorker{
		logger:   logger,
		handlers: make(map[string]TaskHandler),
	}
}

// RegisterHandler 注册任务类型处理器（并发安全，可在 worker 启动前后调用）。
func (w *TaskWorker) RegisterHandler(taskType string, handler TaskHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[taskType] = handler
}

// Work 处理通用任务。
func (w *TaskWorker) Work(ctx context.Context, job *river.Job[TaskJobArgs]) error {
	args := job.Args
	start := time.Now()

	w.logger.Info("task job started",
		"task_type", args.TaskType,
		"job_id", job.ID,
	)

	w.mu.RLock()
	handler, ok := w.handlers[args.TaskType]
	w.mu.RUnlock()

	if !ok {
		w.logger.Error("unknown task type", "task_type", args.TaskType)
		return fmt.Errorf("unknown task type: %s", args.TaskType)
	}

	if err := handler(ctx, args.Payload); err != nil {
		w.logger.Error("task job failed",
			"task_type", args.TaskType,
			"attempt", job.Attempt,
			"error", err,
		)
		return fmt.Errorf("handle task %s: %w", args.TaskType, err)
	}

	w.logger.Info("task job completed",
		"task_type", args.TaskType,
		"duration", time.Since(start),
	)
	return nil
}

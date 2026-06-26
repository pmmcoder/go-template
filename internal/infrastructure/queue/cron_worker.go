package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

// CronQueue cron 任务使用的 river job kind。
const CronQueue = "cron"

type CronTaskHandler func(ctx context.Context) error

// CronJobArgs cron 周期任务参数。与 TaskJobArgs 字段相同但 Kind 不同，
// 从而由独立的 CronWorker 处理，便于对周期任务单独配置队列/重试等策略。
type CronJobArgs struct {
	TaskType string `json:"task_type"` // cron 任务类型，分发到对应 handler
	Payload  string `json:"payload"`   // 任务负载
}

// Kind 实现 river.JobArgs。
func (CronJobArgs) Kind() string { return CronQueue }

// CronWorker cron 任务处理器（river Worker），按 TaskType 分发到已注册的 handler。
// 与 TaskWorker 分离：即时/一次性任务走 TaskWorker（kind "ai"），
// 周期性 cron 任务走 CronWorker（kind "cron"）。
type CronWorker struct {
	river.WorkerDefaults[CronJobArgs]
	logger *slog.Logger

	mu       sync.RWMutex
	handlers map[string]CronTaskHandler
}

// NewCronWorker 创建 cron worker。
func NewCronWorker(logger *slog.Logger) *CronWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CronWorker{logger: logger, handlers: make(map[string]CronTaskHandler)}
}

// RegisterHandler 注册 cron 任务处理器（并发安全，可在 Start 前后调用）。
func (w *CronWorker) RegisterHandler(taskType string, handler CronTaskHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[taskType] = handler
}

// Work 处理 cron 任务。
func (w *CronWorker) Work(ctx context.Context, job *river.Job[CronJobArgs]) error {
	args := job.Args
	start := time.Now()

	w.logger.Info("cron job started", "task_type", args.TaskType, "job_id", job.ID)

	w.mu.RLock()
	handler, ok := w.handlers[args.TaskType]
	w.mu.RUnlock()

	if !ok {
		w.logger.Error("unknown cron job", "task_type", args.TaskType)
		return fmt.Errorf("unknown cron job: %s", args.TaskType)
	}

	if err := handler(ctx); err != nil {
		w.logger.Error("cron job failed",
			"task_type", args.TaskType,
			"attempt", job.Attempt,
			"error", err,
		)
		return fmt.Errorf("handle cron %s: %w", args.TaskType, err)
	}

	w.logger.Info("cron job completed",
		"task_type", args.TaskType,
		"duration", time.Since(start),
	)
	return nil
}

// --- 周期任务调度 ---

// CronHandle 周期任务句柄，由 RegisterCron 返回，可用于 RemoveCron 移除。
type CronHandle rivertype.PeriodicJobHandle

// CronOption 配置周期任务。
type CronOption func(*cronConfig)

type cronConfig struct {
	opts       river.PeriodicJobOpts
	insertOpts []SubmitOption
}

// CronRunOnStart 启动（或动态注册）时立即插入一次任务，之后按计划运行。
// 适用于长间隔任务的兜底，避免因进程重启丢失运行窗口。
func CronRunOnStart() CronOption {
	return func(c *cronConfig) { c.opts.RunOnStart = true }
}

// CronWithID 设置周期任务唯一标识。同 ID 重复注册会返回错误。
func CronWithID(id string) CronOption {
	return func(c *cronConfig) { c.opts.ID = id }
}

// CronWithInsertOpts 为每次入队任务附加提交选项（如优先级、最大尝试次数）。
func CronWithInsertOpts(opts ...SubmitOption) CronOption {
	return func(c *cronConfig) { c.insertOpts = append(c.insertOpts, opts...) }
}

// CronInterval 构造固定间隔的调度计划。
// River 建议周期任务间隔不小于 1 分钟。
func CronInterval(d time.Duration) cron.Schedule {
	return river.PeriodicInterval(d)
}

// CronExpr 解析标准 5 字段 cron 表达式（分 时 日 月 周），如 "0 3 * * *"。
// 底层使用 robfig/cron，与 river 官方推荐一致。
func CronExpr(expr string) (cron.Schedule, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("queue: parse cron expr %q: %w", expr, err)
	}
	return sched, nil
}

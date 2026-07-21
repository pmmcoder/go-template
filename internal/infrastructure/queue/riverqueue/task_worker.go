package riverqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"my_project/internal/infrastructure/queue/contract"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// TaskQueue 通用任务队列名称（即 river job kind）。
const TaskQueue = "task"

// TaskJobArgs 通用任务参数。
type TaskJobArgs struct {
	TaskID   int64             `json:"task_id"`            //任务id，业务生成
	TaskType string            `json:"task_type"`          // 任务类型，用于分发到对应 handler
	Payload  string            `json:"payload"`            // 任务负载（通常为 JSON 字符串）
	Metadata map[string]string `json:"metadata,omitempty"` // 扩展元数据
}

// Kind 实现 river.JobArgs 接口。
func (TaskJobArgs) Kind() string { return TaskQueue }

func (TaskJobArgs) Hooks() []rivertype.Hook {
	return []rivertype.Hook{defaultHook}
}

// TaskWorker 通用任务处理器，按 TaskType 分发到已注册的 handler。
type TaskWorker struct {
	river.WorkerDefaults[TaskJobArgs]
	logger *slog.Logger

	mu       sync.RWMutex
	handlers map[string]contract.TaskHandler
}

// NewTaskWorker 创建通用 task worker。
func NewTaskWorker(logger *slog.Logger) *TaskWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &TaskWorker{
		logger:   logger,
		handlers: make(map[string]contract.TaskHandler),
	}
}

// RegisterHandler 注册任务类型处理器（并发安全，可在 worker 启动前后调用）。
func (w *TaskWorker) RegisterHandler(taskType string, handler contract.TaskHandler) {
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

	runtimeArgs := contract.RuntimeArgs{
		Attempt:     job.Attempt,
		MaxAttempts: job.MaxAttempts,
	}

	if err := handler(ctx, args.Payload, runtimeArgs); err != nil {
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

// 检查hook的实现是否出现问题
var (
	_ rivertype.HookInsertBegin = &JobHook{}
	_ rivertype.HookWorkBegin   = &JobHook{}
	_ rivertype.HookWorkEnd     = &JobHook{}
)

// 任务状态机：订阅方根据 Status 字段判断 job 当前阶段。
// StatusRetrying 表示当次执行失败但仍有剩余 attempts，会被 river 重新调度。
const (
	StatusPending   = iota + 1 // 占位，零值非法
	StatusAvailable            // 已入库，等待 worker 拉取
	StatusRunning              // worker 已开始执行
	StatusRetrying             // 当次失败，等待重试
	StatusSuccess              // 执行成功
	StatusFailed               // 重试用尽，最终失败
)

// HookNotifier 把 hook 事件推送给订阅方的能力。
// 由 riverQueue 实现，启动时通过 SetHookNotifier 注入到 defaultHook。
// 抽出接口是因为 river 强制 JobArgs.Hooks() 是 value-receiver 静态方法，
// JobHook 只能持有包级单例，无法通过构造函数注入 queue 实例。
type HookNotifier interface {
	NotifyHook(taskID int64, msg contract.HookData)
}

// defaultHook 是 TaskJobArgs.Hooks() 返回的单例，避免每次 Insert/Work 都新建对象。
var defaultHook = &JobHook{}

// SetHookNotifier 注入 hook 推送实现。InitQueue 必须在返回前调用一次。
// 多次调用以最后一次为准（便于测试替换）。
func SetHookNotifier(n HookNotifier) {
	defaultHook.notifier = n
}

type JobHook struct {
	river.HookDefaults
	notifier HookNotifier
}

func (j *JobHook) InsertBegin(ctx context.Context, job *rivertype.JobInsertParams) error {
	taskID, ok := extractTaskID(job.EncodedArgs)
	if !ok {
		return nil
	}
	j.notify(ctx, contract.HookData{
		JobID:  taskID,
		Status: StatusAvailable,
		Data:   fmt.Sprintf("JobHook.InsertBegin task_id:%d kind:%s state:%s", taskID, job.Kind, job.State),
	})
	return nil
}

func (j *JobHook) WorkBegin(ctx context.Context, job *rivertype.JobRow) error {
	taskID, ok := extractTaskID(job.EncodedArgs)
	if !ok {
		return nil
	}
	j.notify(ctx, contract.HookData{
		JobID:  taskID,
		Status: StatusRunning,
		Data:   fmt.Sprintf("JobHook.WorkBegin task_id:%d kind:%s state:%s", taskID, job.Kind, job.State),
	})
	return nil
}

func (j *JobHook) WorkEnd(ctx context.Context, job *rivertype.JobRow, interErr error) error {
	taskID, ok := extractTaskID(job.EncodedArgs)
	if !ok {
		return nil
	}

	hookData := contract.HookData{
		JobID: taskID,
		IsEnd: true,
		RuntimeArgs: contract.RuntimeArgs{
			Attempt:     job.Attempt,
			MaxAttempts: job.MaxAttempts,
		},
	}
	if interErr != nil {
		hookData.ErrMsg = interErr.Error()
		if job.Attempt >= job.MaxAttempts {
			hookData.Status = StatusFailed
		} else {
			hookData.Status = StatusRetrying
		}
	} else {
		hookData.Status = StatusSuccess
	}

	hookData.Data = fmt.Sprintf("JobHook.WorkEnd task_id:%d status:%d attempt:%d/%d", taskID, hookData.Status, job.Attempt, job.MaxAttempts)
	j.notify(ctx, hookData)
	return interErr
}

func (j *JobHook) notify(_ context.Context, message contract.HookData) {
	if j.notifier == nil {
		return
	}
	j.notifier.NotifyHook(message.JobID, message)
}

func extractTaskID(encodedArgs []byte) (int64, bool) {
	var probe struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(encodedArgs, &probe); err != nil {
		return 0, false
	}
	if probe.TaskID == 0 {
		return 0, false
	}
	return probe.TaskID, true
}

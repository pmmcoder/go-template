package riverqueue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"my_project/internal/infrastructure/queue/contract"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

const (
	DefaultQueue               = "task_queue"
	DefaultQueueMaxWorkers     = 10
	DefaultCronQueue           = "task_cron_queue"
	DefaultCronQueueMaxWorkers = 5
)

// WithMaxAttempts 设置最大尝试次数（含首次执行）。
func WithMaxAttempts(n int) contract.SubmitOption {
	return func(o *river.InsertOpts) { o.MaxAttempts = n }
}

// WithQueue 指定目标队列。
func WithQueue(name string) contract.SubmitOption {
	return func(o *river.InsertOpts) { o.Queue = name }
}

// WithSchedule 指定调度时间。传入的过去时间会让 river 立即调度执行，
// 不做拦截 —— 由调用方在业务层判断时间合理性。
func WithSchedule(timer time.Time) contract.SubmitOption {
	return func(o *river.InsertOpts) {
		o.ScheduledAt = timer
	}
}

// WithPriority 设置任务优先级（数值越大越优先）。
func WithPriority(p int) contract.SubmitOption {
	return func(o *river.InsertOpts) { o.Priority = p }
}

// WithTags 附加标签。
func WithTags(tags ...string) contract.SubmitOption {
	return func(o *river.InsertOpts) { o.Tags = tags }
}

// riverQueue 基于 river 的 Queue 实现。
type riverQueue struct {
	client       *Client
	taskWorker   *TaskWorker
	cronWorker   *CronWorker
	hookListener map[int64]chan contract.HookData
	mu           sync.RWMutex
}

// InitQueue 基于给定配置创建 Queue。
func InitQueue(ctx context.Context, cfg Config) (contract.Queue, error) {
	taskWorker := NewTaskWorker(cfg.Logger)
	cronWorker := NewCronWorker(cfg.Logger)

	workers := river.NewWorkers()

	err := river.AddWorkerSafely(workers, taskWorker)
	if err != nil {
		return nil, err
	}
	err = river.AddWorkerSafely(workers, cronWorker)
	if err != nil {
		return nil, err
	}

	cfg.Workers = workers
	client, err := NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	q := &riverQueue{
		client:       client,
		taskWorker:   taskWorker,
		cronWorker:   cronWorker,
		hookListener: make(map[int64]chan contract.HookData),
	}
	// 把 hook 推送能力注入到包级单例 JobHook。
	// 因为 river 强制 JobArgs.Hooks() 返回静态切片，JobHook 只能持有包级
	// 单例，无法通过构造函数注入。这里集中接线，调用方无感知。
	SetHookNotifier(q)
	return q, nil
}

func (q *riverQueue) RegisterHandler(taskType string, handler contract.TaskHandler) error {
	if taskType == "" {
		return fmt.Errorf("queue: task type is empty")
	}
	if handler == nil {
		return fmt.Errorf("queue: handler is nil")
	}
	q.taskWorker.RegisterHandler(taskType, handler)
	return nil
}

func (q *riverQueue) Submit(ctx context.Context, taskType, payload string, taskID int64, opts ...contract.SubmitOption) (int64, error) {
	if taskType == "" {
		return 0, fmt.Errorf("queue: task type is empty")
	}

	insertOpts := &river.InsertOpts{}
	for _, opt := range opts {
		opt(insertOpts)
	}

	if insertOpts.Queue == "" {
		insertOpts.Queue = DefaultQueue
	}

	res, err := q.client.Insert(ctx, TaskJobArgs{
		TaskID:   taskID,
		TaskType: taskType,
		Payload:  payload,
	}, insertOpts)
	if err != nil {
		return 0, fmt.Errorf("queue: submit %s: %w", taskType, err)
	}
	return res.Job.ID, nil
}

func (q *riverQueue) Start(ctx context.Context) error { return q.client.Start(ctx) }
func (q *riverQueue) Stop(ctx context.Context) error  { return q.client.Stop(ctx) }
func (q *riverQueue) Close() error                    { return q.client.Close() }

func (q *riverQueue) SubmitCron(taskType string, schedule cron.Schedule, opts ...contract.SubmitOption) (contract.CronHandle, error) {
	if taskType == "" {
		return 0, fmt.Errorf("queue: task type is empty")
	}
	if schedule == nil {
		return 0, fmt.Errorf("queue: cron schedule is nil")
	}

	constructor := func() (river.JobArgs, *river.InsertOpts) {
		// cron 任务默认走专用队列；可通过 WithQueue 等 opts 覆盖。
		insertOpts := &river.InsertOpts{Queue: DefaultCronQueue}
		for _, opt := range opts {
			opt(insertOpts)
		}
		// 由 CronWorker（kind "cron"）处理，而非 TaskWorker。
		return CronJobArgs{TaskType: taskType}, insertOpts
	}

	periodicJob := river.NewPeriodicJob(schedule, constructor, nil)
	handle, err := q.client.PeriodicJobs().AddSafely(periodicJob)
	if err != nil {
		return 0, fmt.Errorf("queue: register cron %s: %w", taskType, err)
	}
	return contract.CronHandle(handle), nil
}

// RegisterCronHandler 注册 cron 任务处理器到 CronWorker。
// 必须在对应 RegisterCron 触发执行前注册，否则任务会因未知 taskType 而失败重试。
func (q *riverQueue) RegisterCronHandler(taskType string, handler contract.CronTaskHandler) error {
	if taskType == "" {
		return fmt.Errorf("queue: task type is empty")
	}
	if handler == nil {
		return fmt.Errorf("queue: handler is nil")
	}
	q.cronWorker.RegisterHandler(taskType, handler)
	return nil
}

func (q *riverQueue) RemoveCron(handle contract.CronHandle) {
	q.client.PeriodicJobs().Remove(rivertype.PeriodicJobHandle(handle))
}

func (q *riverQueue) SubscribeHook(taskID int64) <-chan contract.HookData {
	q.mu.RLock()
	ch, ok := q.hookListener[taskID]
	q.mu.RUnlock()

	if ok {
		return ch
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if ch, ok := q.hookListener[taskID]; ok {
		return ch
	}

	ch = make(chan contract.HookData, 100)
	q.hookListener[taskID] = ch
	return ch
}

// NotifyHook 把 hook 事件推送给已订阅的 channel。
func (q *riverQueue) NotifyHook(taskID int64, msg contract.HookData) {
	q.mu.RLock()
	ch, ok := q.hookListener[taskID]
	q.mu.RUnlock()
	if !ok {
		return
	}
	defer func() { _ = recover() }()
	select {
	case ch <- msg:
	default:
	}
}

func (q *riverQueue) UnSubscribeHook(taskID int64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ch, ok := q.hookListener[taskID]; ok {
		delete(q.hookListener, taskID)
		close(ch)
	}
}

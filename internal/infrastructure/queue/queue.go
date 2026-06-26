// Package queue 提供基于 river 的任务队列封装。
//
// 对外抽象出 Queue 接口，并通过包级函数暴露常用能力：
//
//	queue.Init(ctx)                          // 初始化全局队列
//	queue.RegisterWorker("email", handler)   // 注册任务处理器
//	queue.Submit(ctx, "email", payload)      // 提交任务
//	queue.Start(ctx)                         // 启动 worker 循环
//	queue.Stop(ctx) / queue.Close()          // 停止 / 释放
//
// 需要更细粒度控制时，可直接使用底层 NewClient 构造 *Client。

//todo
//1.动态控制并发：
//现在的实现：通过切换队列来实现并发控制，
//但是有个问题：已经插入的任务，还是会继续在原队列执行，新的任务会在新的队列执行，同时执行并发失控一段时间

//2.hook消息通知，需要更改：客户端注册通道，服务端如果有通道则插入消息

package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"my_project/pkg/config"
	"my_project/pkg/logger"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

var queueMap = map[string]int{
	"cron": 5,
	"2":    2,
	"5":    5,
}

// Queue 队列对外接口，抽象 river 实现细节。
type Queue interface {
	// RegisterHandler 注册某任务类型的处理器。
	RegisterHandler(taskType string, handler TaskHandler) error
	// Submit 提交任务，返回 river job id。
	Submit(ctx context.Context, taskType, payload string, opts ...SubmitOption) (int64, error)
	// CleanJob 重置处于 running 状态的卡死任务（通常在进程重启后调用）。
	CleanJob(ctx context.Context) error
	// SubmitCron 注册周期任务：按 schedule 周期性提交指定 taskType 的任务，
	// 由 CronWorker 处理（需先通过 RegisterCronHandler 注册对应 handler）。
	// 返回句柄用于后续移除。
	SubmitCron(taskType, payload string, schedule cron.Schedule, opts ...CronOption) (CronHandle, error)
	// RegisterCronHandler 注册 cron 任务处理器到 CronWorker。
	RegisterCronHandler(taskType string, handler CronTaskHandler) error
	// RegisterListenerHandler 注册全局监听器
	RegisterListenerHandler(jobID int64, handler ListenerHandler) error
	// GetHookChan 获取hook的channel
	GetHookChan() chan HookData
	// RemoveCron 按 handle 移除已注册的周期任务。
	RemoveCron(handle CronHandle)
	// Start 启动 worker 循环（阻塞至 ctx 取消或 Stop）。
	Start(ctx context.Context) error
	// Stop 优雅停止 worker 循环。
	Stop(ctx context.Context) error
	// Close 关闭客户端并释放连接池。
	Close() error
	// GetQueue 动态获取队列
	GetQueue() string
}

// SubmitOption 自定义任务提交选项。
type SubmitOption func(*river.InsertOpts)

// WithMaxAttempts 设置最大尝试次数（含首次执行）。
func WithMaxAttempts(n int) SubmitOption {
	return func(o *river.InsertOpts) { o.MaxAttempts = n }
}

// WithQueue 指定目标队列。
func WithQueue(name string) SubmitOption {
	return func(o *river.InsertOpts) { o.Queue = name }
}

// WithSchedule 指定时间执行
func WithSchedule(timer time.Time) SubmitOption {
	return func(o *river.InsertOpts) {
		if timer.After(time.Now()) {
			o.ScheduledAt = timer
		}
	}
}

// WithPriority 设置任务优先级（数值越大越优先）。
func WithPriority(p int) SubmitOption {
	return func(o *river.InsertOpts) { o.Priority = p }
}

// WithTags 附加标签。
func WithTags(tags ...string) SubmitOption {
	return func(o *river.InsertOpts) { o.Tags = tags }
}

// riverQueue 基于 river 的 Queue 实现。
// 所有业务任务复用同一通用 TaskWorker（按 TaskType 分发），
// 因此 worker 注册可在 client 创建后、Start 前任意时刻进行。
type riverQueue struct {
	client       *Client
	taskWorker   *TaskWorker
	cronWorker   *CronWorker
	listener     *Listener
	hookListener chan HookData
}

func (q *riverQueue) CleanJob(ctx context.Context) error {
	result, err := q.client.pool.Exec(ctx, ""+
		"UPDATE river_job SET state = 'available',"+
		" attempted_at = NULL WHERE state = 'running'")

	if err != nil {
		return err
	}

	if result.RowsAffected() > 0 {

		fmt.Printf("Rescued %d stuck jobs after restart", result.RowsAffected())
	}

	return nil
}

// New 基于给定配置创建 Queue。
// 内部会创建通用 TaskWorker 并注册到 river，调用方随后通过
// RegisterWorker 注册各任务类型处理器，再调用 Start。
func New(ctx context.Context, cfg Config) (Queue, error) {
	taskWorker := NewTaskWorker(cfg.Logger)
	cronWorker := NewCronWorker(cfg.Logger)

	workers := river.NewWorkers()
	//会panic
	river.AddWorker(workers, taskWorker)
	river.AddWorker(workers, cronWorker)
	//会返回err
	//err := river.AddWorkerSafely(workers, taskWorker)
	//if err != nil {
	//	return nil, err
	//}

	cfg.Workers = workers
	client, err := NewClient(ctx, cfg)
	if err != nil {
		return nil, err
	}

	listenerChan, _ := client.Client.Subscribe(
		river.EventKindJobCompleted,
		river.EventKindJobFailed,
	)
	listener := NewListener(cfg.Logger, listenerChan)
	listener.start(ctx)

	return &riverQueue{
		client:       client,
		taskWorker:   taskWorker,
		cronWorker:   cronWorker,
		listener:     listener,
		hookListener: make(chan HookData, 1000),
	}, nil
}

func (q *riverQueue) RegisterHandler(taskType string, handler TaskHandler) error {
	if taskType == "" {
		return fmt.Errorf("queue: task type is empty")
	}
	if handler == nil {
		return fmt.Errorf("queue: handler is nil")
	}
	q.taskWorker.RegisterHandler(taskType, handler)
	return nil
}

func (q *riverQueue) Submit(ctx context.Context, taskType, payload string, opts ...SubmitOption) (int64, error) {
	if taskType == "" {
		return 0, fmt.Errorf("queue: task type is empty")
	}

	insertOpts := &river.InsertOpts{}
	for _, opt := range opts {
		opt(insertOpts)
	}

	if insertOpts.Queue == "" {
		insertOpts.Queue = q.GetQueue()
	}

	res, err := q.client.Insert(ctx, TaskJobArgs{
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

func (q *riverQueue) SubmitCron(taskType, payload string, schedule cron.Schedule, opts ...CronOption) (CronHandle, error) {
	if taskType == "" {
		return 0, fmt.Errorf("queue: task type is empty")
	}
	if schedule == nil {
		return 0, fmt.Errorf("queue: cron schedule is nil")
	}

	cfg := &cronConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	constructor := func() (river.JobArgs, *river.InsertOpts) {
		insertOpts := &river.InsertOpts{}
		for _, o := range cfg.insertOpts {
			o(insertOpts)
		}
		// cron 任务默认走通用队列，复用已配置的 worker 池；可通过 WithQueue 覆盖。
		if insertOpts.Queue == "" {
			insertOpts.Queue = CronQueue
		}
		// 由 CronWorker（kind "cron"）处理，而非 TaskWorker。
		return CronJobArgs{TaskType: taskType, Payload: payload}, insertOpts
	}

	periodicJob := river.NewPeriodicJob(schedule, constructor, &cfg.opts)
	handle, err := q.client.PeriodicJobs().AddSafely(periodicJob)
	if err != nil {
		return 0, fmt.Errorf("queue: register cron %s: %w", taskType, err)
	}
	return CronHandle(handle), nil
}

// RegisterCronHandler 注册 cron 任务处理器到 CronWorker。
// 必须在对应 RegisterCron 触发执行前注册，否则任务会因未知 taskType 而失败重试。
func (q *riverQueue) RegisterCronHandler(taskType string, handler CronTaskHandler) error {
	if taskType == "" {
		return fmt.Errorf("queue: task type is empty")
	}
	if handler == nil {
		return fmt.Errorf("queue: handler is nil")
	}
	q.cronWorker.RegisterHandler(taskType, handler)
	return nil
}

func (q *riverQueue) RemoveCron(handle CronHandle) {
	q.client.PeriodicJobs().Remove(rivertype.PeriodicJobHandle(handle))
}

func (q *riverQueue) RegisterListenerHandler(jobID int64, handler ListenerHandler) error {
	if jobID == 0 {
		return fmt.Errorf("queue: listener jobID is empty")
	}
	if handler == nil {
		return fmt.Errorf("queue: listener handler is nil")
	}
	q.listener.RegisterHandler(jobID, handler)
	return nil
}

func (q *riverQueue) GetHookChan() chan HookData {
	return q.hookListener
}

// test 测试代码
var tmpNum = 1

func (q *riverQueue) GetQueue() string {
	if tmpNum > 1000 {
		return "5"
	}

	tmpNum++
	return "2"
}

// 全局单例，供包级 facade 函数使用。
var (
	globalOnce  sync.Once
	globalQueue Queue
	globalErr   error
)

// Init 基于 config.MConfig 初始化全局队列，仅生效一次。
// 必须在 config.Init 之后调用。
func Init(ctx context.Context) error {
	globalOnce.Do(func() {
		q, err := New(ctx, defaultConfig())
		globalQueue, globalErr = q, err
	})
	return globalErr
}

// defaultConfig 从全局配置构造队列配置。
func defaultConfig() Config {
	maxWorkers := config.MConfig.Queue.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 10
	}

	return Config{
		DatabaseURL: config.MConfig.DB.DSN,
		MaxWorkers:  maxWorkers,
		Queues:      queueMap,
		Logger:      logger.L(),
	}
}

// Get 返回全局队列实例（Init 后可用）。
func Get() (Queue, error) {
	if globalQueue == nil {
		return nil, fmt.Errorf("queue: not initialized, call Init first")
	}
	return globalQueue, nil
}

// RegisterHandler 注册任务处理器到全局队列。
func RegisterHandler(taskType string, handler TaskHandler) error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.RegisterHandler(taskType, handler)
}

// Submit 提交任务到全局队列。
func Submit(ctx context.Context, taskType, payload string, opts ...SubmitOption) (int64, error) {
	q, err := Get()
	if err != nil {
		return 0, err
	}
	return q.Submit(ctx, taskType, payload, opts...)
}

// Start 启动全局队列的 worker 循环。
func Start(ctx context.Context) error {
	q, err := Get()
	if err != nil {
		return err
	}

	return q.Start(ctx)
}

// CleanJob 可以手动清理卡死任务
func CleanJob(ctx context.Context) error {
	q, err := Get()
	if err != nil {
		return err
	}

	return q.CleanJob(ctx)
}

// Stop 优雅停止全局队列的 worker 循环。
func Stop(ctx context.Context) error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.Stop(ctx)
}

// Close 关闭全局队列并释放资源。
func Close() error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.Close()
}

// SubmitCron 注册周期任务到全局队列。
func SubmitCron(taskType, payload string, schedule cron.Schedule, opts ...CronOption) (CronHandle, error) {
	q, err := Get()
	if err != nil {
		return 0, err
	}
	return q.SubmitCron(taskType, payload, schedule, opts...)
}

// RegisterCronHandler 注册 cron 任务处理器到全局队列的 CronWorker。
func RegisterCronHandler(taskType string, handler CronTaskHandler) error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.RegisterCronHandler(taskType, handler)
}

// RemoveCron 从全局队列移除周期任务。
func RemoveCron(handle CronHandle) error {
	q, err := Get()
	if err != nil {
		return err
	}
	q.RemoveCron(handle)
	return nil
}

// RegisterListenerHandler 注册 listener 任务处理器。
func RegisterListenerHandler(jobID int64, handler ListenerHandler) error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.RegisterListenerHandler(jobID, handler)
}

// GetHookChan 获取 hook Read通道。
func GetHookChan() (<-chan HookData, error) {
	q, err := Get()
	if err != nil {
		return nil, err
	}

	return q.GetHookChan(), nil
}

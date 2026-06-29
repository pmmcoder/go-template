package queue

import (
	"context"
	"sync"

	"my_project/internal/infrastructure/queue/contract"
	"my_project/internal/infrastructure/queue/riverqueue"
	"my_project/pkg/config"
	"my_project/pkg/logger"

	"github.com/robfig/cron/v3"
)

// 全局单例，供包级 facade 函数使用。
var (
	globalOnce  sync.Once
	globalQueue contract.Queue
	globalErr   error
)

// Init 基于 config.MConfig 初始化全局队列，仅生效一次。
// 必须在 config.Init 之后调用。
func Init(ctx context.Context) error {
	globalOnce.Do(func() {
		globalQueue, globalErr = riverqueue.InitQueue(ctx, defaultConfig())
	})

	return globalErr
}

// defaultConfig 从全局配置构造队列配置。
func defaultConfig() riverqueue.Config {
	maxWorkers := config.MConfig.Queue.MaxWorkers
	jobTimeout := config.MConfig.Queue.JobTimeout
	maxAttempts := config.MConfig.Queue.MaxAttempts
	if maxWorkers <= 0 {
		maxWorkers = 10
	}

	if jobTimeout <= 0 {
		jobTimeout = 10
	}

	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	return riverqueue.Config{
		DatabaseURL: config.MConfig.DB.DSN,
		MaxWorkers:  maxWorkers,
		JobTimeout:  jobTimeout,
		MaxAttempts: maxAttempts,
		Queues: map[string]int{
			riverqueue.DefaultQueue:     riverqueue.DefaultQueueMaxWorkers,
			riverqueue.DefaultCronQueue: riverqueue.DefaultCronQueueMaxWorkers},
		Logger: logger.L(),
	}
}

// Get 返回全局队列实例（Init 后可用）。
func Get() (contract.Queue, error) {
	if globalQueue == nil {
		return nil, globalErr
	}
	return globalQueue, nil
}

// RegisterHandler 注册任务处理器到全局队列。
func RegisterHandler(taskType string, handler contract.TaskHandler) error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.RegisterHandler(taskType, handler)
}

// Submit 提交任务到全局队列。
func Submit(ctx context.Context, taskType, payload string, taskID int64, opts ...contract.SubmitOption) (int64, error) {
	q, err := Get()
	if err != nil {
		return 0, err
	}
	return q.Submit(ctx, taskType, payload, taskID, opts...)
}

// Start 启动全局队列的 worker 循环。
func Start(ctx context.Context) error {
	q, err := Get()
	if err != nil {
		return err
	}

	return q.Start(ctx)
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
func SubmitCron(taskType string, schedule cron.Schedule, opts ...contract.SubmitOption) (contract.CronHandle, error) {
	q, err := Get()
	if err != nil {
		return 0, err
	}
	return q.SubmitCron(taskType, schedule, opts...)
}

// RegisterCronHandler 注册 cron 任务处理器到全局队列的 CronWorker。
func RegisterCronHandler(taskType string, handler contract.CronTaskHandler) error {
	q, err := Get()
	if err != nil {
		return err
	}
	return q.RegisterCronHandler(taskType, handler)
}

// RemoveCron 从全局队列移除周期任务。
func RemoveCron(handle contract.CronHandle) error {
	q, err := Get()
	if err != nil {
		return err
	}
	q.RemoveCron(handle)
	return nil
}

// SubscribeHook 按业务 taskID 订阅 hook 事件通道。
// taskID 必须与 Submit 时传入的值一致；channel 容量 100，满则丢弃事件。
func SubscribeHook(taskID int64) (<-chan contract.HookData, error) {
	q, err := Get()
	if err != nil {
		return nil, err
	}

	return q.SubscribeHook(taskID), nil
}

// UnSubscribeHook 取消订阅并关闭对应 channel。
func UnSubscribeHook(taskID int64) error {
	q, err := Get()
	if err != nil {
		return err
	}

	q.UnSubscribeHook(taskID)

	return nil
}

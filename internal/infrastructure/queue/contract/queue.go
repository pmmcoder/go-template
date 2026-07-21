package contract

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

// Queue 队列对外接口，抽象 river 实现细节。
type Queue interface {
	// Start 启动 worker 循环（阻塞至 ctx 取消或 Stop）。
	Start(ctx context.Context) error
	// Stop 优雅停止 worker 循环。
	Stop(ctx context.Context) error
	// Close 关闭客户端并释放连接池。
	Close() error
	// RegisterHandler 注册某任务类型的处理器。
	RegisterHandler(taskType string, handler TaskHandler) error
	// Submit 提交任务，返回 river job id。
	Submit(ctx context.Context, taskType, payload string, taskID int64, opts ...SubmitOption) (int64, error)
	// RegisterCronHandler 注册 cron 任务处理器到 CronWorker。
	RegisterCronHandler(taskType string, handler CronTaskHandler) error
	// SubmitCron 注册周期任务：按 schedule 周期性提交指定 taskType 的任务，
	// 由 CronWorker 处理（需先通过 RegisterCronHandler 注册对应 handler）。
	// 通过 opts 可覆盖默认 cron 队列、优先级等。返回句柄用于后续移除。
	SubmitCron(taskType string, schedule cron.Schedule, opts ...SubmitOption) (CronHandle, error)
	// SubscribeHook 按业务 taskID 订阅 hook 事件。
	// taskID 必须与 Submit 时传入的 taskID 一致，hook 推送以该 ID 路由。
	SubscribeHook(taskID int64) <-chan HookData
	// UnSubscribeHook 取消订阅并关闭对应 channel。
	UnSubscribeHook(taskID int64)
	// RemoveCron 按 handle 移除已注册的周期任务。
	RemoveCron(handle CronHandle)
}

type HookData struct {
	RuntimeArgs
	JobID  int64  `json:"job_id"`
	ErrMsg string `json:"err_msg"`
	IsEnd  bool   `json:"is_end"`
	Status int    `json:"status"`
	Data   string `json:"data"`
}

type RuntimeArgs struct {
	Attempt     int
	MaxAttempts int
}

type TaskHandler func(ctx context.Context, payload string, runtimeArgs RuntimeArgs) error

type CronHandle rivertype.PeriodicJobHandle

type SubmitOption func(*river.InsertOpts)

type CronTaskHandler func(ctx context.Context) error

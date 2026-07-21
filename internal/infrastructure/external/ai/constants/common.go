package constants

import (
	"errors"
)

// SharedQueueName 是所有能力共用的单一队列名。
// 注意：该队列必须在 public.queue_configs 中存在且 enabled=true，否则注册失败。
const SharedQueueName = "common_gen"
const WorkerNum = 10

var ErrEndpointsSaturated = errors.New("scheduler: all endpoints saturated, rejecting")

type SchedulerResp struct {
	TaskID   int64
	UserID   string
	Platform string
	Account  string
	Model    string
	Content  string
	Error    string
}

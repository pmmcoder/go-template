package testai

import "my_project/internal/infrastructure/external/testai/contacts"

type Scheduler struct{}

func NewScheduler() *Scheduler {
	return &Scheduler{}
}

func (s *Scheduler) Name() string {
	return "scheduler"
}

func (s *Scheduler) Invoke() contacts.InvokeResp {
	//路由	按 req.Model 过滤匹配的 endpoints；无匹配则用"兜底"组
	//权重	在候选集合内加权随机（或加权轮询）
	//并发控制	每个 endpoint 一个 chan struct{} 信号量，Acquire 失败则跳过该端点
	//重试	只对 ErrCodeRateLimited / ErrCodeTimeout / ErrCodeServer 重试；指数退避 + jitter；最大次数从 cfg
	//故障转移	当前 endpoint 耗尽重试或不可重试错误 → 按权重换下一个；全部失败返回最后一次错误
	return contacts.InvokeResp{}
}

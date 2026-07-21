package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"my_project/internal/domain/scheduler"
	"my_project/internal/infrastructure/external/ai/constants"
	"my_project/internal/infrastructure/queue"
)

// Scheduler 装配调度层：把共享队列注册进 QueueManager，
// 并在队列 worker 取到任务时按 model_capability 路由 + 端点加权选择。
type Scheduler struct {
	logger        *slog.Logger
	schedulerRepo scheduler.Repository
}

// NewScheduler 构造调度器。
func NewScheduler(log *slog.Logger, repo scheduler.Repository) (*Scheduler, error) {
	return &Scheduler{logger: log, schedulerRepo: repo}, nil
}

func (s *Scheduler) Name() string {
	return "Scheduler"
}

// Invoke 构造共享队列的分发闭包：按 model_capability 解析端点
func (s *Scheduler) Invoke(ctx context.Context, params scheduler.AIRequest) (*scheduler.AIResult, error) {
	if params.Model == "" {
		return nil, fmt.Errorf("scheduler: queue %q task missing model_capability", queue.TaskTypeAi)
	}

	eps, err := endpointsFor(ctx, s.schedulerRepo, params.Model)
	if err != nil {
		return nil, fmt.Errorf("scheduler: load endpoints for task:%d capability %q: %w", params.TaskID, params.Model, err)
	}
	if len(eps) == 0 {
		return nil, fmt.Errorf("scheduler: no enabled endpoint for task:%d capability %q", params.TaskID, params.Model)
	}

	ep, pool, err := s.acquireEndpoint(eps)
	if err != nil {
		return nil, err
	}
	if pool != nil {
		defer pool.Release()
	}

	// 超时决策 + 过程日志：可观察 p95/失败数/在途并发是否正常。
	w := windowFor(ep.Key())
	timeout := timeoutFor(ep, w)
	nOK, p95, _ := w.stats()
	cur, size := int64(-1), int64(-1)
	if pool != nil {
		cur, size = pool.GetState()
	}
	s.logger.Info("scheduler: dispatching",
		"queue", queue.TaskTypeAi, "taskID", params.TaskID, "capability", params.Model,
		"platform", ep.Platform, "account", ep.Account, "gateKey", ep.GateKey,
		"timeout", timeout, "dynamic", ep.Dynamic,
		"timeoutFailures", w.failures(), "successSamples", nOK, "p95Sec", p95,
		"inflight", cur, "maxInflight", size)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	res, genErr := callWithTimeout(ctx, ep.Model, params.ModelOpt)
	elapsed := time.Since(start)
	switch {
	case genErr == nil:
		recordSuccess(ep.Key(), elapsed)
		s.logger.Info("scheduler: generate ok", "taskID", params.TaskID,
			"capability", params.Model, "platform", ep.Platform, "account", ep.Account,
			"elapsed", elapsed, "res", res)
	case errors.Is(genErr, context.DeadlineExceeded):
		// 仅超时类失败计入失败窗口（错误收敛）：它们才与超时松紧相关。
		recordTimeout(ep.Key())
		s.logger.Warn("scheduler: generate timeout", "taskID", params.TaskID,
			"capability", params.Model, "platform", ep.Platform, "account", ep.Account,
			"elapsed", elapsed, "timeout", timeout, "res", res)
	default:
		recordError(ep.Key())
		s.logger.Warn("scheduler: generate error", "taskID", params.TaskID,
			"capability", params.Model, "platform", ep.Platform, "account", ep.Account,
			"elapsed", elapsed, "error", genErr, "res", res)
	}
	return &scheduler.AIResult{
		TaskID:       res.TaskID,
		UserID:       res.UserID,
		Platform:     res.Platform,
		Account:      res.Account,
		Content:      res.Content,
		Model:        res.Model,
		FinishReason: res.Error,
	}, genErr
}

type outContent struct {
	SchedulerResp constants.SchedulerResp
	Err           error
}

// callWithTimeout 执行 Generate
func callWithTimeout(ctx context.Context, m Model, params map[string]interface{}) (constants.SchedulerResp, error) {
	done := make(chan outContent, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outContent{constants.SchedulerResp{}, fmt.Errorf("scheduler: model panic: %v", r)}
			}
		}()
		res, err := m.Generate(ctx, params)
		done <- outContent{res, err}
	}()

	select {
	case <-ctx.Done():
		return constants.SchedulerResp{}, fmt.Errorf("scheduler: generate timeout: %w", ctx.Err())
	case output := <-done:
		return output.SchedulerResp, output.Err
	}
}

// acquireEndpoint 在候选端点里选一个并占用其并发槽。
func (s *Scheduler) acquireEndpoint(eps []Endpoint) (Endpoint, *Pool, error) {
	avail := make([]Endpoint, len(eps))
	copy(avail, eps)
	for len(avail) > 0 {
		i := weightedIndex(avail)
		if i < 0 {
			break // 兜底：权重恒 ≥1 时不会发生，仅防御空/零权重导致的死循环
		}
		ep := avail[i]
		pool := gateFor(ep.GateKey)
		if pool == nil { // 不限并发
			s.logger.Debug("scheduler: endpoint has no inflight limit", "ep", ep.Key().String(), "gateKey", ep.GateKey)
			return ep, nil, nil
		}
		if pool.TryAcquire() == nil {
			cur, size := pool.GetState()
			s.logger.Debug("scheduler: acquired slot",
				"ep", ep.Key().String(), "gateKey", ep.GateKey, "inflight", cur, "maxInflight", size)
			return ep, pool, nil
		}
		// 该端点满载：剔除后在其余端点里重新加权
		s.logger.Debug("scheduler: endpoint saturated, trying others", "ep", ep.Key().String(), "gateKey", ep.GateKey)
		avail = append(avail[:i], avail[i+1:]...)
	}

	s.logger.Warn("scheduler: all endpoints saturated, rejecting",
		"capability", eps[0].Capability, "endpoints", len(eps))
	return Endpoint{}, nil, constants.ErrEndpointsSaturated
}

// weightedIndex 在端点集合内按 Weight 加权随机返回一个下标；总权重为 0 时返回 -1。
func weightedIndex(eps []Endpoint) int {
	total := 0
	for i := range eps {
		total += eps[i].Weight
	}
	if total <= 0 {
		return -1
	}
	n := rand.IntN(total)
	for i := range eps {
		n -= eps[i].Weight
		if n < 0 {
			return i
		}
	}
	return -1
}

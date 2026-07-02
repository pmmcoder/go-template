package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"my_project/internal/domain/task"
)

// Scheduler 实现 task.AIProvider，对 application 层透明。
//
// 职责：
//  1. 路由：按 req.Model 选匹配的端点，无匹配回落到兜底端点（Models 为空）
//  2. 加权选择：候选集合内按 Endpoint.Weight 加权随机
//  3. 并发控制：每个端点维护一个 semaphore，满载时跳过该端点（不阻塞）
//  4. 重试：可重试错误在单端点内按指数退避重试 RetryConfig.MaxAttempts 次
//  5. 故障转移：当前端点失败（或满载）后，按权重换下一个端点；fatal 错误直接返回
type Scheduler struct {
	endpoints []*runtime
	retry     RetryConfig
	logger    *slog.Logger
}

type runtime struct {
	ep  Endpoint
	sem chan struct{} // nil 表示不限并发
}

// NewScheduler 装配调度器。endpoints 为空将返回错误。
func NewScheduler(endpoints []Endpoint, retry RetryConfig, logger *slog.Logger) (*Scheduler, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("ai scheduler: at least one endpoint required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	rts := make([]*runtime, len(endpoints))
	for i, ep := range endpoints {
		var sem chan struct{}
		if ep.MaxInflight > 0 {
			sem = make(chan struct{}, ep.MaxInflight)
		}
		rts[i] = &runtime{ep: ep, sem: sem}
	}
	return &Scheduler{
		endpoints: rts,
		retry:     retry.normalize(),
		logger:    logger,
	}, nil
}

func (s *Scheduler) Name() string { return "scheduler" }

// Invoke 路由 + 故障转移：每个候选端点至多尝试一次（端点内自身可重试 MaxAttempts 次）。
func (s *Scheduler) Invoke(ctx context.Context, req task.AIRequest) (*task.AIResult, error) {
	candidates := s.match(req.Model)
	if len(candidates) == 0 {
		return nil, &task.AIError{
			Code:    task.ErrCodeInvalidRequest,
			Message: fmt.Sprintf("no endpoint matches model %q (and no fallback configured)", req.Model),
			Vendor:  s.Name(),
		}
	}

	tried := make(map[*runtime]struct{}, len(candidates))
	var lastErr error

	for len(tried) < len(candidates) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r := pickWeighted(candidates, tried)
		if r == nil {
			break
		}

		if !r.tryAcquire() {
			tried[r] = struct{}{}
			s.logger.Debug("ai scheduler: endpoint at capacity, failover", "endpoint", r.ep.Name)
			continue
		}

		result, err := s.invokeWithRetry(ctx, r, req)
		r.release()

		if err == nil {
			return result, nil
		}
		lastErr = err
		tried[r] = struct{}{}

		if isFatal(err) {
			return nil, err
		}
		s.logger.Warn("ai scheduler: endpoint failed, failover",
			"endpoint", r.ep.Name, "error", err)
	}

	if lastErr == nil {
		lastErr = &task.AIError{
			Code:    task.ErrCodeServer,
			Message: "all candidate endpoints at capacity",
			Vendor:  s.Name(),
		}
	}
	return nil, lastErr
}

// match 按模型筛选候选端点；无精确匹配时回落到兜底端点（Models 为空者）。
func (s *Scheduler) match(model string) []*runtime {
	var exact, fallback []*runtime
	for _, r := range s.endpoints {
		if len(r.ep.Models) == 0 {
			fallback = append(fallback, r)
			continue
		}
		for _, m := range r.ep.Models {
			if m == model {
				exact = append(exact, r)
				break
			}
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return fallback
}

// pickWeighted 在未尝试过的候选中按权重随机抽取一个。
// 总权重为 0（候选全在 exclude 中）时返回 nil。
func pickWeighted(candidates []*runtime, exclude map[*runtime]struct{}) *runtime {
	total := 0
	for _, r := range candidates {
		if _, skip := exclude[r]; skip {
			continue
		}
		total += r.ep.Weight
	}
	if total <= 0 {
		return nil
	}
	n := rand.IntN(total)
	for _, r := range candidates {
		if _, skip := exclude[r]; skip {
			continue
		}
		n -= r.ep.Weight
		if n < 0 {
			return r
		}
	}
	return nil
}

// invokeWithRetry 在单端点上按 RetryConfig 重试。
func (s *Scheduler) invokeWithRetry(ctx context.Context, r *runtime, req task.AIRequest) (*task.AIResult, error) {
	var lastErr error
	for attempt := 1; attempt <= s.retry.MaxAttempts; attempt++ {
		result, err := r.ep.Provider.Invoke(ctx, req)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == s.retry.MaxAttempts {
			return nil, err
		}
		d := backoff(s.retry, attempt)
		s.logger.Debug("ai scheduler: retrying",
			"endpoint", r.ep.Name, "attempt", attempt, "delay", d, "error", err)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (r *runtime) tryAcquire() bool {
	if r.sem == nil {
		return true
	}
	select {
	case r.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *runtime) release() {
	if r.sem == nil {
		return
	}
	<-r.sem
}

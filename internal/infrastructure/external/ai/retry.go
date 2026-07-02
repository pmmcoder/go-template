package ai

import (
	"errors"
	"math/rand/v2"
	"time"

	"my_project/internal/domain/task"
)

// RetryConfig 单端点重试策略。
// 单端点重试用尽后由 Scheduler 故障转移到下一个端点。
type RetryConfig struct {
	MaxAttempts int           // 单端点最大尝试次数（含首次）；<=0 视为 1
	BaseDelay   time.Duration // 退避基准；<=0 取 200ms
	MaxDelay    time.Duration // 退避上限；<=0 取 5s
}

func (c RetryConfig) normalize() RetryConfig {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 1
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = 200 * time.Millisecond
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = 5 * time.Second
	}
	return c
}

// backoff 指数退避 + 半幅 jitter。attempt 从 1 开始。
// shift 限 20 位以防 Duration（int64）溢出。
func backoff(cfg RetryConfig, attempt int) time.Duration {
	shift := attempt - 1
	if shift > 20 {
		shift = 20
	}
	d := cfg.BaseDelay << shift
	if d <= 0 || d > cfg.MaxDelay {
		d = cfg.MaxDelay
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + time.Duration(rand.Int64N(int64(half)))
}

// isRetryable 错误是否可在**同一端点**重试。
func isRetryable(err error) bool {
	var ae *task.AIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Code {
	case task.ErrCodeRateLimited, task.ErrCodeTimeout, task.ErrCodeServer:
		return true
	}
	return false
}

// isFatal 错误是否应直接终结调度（无需向其他端点故障转移）。
// 这类错误源于调用方或内容本身，换端点也徒劳。
func isFatal(err error) bool {
	var ae *task.AIError
	if !errors.As(err, &ae) {
		return false
	}
	switch ae.Code {
	case task.ErrCodeInvalidRequest, task.ErrCodeContentFilter:
		return true
	}
	return false
}

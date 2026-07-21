package ai

import (
	"context"
	"errors"
	"sync"
)

var ErrPoolFull = errors.New("pool is full")
var ErrInvalidSize = errors.New("invalid pool size: must be greater than 0")

// Pool 并发控制池，提供非阻塞的租借模式
// 支持动态调整容量，缩容时已租借资源不受影响
type Pool struct {
	size int64
	cur  int64
	mu   sync.Mutex
	cond *sync.Cond // 唤醒等待空位的 Acquire 调用者；L 绑定到 mu
}

// PoolOptions 初始化并发池的配置
type PoolOptions struct {
	Size int64
}

// NewPool 创建并发池实例
// 如果 opts.Size <= 0，返回 ErrInvalidSize
func NewPool(opts PoolOptions) (*Pool, error) {
	if opts.Size <= 0 {
		return nil, ErrInvalidSize
	}
	p := &Pool{size: opts.Size}
	p.cond = sync.NewCond(&p.mu)
	return p, nil
}

// TryAcquire 非阻塞的获取令牌
func (p *Pool) TryAcquire() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur >= p.size {
		return ErrPoolFull
	}

	p.cur++

	return nil
}

// Acquire 阻塞获取令牌，直到有空位或 ctx 结束。
// ctx 取消/超时时返回 ctx.Err()，此时不占用令牌。
func (p *Pool) Acquire(ctx context.Context) error {
	// 快路径：无需等待直接占位
	p.mu.Lock()
	if p.cur < p.size {
		p.cur++
		p.mu.Unlock()
		return nil
	}

	// 慢路径：起一个观察者，在 ctx 结束时唤醒本等待者重新判定
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.cond.Broadcast()
			p.mu.Unlock()
		case <-stop:
		}
	}()
	defer close(stop)

	for p.cur >= p.size {
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return err
		}
		p.cond.Wait()
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return err
	}
	p.cur++
	p.mu.Unlock()
	return nil
}

// Release 释放令牌
func (p *Pool) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur > 0 {
		p.cur--
		p.cond.Signal() // 唤醒一个等待者
	}
}

// GetState 获取已占用的令牌和大小
func (p *Pool) GetState() (int64, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur, p.size
}

// GetCur 获取已占用的令牌
func (p *Pool) GetCur() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur
}

// GetSize 获取令牌池大小
func (p *Pool) GetSize() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size
}

// SetSize 动态调整池容量
//
// 注意：
//   - 扩容时，允许更多的并发租借
//   - 缩容时，已租借的资源不受影响，但新的租借请求会被拒绝，
//     直到当前并发数降到新容量以下。
func (p *Pool) SetSize(n int64) error {
	if n <= 0 {
		return ErrInvalidSize
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	grew := n > p.size
	p.size = n
	if grew {
		p.cond.Broadcast() // 扩容后可能有新空位，唤醒等待者
	}

	return nil
}

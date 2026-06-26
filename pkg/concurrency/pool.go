package concurrency

import (
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
	mu   sync.RWMutex
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
	return &Pool{size: opts.Size}, nil
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

// Release 释放令牌
func (p *Pool) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur > 0 {
		p.cur--
	}
}

// GetState 获取已占用的令牌和大小
func (p *Pool) GetState() (int64, int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cur, p.size
}

// GetCur 获取已占用的令牌
func (p *Pool) GetCur() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cur
}

// GetSize 获取令牌池大小
func (p *Pool) GetSize() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.size
}

// SetSize 动态调整池容量
//
// 注意：
//   - 扩容时，允许更多的并发租借
//   - 缩容时，已租借的资源不受影响，但新的租借请求会被拒绝，
//     直到当前并发数降到新容量以下。
func (p *Pool) SetSize(n int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n <= 0 {
		return ErrInvalidSize
	}
	p.size = n

	return nil
}

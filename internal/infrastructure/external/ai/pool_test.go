package ai

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPoolTryAcquireRelease(t *testing.T) {
	p, err := NewPool(PoolOptions{Size: 2})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	if err := p.TryAcquire(); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := p.TryAcquire(); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	// 已满，第 3 次应失败
	if err := p.TryAcquire(); err != ErrPoolFull {
		t.Fatalf("acquire 3 = %v, want ErrPoolFull", err)
	}
	// 释放一个后应能再取
	p.Release()
	if err := p.TryAcquire(); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if cur, size := p.GetState(); cur != 2 || size != 2 {
		t.Fatalf("state = (%d,%d), want (2,2)", cur, size)
	}
}

func TestPoolInvalidSize(t *testing.T) {
	if _, err := NewPool(PoolOptions{Size: 0}); err != ErrInvalidSize {
		t.Fatalf("NewPool(0) = %v, want ErrInvalidSize", err)
	}
	p, _ := NewPool(PoolOptions{Size: 1})
	if err := p.SetSize(0); err != ErrInvalidSize {
		t.Fatalf("SetSize(0) = %v, want ErrInvalidSize", err)
	}
}

func TestPoolSetSizeShrinkGrow(t *testing.T) {
	p, _ := NewPool(PoolOptions{Size: 3})
	_ = p.TryAcquire()
	_ = p.TryAcquire()
	_ = p.TryAcquire() // cur=3

	// 缩容到 1：已租借不受影响，但不能再取
	if err := p.SetSize(1); err != nil {
		t.Fatalf("SetSize(1): %v", err)
	}
	if err := p.TryAcquire(); err != ErrPoolFull {
		t.Fatalf("acquire after shrink = %v, want ErrPoolFull", err)
	}
	// 释放到 cur<size 后才能取
	p.Release()
	p.Release() // cur=1, size=1 → 仍满
	if err := p.TryAcquire(); err != ErrPoolFull {
		t.Fatalf("acquire at cur==size = %v, want ErrPoolFull", err)
	}
	p.Release() // cur=0
	if err := p.TryAcquire(); err != nil {
		t.Fatalf("acquire after drain: %v", err)
	}
}

func TestPoolAcquireBlocksUntilRelease(t *testing.T) {
	p, _ := NewPool(PoolOptions{Size: 1})
	if err := p.TryAcquire(); err != nil {
		t.Fatalf("initial acquire: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		// 满载，应阻塞直到下面 Release
		if err := p.Acquire(context.Background()); err == nil {
			close(acquired)
		}
	}()

	select {
	case <-acquired:
		t.Fatal("Acquire returned before a slot was freed")
	case <-time.After(50 * time.Millisecond):
	}

	p.Release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("Acquire did not return after Release")
	}
}

func TestPoolAcquireRespectsContext(t *testing.T) {
	p, _ := NewPool(PoolOptions{Size: 1})
	_ = p.TryAcquire() // 占满

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.Acquire(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("Acquire = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Acquire blocked well past the deadline")
	}
	// 超时不应占用令牌
	if cur, _ := p.GetState(); cur != 1 {
		t.Fatalf("cur = %d after timed-out Acquire, want 1", cur)
	}
}

func TestPoolConcurrentNeverExceedsSize(t *testing.T) {
	const size = 4
	p, _ := NewPool(PoolOptions{Size: size})

	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
		wg       sync.WaitGroup
	)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p.TryAcquire() != nil {
				return
			}
			defer p.Release()
			mu.Lock()
			inFlight++
			if inFlight > maxSeen {
				maxSeen = inFlight
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inFlight--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if maxSeen > size {
		t.Fatalf("observed %d concurrent holders, exceeds size %d", maxSeen, size)
	}
}

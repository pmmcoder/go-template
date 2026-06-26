package concurrency

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	wg := sync.WaitGroup{}
	stop := 0
	pool, _ := NewPool(PoolOptions{
		Size: 5,
	})

	go func() {
		tmpTimer := time.NewTicker(1 * time.Second)
		defer tmpTimer.Stop()

		for {
			select {
			case <-tmpTimer.C:
				fmt.Printf("current:%d max:%d \n", pool.GetCur(), pool.GetSize())
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		timer := time.NewTicker(20 * time.Second)
		defer timer.Stop()

		tmpTimer1 := time.NewTicker(1 * time.Second)
		defer tmpTimer1.Stop()

		for {
			select {
			case <-timer.C:
				num := 5
				stop++
				if stop > 1 {
					num = 8
				}
				if stop > 2 {
					num = 2
				}
				if stop > 3 {
					continue
				}
				_ = pool.SetSize(int64(num))
			case <-tmpTimer1.C:
				for i := 0; i < 10; i++ {
					go working(pool)
				}
			}
		}
	}()

	wg.Wait()
	fmt.Println("done")
}

func working(pool *Pool) {
	if err := pool.TryAcquire(); err != nil {
		fmt.Println("failed to acquire pool")
		return
	}
	defer pool.Release()

	fmt.Printf("date:%s pool acquire \n", time.Now().Format("2006-01-02 15:04:05"))

	//working
	time.Sleep(15 * time.Second)

	return
}

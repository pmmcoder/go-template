package queue

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // 匿名导入自动注册pprof路由
	"os"
	"sync"
	"testing"
	"time"

	"my_project/internal/infrastructure/queue/riverqueue"
	"my_project/pkg/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func TestMain(m *testing.M) {
	config.Init("../../../configs")
	ctx := context.Background()

	if err := migrateRiver(ctx, config.MConfig.DB.DSN); err != nil {
		fmt.Fprintf(os.Stderr, "queue test: river migrate: %v\n", err)
		os.Exit(1)
	}

	startServiceInit(ctx)

	go func() {
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()

	// os.Exit 不会触发 defer，必须显式清理。
	code := m.Run()
	_ = Stop(ctx)
	_ = Close()
	os.Exit(code)
}

// migrateRiver 对目标数据库执行 river schema 迁移（幂等）。
func migrateRiver(ctx context.Context, dsn string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("new migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func startServiceInit(ctx context.Context) {
	if err := Init(ctx); err != nil {
		fmt.Printf("queue test: init: %v\n", err)
		os.Exit(1)
	}

	if err := RegisterHandler("email", func(ctx context.Context, payload string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		fmt.Printf("task received payload: %s\n", payload)
		time.Sleep(300 * time.Millisecond)

		return nil
	}); err != nil {
		fmt.Printf("queue test: register handler: %v\n", err)
		os.Exit(1)
	}

	if err := RegisterCronHandler("cron_task", func(ctx context.Context) error {
		fmt.Printf("cron_task start running \n")
		time.Sleep(30 * time.Second)
		return nil
	}); err != nil {
		fmt.Printf("queue test: register cron handler: %v\n", err)
		os.Exit(1)
	}

	if err := Start(ctx); err != nil {
		fmt.Printf("queue test: start service: %v\n", err)
		os.Exit(1)
	}
}

// TestQueue 验证 Submit + Hook 订阅完整链路：
// 提交 N 个任务，每个订阅 hook 通道，期望在 timeout 前收到 Success/Failed 终态。
func TestQueue(t *testing.T) {
	ctx := context.Background()
	const wantPayload = "send welcome email to user 123"
	const concurrency = 5
	const perJobTimeout = 30 * time.Second

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			taskID := int64(uuid.New().ID())

			hookChan, err := SubscribeHook(taskID)
			if err != nil {
				t.Errorf("[#%d] SubscribeHook: %v", idx, err)
				return
			}
			defer func() { _ = UnSubscribeHook(taskID) }()

			jobID, err := Submit(ctx, "email", wantPayload, taskID)
			if err != nil {
				t.Errorf("[#%d] Submit: %v", idx, err)
				return
			}
			if jobID == 0 {
				t.Errorf("[#%d] Submit returned zero job id", idx)
				return
			}
			t.Logf("[#%d] submit task_id=%d job_id=%d", idx, taskID, jobID)

			ctxTimeout, cancel := context.WithTimeout(ctx, perJobTimeout)
			defer cancel()
			for {
				select {
				case msg, ok := <-hookChan:
					if !ok {
						return
					}
					t.Logf("[#%d] hook task_id=%d status=%d", idx, msg.JobID, msg.Status)
					if msg.Status == riverqueue.StatusSuccess || msg.Status == riverqueue.StatusFailed {
						return
					}
				case <-ctxTimeout.Done():
					t.Errorf("[#%d] task_id=%d hook timed out", idx, taskID)
					return
				}
			}
		}(i)
		// 错峰提交，避免一次性写满 channel 的 burst。
		time.Sleep(100 * time.Millisecond)
	}
	wg.Wait()
}

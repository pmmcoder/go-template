package queue

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"my_project/pkg/config"
	"my_project/pkg/logger"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"
)

func TestMain(m *testing.M) {
	config.Init("../../../configs")

	if err := migrateRiver(context.Background(), config.MConfig.DB.DSN); err != nil {
		fmt.Fprintf(os.Stderr, "queue test: river migrate: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
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

// newTestQueue 构造一个未启动的 Queue，供集成测试使用。
func newTestQueue(t *testing.T) Queue {
	t.Helper()
	q, err := New(context.Background(), Config{
		DatabaseURL: config.MConfig.DB.DSN,
		MaxWorkers:  10,
		Queues:      map[string]int{TaskQueue: 10},
		Logger:      logger.L(),
	})
	if err != nil {
		t.Fatalf("New queue: %v", err)
	}
	return q
}

// TestQueue 通过对外 Queue 接口验证完整链路：
// 注册处理器 -> 启动 -> 提交任务 -> 处理器被调用。
func TestQueue(t *testing.T) {
	ctx := context.Background()
	err := Init(ctx)
	if err != nil {
		t.Fatalf("Init queue: %v", err)
	}
	defer Close()

	const wantPayload = "send welcome email to user 123"
	const wantCronPayload = "send welcome email to user 456"
	const wantChildPayload = "send welcome email to user kid"

	if err := RegisterHandler("email", func(ctx context.Context, payload string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if payload != wantPayload {
			return fmt.Errorf("unexpected payload: %s", payload)
		}
		fmt.Printf("task received payload: %s\n", payload)
		time.Sleep(1000 * time.Millisecond)

		//再次插入
		//jobID, err := Submit(ctx, "email", wantChildPayload)
		//if err != nil {
		//	return err
		//}
		//if jobID == 0 {
		//	return errors.New("jobID is zero")
		//}

		return nil
	}); err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	if err := RegisterCronHandler("cron_task", func(ctx context.Context) error {
		fmt.Printf("cron_task start running \n")
		time.Sleep(30 * time.Second)
		return nil
	}); err != nil {
		t.Fatalf("RegisterCronHandler: %v", err)
	}

	if err := CleanJob(ctx); err != nil {
		t.Fatalf("CleanJob: %v", err)
	}

	if err := Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer Stop(ctx)

	schedule, err := cron.ParseStandard("@every 1m")
	if err != nil {
		t.Fatalf("ParseStandard: %v", err)
	}
	if _, err := SubmitCron("cron_task", wantCronPayload, schedule); err != nil {
		t.Fatalf("SubmitCron: %v", err)
	}

	if _, err := Submit(ctx, "email", wantPayload, WithSchedule(time.Now().Add(3*time.Minute))); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	//监听hook消息通知
	go func() {
		channel, err := GetHookChan()
		if err != nil {
			fmt.Printf("GetHookChan: %v", err)
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-channel:
				fmt.Printf("hook channel got message: %+v\n", message)
			}
		}
	}()

	for {
		go func() {
			jobID, err := Submit(ctx, "email", wantPayload)
			if err != nil {
				fmt.Printf("Submit: %v", err)
			}
			if jobID == 0 {
				fmt.Printf("Submit returned zero job id")
			}
			fmt.Printf("job ID start: %d\n", jobID)

			//注册job级别的listener，只有完成，失败，取消，延迟，暂停，恢复
			if err := RegisterListenerHandler(jobID, func(ctx context.Context, event *river.Event) error {
				fmt.Printf("listener received taskType: %s jobID:%d status:%s \n", event.Kind, event.Job.ID, event.Kind)
				return nil
			}); err != nil {
				fmt.Printf("RegisterListenerHandler: %v", err)
			}

		}()

		select {
		case <-time.After(100000 * time.Millisecond):
		}
	}
}

// TestQueue_RegisterWorkerValidation 验证注册参数校验。
func TestQueue_RegisterWorkerValidation(t *testing.T) {
	q := newTestQueue(t)
	defer q.Close()

	if err := q.RegisterHandler("", func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("expected error for empty task type")
	}
	if err := q.RegisterHandler("noop", nil); err == nil {
		t.Fatal("expected error for nil handler")
	}
}

// TestQueue_DiscardOnHandlerError 保留原有 JobGet 轮询逻辑：
// 处理器持续失败 -> 重试耗尽 -> 任务被丢弃。
func TestQueue_DiscardOnHandlerError(t *testing.T) {
	ctx := context.Background()
	log := logger.L()

	taskWorker := NewTaskWorker(log)
	taskWorker.RegisterHandler("fail", func(ctx context.Context, payload string) error {
		return fmt.Errorf("intentional failure")
	})

	workers := river.NewWorkers()
	river.AddWorker(workers, taskWorker)

	queueClient, err := NewClient(ctx, Config{
		DatabaseURL: config.MConfig.DB.DSN,
		MaxWorkers:  10,
		Queues:      map[string]int{TaskQueue: 10},
		Workers:     workers,
		Logger:      log,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer queueClient.Close()

	if err := queueClient.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer queueClient.Stop(ctx)

	// MaxAttempts=1：首次失败即耗尽重试，进入 discarded。
	insertRes, err := queueClient.Insert(ctx,
		TaskJobArgs{TaskType: "fail", Payload: "boom"},
		&river.InsertOpts{MaxAttempts: 1},
	)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	jobID := insertRes.Job.ID
	deadline := time.After(5 * time.Second)
	var finalState rivertype.JobState
	for {
		select {
		case <-deadline:
			t.Fatalf("job %d not terminal in time; last state=%q", jobID, finalState)
		case <-time.After(50 * time.Millisecond):
		}

		job, err := queueClient.JobGet(ctx, jobID)
		if err != nil {
			t.Fatalf("JobGet %d: %v", jobID, err)
		}
		finalState = job.State

		switch job.State {
		case rivertype.JobStateDiscarded:
			return // 预期：重试耗尽后丢弃
		case rivertype.JobStateCompleted:
			t.Fatalf("job %d unexpectedly completed", jobID)
		}
	}
}

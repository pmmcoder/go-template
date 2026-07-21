package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	httpadapter "my_project/internal/adapters/http"
	apptask "my_project/internal/application/task"
	"my_project/internal/infrastructure/external/ai"
	_ "my_project/internal/infrastructure/external/ai/bundle"
	gormdb "my_project/internal/infrastructure/persistence/gorm"
	"my_project/internal/infrastructure/queue"
	"my_project/pkg/config"
	"my_project/pkg/logger"
	"my_project/pkg/utils"

	"github.com/gin-gonic/gin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 配置
	config.Init(os.Getenv("ROOT_PATH"))
	log := logger.L()

	// gin 模式随环境切换
	switch utils.GetEnv() {
	case utils.PROD:
		gin.SetMode(gin.ReleaseMode)
	case utils.TEST:
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	// DB 连接池（GORM）
	db, err := gormdb.New(config.MConfig.DB)
	if err != nil {
		panic(err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				log.Error(err.Error())
			}
		}
	}()

	schedulerRepo := gormdb.NewSchedulerEndpointRepo(db)
	// AI 调度器
	sch, err := ai.NewScheduler(log, schedulerRepo)
	if err != nil {
		panic(err)
	}

	// 队列（river内部自建 pgx 池 + worker）
	if err := queue.Init(ctx); err != nil {
		panic(err)
	}
	defer func() {
		if err := queue.Close(); err != nil {
			log.Error("Failed to close queue", "error", err)
		}
	}()

	// 仓储
	repo := gormdb.NewTaskRepo(db)

	// application service（同时供 API 侧 Submit & worker 使用）
	taskSvc := apptask.NewService(repo, sch)

	if err := queue.RegisterHandler(queue.TaskTypeAi, taskSvc.HandleAITask); err != nil {
		panic(err)
	}

	// HTTP 路由（一处装配，多聚合扩展只加 Services 字段）
	router := httpadapter.New(httpadapter.Services{
		Task: taskSvc,
	})

	// 启动 queue worker（后台）
	go func() {
		if err := queue.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("queue stopped with error", "err", err)
			stop()
		}
	}()

	// 启动 HTTP server + 优雅停机
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(config.MConfig.App.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		fmt.Printf("http listening: %s\n", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = queue.Stop(shutdownCtx)
	log.Info("shutdown complete")
}

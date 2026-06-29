package riverqueue

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Config River 队列客户端配置
type Config struct {
	DatabaseURL        string         // PostgreSQL 连接字符串
	MaxWorkers         int            // 最大 worker 并发数
	Queues             map[string]int // 队列名称 -> 并发数配置，nil 时使用默认
	Workers            *river.Workers // 已注册 worker 的集合（处理任务必填，仅插入可省略）
	Logger             *slog.Logger   // 日志记录器
	JobTimeout         int            // job 运行的超时时间（单位：分钟），<=0 由上层使用默认值
	MaxAttempts        int            // job 失败后最大尝试次数（含首次执行）
	AdvisoryLockPrefix int32          // 咨询锁前缀（多租户隔离用）
}

// Client River 队列客户端封装
type Client struct {
	*river.Client[pgx.Tx]
	pool *pgxpool.Pool
}

// NewClient 创建 River 队列客户端
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	driver := riverpgxv5.New(pool)

	riverCfg := &river.Config{
		JobTimeout:  time.Duration(cfg.JobTimeout) * time.Minute,
		MaxAttempts: cfg.MaxAttempts,
	}
	if cfg.Workers != nil {
		riverCfg.Workers = cfg.Workers
	}

	if len(cfg.Queues) > 0 {
		riverCfg.Queues = buildQueueConfigs(cfg.Queues, cfg.MaxWorkers)
	}

	if cfg.Logger != nil {
		riverCfg.Logger = cfg.Logger
	}
	if cfg.AdvisoryLockPrefix != 0 {
		riverCfg.AdvisoryLockPrefix = cfg.AdvisoryLockPrefix
	}

	riverClient, err := river.NewClient(driver, riverCfg)
	if err != nil {
		pool.Close()
		return nil, err
	}

	return &Client{
		Client: riverClient,
		pool:   pool,
	}, nil
}

// Close 关闭客户端并释放连接池
func (c *Client) Close() error {
	c.pool.Close()
	return nil
}

// Start 启动 worker 循环
func (c *Client) Start(ctx context.Context) error {
	return c.Client.Start(ctx)
}

// Stop 优雅停止 worker 循环
func (c *Client) Stop(ctx context.Context) error {
	return c.Client.Stop(ctx)
}

// Pool 返回底层连接池（用于迁移等管理操作）
func (c *Client) Pool() *pgxpool.Pool {
	return c.pool
}

// buildQueueConfigs 将 map 配置转为 River 队列配置
func buildQueueConfigs(queues map[string]int, defaultWorkers int) map[string]river.QueueConfig {
	if len(queues) == 0 {
		return map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: defaultWorkers},
		}
	}

	result := make(map[string]river.QueueConfig, len(queues))
	for name, workers := range queues {
		if workers <= 0 {
			workers = defaultWorkers
		}
		result[name] = river.QueueConfig{MaxWorkers: workers}
	}
	return result
}

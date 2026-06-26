package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/riverqueue/river"
)

type ListenerHandler func(ctx context.Context, event *river.Event) error

type Listener struct {
	mu          sync.RWMutex
	logger      *slog.Logger
	handlers    map[int64]ListenerHandler
	receiveChan <-chan *river.Event
}

// NewListener 创建通用listener。
func NewListener(logger *slog.Logger, channel <-chan *river.Event) *Listener {
	if logger == nil {
		logger = slog.Default()
	}

	return &Listener{
		logger:      logger,
		handlers:    make(map[int64]ListenerHandler),
		receiveChan: channel,
	}
}

// RegisterHandler 注册 监听器 callback 任务处理器（并发安全，可在 Start 后调用）。
func (l *Listener) RegisterHandler(jobID int64, handler ListenerHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handlers[jobID] = handler
}

// Work 处理 cron 任务。
func (l *Listener) start(ctx context.Context) {
	l.logger.Info("listener job started")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				l.logger.Error("listener job panicked", "panic", r)
			}
		}()
		fmt.Println("listener job started")
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-l.receiveChan:
				jobID := event.Job.ID
				l.mu.RLock()
				handler, ok := l.handlers[jobID]
				l.mu.RUnlock()

				fmt.Printf("listener job %v ok: %v\n", jobID, ok)

				if ok {
					if err := handler(ctx, event); err != nil {
						l.logger.Error("listener job failed",
							"event", event.Kind,
							"job_id", jobID,
							"attempt", event.Job.Attempt,
							"error", err,
						)
					} else {
						l.logger.Info("listener job finished", "job_id", jobID)
					}
				}
			}
		}
	}()
}

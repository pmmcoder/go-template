package ai

import (
	"fmt"
	"log/slog"
	"time"

	"my_project/pkg/config"
)

func NewFromConfig(cfg config.AIConfig, logger *slog.Logger) (*Scheduler, error) {
	eps := make([]Endpoint, 0, len(cfg.Endpoints))
	for i, ec := range cfg.Endpoints {
		ep, err := BuildEndpoint(ec)
		if err != nil {
			return nil, fmt.Errorf("ai endpoint #%d (%s): %w", i, ec.Name, err)
		}
		eps = append(eps, ep)
	}
	return NewScheduler(eps, RetryConfig{
		MaxAttempts: cfg.Retry.MaxAttempts,
		BaseDelay:   time.Duration(cfg.Retry.BaseDelayMS) * time.Millisecond,
		MaxDelay:    time.Duration(cfg.Retry.MaxDelayMS) * time.Millisecond,
	}, logger)
}

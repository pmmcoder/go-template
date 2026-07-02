package task

import (
	"context"
	"encoding/json"
	"log/slog"

	domaintask "my_project/internal/domain/task"
	"my_project/internal/infrastructure/queue"
	"my_project/pkg/logger"
)

type Service struct {
	ai   domaintask.AIProvider
	repo domaintask.Repository
}

func NewService(ai domaintask.AIProvider, repo domaintask.Repository) *Service {
	return &Service{ai: ai, repo: repo}
}

// ---- 用例 DTO / 视图对象（对外只暴露值语义，不泄漏聚合） ----

// SubmitInput API 端提交任务的输入。
type SubmitInput struct {
	Model    string
	Messages []domaintask.AIMessage
}

// View API 端查询任务的返回视图。
type View struct {
	ID      int64
	Status  domaintask.Status
	Model   string
	Content string
}

// aiTaskPayload worker 端 payload 结构，仅本包可见。
type aiTaskPayload struct {
	TaskID   int64                  `json:"task_id"`
	Model    string                 `json:"model"`
	Messages []domaintask.AIMessage `json:"messages"`
}

// ---- 用例：API 端 ----

// Submit API 提交 AI 任务：入库 + 入队，返回任务 ID。
// HTTP 层拿到 ID 后即可 202 Accepted 给客户端。
func (s *Service) Submit(ctx context.Context, in SubmitInput) (int64, error) {
	t := domaintask.NewAITask(in.Model, in.Messages)
	if err := s.repo.Save(ctx, t); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(aiTaskPayload{
		TaskID:   t.ID(),
		Model:    in.Model,
		Messages: in.Messages,
	})
	if err != nil {
		return 0, err
	}
	if _, err := queue.Submit(ctx, queue.TaskTypeAi, string(payload), t.ID()); err != nil {
		return 0, err
	}
	return t.ID(), nil
}

// Get API 查询任务当前状态与结果。
func (s *Service) Get(ctx context.Context, id int64) (View, error) {
	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return View{}, err
	}
	return View{
		ID:      t.ID(),
		Status:  t.Status(),
		Model:   t.Model(),
		Content: t.Content(),
	}, nil
}

// ---- 用例：worker 端 ----

// HandleAITask worker 拉到 job 后调用。
// 解 payload → 调 AI → 落库结果。
func (s *Service) HandleAITask(ctx context.Context, payload string) error {
	var p aiTaskPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return err
	}

	t, err := s.repo.FindByID(ctx, p.TaskID)
	if err != nil {
		return err
	}
	t.MarkRunning()
	if err := s.repo.Save(ctx, t); err != nil {
		return err
	}

	res, err := s.ai.Invoke(ctx, domaintask.AIRequest{
		Model:    p.Model,
		Messages: p.Messages,
	})
	if err != nil {
		t.MarkFailed(err.Error())
		_ = s.repo.Save(ctx, t)
		return err // 让 river 按策略重试
	}

	t.MarkSucceeded(res.Content, res.Usage.TotalTokens)
	if err := s.repo.Save(ctx, t); err != nil {
		return err
	}

	logger.Info("HandleAITask: complete",
		slog.Int64("task_id", p.TaskID),
		slog.Int("tokens", res.Usage.TotalTokens),
	)
	return nil
}

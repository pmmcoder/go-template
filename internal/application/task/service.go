package task

import (
	"context"
	"encoding/json"
	"errors"
	domainscheduler "my_project/internal/domain/scheduler"
	domaintask "my_project/internal/domain/task"
	"my_project/internal/infrastructure/queue"
	"my_project/internal/infrastructure/queue/contract"
)

type Service struct {
	repo domaintask.Repository
	sch  domainscheduler.AiProvider
}

func NewService(repo domaintask.Repository, sch domainscheduler.AiProvider) *Service {
	return &Service{repo: repo, sch: sch}
}

// ---- 用例 DTO / 视图对象（对外只暴露值语义，不泄漏聚合） ----

// SubmitInput API 端提交任务的输入。
type SubmitInput struct {
	Model     string
	Messages  []domaintask.AIMessage
	UserID    string
	ModelOpts map[string]interface{}
}

// View API 端查询任务的返回视图。
type View struct {
	ID        int64
	UserID    string
	Status    domaintask.Status
	Model     string
	ModelOpts map[string]interface{}
	Content   string
}

// aiTaskPayload worker 端 payload 结构，仅本包可见。
type aiTaskPayload struct {
	TaskID   int64                  `json:"task_id"`
	UserID   string                 `json:"user_id"`
	Model    string                 `json:"model"`
	ModelOpt map[string]interface{} `json:"model_opt"`
	Messages []domaintask.AIMessage `json:"messages"`
}

// ---- 用例：API 端 ----

// Submit API 提交 AI 任务：入库 + 入队，返回任务 ID。
// HTTP 层拿到 ID 后即可 202 Accepted 给客户端。
func (s *Service) Submit(ctx context.Context, in SubmitInput) (int64, error) {
	t := domaintask.NewAITask(in.Model, in.ModelOpts, in.UserID, in.Messages)
	if err := s.repo.Save(ctx, t); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(aiTaskPayload{
		TaskID:   t.ID(),
		UserID:   in.UserID,
		Model:    in.Model,
		ModelOpt: in.ModelOpts,
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
		ID:        t.ID(),
		UserID:    t.UserID(),
		Status:    t.Status(),
		Model:     t.Model(),
		ModelOpts: t.ModelOpts(),
		Content:   t.Content(),
	}, nil
}

func (s *Service) AsyncGet(ctx context.Context, id int64, send chan string) (View, error) {
	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return View{}, err
	}

	if t.Snapshot().Status == domaintask.StatusSucceeded {
		return View{
			ID:     t.ID(),
			UserID: t.UserID(),
			Status: t.Status(),
			Model:  t.Model(),
		}, nil
	}
	if t.Snapshot().Status == domaintask.StatusFailed {
		return View{}, errors.New(t.ErrorMessage())
	}

	resChan, err := queue.SubscribeHook(id)
	if err != nil {
		return View{}, err
	}
	defer func() {
		_ = queue.UnSubscribeHook(id)
	}()

	for {
		select {
		case res := <-resChan:
			if !res.IsEnd {
				send <- res.Data
			} else {
				if res.ErrMsg != "" {
					if res.RuntimeArgs.MaxAttempts > 0 {
						if res.RuntimeArgs.Attempt < res.RuntimeArgs.MaxAttempts {
							send <- res.ErrMsg
						}
						if res.RuntimeArgs.Attempt >= res.RuntimeArgs.MaxAttempts {
							send <- res.ErrMsg
							send <- "failed"
							return View{}, errors.New(res.ErrMsg)
						}
					} else {
						send <- res.ErrMsg
						return View{}, errors.New(res.ErrMsg)
					}
				} else {
					send <- res.Data
					return View{
						ID:     t.ID(),
						UserID: t.UserID(),
						Status: t.Status(),
						Model:  t.Model(),
					}, nil
				}
			}
		case <-ctx.Done():
			return View{}, ctx.Err()
		}
	}
}

func (s *Service) HandleAITask(ctx context.Context, payload string, runtimeArgs contract.RuntimeArgs) error {
	var p aiTaskPayload
	err := json.Unmarshal([]byte(payload), &p)
	if err != nil {
		return err
	}

	t, err := s.repo.FindByID(ctx, p.TaskID)
	if err != nil {
		return err
	}

	t.MarkRunning()
	err = s.repo.Save(ctx, t)
	if err != nil {
		return err
	}

	var aiMessage []domainscheduler.AIMessage
	for _, m := range p.Messages {
		aiMessage = append(aiMessage, domainscheduler.AIMessage{
			Role:    domainscheduler.AIRole(m.Role),
			Content: m.Content,
		})
	}

	result, err := s.sch.Invoke(ctx, domainscheduler.AIRequest{
		TaskID:   t.ID(),
		Model:    t.Model(),
		UserID:   p.UserID,
		ModelOpt: p.ModelOpt,
		Messages: aiMessage,
	})
	if err != nil {
		if runtimeArgs.MaxAttempts > 0 {
			if runtimeArgs.Attempt < runtimeArgs.MaxAttempts {
				return err
			}
			if runtimeArgs.Attempt >= runtimeArgs.MaxAttempts {
				t.MarkFailed(err.Error())
				s.repo.Save(ctx, t)
			}
		}

		return err
	}

	t.MarkSucceeded(result.Content)
	return s.repo.Save(ctx, t)
}

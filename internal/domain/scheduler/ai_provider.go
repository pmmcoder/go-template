package scheduler

import (
	"context"
)

// AiProvider 是 task 领域消费 分发 能力的端口。
type AiProvider interface {
	Name() string
	Invoke(ctx context.Context, req AIRequest) (*AIResult, error)
}

// AIMessage 单条对话消息。
type AIMessage struct {
	Role    AIRole `json:"role"`
	Content string `json:"content"`
}

// AIRole 消息角色。
type AIRole string

const (
	RoleSystem    AIRole = "system"
	RoleUser      AIRole = "user"
	RoleAssistant AIRole = "assistant"
)

// AIRequest 统一请求协议，不携带任何厂商专有字段。
// 厂商专属调优参数走 Extra，由各 adapter 自行识别。
type AIRequest struct {
	TaskID   int64                  `json:"task_id"`
	UserID   string                 `json:"user_id"`
	Model    string                 `json:"model"`
	ModelOpt map[string]interface{} `json:"model_opt"`
	Messages []AIMessage            `json:"messages"`
}

// AIResult 统一返回协议。
type AIResult struct {
	UserID       string `json:"user_id"`
	TaskID       int64  `json:"task_id"`
	Platform     string `json:"platform"`
	Account      string `json:"account"`
	Content      string `json:"content"`                 // 模型输出文本
	Model        string `json:"model"`                   // 实际使用的模型名
	FinishReason string `json:"finish_reason,omitempty"` // stop / length / content_filter ...
}

// AIError 统一错误，便于上层按 Code 做重试/降级判断。
// adapter 必须把厂商错误映射到下列 Code 之一。
type AIError struct {
	Code    AIErrorCode
	Message string
	Vendor  string // 来源 adapter 名称
	Cause   error  // 原始错误，可为 nil
}

func (e *AIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Vendor + ": " + string(e.Code) + ": " + e.Message + ": " + e.Cause.Error()
	}
	return e.Vendor + ": " + string(e.Code) + ": " + e.Message
}

func (e *AIError) Unwrap() error { return e.Cause }

// AIErrorCode 统一错误码，跨厂商一致。
type AIErrorCode string

const (
	ErrCodeUnknown        AIErrorCode = "unknown"
	ErrCodeInvalidRequest AIErrorCode = "invalid_request" // 入参非法，不可重试
	ErrCodeAuth           AIErrorCode = "auth"            // 鉴权失败，不可重试
	ErrCodeRateLimited    AIErrorCode = "rate_limited"    // 限流，可退避重试
	ErrCodeTimeout        AIErrorCode = "timeout"         // 超时，可重试
	ErrCodeServer         AIErrorCode = "server"          // 厂商 5xx，可重试
	ErrCodeContentFilter  AIErrorCode = "content_filter"  // 内容安全拦截，不可重试
)

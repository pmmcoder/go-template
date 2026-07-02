package task

import "context"

// AIProvider 是 task 领域消费 AI 能力的端口。
// 实现位于 internal/infrastructure/external/ai/<vendor>/，
// 由 cmd 启动时按配置选型并注入到 application 层。
type AIProvider interface {
	// Name 返回实现标识（如 "openai" / "anthropic"），用于日志与可观测性。
	Name() string
	// Invoke 同步调用一次模型，输入输出均为统一协议。
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
	Model       string         `json:"model"`                  // 模型名，留空则由 adapter 用默认
	Messages    []AIMessage    `json:"messages"`               // 对话历史 + 当前输入
	Temperature float32        `json:"temperature,omitempty"`  // 0~2，0 表示由 adapter 取默认
	MaxTokens   int            `json:"max_tokens,omitempty"`   // 0 表示由 adapter 取默认
	Extra       map[string]any `json:"extra,omitempty"`        // 厂商私有参数，可选
}

// AIResult 统一返回协议。
type AIResult struct {
	Content      string      `json:"content"`                 // 模型输出文本
	Model        string      `json:"model"`                   // 实际使用的模型名
	FinishReason string      `json:"finish_reason,omitempty"` // stop / length / content_filter ...
	Usage        AIUsage     `json:"usage"`                   // token 用量
	Raw          interface{} `json:"-"`                       // 原始响应，仅排障用，禁止序列化外发
}

// AIUsage token 用量统计。
type AIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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

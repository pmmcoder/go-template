package contacts

type InvokeResp struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	ErrorMsg string `json:"error_msg"`
}

type AiProvider interface {
	Name() string
	Invoke() InvokeResp
}

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

const (
	OpenAiModelName = "openai"
)

type OpenAiProvider interface{}

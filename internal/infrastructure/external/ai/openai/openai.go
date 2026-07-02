// Package openai 提供 OpenAI 协议（含官方端点、DeepSeek / Qwen DashScope 等 OpenAI 兼容端点）
// 与 Azure OpenAI 部署的 task.AIProvider 实现。
//
// 通过 init() 在 ai.Registry 中自注册两种装配方式：
//
//	"openai"       标准 Bearer 鉴权，URL: {base_url}/chat/completions
//	"openai-azure" Azure 部署：api-key 头 + URL: {endpoint}/openai/deployments/{deployment}/chat/completions?api-version=...
//
// cmd 端通过 `import _ "my_project/internal/infrastructure/external/ai/openai"` 触发注册。
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"my_project/internal/domain/task"
	"my_project/internal/infrastructure/external/ai"
)

func init() {
	ai.Register("openai", buildStandard)
	ai.Register("openai-azure", buildAzure)
}

// Provider 实现 task.AIProvider。
// chatURL 与 setAuth 由不同装配方式注入，使两种端点共用同一份请求/解析逻辑。
type Provider struct {
	name         string
	defaultModel string
	client       *http.Client
	chatURL      func(model string) string
	setAuth      func(*http.Request)
}

func (p *Provider) Name() string { return p.name }

// ---- 装配 1：OpenAI / OpenAI 兼容标准端点 ----

type StandardConfig struct {
	BaseURL      string `json:"base_url"` // 例：https://api.openai.com/v1
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
	TimeoutMS    int    `json:"timeout_ms"` // 0 取 60s
}

func buildStandard(raw json.RawMessage) (task.AIProvider, error) {
	var c StandardConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("openai: parse standard config: %w", err)
	}
	if c.BaseURL == "" {
		return nil, errors.New("openai: base_url required")
	}
	if c.APIKey == "" {
		return nil, errors.New("openai: api_key required")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	apiKey := c.APIKey
	return &Provider{
		name:         "openai",
		defaultModel: c.DefaultModel,
		client:       &http.Client{Timeout: durationOr(c.TimeoutMS, 60*time.Second)},
		chatURL:      func(string) string { return base + "/chat/completions" },
		setAuth:      func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+apiKey) },
	}, nil
}

// ---- 装配 2：Azure OpenAI ----

type AzureConfig struct {
	Endpoint   string `json:"endpoint"`    // 例：https://my-resource.openai.azure.com
	Deployment string `json:"deployment"`  // Azure 部署名（取代 model）
	APIVersion string `json:"api_version"` // 例：2024-02-15-preview
	APIKey     string `json:"api_key"`
	TimeoutMS  int    `json:"timeout_ms"`
}

func buildAzure(raw json.RawMessage) (task.AIProvider, error) {
	var c AzureConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("openai-azure: parse config: %w", err)
	}
	if c.Endpoint == "" || c.Deployment == "" || c.APIVersion == "" || c.APIKey == "" {
		return nil, errors.New("openai-azure: endpoint, deployment, api_version, api_key all required")
	}
	endpoint := strings.TrimRight(c.Endpoint, "/")
	url := endpoint + "/openai/deployments/" + c.Deployment + "/chat/completions?api-version=" + c.APIVersion
	apiKey := c.APIKey
	return &Provider{
		name:         "openai-azure",
		defaultModel: c.Deployment,
		client:       &http.Client{Timeout: durationOr(c.TimeoutMS, 60*time.Second)},
		chatURL:      func(string) string { return url }, // Azure 端点忽略请求体的 model，URL 锁定 deployment
		setAuth:      func(r *http.Request) { r.Header.Set("api-key", apiKey) },
	}, nil
}

func durationOr(ms int, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}

// ---- 共享请求逻辑 ----

func (p *Provider) Invoke(ctx context.Context, req task.AIRequest) (*task.AIResult, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    toVendorMessages(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return nil, p.wrap(task.ErrCodeInvalidRequest, "marshal request", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL(model), bytes.NewReader(body))
	if err != nil {
		return nil, p.wrap(task.ErrCodeInvalidRequest, "build http request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.setAuth(httpReq)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, p.wrap(task.ErrCodeTimeout, "request timeout", err)
		}
		return nil, p.wrap(task.ErrCodeServer, "do http request", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, p.wrap(task.ErrCodeServer, "read response", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, p.wrap(mapHTTPStatus(resp.StatusCode),
			fmt.Sprintf("http %d: %s", resp.StatusCode, truncate(raw, 512)), nil)
	}

	var vr chatResponse
	if err := json.Unmarshal(raw, &vr); err != nil {
		return nil, p.wrap(task.ErrCodeServer, "decode response", err)
	}
	if len(vr.Choices) == 0 {
		return nil, p.wrap(task.ErrCodeServer, "empty choices", nil)
	}

	choice := vr.Choices[0]
	return &task.AIResult{
		Content:      choice.Message.Content,
		Model:        vr.Model,
		FinishReason: choice.FinishReason,
		Usage: task.AIUsage{
			PromptTokens:     vr.Usage.PromptTokens,
			CompletionTokens: vr.Usage.CompletionTokens,
			TotalTokens:      vr.Usage.TotalTokens,
		},
		Raw: vr,
	}, nil
}

func (p *Provider) wrap(code task.AIErrorCode, msg string, cause error) error {
	return &task.AIError{Code: code, Message: msg, Vendor: p.name, Cause: cause}
}

func mapHTTPStatus(status int) task.AIErrorCode {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return task.ErrCodeAuth
	case status == http.StatusTooManyRequests:
		return task.ErrCodeRateLimited
	case status == http.StatusRequestTimeout, status == http.StatusGatewayTimeout:
		return task.ErrCodeTimeout
	case status >= 500:
		return task.ErrCodeServer
	case status >= 400:
		return task.ErrCodeInvalidRequest
	default:
		return task.ErrCodeUnknown
	}
}

func toVendorMessages(in []task.AIMessage) []chatMessage {
	out := make([]chatMessage, len(in))
	for i, m := range in {
		out[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}
	return out
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// ---- 厂商私有报文 ----

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float32       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type chatChoice struct {
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

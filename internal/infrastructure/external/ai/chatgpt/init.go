// Package chatgpt 是「chatgpt」平台的调度
package chatgpt

import (
	"context"

	"my_project/internal/infrastructure/external/ai"
	chatGptConstants "my_project/internal/infrastructure/external/ai/chatgpt/constants"
	"my_project/internal/infrastructure/external/ai/constants"
)

func init() {
	ai.Register(ChatGpt{chatGptConstants.PlatformName, chatGptConstants.AccountAzure, chatGptConstants.GPTImage1_5})
	ai.Register(ChatGpt{chatGptConstants.PlatformName, chatGptConstants.AccountAzure, chatGptConstants.GPTImage2})
	ai.Register(ChatGpt{chatGptConstants.PlatformName, chatGptConstants.AccountOfficial, chatGptConstants.GPTImage2})
	ai.Register(ChatGpt{chatGptConstants.PlatformName, chatGptConstants.AccountGateway, chatGptConstants.GPTImage2})
}

type ChatGpt struct {
	PlatformName   string
	AccountName    string
	CapabilityName string
}

func (m ChatGpt) Platform() string   { return m.PlatformName }
func (m ChatGpt) Account() string    { return m.AccountName }
func (m ChatGpt) Capability() string { return m.CapabilityName }

func (m ChatGpt) Generate(ctx context.Context, params map[string]interface{}) (constants.SchedulerResp, error) {
	//调用外部API 业务
	return constants.SchedulerResp{}, nil
}

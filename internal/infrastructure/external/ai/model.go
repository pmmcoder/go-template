package ai

import (
	"context"
	"my_project/internal/infrastructure/external/ai/constants"
)

type Model interface {
	Platform() string   // 平台名，如 "chatgpt"
	Account() string    // 账号/接入标识，如 "azure" / "official" / "gateway"
	Capability() string // 所属 model_capability，如 "gpt-image-2"
	Generate(context.Context, map[string]interface{}) (constants.SchedulerResp, error)
}

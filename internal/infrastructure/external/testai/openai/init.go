package openai

import (
	"encoding/json"
	"my_project/internal/infrastructure/external/testai"
	"my_project/internal/infrastructure/external/testai/contacts"
)

type OpenAi struct{}

func init() {
	testai.RegisterModel(contacts.OpenAiModelName, build)
}

func (a *OpenAi) Name() string {
	return "open_ai"
}

func (a *OpenAi) Invoke() contacts.InvokeResp {
	return contacts.InvokeResp{}
}

func build(param json.RawMessage) (contacts.AiProvider, error) {
	return &OpenAi{}, nil
}

package testai

import (
	"encoding/json"
	"sync"

	"my_project/internal/infrastructure/external/testai/contacts"
)

type BuildFunc func(params json.RawMessage) (contacts.AiProvider, error)

var (
	mu            sync.Mutex
	ModelProvider map[string]BuildFunc
)

func RegisterModel(modelName string, fn BuildFunc) {
	mu.Lock()
	defer mu.Unlock()
	ModelProvider[modelName] = fn
}

func BuildModel(name string, param json.RawMessage) (contacts.AiProvider, error) {
	return ModelProvider[name](param)
}

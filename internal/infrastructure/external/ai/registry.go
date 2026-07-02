// Package ai 提供模型 provider 注册表与调度器。
//
// 各厂商 adapter 子包（如 ./openai）通过 init() 调用 Register 自注册。
// 新增厂商只需 `import _ "my_project/internal/infrastructure/external/ai/<vendor>"`，
// 不必修改本包代码（满足开闭原则）。
//
// 同一厂商可注册多个名字以提供多种装配方式，例如：
//
//	ai.Register("openai",       buildStandard)
//	ai.Register("openai-azure", buildAzure)
//
// 上层（cmd）从配置中读 EndpointConfig 列表，调用 BuildEndpoint 实例化，
// 再交给 NewScheduler 包装。application 层只看到 task.AIProvider 接口。
package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"my_project/internal/domain/task"
)

// Builder 用厂商私有配置（raw json）构造一个 AIProvider 实例。
// 配置结构由各 Builder 自己定义并解析。
type Builder func(raw json.RawMessage) (task.AIProvider, error)

var (
	mu       sync.RWMutex
	builders = map[string]Builder{}
)

// Register 注册一个 Builder。必须在 init() 中调用。
// 重复注册会 panic：这属于启动期编程错误，越早暴露越好。
func Register(name string, b Builder) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := builders[name]; ok {
		panic("ai: duplicate provider registration: " + name)
	}
	builders[name] = b
}

// Build 按名字与厂商私有配置构造 provider。
// 未知名字返回错误（通常是忘了 import 对应 adapter 包）。
func Build(name string, raw json.RawMessage) (task.AIProvider, error) {
	mu.RLock()
	b, ok := builders[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("ai: unknown provider %q (forgot to import the adapter package?)", name)
	}
	return b(raw)
}

// Names 列出已注册 builder 名字，仅用于启动日志/诊断。
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(builders))
	for k := range builders {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

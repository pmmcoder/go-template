// Package bundle 聚合所有 AI 厂商 adapter 的副作用 import，
// 触发它们各自的 init() 完成自注册到 ai.Registry。
//
// cmd 端只需：
//
//	import _ "my_project/internal/infrastructure/external/ai/bundle"
//
// 新增厂商时在这里加一行 `_ "..."`，main.go 不动。
//
// 若需要按构建场景裁剪（例如某些环境不带 Azure），可以把该 import 拆到
// 带 build tag 的文件，如 bundle_azure.go (//go:build with_azure)。
package bundle

import (
	_ "my_project/internal/infrastructure/external/ai/openai" // "openai" + "openai-azure"
	// _ "my_project/internal/infrastructure/external/ai/anthropic"
	// _ "my_project/internal/infrastructure/external/ai/qwen"
	// _ "my_project/internal/infrastructure/external/ai/deepseek"
)

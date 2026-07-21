// Package bundle 聚合所有 AI 厂商 adapter 的副作用 import，
// 触发它们各自的 init() 完成自注册到 ai.Registry。
//
// cmd 端只需：
//
//	import _ "my_project/internal/infrastructure/external/ai/bundle"
//
// 新增厂商时在这里加一行 `_ "..."`，main.go 不动。
package bundle

import (
	_ "my_project/internal/infrastructure/external/ai/chatgpt"
)

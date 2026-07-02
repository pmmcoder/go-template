//如果想要build的时候，选择性初始化，就是用下面命令
////go:build with_azure
//执行的时候go build -tags="with_azure with_qwen"

package bundle

import (
	_ "my_project/internal/infrastructure/external/testai/openai"
)

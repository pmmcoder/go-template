package ai

import "time"

// HandlerKey 唯一标识一个平台账号端点的「代码侧」身份。
type HandlerKey struct {
	Platform   string // 平台名，如 "chatgpt"
	Account    string // 账号/接入标识，如 "azure-1" / "official" / "gateway"
	Capability string // 所属 model_capability
}

func (k HandlerKey) String() string {
	return k.Platform + "/" + k.Account + "/" + k.Capability
}

// Endpoint 是运行期解析出的一个可调用端点：由「代码侧 Model」+「DB 侧权重/并发/超时配置」合并而成。
type Endpoint struct {
	Platform    string
	Account     string
	Capability  string
	Weight      int
	MaxInflight int // ≤0 表示不限并发（不建 pool）

	GateKey string

	MinTimeout time.Duration
	MaxTimeout time.Duration
	Dynamic    bool

	Model Model
}

// Key 返回该端点的代码侧身份键。
func (e Endpoint) Key() HandlerKey {
	return HandlerKey{Platform: e.Platform, Account: e.Account, Capability: e.Capability}
}

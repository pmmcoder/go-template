package ai

// GateScope 决定并发池（闸门）的粒度：一个池覆盖多大范围的端点，即哪些端点共享同一份并发额度。
type GateScope int

const (
	// ScopeCapability 每 (platform, account, capability) 一个池（默认，按模型能力并发）。
	ScopeCapability GateScope = iota
	// ScopeAccount 每 (platform, account) 一个池：账号下所有 capability 共享并发额度。
	ScopeAccount
	// ScopePlatform 每 platform 一个池：平台下所有账号/capability 共享并发额度。
	ScopePlatform
)

func (s GateScope) String() string {
	switch s {
	case ScopeAccount:
		return "account"
	case ScopePlatform:
		return "platform"
	default:
		return "capability"
	}
}

// scopeFromDB 把 DB 的 inflight_scope 数值映射为 GateScope；越界值默认 ScopeCapability（按模型能力并发）。
func scopeFromDB(v int16) GateScope {
	switch GateScope(v) {
	case ScopeAccount, ScopePlatform:
		return GateScope(v)
	default:
		return ScopeCapability
	}
}

// gateKeyOf 依据粒度算出端点归属的并发池 key：
//   - ScopePlatform   -> "platform"
//   - ScopeAccount    -> "platform/account"
//   - ScopeCapability -> "platform/account/capability"
func gateKeyOf(platform, account, capability string, s GateScope) string {
	switch s {
	case ScopePlatform:
		return platform
	case ScopeAccount:
		return platform + "/" + account
	default:
		return platform + "/" + account + "/" + capability
	}
}

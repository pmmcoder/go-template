package ai

import "testing"

func TestGateKeyOf(t *testing.T) {
	cases := []struct {
		scope GateScope
		want  string
	}{
		{ScopeCapability, "chatgpt/azure/gpt-image-2"},
		{ScopeAccount, "chatgpt/azure"},
		{ScopePlatform, "chatgpt"},
	}
	for _, c := range cases {
		if got := gateKeyOf("chatgpt", "azure", "gpt-image-2", c.scope); got != c.want {
			t.Fatalf("gateKeyOf(%v) = %q, want %q", c.scope, got, c.want)
		}
	}
}

func TestScopeFromDB(t *testing.T) {
	cases := []struct {
		in   int16
		want GateScope
	}{
		{0, ScopeCapability},
		{1, ScopeAccount},
		{2, ScopePlatform},
		{-1, ScopeCapability}, // 越界 → 默认 capability
		{9, ScopeCapability},  // 越界 → 默认 capability
	}
	for _, c := range cases {
		if got := scopeFromDB(c.in); got != c.want {
			t.Fatalf("scopeFromDB(%d) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSyncGatesMaxAggregationAndPrune(t *testing.T) {
	// 清空全局，避免与其他用例串扰。
	gateMu.Lock()
	gates = map[string]*Pool{}
	gateMu.Unlock()

	// 账号级池取覆盖行的 max_inflight 最大值；capability 级 ≤0 不建池。
	syncGates(map[string]int{
		"chatgpt/azure":       6, // 例如两行 max_inflight=4/6 聚合后为 6
		"gemini/vertex/g25":   3,
		"gemini/vertex/g3pro": 0, // ≤0 不建池
	})

	if p := gateFor("chatgpt/azure"); p == nil {
		t.Fatal("expected pool for chatgpt/azure")
	} else if _, size := p.GetState(); size != 6 {
		t.Fatalf("chatgpt/azure size = %d, want 6", size)
	}
	if p := gateFor("gemini/vertex/g25"); p == nil || func() int64 { _, s := p.GetState(); return s }() != 3 {
		t.Fatalf("gemini/vertex/g25 pool wrong: %v", p)
	}
	if p := gateFor("gemini/vertex/g3pro"); p != nil {
		t.Fatal("expected no pool for size<=0 gateKey")
	}

	// 扩容 + prune：azure 调到 10，g25 从快照消失应被删除。
	syncGates(map[string]int{"chatgpt/azure": 10})
	if p := gateFor("chatgpt/azure"); p == nil {
		t.Fatal("chatgpt/azure pool missing after resize")
	} else if _, size := p.GetState(); size != 10 {
		t.Fatalf("chatgpt/azure size = %d, want 10 after resize", size)
	}
	if p := gateFor("gemini/vertex/g25"); p != nil {
		t.Fatal("gemini/vertex/g25 should be pruned when absent from snapshot")
	}

	// 复原全局。
	gateMu.Lock()
	gates = map[string]*Pool{}
	gateMu.Unlock()
}

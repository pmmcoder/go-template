package ai

import (
	"context"
	"errors"
	"my_project/internal/infrastructure/external/ai/constants"
	"testing"
	"time"
)

type testModel struct {
	PlatformName   string
	AccountName    string
	CapabilityName string
	block          bool // true: 阻塞到 ctx 结束，模拟忽略 ctx 的慢调用
}

func (t testModel) Platform() string { return t.PlatformName }
func (t testModel) Account() string  { return t.AccountName }

func (t testModel) Capability() string { return t.CapabilityName }

func (t testModel) Generate(ctx context.Context, _ map[string]interface{}) (constants.SchedulerResp, error) {
	if t.block {
		<-ctx.Done()
		return constants.SchedulerResp{}, ctx.Err()
	}
	return constants.SchedulerResp{}, nil
}

// pickWeighted 仅测试用：生产调度走 weightedIndex，这里保留以验证加权选择逻辑。
func pickWeighted(eps []Endpoint) *Endpoint {
	i := weightedIndex(eps)
	if i < 0 {
		return nil
	}
	return &eps[i]
}

func TestPickWeighted(t *testing.T) {
	// 空集合 / 零权重 → nil。
	if pickWeighted(nil) != nil {
		t.Fatal("expected nil for empty endpoints")
	}
	if pickWeighted([]Endpoint{{Weight: 0}}) != nil {
		t.Fatal("expected nil when total weight is 0")
	}

	// 单端点必命中。
	only := []Endpoint{{Platform: "p", Account: "a", Capability: "c", Weight: 3}}
	for i := 0; i < 20; i++ {
		if ep := pickWeighted(only); ep == nil || ep.Platform != "p" {
			t.Fatalf("single endpoint not picked: %+v", ep)
		}
	}

	// 多端点：每个正权重端点都应可能被选中（覆盖加权遍历分支）。
	eps := []Endpoint{
		{Account: "a", Weight: 1},
		{Account: "b", Weight: 1},
		{Account: "c", Weight: 1},
	}
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		ep := pickWeighted(eps)
		if ep == nil {
			t.Fatal("unexpected nil pick")
		}
		seen[ep.Account] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected all 3 accounts to be picked, saw %v", seen)
	}
}

func TestTimeoutBoundsFromExtra(t *testing.T) {
	// 缺失 → 默认值、非动态。
	min, max, dyn := timeoutBoundsFromExtra(nil)
	if min != dynTimeoutDefaultMin || max != dynTimeoutDefaultMax || dyn {
		t.Fatalf("defaults = (%v,%v,%v)", min, max, dyn)
	}
	// 完整配置。
	min, max, dyn = timeoutBoundsFromExtra([]byte(`{"is_dynamic_timeout":true,"min_timeout_seconds":180,"max_timeout_seconds":600}`))
	if min != 180*time.Second || max != 600*time.Second || !dyn {
		t.Fatalf("parsed = (%v,%v,%v)", min, max, dyn)
	}
	// min>max → 收敛到 max。
	min, max, _ = timeoutBoundsFromExtra([]byte(`{"min_timeout_seconds":900,"max_timeout_seconds":300}`))
	if min != max || max != 300*time.Second {
		t.Fatalf("clamp min>max = (%v,%v)", min, max)
	}
}

func TestCallWithTimeoutEarlyReturn(t *testing.T) {
	// 旧式 ModelFunc 忽略 ctx、阻塞很久：callWithTimeout 应在超时后提前返回。
	m := testModel{
		PlatformName: "p", AccountName: "a", CapabilityName: "c", block: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := callWithTimeout(ctx, m, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// 关键：超时错误须能被 errors.Is 识别为 DeadlineExceeded（dispatch 据此分流到 recordTimeout）。
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout err %v not matched by errors.Is(DeadlineExceeded)", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("callWithTimeout did not return early on timeout")
	}
}

func TestCallWithTimeoutSuccess(t *testing.T) {
	m := testModel{
		PlatformName: "p", AccountName: "a", CapabilityName: "c",
	}
	_, err := callWithTimeout(context.Background(), m, nil)
	if err != nil {
		t.Fatalf("callWithTimeout = (%v), want (ok,nil)", err)
	}
}

func TestTimeoutForNonDynamicUsesMax(t *testing.T) {
	ep := Endpoint{MinTimeout: time.Minute, MaxTimeout: 10 * time.Minute, Dynamic: false}
	if got := timeoutFor(ep, newStatWindow()); got != ep.MaxTimeout {
		t.Fatalf("non-dynamic timeout = %v, want %v", got, ep.MaxTimeout)
	}
}

func TestTimeoutForDynamicClampAndSamples(t *testing.T) {
	ep := Endpoint{MinTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute, Dynamic: true}
	w := newStatWindow()

	// 样本不足 → 回退到 max。
	for i := 0; i < dynTimeoutMinSamples-1; i++ {
		w.recordSuccess(time.Second)
	}
	if got := timeoutFor(ep, w); got != ep.MaxTimeout {
		t.Fatalf("insufficient samples timeout = %v, want max %v", got, ep.MaxTimeout)
	}

	// 足量的小耗时样本 → p95*2 很小，被 MinTimeout 兜底。
	w = newStatWindow()
	for i := 0; i < dynTimeoutMinSamples; i++ {
		w.recordSuccess(3 * time.Second)
	}
	if got := timeoutFor(ep, w); got != ep.MinTimeout {
		t.Fatalf("small samples timeout = %v, want min %v", got, ep.MinTimeout)
	}

	// 足量的大耗时样本 → 被 MaxTimeout 封顶。
	w = newStatWindow()
	for i := 0; i < dynTimeoutMinSamples; i++ {
		w.recordSuccess(30 * time.Minute)
	}
	if got := timeoutFor(ep, w); got != ep.MaxTimeout {
		t.Fatalf("large samples timeout = %v, want max %v", got, ep.MaxTimeout)
	}
}

func TestTimeoutForWindowedFailuresEscalatesToMax(t *testing.T) {
	ep := Endpoint{MinTimeout: 2 * time.Minute, MaxTimeout: 10 * time.Minute, Dynamic: true}
	w := newStatWindow()

	// 先灌满快成功样本 → 动态超时压到 min（这些成功也占失败窗口的非超时槽）。
	for i := 0; i < dynTimeoutMinSamples; i++ {
		w.recordSuccess(3 * time.Second)
	}
	if got := timeoutFor(ep, w); got != ep.MinTimeout {
		t.Fatalf("baseline timeout = %v, want min %v", got, ep.MinTimeout)
	}

	// 窗口内超时失败数未超过阈值 → 仍按样本（min）。
	for i := 0; i < dynTimeoutFailureThreshold; i++ {
		w.recordTimeout()
	}
	if got := timeoutFor(ep, w); got != ep.MinTimeout {
		t.Fatalf("at-threshold timeout = %v, want min %v (需 > 阈值)", got, ep.MinTimeout)
	}

	// 再来一次，超时失败数 > 阈值 → 给 max。
	w.recordTimeout()
	if got := timeoutFor(ep, w); got != ep.MaxTimeout {
		t.Fatalf("above-threshold timeout = %v, want max %v", got, ep.MaxTimeout)
	}

	// 恢复：端点重新出成功，逐步挤出旧的超时失败并补足 p95 样本 → 回落到 min。
	// （合并为单一窗口后，成功记录同时承担"降低失败数"与"提供 p95 样本"两件事。）
	for i := 0; i < dynTimeoutWindow; i++ {
		w.recordSuccess(3 * time.Second)
	}
	if got := timeoutFor(ep, w); got != ep.MinTimeout {
		t.Fatalf("after success recovery timeout = %v, want min %v", got, ep.MinTimeout)
	}
}

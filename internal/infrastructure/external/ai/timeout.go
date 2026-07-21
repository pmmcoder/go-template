package ai

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// 动态超时相关配置（scheduler 自实现，不依赖 queue 侧）。
const (
	// dynTimeoutWindow 单一调用窗口容量：保留最近多少次调用（成功算 p95 + 统计超时失败）。
	dynTimeoutWindow = 50
	// dynTimeoutMinSamples 窗口内成功样本不足此值时不启用动态超时，回退到 max。
	dynTimeoutMinSamples = 10
	// dynTimeoutFailureThreshold 窗口内超时失败数 > 此值 → 超时直接给 max
	dynTimeoutFailureThreshold = 20
	// dynTimeoutMultiplier 超时 = p95 * 该乘子（放大以降低误杀慢任务的概率）。
	dynTimeoutMultiplier = 2.0
	// dynTimeoutDefaultMin/Max 是超时上下限默认值，可被端点 extra_params 覆盖。
	dynTimeoutDefaultMin = 3 * time.Minute
	dynTimeoutDefaultMax = 30 * time.Minute
	// dynTimeoutCacheTTL 统计结果缓存有效期，期间不重复排序计算。
	dynTimeoutCacheTTL = 60 * time.Second

	// extra_params 中的超时字段（单位：秒）。
	extraParamMinTimeoutSec    = "min_timeout_seconds"
	extraParamMaxTimeoutSec    = "max_timeout_seconds"
	extraParamIsDynamicTimeout = "is_dynamic_timeout"
)

// timeoutBoundsFromExtra 从端点 extra_params 解析超时边界；缺失/非法用默认值。
func timeoutBoundsFromExtra(raw []byte) (min, max time.Duration, dynamic bool) {
	min, max, dynamic = dynTimeoutDefaultMin, dynTimeoutDefaultMax, false
	if len(raw) == 0 {
		return
	}
	var extra map[string]interface{}
	if err := json.Unmarshal(raw, &extra); err != nil {
		return
	}
	if v, ok := parsePositiveSeconds(extra[extraParamMinTimeoutSec]); ok {
		min = v
	}
	if v, ok := parsePositiveSeconds(extra[extraParamMaxTimeoutSec]); ok {
		max = v
	}
	if b, ok := extra[extraParamIsDynamicTimeout].(bool); ok {
		dynamic = b
	}
	if min > max {
		min = max
	}
	return
}

// parsePositiveSeconds 将 JSON number 秒数转为 Duration，仅正数有效。
func parsePositiveSeconds(v interface{}) (time.Duration, bool) {
	f, ok := v.(float64)
	if !ok || f <= 0 {
		return 0, false
	}
	return time.Duration(f * float64(time.Second)), true
}

// outcomeKind 一次调用的结果类型。
type outcomeKind uint8

const (
	outcomeSuccess outcomeKind = iota // 成功
	outcomeTimeout                    // 超时失败（与超时松紧相关，计入失败升级）
	outcomeError                      // 非超时失败（占窗口槽但不计为超时失败）
)

// callRecord 一次调用的记录：结果类型 + 耗时（成功时用于 p95） + 完成时刻（备时间窗/诊断）。
type callRecord struct {
	kind outcomeKind
	dur  time.Duration
	at   time.Time
}

// statWindow 是单个端点最近若干次调用的统一环形窗口：
// 由同一串记录派生 p95（成功耗时）与超时失败数，单一数据源、单锁。
type statWindow struct {
	mu   sync.Mutex
	ring []callRecord // 环形缓冲，容量 dynTimeoutWindow
	next int

	// 增量维护的计数，避免每次遍历全窗口。
	nSuccess int
	nTimeout int

	cachedP95 float64
	cachedMax float64
	cachedN   int
	cachedAt  time.Time
}

func newStatWindow() *statWindow {
	return &statWindow{ring: make([]callRecord, 0, dynTimeoutWindow)}
}

// push 压入一条调用记录，增量维护计数；成功集合变化时失效 p95 缓存。调用者须持锁。
func (w *statWindow) push(rec callRecord) {
	var evicted callRecord
	full := len(w.ring) >= dynTimeoutWindow
	if full {
		evicted = w.ring[w.next]
		w.ring[w.next] = rec
		w.next = (w.next + 1) % dynTimeoutWindow
	} else {
		w.ring = append(w.ring, rec)
	}

	// 维护计数
	switch rec.kind {
	case outcomeSuccess:
		w.nSuccess++
	case outcomeTimeout:
		w.nTimeout++
	}
	if full {
		switch evicted.kind {
		case outcomeSuccess:
			w.nSuccess--
		case outcomeTimeout:
			w.nTimeout--
		}
	}

	// 成功集合有增减 → p95 需重算
	if rec.kind == outcomeSuccess || (full && evicted.kind == outcomeSuccess) {
		w.cachedAt = time.Time{}
	}
}

// recordSuccess 记录一次成功（耗时进 p95）。
func (w *statWindow) recordSuccess(d time.Duration) {
	w.mu.Lock()
	w.push(callRecord{kind: outcomeSuccess, dur: d, at: time.Now()})
	w.mu.Unlock()
}

// recordTimeout 记录一次超时失败。
func (w *statWindow) recordTimeout() {
	w.mu.Lock()
	w.push(callRecord{kind: outcomeTimeout, at: time.Now()})
	w.mu.Unlock()
}

// recordError 记录一次非超时失败（占窗口槽，不计为超时失败）。
func (w *statWindow) recordError() {
	w.mu.Lock()
	w.push(callRecord{kind: outcomeError, at: time.Now()})
	w.mu.Unlock()
}

// failures 返回窗口内的超时失败条数。
func (w *statWindow) failures() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nTimeout
}

// stats 返回窗口内成功样本数、p95、max（秒），dynTimeoutCacheTTL 内走缓存。
func (w *statWindow) stats() (n int, p95, max float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.cachedAt.IsZero() && time.Since(w.cachedAt) < dynTimeoutCacheTTL {
		return w.cachedN, w.cachedP95, w.cachedMax
	}
	if w.nSuccess == 0 {
		w.cachedN, w.cachedP95, w.cachedMax, w.cachedAt = 0, 0, 0, time.Now()
		return 0, 0, 0
	}
	durs := make([]float64, 0, w.nSuccess)
	for i := range w.ring {
		if w.ring[i].kind == outcomeSuccess {
			durs = append(durs, w.ring[i].dur.Seconds())
		}
	}
	sort.Float64s(durs)
	n = len(durs)
	max = durs[n-1]
	p95 = percentileCont(durs, 0.95)
	w.cachedN, w.cachedP95, w.cachedMax, w.cachedAt = n, p95, max, time.Now()
	return n, p95, max
}

// percentileCont 对已排序切片做连续百分位线性插值（对齐 Postgres PERCENTILE_CONT）。
func percentileCont(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	pos := q * float64(n-1)
	lo := int(pos)
	if lo+1 >= n {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

// timeoutFor 依据端点 bounds + 该端点窗口计算本次超时。
// 窗口内超时失败数 > 阈值 → max（打破单向收敛）；非动态 / 无窗口 / 样本不足 → max；
// 否则取 max(p95*multiplier, 最大耗时) 并 clamp 到 [min,max]。
func timeoutFor(ep Endpoint, w *statWindow) time.Duration {
	if !ep.Dynamic || w == nil {
		return ep.MaxTimeout
	}
	if w.failures() > dynTimeoutFailureThreshold {
		return ep.MaxTimeout
	}
	n, p95, maxSec := w.stats()
	if n < dynTimeoutMinSamples {
		return ep.MaxTimeout
	}
	sec := p95 * dynTimeoutMultiplier
	if maxSec > sec {
		sec = maxSec
	}
	d := time.Duration(sec * float64(time.Second))
	if d < ep.MinTimeout {
		d = ep.MinTimeout
	}
	if d > ep.MaxTimeout {
		d = ep.MaxTimeout
	}
	return d
}

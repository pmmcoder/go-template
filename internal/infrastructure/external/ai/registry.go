package ai

import (
	"context"
	"my_project/internal/domain/scheduler"
	"sync"
	"sync/atomic"
	"time"
)

// cacheTTL 是端点快照的缓存时长；DB 中权重改动在此时长内（≤cacheTTL）生效。
const cacheTTL = 60 * time.Second

var (
	mu       sync.RWMutex
	handlers = map[HandlerKey]Model{}

	snap   atomic.Pointer[snapshot]
	loadMu sync.Mutex

	gateMu sync.Mutex
	gates  = map[string]*Pool{}

	winMu   sync.Mutex
	windows = map[HandlerKey]*statWindow{}
)

type snapshot struct {
	byCap  map[string][]Endpoint
	loaded time.Time
}

// Register 为某个平台账号端点登记一个 Model。必须在 init() 中调用。
func Register(m Model) {
	if m == nil {
		panic("scheduler: Register with nil model")
	}
	key := HandlerKey{Platform: m.Platform(), Account: m.Account(), Capability: m.Capability()}
	if key.Platform == "" || key.Account == "" || key.Capability == "" {
		panic("scheduler: Register with empty key field: " + key.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := handlers[key]; dup {
		panic("scheduler: duplicate handler for " + key.String())
	}
	handlers[key] = m
}

// endpointsFor 返回某能力当前可用的端点集合（副本）。
func endpointsFor(ctx context.Context, repo scheduler.Repository, capability string) ([]Endpoint, error) {
	cur := snap.Load()
	if cur == nil || time.Since(cur.loaded) > cacheTTL {
		ns, err := refresh(ctx, repo)
		if err != nil {
			if cur == nil {
				return nil, err
			}
		} else {
			cur = ns
		}
	}
	src := cur.byCap[capability]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]Endpoint, len(src))
	copy(out, src)
	return out, nil
}

// refresh 重新从 DB 解析快照并原子替换。持 loadMu 并二次检查，避免并发重复查库。
func refresh(ctx context.Context, repo scheduler.Repository) (*snapshot, error) {
	loadMu.Lock()
	defer loadMu.Unlock()
	if cur := snap.Load(); cur != nil && time.Since(cur.loaded) <= cacheTTL {
		return cur, nil // 等锁期间已有他人刷新
	}
	m, err := resolve(ctx, repo)
	if err != nil {
		return nil, err
	}
	ns := &snapshot{byCap: m, loaded: time.Now()}
	snap.Store(ns)
	return ns, nil
}

// resolve 读 DB 启用端点，按 (platform, account, capability) 关联代码侧 Model，
// 并按 DB 的 inflight_scope 粒度（scopeFromDB）把端点归入并发池：
// 同一 gateKey 的多行取 max(max_inflight) 作为该池容量（≤0 视为不限）。
func resolve(ctx context.Context, repo scheduler.Repository) (map[string][]Endpoint, error) {
	rows, err := repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string][]Endpoint)
	gateSize := make(map[string]int)
	for _, r := range rows {
		key := HandlerKey{Platform: r.Platform, Account: r.Account, Capability: r.Capability}
		m, ok := handlers[key]
		if !ok {
			continue
		}
		w := r.Weight
		if w <= 0 {
			w = 1
		}
		minT, maxT, dyn := timeoutBoundsFromExtra(r.ExtraParams)

		gk := gateKeyOf(r.Platform, r.Account, r.Capability, scopeFromDB(r.InflightScope))
		if r.MaxInflight > gateSize[gk] {
			gateSize[gk] = r.MaxInflight
		}

		out[r.Capability] = append(out[r.Capability], Endpoint{
			Platform:    r.Platform,
			Account:     r.Account,
			Capability:  r.Capability,
			Weight:      w,
			MaxInflight: r.MaxInflight,
			MinTimeout:  minT,
			MaxTimeout:  maxT,
			Dynamic:     dyn,
			GateKey:     gk,
			Model:       m,
		})
	}
	syncGates(gateSize)
	return out, nil
}

// syncGates 依本次快照的 gateKey->容量 同步并发闸门：
//   - 容量 >0：无则建、有则调容量（SetSize）。
//   - 容量 ≤0：视为不限并发，移除既有池。
//   - prune：删除本次快照未再出现的 gateKey（如粒度切换/端点下线遗留的旧池）。
func syncGates(gateSize map[string]int) {
	gateMu.Lock()
	defer gateMu.Unlock()
	for gk, size := range gateSize {
		if size <= 0 {
			delete(gates, gk)
			continue
		}
		if p, ok := gates[gk]; ok {
			_ = p.SetSize(int64(size))
			continue
		}
		p, _ := NewPool(PoolOptions{Size: int64(size)})
		gates[gk] = p
	}
	for gk := range gates {
		if _, ok := gateSize[gk]; !ok {
			delete(gates, gk)
		}
	}
}

// gateFor 返回并发池闸门；nil 表示该 gateKey 不限并发。
func gateFor(gateKey string) *Pool {
	gateMu.Lock()
	defer gateMu.Unlock()
	return gates[gateKey]
}

// windowFor 返回端点耗时窗口，按需创建。
func windowFor(key HandlerKey) *statWindow {
	winMu.Lock()
	defer winMu.Unlock()
	w, ok := windows[key]
	if !ok {
		w = newStatWindow()
		windows[key] = w
	}
	return w
}

// recordSuccess 记录一次成功调用的耗时（进 p95 样本 + 失败窗口非超时槽）。
func recordSuccess(key HandlerKey, d time.Duration) {
	windowFor(key).recordSuccess(d)
}

// recordTimeout 记录一次超时失败（进失败窗口的超时槽）。
func recordTimeout(key HandlerKey) {
	windowFor(key).recordTimeout()
}

// recordError 记录一次非超时失败（占失败窗口一个槽，但不计为超时失败）。
func recordError(key HandlerKey) {
	windowFor(key).recordError()
}

# 多平台模型调度器（Scheduler）设计文档

> 目的：记录 `internal/service/scheduler` 的设计思路与关键机制，供在其他项目中按此文档重新实现（不依赖本项目代码）。

## 1. 要解决的问题

系统需要把"生成图片/内容"这类任务，从一批**逻辑能力**（例如 `gpt-image-2`、`gemini-2.5-flash-image`）路由到背后一个或多个**具体的平台账号端点**（例如 chatgpt 走 azure / official / gateway 三个账号，都能生成 `gpt-image-2`）。

多端点存在的原因：
- 同一能力可能有多个供应商/账号可用（多云、多账号分摊限流）。
- 不同端点的**限流额度**、**超时特性**、**成本/优先级（权重）**不同，需要按配置动态调整，而不是写死在代码里。
- 端点的启用/禁用、权重、并发上限要能在运行时通过配置（DB）修改，不能重启进程。

调度器要在这些约束下，对每个任务做三件事：**选端点 → 控并发 → 定超时**，并在调用结束后把结果反馈进统计，用于后续动态超时判断。

## 2. 核心概念与静态模型

### 2.1 两侧世界：代码侧 Model vs 配置侧 Endpoint

调度器把"能调用什么"拆成两个正交的来源，运行时合并：

- **代码侧（Model / Handler）**：谁知道怎么调用。由各平台包在 `init()` 时注册一个实现了 `Generate` 的对象，用三元组 `(platform, account, capability)` 作为身份键（`HandlerKey`）登记进全局表。这部分是"能力"，编译期固定，不依赖任何存储。

  ```go
  type Model interface {
      Platform() string
      Account() string
      Capability() string
      Generate(ctx context.Context, params map[string]interface{}) (interface{}, error)
  }

  type HandlerKey struct {
      Platform, Account, Capability string
  }
  ```

  典型注册方式（每个平台一个 init.go）：

  ```go
  func init() {
      reg := func(account, capability string, fn func(map[string]interface{}) (interface{}, error)) {
          scheduler.Register(scheduler.ModelFunc{
              PlatformName: "chatgpt", AccountName: account, CapabilityName: capability, Fn: fn,
          })
      }
      reg("azure", "gpt-image-1.5", ChatGPTGenerateImageMethod)
      reg("azure", "gpt-image-2", ChatGPTImage2GenerateImageMethod)
      reg("official", "gpt-image-2", ChatGPTImage2OfficialGenerateImageMethod)
      reg("gateway", "gpt-image-2", ChatGPTImage2GatewayGenerateImageMethod)
  }
  ```

  各平台包通过一个 `bundle` 包用空导入（`_ "xxx/chatgpt"`）统一挂载，保证所有 `init()` 都执行、全局注册表在服务启动前就填好。`Register` 对重复 key / 空字段直接 panic —— 这是启动期契约错误，理应快速失败。

- **配置侧（Endpoint 行）**：谁允许被调用、调用限制是什么。存 DB 表（每行是一个 `(platform, account, capability)` 的运行期配置）：

  | 列 | 含义 |
  |---|---|
  | platform / account / capability | 与代码侧 `HandlerKey` 对应的三元组 |
  | weight | 加权随机的权重，≤0 时视为 1 |
  | max_inflight | 该端点所属并发池的容量；≤0 表示不限并发 |
  | inflight_scope | 并发池的粒度（下面 2.2 详述） |
  | enabled | 是否参与调度 |
  | extra_params (jsonb) | 超时相关的可选覆盖参数 |

  DB 只描述限制和权重，**不描述怎么调用**——调用逻辑永远在代码侧的 `Model.Generate` 里。这样运营可以纯改配置调整流量分配/限流，不需要发版。

### 2.2 并发池粒度（GateScope）

一个"并发池"（本文档称为闸门 / gate）限制的是同时在途的调用数。粒度可以按需选择：

```
ScopeCapability (默认): 每 (platform, account, capability) 一个池 —— 按模型能力隔离并发
ScopeAccount:           每 (platform, account) 一个池     —— 账号下所有能力共享并发额度
ScopePlatform:          每 platform 一个池                 —— 平台下所有账号/能力共享并发额度
```

`gateKey` 的计算方式随粒度而定：

```
ScopePlatform   -> platform
ScopeAccount    -> platform + "/" + account
ScopeCapability -> platform + "/" + account + "/" + capability
```

同一个 `gateKey` 下如果有多行配置（比如同一账号被多个 capability 复用），取这些行 `max_inflight` 的**最大值**作为该池容量——避免因为某一行配置偏保守就把整体并发压低。

### 2.3 运行期合并出的 Endpoint

代码侧 Model 与配置侧行按 `(platform, account, capability)` 做内连接（inner join），得到调度器实际使用的结构：

```go
type Endpoint struct {
    Platform, Account, Capability string
    Weight       int
    MaxInflight  int    // ≤0 表示不限并发
    GateKey      string
    MinTimeout, MaxTimeout time.Duration
    Dynamic      bool
    Model        Model  // 代码侧引用，真正被调用的对象
}
```

- DB 里配置了但代码没注册 handler 的行：跳过并打警告日志（配置超前于代码上线时的正常现象，不应崩溃）。
- 代码注册了但 DB 没有配置行 / 未启用 / weight=0 的：该端点不会出现在候选集合里，等价于被禁用。

## 3. 运行期数据流：从任务到结果

```
task { model_capability, ... }
        │
        ▼
 endpointsFor(capability)  ── 从内存快照按 capability 取候选端点列表
        │
        ▼
 acquireEndpoint(candidates) ── 加权随机选端点 + 尝试拿并发槍位，满载则从候选中剔除重选
        │
        ▼
 timeoutFor(endpoint, statWindow) ── 依据历史耗时/失败率决定本次调用超时
        │
        ▼
 callWithTimeout(ctx, model, params) ── 在独立 goroutine 里跑 Generate，ctx 超时或完成谁先到算谁
        │
        ▼
 记录结果到 statWindow（成功耗时 / 超时失败 / 其他失败）─→ 影响未来的动态超时判定
        │
        ▼
 释放并发槍位（defer pool.Release()）
```

### 3.1 端点快照与缓存（避免每个任务都查库）

`endpointsFor(capability)` 不直接查 DB，而是查一个内存快照 `snapshot{ byCap map[capability][]Endpoint, loaded time.Time }`：

- 快照用 `atomic.Pointer` 存，读路径无锁（只有 `CompareAndSwap`/`Store`/`Load`）。
- `cacheTTL`（例如 60s）内直接复用；过期后调用 `refresh()`。
- `refresh()` 用一个 `loadMu` 互斥锁 + "拿到锁后二次检查是否已被其他 goroutine 刷新过"的模式，防止缓存过期瞬间多个并发任务同时打 DB（惊群）。
- 查库失败且已有旧快照时：记错误日志、**继续返回旧快照**（宁可用稍微过期的配置，也不要因为一次 DB 抖动打断所有调度）。只有从未成功加载过快照时才把错误往上抛。
- 每次 `refresh()` 成功后，顺带用本次快照的 `gateKey -> maxInflight` 去同步所有并发池（`syncGates`）：新增池、扩缩容已有池、删除本次快照里已经不存在的 `gateKey` 对应的池（清理粒度切换或端点下线后的残留）。
- 提供一个显式失效入口（如 `ReloadEndpoints()`）用于管理操作/测试，把快照指针清空强制下次重新查库。

设计取舍：这是"最终一致"的配置生效模型——DB 改了权重/并发/开关，最多 `cacheTTL` 之后才在所有调度决策里体现。换取的是绝大多数任务路径完全不用查库。

### 3.2 端点选择：加权随机 + 满载剔除重选

```
avail = copy(candidates)
while avail 非空:
    i = weightedIndex(avail)          # 按 Weight 做加权随机选一个下标
    ep = avail[i]
    pool = gateFor(ep.GateKey)
    if pool == nil:                   # 该端点不限并发
        return ep, nil
    if pool.TryAcquire() 成功:
        return ep, pool
    avail.remove(i)                   # 该端点满载，剔除后在剩余端点里重新加权
返回 "全部端点满载" 错误
```

要点：
- 加权随机而非轮询：权重代表"这个端点应该承担多大比例流量"，用 `rand.IntN(totalWeight)` 落在哪个权重区间决定选谁。总权重为 0（不应发生，因为注册/解析阶段已把 weight≤0 归一到 1）时兜底返回 -1，防止死循环。
- **非阻塞获取**（`TryAcquire`，不是阻塞等待）：一个端点满了立刻换下一个候选端点，而不是排队等它腾出位置——因为还有别的端点可以用，没必要为了保序牺牲吞吐/延迟。
- 每剔除一个端点就重新加权一次（不是简单顺序遍历剩余端点），保证剩余端点之间仍然遵守相对权重比例。
- 所有候选端点都满载 → 明确的"资源饱和"错误，交给上层（队列/重试机制）决定要不要重试或降级，调度器本身不做阻塞等待。

### 3.3 并发池（Pool）的实现要点

Pool 是一个简单的计数器 + 互斥锁 + 条件变量，提供两种取号方式：

- `TryAcquire()`：非阻塞。`cur < size` 才能成功，否则立刻返回"池满"错误。调度器的主路径只用这个。
- `Acquire(ctx)`：阻塞直到有空位或 ctx 结束（本设计里主调度路径不使用，但作为通用能力保留，便于需要"排队等一个稀缺端点"的场景复用这套 Pool）。实现上用一个后台 goroutine 监听 `ctx.Done()`，一旦触发就 `cond.Broadcast()` 唤醒所有等待者重新判断（否则等待者会一直卡在 `cond.Wait()` 上，感知不到 ctx 取消）。
- `Release()`：`cur--` 并 `cond.Signal()` 唤醒一个等待者。
- `SetSize(n)`：动态调整容量。**扩容**时广播唤醒等待者（可能有新空位了）；**缩容**时不影响已经拿到槍位的调用，只是让新的 `Acquire/TryAcquire` 在 `cur` 降到新容量以下之前一直失败——不做"强行收回已发放的令牌"这种破坏性操作。

并发池按 `gateKey` 存一张全局表（`map[string]*Pool`），由 `resolve()`/`syncGates()` 维护生命周期。`gateFor(key)` 返回 `nil` 表示这个 gate 不限并发（未创建池），调用方要能正确处理"没有池 = 不需要 acquire/release"这个分支。

### 3.4 动态超时

目标：给每次调用一个"够用但不过分宽松"的超时时间，且能随端点实际表现自适应，而不是所有端点用同一个固定超时。

**每个端点各自维护一个环形统计窗口**（`statWindow`，key 是 `HandlerKey`），容量固定（例如最近 50 次调用）：

- 记录三类结果：`成功(耗时)` / `超时失败` / `其他失败`。
- 增量维护 `成功样本数` 和 `超时失败数`（进出环形缓冲时 O(1) 更新），避免每次都要重新扫全窗口去计数。
- p95（以及窗口内最大值）走短 TTL 缓存（例如 60s），因为排序计算相对贵，而这个统计不需要每次调用都精确到毫秒级实时。

**计算某次调用超时**（`timeoutFor`）：

```
if 端点未开启动态超时 or 该端点还没有统计窗口:
    return 端点配置的 MaxTimeout

if 窗口内超时失败数 > 失败阈值:
    return MaxTimeout        # 打破"超时太短→更多超时失败→但没被纳入判断"的单向恶化

n, p95, maxSeen = 窗口统计(成功样本数, p95 耗时, 观测到的最大耗时)
if n < 最小样本数阈值:
    return MaxTimeout        # 数据不够，不敢激进

超时 = max(p95 * 放大倍数, maxSeen)
clamp 到 [端点的 MinTimeout, 端点的 MaxTimeout]
return 超时
```

设计取舍说明：
- **只用成功调用的耗时算 p95**，失败/超时的耗时不计入（数据不可信——超时失败的"耗时"本身就是超时阈值，不代表真实处理时间）。
- **超时失败**单独计数并作为"熔断"信号：如果最近窗口里超时失败偏多，说明当前算出来的动态超时可能偏紧，直接退回到保守的 `MaxTimeout`，而不是继续用一个已经被证明会导致大量超时的动态值。这是防止"p95 计算出一个偏小的超时 → 大量任务因此提前超时 → 但这些超时耗时不进入 p95 统计 → 系统学不到教训、继续用偏小超时"这个死循环。
- 放大倍数（如 2.0）是安全边际：p95 本身只保证 95% 的历史调用能在这个时间内完成，直接拿 p95 当超时会让至少 5% 的正常慢请求被误杀。
- `MinTimeout`/`MaxTimeout`/是否启用动态超时，都是端点级别可配置项（存在 DB 的 `extra_params` JSON 里，比如 `min_timeout_seconds` / `max_timeout_seconds` / `is_dynamic_timeout`），未配置则用全局默认值。这样"这个端点特别慢，要给更宽的超时"这类调整可以只改配置不改代码。

**记录调用结果**（在超时/成功/其他错误三个分支里分别调用）：
- 成功 → `recordSuccess(key, elapsed)`
- `errors.Is(err, context.DeadlineExceeded)` → `recordTimeout(key)`
- 其他错误 → `recordError(key)`（占用窗口一个槍位但不算作"超时失败"，避免业务错误污染超时判断，同时不让业务错误在窗口里"免费"不占位从而稀释统计意义）

### 3.5 调用执行与 panic 隔离

真正调用 `Model.Generate` 时，用一个独立 goroutine 执行，配合 `select` 在"ctx 超时"和"调用完成"之间取先到者：

```go
done := make(chan outcome, 1)
go func() {
    defer func() {
        if r := recover(); r != nil {
            done <- outcome{nil, fmt.Errorf("model panic: %v", r)}
        }
    }()
    res, err := model.Generate(ctx, params)
    done <- outcome{res, err}
}()

select {
case <-ctx.Done():
    return nil, fmt.Errorf("generate timeout: %w", ctx.Err())
case o := <-done:
    return o.res, o.err
}
```

要点：
- **`recover()` 必须在被启动的 goroutine 内部**，否则第三方 `Model.Generate` 里的一个 panic 会直接打崩整个进程（跨 goroutine 无法 recover）。
- 超时返回后，被超时抛弃的那个 goroutine 仍在后台运行到自然结束（channel 带缓冲为 1，不会阻塞泄漏，但底层实际的第三方调用是否真正取消要看 `Generate` 实现是否遵守 ctx）。这是"胖客户端超时"的常见权衡：调度器只能保证**自己这一侧**及时返回，管不了下游是否真的停止。

## 4. 与外部系统的集成契约

调度器本身不跑事件循环，需要一个**任务执行框架**（本项目里是一个通用队列/worker 系统）来驱动它。要复刻这个设计，宿主系统需要提供：

1. **一个统一的任务分发钩子**：`func(params map[string]interface{}) (interface{}, error)`。调度器把自己包装成这一种函数注册进宿主的某个"共享队列"，所有能力的任务都进这一个队列，由调度器内部根据任务携带的 `model_capability` 字段做二次路由——好处是 worker 池是共享的，不需要为每个 capability 单独配置 worker 数量。
2. **任务参数里必须能取到**：`model_capability`（决定用哪批候选端点）；建议再带一个任务 ID 用于日志追踪。
3. （可选）**任务状态回调/事件总线**：调度器在真正开始调用模型前，可以顺带通知宿主"这个任务进入 processing 状态"，用于给排队中的其它任务更新排队位置预估、或者推送 SSE/WS 给前端。这一步是可选的旁路通知，不影响调度决策本身。

如果新项目的任务框架形态不同（比如是一个 worker pool 直接 poll DB，而不是内存队列），只需保证"调度器暴露一个 `handle(params) (result, error)` 函数供框架调用"这一个契约即可，剩下都是调度器内部逻辑,与队列实现无关。

## 5. 可观测性

调度前后各打一条结构化日志，至少包含：

- 调度前：`capability`、选中的 `platform/account`、`gateKey`、本次算出的 `timeout` 及是否动态、当前窗口的 `p95`/超时失败数/成功样本数、并发池的 `当前占用/容量`。
- 调度后：耗时、成功/超时/错误三种结果之一。

这些字段组合起来，出问题时能直接回答"这次为什么选了这个端点/为什么这个超时/是不是快满载了"，不需要额外埋点。

## 6. 关键设计原则总结（复刻时优先保留）

1. **代码侧能力 与 配置侧限制 分离**：谁会调用 vs 谁被允许调用/调用限制，是两个独立可演进的东西，用三元组 key 内连接。
2. **配置变更走缓存+定期刷新，不做实时推送**：接受几十秒的生效延迟，换取调用路径零 DB 依赖；DB 抖动时优雅退化为"用旧配置"而不是报错。
3. **并发限制用非阻塞 TryAcquire + 端点间重新加权剔除**，而不是排队等待——同一能力有多个端点时，宁可换一个端点也不要阻塞等待，把"等待"这个决策留给调用方/队列层。
4. **超时自适应但要有熔断**：只用成功样本算 p95，超时失败单独计数并在超标时强制回退到保守超时，避免"超时太紧→更多超时→更学不到"的正反馈循环。
5. **缩容不影响已发放的资源**，只影响新请求——避免"运营调小并发配置"这个动作打断正在跑的任务。
6. **第三方调用必须 goroutine 级 panic 隔离**，否则一个模型 SDK 的 panic 能打崩整个调度器进程。
7. **配置与代码不匹配时降级而不是失败**：DB 有配置但代码没注册 → 跳过并警告；代码注册了但 DB 没配置/未启用 → 该端点视为不存在。只有代码侧登记本身冲突（重复 key/空字段）才应该在启动期直接 panic。

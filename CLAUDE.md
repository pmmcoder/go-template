# CLAUDE.md

Go DDD + 整洁架构项目。本文件是所有迭代的**主框架和约束**，先读它再动代码。补充细节看 [README.md](README.md)。

---

## 1. 项目定位

一个多进程 Go 服务：
- [cmd/api](cmd/api/main.go) — HTTP API（gin），接收任务 + 查询
- [cmd/worker](cmd/worker/main.go) — 队列消费者（当前为空壳，逻辑与 api 共用同一 binary，见 [main.go:82-100](cmd/api/main.go#L82-L100)）
- [cmd/migrate](cmd/migrate/main.go) — 数据库迁移 CLI（golang-migrate）

核心域是 **AI 任务分发**：外部提交 → 入库 → 入队 → worker 拉起 → 调用 AI provider → 落结果。业务领域按需扩展（`task` 已有，未来 `user`/`rule`/`settlement` 见 README 目录规划）。

**技术栈**：Go 1.25 · gin · GORM + PostgreSQL · River queue（PG-backed）· viper · slog + lumberjack · robfig/cron。

---

## 2. 分层与依赖方向（**不可逆**）

```
adapters ──► application ──► domain ◄── infrastructure
                                ▲              ▲
                                └── seedwork ──┘
                                       ▲
                                      pkg (横切)
```

| 层 | 目录 | 职责 | 允许依赖 |
|---|---|---|---|
| **domain** | [internal/domain/](internal/domain/) | 聚合、值对象、仓储/网关接口、领域事件 | 仅 `seedwork/` + 标准库 |
| **application** | [internal/application/](internal/application/) | 用例编排、CQRS command/query、DTO | domain + seedwork + `infrastructure/queue`（作为技术能力接口）|
| **adapters** | [internal/adapters/](internal/adapters/) | HTTP/gRPC 入口、DTO 转换、中间件 | application + domain（读类型） |
| **infrastructure** | [internal/infrastructure/](internal/infrastructure/) | GORM、river、redis、AI vendor、cron | domain（实现接口）+ pkg |
| **seedwork** | [internal/seedwork/](internal/seedwork/) | DDD 基类（聚合根、事件、ID 生成） | 仅标准库 |
| **pkg** | [pkg/](pkg/) | 横切关注点（config/logger/utils/concurrency） | 仅标准库 + 第三方库，**禁止依赖 internal** |

**硬约束**：
- `domain/` **绝不引入** GORM、redis、gin、river、http 等技术包。仓储接口在这里，实现在 `infrastructure/`。
- `pkg/` 是可对外复用的通用库；业务相关认证/鉴权流程放 `application/` 或 `adapters/http/middleware/`。
- 领域按需展开：简单 CRUD 领域只写 `entity + repository`；复杂领域才加 `service + event`。参考 [internal/domain/task](internal/domain/task/) —— 目前只用 `task.go`（含 entity 与 State）+ `repository.go` + `ai_provider.go`（外部端口）。

---

## 3. 关键约定（写代码前必看）

### 3.1 聚合根封装
参考 [task.Task](internal/domain/task/task.go):
- 字段全**小写**私有，外部只通过构造函数（`NewAITask`）+ `Mark*` 方法访问。
- **`Snapshot()`/`Restore()` 双向映射** 是聚合与持久化的唯一接口。业务代码禁止直接读 `State`。
- `SetID` 仅供 repo 在 `INSERT RETURNING` 之后回填。

### 3.2 CQRS 与用例编排
参考 [application/task/service.go](internal/application/task/service.go):
- 每个用例一个方法（`Submit` / `Get` / `HandleAITask`），入参/出参用**独立 DTO**（`SubmitInput` / `View`），不泄漏聚合类型给上层。
- application 层可以同时被 HTTP handler（`Submit`）和 worker 回调（`HandleAITask`）复用 —— 用例是"业务动作"，不绑定入口。

### 3.3 HTTP 路由装配
参考 [adapters/http/router.go](internal/adapters/http/router.go):
- `New(Services)` 是唯一装配点。新增聚合 = `Services` 加字段 + 一行 `Register(r, svcs.X)`。
- 每个聚合在 [adapters/http/<agg>/routes.go](internal/adapters/http/task/routes.go) 提供 `Register(r gin.IRouter, svc)`；handler 和 DTO 都在同包内。
- HTTP DTO 与 application DTO **独立**（HTTP 层的 `submitRequest` ≠ application 的 `SubmitInput`），转换在 handler 里做。

### 3.4 错误处理
- domain 定义哨兵错误（如 [task.ErrNotFound](internal/domain/task/repository.go)）。
- infrastructure 用 `fmt.Errorf("layer: op: %w", err)` 包裹底层错误。
- adapter 层用 `errors.Is` 判断哨兵，映射到 HTTP 状态码。
- AI vendor 错误统一映射到 [task.AIError](internal/domain/task/ai_provider.go) + `AIErrorCode`（`invalid_request` / `auth` / `rate_limited` / `timeout` / `server` / `content_filter`），供 scheduler 判断可重试性。

### 3.5 外部端口 + 自注册
参考 [infrastructure/external/ai/](internal/infrastructure/external/ai/):
- domain 定义 `AIProvider` 接口。
- 每个 vendor 在 `ai/<vendor>/xxx.go` 里 `init()` 调 `ai.Register("name", buildFn)`。
- 新增 vendor 只需：新增子包 + 让 `bundle` 包空导入即可，禁止修改 registry 代码（开闭原则）。
- 上层通过 [ai.NewFromConfig](internal/infrastructure/external/ai/setup.go) 从 `config.AIConfig.Endpoints` 装配 Scheduler，Scheduler 也实现 `task.AIProvider`，对 application 透明。

### 3.6 队列使用（facade 模式）
参考 [infrastructure/queue/queue.go](internal/infrastructure/queue/queue.go):
- 全局单例 + 包级函数：`queue.Init` → `queue.RegisterHandler` → `queue.Submit` / `queue.Start`。
- 抽象接口在 [queue/contract/queue.go](internal/infrastructure/queue/contract/queue.go)，River 实现在 [queue/riverqueue/](internal/infrastructure/queue/riverqueue/)。application 只依赖 `queue` facade 或 `contract` 接口。
- 任务类型常量集中在 `queue.TaskTypeAi` 等。新增任务类型 = 加常量 + application 里 `RegisterHandler`。
- **周期任务**走独立 `CronWorker`（kind=`cron`），与普通任务解耦，见 `RegisterCronHandler` / `SubmitCron`。

### 3.7 持久化模型
参考 [persistence/models/task.go](internal/infrastructure/persistence/models/task.go):
- GORM model 与 domain 聚合**分离**，字段服务于 DB schema。
- 映射代码只写在 repo 里（`Snapshot → model` / `model → State → Restore`）。
- JSON 字段（如 `Messages`）在 model 里用 `[]byte`，编解码在 repo 里做，避免依赖 `datatypes` 包。

### 3.8 配置
- [pkg/config](pkg/config/config.go) 用 viper + mapstructure；从 `configs/config.<env>.yaml` 加载。
- 环境由 `APP_ENV` 环境变量选择（`dev`/`test`/`prod`），默认 `dev`。
- `ROOT_PATH` 环境变量可覆盖 `configs/` 的搜索根路径。
- 全局 `config.MConfig` 在 `main` 里 `Init` 一次。
- vendor 私有配置走 `EndpointConfig.Config json.RawMessage`，由各 vendor Builder 自己反序列化 —— 保持 config 包对 vendor 无感知。

### 3.9 日志
- [pkg/logger](pkg/logger/logger.go) 封装 `log/slog` + lumberjack 轮转。
- 全局单例 `logger.L()`；便捷函数 `logger.Info(msg, slog.String(...))`。
- dev 默认 debug + JSON + AddSource + 文件轮转；prod 是 info + JSON + stdout。
- **不要用 `fmt.Println` / `log.Printf` 打业务日志**。

### 3.10 迁移
- SQL 文件在顶层 [migrations/](migrations/)，命名 `<n>_<name>.up.sql` / `.down.sql`。
- 用 `go run cmd/migrate/main.go -cmd create -name xxx` 生成模板。
- 上线通过 `-cmd up` 应用；不通过 GORM 的 `AutoMigrate`。

---

## 4. 常用命令

```bash
# 环境
export APP_ENV=dev          # 默认 dev，可切 test/prod
export ROOT_PATH=$(pwd)     # 可选：指定 configs/ 根

# 迁移
go run cmd/migrate/main.go -cmd up
go run cmd/migrate/main.go -cmd create -name add_users_table
go run cmd/migrate/main.go -cmd version

# 启动 API + worker（当前为同一 binary）
go run cmd/api/main.go

# 测试
go test ./...                # 全量
go test -race ./...          # 竞态检查（河流队列相关代码强烈建议）
go test ./pkg/... ./internal/domain/... -count=1

# 静态检查
go vet ./...
```

**Makefile 目前是空的**（[Makefile](Makefile)）—— 有共用命令时补进来。

---

## 5. 迭代规则（AI 修改代码时**必须**遵循）

### 5.1 修改前
1. 先读本文件的分层与约定，确认目标代码属于哪一层。
2. 简单改动直接开工；结构性改动（新增聚合、跨层调用、修接口）必须先 `EnterPlanMode` 出方案。
3. **不要跨层引入依赖**。若发现 domain 里出现 `gorm/redis/gin` 等 import，那是 bug，先修 bug 再谈功能。

### 5.2 修改中
- 新增聚合：`domain/<agg>/` → `application/<agg>/` → `infrastructure/persistence/gorm/<agg>_repo.go` + `models/<agg>.go` → `adapters/http/<agg>/routes.go` → 在 `adapters/http/router.go` 的 `Services` 加字段 + 一行 `Register`。
- 新增队列 handler：在 [queue/queue.go](internal/infrastructure/queue/queue.go) 加 `TaskType*` 常量 → application 里定义 `HandleXxx` → main 里 `queue.RegisterHandler(TaskTypeXxx, svc.HandleXxx)`。
- 新增 AI vendor：`infrastructure/external/ai/<vendor>/<vendor>.go` + `init(){ai.Register(...)}` → `ai/bundle/bundle.go` 空导入。
- 新增配置字段：先加到 `pkg/config/config.go` 结构，同步更新 `configs/config.*.yaml` 三个环境文件。

### 5.3 修改后
- 编译：`go build ./...`
- 竞态：`go test -race ./...`（涉及 queue/scheduler/pool 时**必跑**）
- 手动验证：涉及 HTTP 起 `go run cmd/api/main.go` + curl；涉及 worker 提交任务观察日志。
- 别自己声称"完成"：跑一次相关 test 再说。

### 5.4 禁止事项
- 禁止在领域层（`domain/`）引入任何第三方库（`gorm`、`gin`、`river` 等）。
- 禁止在 `pkg/` 里放业务逻辑（业务在 `internal/`）。
- 禁止绕过 `Snapshot/Restore` 直接读写聚合内部字段。
- 禁止在多处硬编码同一个字符串常量（如 taskType、queue name）—— 集中在一个包里。
- 禁止 `fmt.Println` 打日志；禁止 `panic` 用于非启动阶段。
- 禁止新建文档文件（`.md`）除非用户明确要求。

---

## 6. 编码风格

- 包注释在每个包的第一个 `.go` 顶部写 `// Package xxx <一句话职责>`。
- 导出符号必须有注释（`golint` 风格）；一句话足够，别写小作文。
- 错误消息小写、不带标点，`fmt.Errorf("layer: op: %w", err)`。
- 结构体字段有对齐要求（`gorm:"..."` / `json:"..."` / `mapstructure:"..."`），照抄现有文件的风格即可。
- 中文注释可以，但**不要中英夹杂**同一句话。
- **只在 WHY 非显然时**写注释（隐藏约束、workaround、跨层契约）；不要解释 WHAT。

---

## 7. 目录速查表

| 想找… | 去看… |
|---|---|
| 服务启动装配 | [cmd/api/main.go](cmd/api/main.go) |
| 领域聚合定义 | [internal/domain/task/task.go](internal/domain/task/task.go) |
| 用例编排 | [internal/application/task/service.go](internal/application/task/service.go) |
| HTTP 路由入口 | [internal/adapters/http/router.go](internal/adapters/http/router.go) |
| GORM 仓储实现 | [internal/infrastructure/persistence/gorm/task_repo.go](internal/infrastructure/persistence/gorm/task_repo.go) |
| 队列 facade | [internal/infrastructure/queue/queue.go](internal/infrastructure/queue/queue.go) |
| AI Scheduler | [internal/infrastructure/external/ai/scheduler.go](internal/infrastructure/external/ai/scheduler.go) |
| 配置结构 | [pkg/config/config.go](pkg/config/config.go) |
| 日志封装 | [pkg/logger/logger.go](pkg/logger/logger.go) |
| 环境变量 | [pkg/utils/env.go](pkg/utils/env.go) |
| DB schema | [migrations/1_create_ai_tasks.up.sql](migrations/1_create_ai_tasks.up.sql) |
| 目录规划全景 | [README.md](README.md) |

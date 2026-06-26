> Go DDD + 整洁架构（Clean Architecture）目录结构
> 依赖方向：`adapters → application → domain ← infrastructure`，箭头不可逆
> 领域层零外部依赖；横切关注点放 `pkg/`，DDD 基类放 `seedwork/`

```
my_project/
├── cmd/                            # 应用入口（每个子目录编译为独立二进制）
│   ├── api/
│   │   └── main.go                 # HTTP/gRPC API 服务
│   ├── worker/
│   │   └── main.go                 # 队列消费 Worker
│   └── migrate/
│       └── main.go                 # 数据库迁移 CLI
│
├── internal/                       # 私有代码（Go 编译器强制隔离）
│   ├── domain/                     # 领域层（按需展开，不强制五件套）
│   │   ├── user/                   # 用户领域
│   │   │   ├── entity.go           # User, UserProfile
│   │   │   ├── value_object.go     # UserID, Email, Password
│   │   │   ├── repository.go       # 仓储接口（实现在 infrastructure）
│   │   │   ├── service.go          # 领域服务（跨实体逻辑）
│   │   │   └── event.go            # 领域事件
│   │   │
│   │   ├── task/                   # 任务领域
│   │   │   ├── entity.go           # Task, TaskResult
│   │   │   ├── value_object.go     # TaskID, TaskStatus
│   │   │   ├── repository.go
│   │   │   ├── service.go
│   │   │   └── event.go
│   │   │
│   │   ├── statistic/              # 统计领域（CRUD 较多，按需省略 service/event）
│   │   │   ├── entity.go           # Statistic, Metric
│   │   │   ├── value_object.go     # TimeRange, MetricType
│   │   │   └── repository.go
│   │   │
│   │   ├── rule/                   # 规则引擎领域
│   │   │   ├── entity.go           # Rule, RuleSet
│   │   │   ├── value_object.go     # RuleCondition, RuleAction
│   │   │   ├── repository.go
│   │   │   └── service.go          # 规则匹配/执行核心算法
│   │   │
│   │   ├── settlement/             # 结算领域
│   │   │   ├── entity.go           # Settlement, Payout
│   │   │   ├── value_object.go     # Amount, SettlementStatus
│   │   │   ├── repository.go
│   │   │   ├── service.go
│   │   │   └── event.go
│   │   │
│   │   └── tga/                    # TGA 集成领域（端口与适配器）
│   │       ├── entity.go           # TGAAccount, TGAEvent
│   │       ├── repository.go
│   │       └── gateway.go          # 只放接口，client 在 infrastructure
│   │
│   ├── application/                # 应用层（用例编排，CQRS）
│   │   ├── user/
│   │   │   ├── command.go          # CreateUserCmd, UpdateUserCmd
│   │   │   ├── query.go            # GetUserQuery, ListUsersQuery
│   │   │   ├── command_handler.go  # 写操作处理器
│   │   │   ├── query_handler.go    # 读操作处理器
│   │   │   └── dto.go              # 跨层数据对象
│   │   │
│   │   ├── task/
│   │   │   ├── command.go
│   │   │   ├── query.go
│   │   │   ├── command_handler.go
│   │   │   ├── query_handler.go
│   │   │   └── scheduler.go        # 任务调度编排
│   │   │
│   │   ├── statistic/
│   │   │   ├── query.go
│   │   │   ├── query_handler.go
│   │   │   └── analytics_service.go
│   │   │
│   │   ├── rule/
│   │   │   ├── command.go
│   │   │   ├── query.go
│   │   │   └── rule_executor.go    # 编排执行（算法在 domain/rule/service.go）
│   │   │
│   │   └── settlement/
│   │       ├── command.go
│   │       ├── query.go
│   │       └── settlement_processor.go
│   │
│   ├── infrastructure/             # 基础设施层（技术实现）
│   │   ├── persistence/
│   │   │   ├── gorm/
│   │   │   │   ├── db.go           # 数据库连接
│   │   │   │   ├── user_repo.go    # 实现 domain/user.Repository
│   │   │   │   ├── task_repo.go
│   │   │   │   ├── statistic_repo.go
│   │   │   │   ├── rule_repo.go
│   │   │   │   └── settlement_repo.go
│   │   │   └── models/             # GORM 数据模型（与领域模型转换）
│   │   │       ├── user.go
│   │   │       ├── task.go
│   │   │       ├── statistic.go
│   │   │       ├── rule.go
│   │   │       └── settlement.go
│   │   │
│   │   ├── queue/                  # 消息队列
│   │   │   └── river/
│   │   │       ├── client.go       # River 客户端
│   │   │       ├── task_worker.go
│   │   │       ├── settlement_worker.go
│   │   │       └── statistic_worker.go
│   │   │
│   │   ├── cache/                  # 缓存
│   │   │   └── redis/
│   │   │       ├── client.go
│   │   │       ├── user_cache.go
│   │   │       └── session_cache.go
│   │   │
│   │   ├── scheduler/              # 定时任务
│   │   │   └── cron/
│   │   │       ├── scheduler.go
│   │   │       ├── daily_report.go
│   │   │       ├── data_sync.go
│   │   │       └── clean_expired.go
│   │   │
│   │   └── external/               # 外部服务集成
│   │       ├── tga/
│   │       │   ├── client.go       # 实现 domain/tga.Gateway
│   │       │   └── dto.go
│   │       └── datakit/
│   │           ├── client.go
│   │           └── csv_processor.go
│   │
│   ├── adapters/                   # 接口适配器（端口入口，原 interfaces/）
│   │   ├── http/
│   │   │   ├── handlers/
│   │   │   │   ├── user_handler.go
│   │   │   │   ├── task_handler.go
│   │   │   │   ├── statistic_handler.go
│   │   │   │   ├── rule_handler.go
│   │   │   │   └── settlement_handler.go
│   │   │   ├── middleware/
│   │   │   │   ├── auth.go
│   │   │   │   ├── cors.go
│   │   │   │   ├── logger.go
│   │   │   │   ├── recovery.go
│   │   │   │   └── rate_limit.go
│   │   │   ├── dto/                # 请求/响应结构
│   │   │   │   ├── user_dto.go
│   │   │   │   ├── task_dto.go
│   │   │   │   └── response.go     # 统一响应格式
│   │   │   └── router.go           # 路由注册
│   │   │
│   │   ├── grpc/
│   │   │   ├── proto/              # .proto 定义
│   │   │   │   ├── user.proto
│   │   │   │   ├── task.proto
│   │   │   │   └── statistic.proto
│   │   │   ├── generated/          # protoc 生成的代码
│   │   │   └── server/
│   │   │       ├── user_server.go
│   │   │       ├── task_server.go
│   │   │       └── statistic_server.go
│   │   │
│   │   └── consumer/               # 队列消费者（订阅领域事件）
│   │       ├── task_consumer.go
│   │       └── settlement_consumer.go
│   │
│   └── seedwork/                   # DDD 基类（原 shared/，惯例命名）
│       ├── event/
│       │   ├── event.go            # 领域事件基类
│       │   └── dispatcher.go       # 事件分发器
│       ├── aggregate/
│       │   └── root.go             # 聚合根基类
│       ├── specification/          # 规格模式（按需）
│       └── identifier/
│           └── id.go               # 统一 ID 生成器
│
├── pkg/                            # 公开库（横切关注点，可对外复用）
│   ├── config/                     # 配置解析逻辑
│   ├── logger/                     # 日志封装
│   ├── auth/                       # 认证技术能力（JWT、密码哈希）
│   ├── errors/                     # 错误类型与包装
│   └── validator/                  # 通用校验器
│
├── migrations/                     # 数据库迁移（顶层，golang-migrate 惯例）
│   ├── 001_create_users_table.up.sql
│   ├── 001_create_users_table.down.sql
│   ├── 002_create_tasks_table.up.sql
│   └── ...
│
├── configs/                        # 配置文件（仅数据，解析逻辑在 pkg/config）
│   ├── config.dev.yaml
│   ├── config.prod.yaml
│   └── config.example.yaml
│
├── scripts/                        # 构建/部署脚本
│   ├── build.sh
│   ├── deploy.sh
│   └── migrate.sh
│
├── docs/                           # 项目文档
│   ├── architecture.md             # 架构决策记录
│   └── api/                        # API 文档（Swagger 等）
│
├── test/                           # 跨包测试（单元测试与代码同包同目录）
│   ├── integration/                # 集成测试（依赖外部服务）
│   └── e2e/                        # 端到端测试
│
├── .env.example                    # 环境变量示例
├── .gitignore
├── go.mod
├── go.sum
├── Makefile                        # 常用命令入口
└── README.md
```

---

## 核心约定

| 约定 | 说明 |
|---|---|
| **依赖方向** | `adapters → application → domain ← infrastructure`，箭头不可逆 |
| **领域层纯净** | `domain/` 只依赖 `seedwork/` 和标准库，不引入 GORM / Redis / HTTP |
| **接口在内、实现在外** | 仓储/网关接口定义在 `domain/`，实现在 `infrastructure/` |
| **CQRS 读写分离** | `application/` 用 `command_handler` / `query_handler` 拆分 |
| **领域按需展开** | 简单领域（如 `statistic`）只需 `entity + repository`；复杂领域才加 `service / event` |
| **单元测试同包** | `*_test.go` 与被测代码同目录；`test/` 仅放 integration/e2e |
| **migrations 顶层** | 与 ORM 解耦，golang-migrate 等工具直接读取 |
| **`pkg/` 仅放横切关注点** | 业务相关的认证流程不进 `pkg/auth`，应在 `application/` 或 `adapters/http/middleware/` |



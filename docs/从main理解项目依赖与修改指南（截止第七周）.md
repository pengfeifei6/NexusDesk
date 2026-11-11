# 从 main.go 理解 NexusDesk 的依赖与修改方法

更新日期：2026-11-11。以当前第七周及 DeepSeek 接入后的代码为准。

本文从 [API 启动入口](../cmd/api/main.go) 出发，解释程序启动时创建了什么、对象之间如何协作、请求最终去了哪里，以及修改功能时应该打开哪些文件。阅读时可以把本文和代码左右分屏，按函数名定位。

**先记住一条主线：`main.go` 负责把组件组装起来，Handler 接收请求，Service 执行业务规则，Repository 或平台适配器访问数据库、文件和模型。** 找业务规则时应沿调用继续往下走，不能只看 `main.go`。

## 一、这里说的依赖有四种

| 类型 | 本项目例子 | 需要去哪里看 |
| --- | --- | --- |
| Go 包依赖 | Gin、pgx、JWT 库，以及项目内部的 `auth`、`ticket` | Go 文件的 `import`、[go.mod](../go.mod) |
| 对象依赖 | `LoginService` 需要用户查询、密码校验和 Token 签发能力 | 结构体字段、`New...` 构造函数参数 |
| 运行环境依赖 | PostgreSQL、Redis、Ollama、DeepSeek、私有文件目录 | 配置、Compose、平台适配器 |
| 前端与开发工具依赖 | Vue、Vite、Node、Goose、Python 验收脚本 | [package.json](../web/package.json)、`scripts/`、`migrations/` |

例如，`pgx` 是编译进 Go 程序的数据库客户端库，PostgreSQL 是另一个运行中的数据库服务。安装了 `pgx` 不代表数据库已经启动。类似地，Asynq 是 Go 库，Redis 是它连接的服务。

## 二、先读懂 main、run、runHTTP 三层

[cmd/api/main.go](../cmd/api/main.go) 可以分成三个函数阅读：

| 函数 | 负责的事情 | 返回或结束意味着什么 |
| --- | --- | --- |
| `main()` | 建立停止信号 Context，调用 `run`，统一记录退出错误 | 整个 API 进程准备退出 |
| `run(ctx)` | 读配置、建立资源、组装业务对象、注册路由、调用 HTTP 服务 | 启动失败，或 HTTP 服务已经结束 |
| `runHTTP(ctx, addr, handler)` | 监听端口、运行 HTTP 服务、响应停止信号 | 监听失败、服务异常或完成停机 |

### main 如何控制退出

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
err := run(ctx)
stop()
```

`ctx` 用于表示进程是否收到停止通知；Ctrl+C 或 SIGTERM 会使它取消。`stop()` 释放信号监听资源。

业务资源的 `defer` 放在 `run` 中。当 `run` 返回时，先执行这些清理动作，然后 `main` 才决定是否 `os.Exit(1)`。如果以后把 `os.Exit` 直接放进持有资源的函数里，要注意它不会执行尚未运行的 `defer`。

### run 为什么单独存在

它允许启动过程返回 `error`，把退出决定留给最外层，也便于测试启动失败和服务退出。数据库连接失败、目录不可用或配置错误，都可以沿同一条返回路径退出。

不要把工单创建、SQL 查询、提示词或上传解析塞进这里；`run` 主要决定“使用哪个实现、把它交给谁”。

## 三、沿 run 的执行顺序看每个对象

### 1. cfg：把环境变量变成经过校验的配置

入口是 [config/config.go](../internal/platform/config/config.go) 中的 `config.Load()`：

```go
cfg, err := config.Load()
```

它返回 `Config`，内部包括 `Database`、`JWT`、`Storage`、`RAG`、`Async` 等配置分组，读取环境变量并检查格式和允许范围。

**Go 的 `config.Load()` 不读取 `.env` 文件。** 当前 [scripts/dev.py](../scripts/dev.py) 负责读取项目根目录 `.env`，把允许的变量放进对应子进程环境，再启动 Go 或前端。因此 `python3 scripts/dev.py api` 和在空环境中直接执行 `go run ./cmd/api` 的效果不同。

配置是启动时读取的。修改 `.env` 后，需要重启使用该配置的进程；它不是自动热更新。

### 2. pool：API 进程共享的数据库连接池

```go
pool, err := database.Open(ctx, cfg.Database)
defer pool.Close()
```

看 [database/postgres.go](../internal/platform/database/postgres.go)。`database.Open` 返回 `*pgxpool.Pool`，创建连接池并在有超时的 Context 中执行 `Ping`。

这里的 `pool` 是一组可复用的连接，不是一条所有请求排队共用的连接。后面多个 Repository 都接收同一个 `pool`，通过它查询数据库或开始事务。

`NewPostgresRepository(pool)` 通常只是创建一个保存该指针的小对象。多次创建 Repository 包装对象不等于多次创建数据库连接池。API 和 Worker 属于不同进程，各自创建自己的连接池，但连接到同一个数据库。

`PostgreSQL connection verified` 表示启动时连接成功，**不代表所有表、迁移、扩展都已准备好**。当前 API 不自动执行 migration；表结构仍由 Goose 与 [migrations/](../migrations/) 管理。

### 3. users：用户表访问能力

```go
users := auth.NewPostgresRepository(pool)
```

实现位于 [auth/repository.go](../internal/auth/repository.go)，提供创建用户、按邮箱查登录凭据、按 ID 查用户等能力。后续注册、登录、认证中间件和本人信息接口共享它。

`users` 这个变量是一个 Repository 对象，不是已经查出来的用户列表。构造它通常不会立即把用户表读入内存。

### 4. passwords：注册和登录共享的密码组件

```go
passwords := auth.NewPasswordHasher()
```

看 [auth/password.go](../internal/auth/password.go)。它封装 Argon2id 的散列和验证，内部有两个并发名额，用于限制昂贵的密码计算。

注册和登录都注入同一个 `passwords` 对象，才能共享这个预算。若在每个请求里重新创建，单个对象的并发限制就无法限制整个进程。

数据库保存密码散列，登录通过验证函数比较，不通过“解密密码”完成。

### 5. registration：注册 Handler

```go
registration := auth.NewHandler(auth.NewService(users, passwords))
```

从括号内部向外读，可以展开为下面的等价写法：

```go
registrationService := auth.NewService(users, passwords)
registration := auth.NewHandler(registrationService)
```

[auth/service.go](../internal/auth/service.go) 中的注册 Service 处理邮箱、显示名和密码规则，调用密码组件得到散列，再调用用户 Repository 写入数据库。[auth/handler.go](../internal/auth/handler.go) 处理 HTTP 输入、输出和错误状态码。

创建 Handler 不等于已经开放接口。后面还要调用它的 `RegisterRoutes`。

### 6. tokens、loginService、login、me：登录和认证

```go
tokens, err := auth.NewTokenManager(auth.TokenConfig{ /* 来自 cfg.JWT */ })
loginService, err := auth.NewLoginService(ctx, users, passwords, tokens)
login := auth.NewLoginHandler(loginService)
me := auth.NewMeHandler(users)
```

以上第一行省略了字段，阅读实际值请看 `main.go`，不要复制这个简写作为完整配置。

| 对象 | 实现位置 | 职责 |
| --- | --- | --- |
| `tokens` | [auth/token.go](../internal/auth/token.go) | 签发并验证 JWT |
| `loginService` | [auth/service.go](../internal/auth/service.go) | 查用户、验证密码、签发 Token |
| `login` | [auth/handler.go](../internal/auth/handler.go) | 暴露登录 HTTP 接口 |
| `me` | [auth/me.go](../internal/auth/me.go) | 根据认证后的身份读取本人信息 |

`NewLoginService` 与多数简单构造函数不同：它在启动时计算一个用于未知邮箱校验的占位散列，所以接收 Context，也可能返回错误。该散列不创建真实账号。

JWT 用于确认身份。组织角色由数据库中的成员关系决定，不能把前端提交的 `role` 或 `user_id` 当作可信权限。

### 7. organizations：组织 Service 和 Handler

```go
organizations := organization.NewHandler(
    organization.NewService(organization.NewPostgresRepository(pool)),
)
```

看 [organization/service.go](../internal/organization/service.go) 与 [organization/repository.go](../internal/organization/repository.go)。创建组织会同时建立创建者的管理员成员关系，二者作为一个数据库事务完成。

成员列表和添加成员由另一组 `MemberService`、`MemberHandler` 负责，它们在后面的路由注册区域组装。`organization` 目录下不只有一个 Service。

### 8. objects：私有文件存储

```go
objects, err := storage.NewLocal(cfg.Storage.Root)
defer objects.Close()
```

看 [storage/local.go](../internal/platform/storage/local.go)。它检查私有目录并打开受根目录边界约束的文件访问对象，后续上传、读取和下载通过它访问原文件。

`objects` 管文件字节；数据库 Repository 管文件名、状态、归属组织、存储键等元数据。两者不是同一份数据，也没有一个同时覆盖文件系统和 PostgreSQL 的普通数据库事务。补偿与清理记录在 [knowledge/service.go](../internal/knowledge/service.go) 和存储实现中处理。

即使只想使用工单，当前 `run` 仍会初始化文件存储。目录初始化失败，会阻止整个 API 启动；当前没有按模块延迟初始化。

### 9. knowledgeService：把文档所需能力组合起来

```go
knowledgeService := knowledge.NewService(
    knowledge.NewPostgresRepository(pool),
    organization.NewPostgresRepository(pool),
    objects,
    knowledge.NewTextExtractor(cfg.Storage.MaxFileBytes, cfg.Storage.MaxTextBytes),
    knowledge.Limits{ /* 文件、请求和解析限制 */ },
)
```

| 参数 | 给文档 Service 提供的能力 |
| --- | --- |
| 文档 Repository | 保存和查询文档元数据、正文与处理状态 |
| 组织 Repository | 查询用户在指定组织中的有效成员关系 |
| `objects` | 保存、读取和删除私有原文件 |
| `TextExtractor` | 从支持的文件类型提取正文 |
| `Limits` | 限制上传大小、请求大小和解析时间 |

这些参数的接口定义在 [knowledge/service.go](../internal/knowledge/service.go)、[storage/storage.go](../internal/platform/storage/storage.go) 和 [knowledge/extractor.go](../internal/knowledge/extractor.go)。

目前 UTF-8 TXT/MD 提取可用，PDF 解析未实现。正文提取与分块也不是一件事：分块函数 `SplitText` 位于 [knowledge/chunker.go](../internal/knowledge/chunker.go)，由 RAG 索引过程使用。

### 10. models 和 generator：把向量化与回答生成分开

```go
models, err := llm.New(cfg.RAG.OllamaBaseURL)
generator := models.Generator()
if cfg.RAG.ChatProvider == "deepseek" {
    generator, err = llm.NewDeepSeek(cfg.RAG.DeepSeekAPIKey, cfg.RAG.ChatModel)
    // 实际代码在这里检查 err。
}
```

这里的 `models` 是本地 Ollama HTTP 客户端，不是把模型权重加载进 Go 内存。`llm.New` 校验地址并准备客户端，构造时不会实际请求模型。

- `models.Embedder()`：提供本地 BGE-M3 向量化能力。
- `models.Generator()`：提供原来的本地 Qwen 回答能力。
- `llm.NewDeepSeek(...)`：返回 DeepSeek 回答生成器，替换 `generator` 变量引用。

因此，选择 DeepSeek 后，本地向量模型仍需要运行；替换的是回答生成器，没有替换检索所用的向量模型。

实现分别在 [llm/client.go](../internal/platform/llm/client.go)、[embedding_client.go](../internal/platform/llm/embedding_client.go)、[chat_client.go](../internal/platform/llm/chat_client.go)、[deepseek.go](../internal/platform/llm/deepseek.go)。DeepSeek 使用 Go 标准库 HTTP，没有引入厂商 SDK。

### 11. ragService：把授权、检索和生成组成问答

```go
ragService := rag.NewService(
    pool,
    knowledgeService,
    models.Embedder(),
    generator,
    rag.Limits{Enabled: cfg.RAG.Enabled, MinimumScore: cfg.RAG.MinimumScore},
)
```

[rag/service.go](../internal/rag/service.go) 接收的 `knowledgeService` 用于文档访问与组织授权；Embedder 用于文档及问题向量化；Generator 用于根据召回片段生成答案。构造函数内部还创建索引 Repository。

RAG Service 负责业务编排和来源复查，模型适配器负责供应商协议。改变 DeepSeek 请求参数时主要看 `llm`；改变“哪些资料允许参加检索”时，应看 RAG、文档授权和 SQL。

### 12. 路由注册区域：继续组装工单、成员和任务模块

`ticket.NewService` 同时接收工单 Repository 和组织成员查询能力；`jobs.NewService` 接收连接池、`knowledgeService` 和 `ragService`，复用文档处理及索引能力。

API 中的 jobs Handler 负责提交任务、读取状态、重试和取消。**API 的 `main.go` 没有创建 Redis 客户端，也没有启动 Worker。** 任务的投递与执行来自另一个入口，见第七节。

## 四、依赖注入和 interface 在这里具体是什么意思

以 [auth/service.go](../internal/auth/service.go) 中的真实定义为例：

```go
type UserCreator interface {
    CreateUser(context.Context, CreateUserParams) (User, error)
}

type PasswordHashing interface {
    HashPassword(context.Context, string) (string, error)
}
```

注册 Service 只要求传入的对象能创建用户、能生成密码散列。`main.go` 选择 PostgreSQL Repository 和 Argon2id 实现，把它们作为参数交进去，这就是本项目的手动依赖注入。

Go 不需要写 `implements UserCreator`。只要某个类型的方法集满足接口，它就能作为该接口的参数。`New...` 也不是语言关键字，而是项目使用的普通构造函数命名习惯；`*Service` 表示指向 Service 对象的指针。

这带来两个直接用途：测试可以传入受控的测试实现；替换某个实现时，业务调用方有机会保持原样。例如本地 Generator 和 DeepSeek Generator 都满足 [rag/models.go](../internal/rag/models.go) 中的 `Generator`：

```go
type Generator interface {
    Model() string
    Generate(context.Context, string, []Source) (Generated, error)
}
```

“满足接口”仍不足以保证兼容：当前配置、`SupportedChatModel`、向量维度和结果校验都有契约，新增模型还要同步这些检查及测试。

项目没有使用自动依赖注入容器，也不是所有模块都完全通过接口隔离数据库。例如 `rag.NewService` 直接接收连接池，`jobs.Service` 也持有连接池；`jobs/repository.go` 中有不少方法的接收者仍是 `*Service`。读代码要看类型和接收者，不能仅凭文件叫 `repository.go` 就假定存在独立 Repository 对象。

## 五、路由如何真正连接到 Handler

[httpserver/router.go](../internal/platform/httpserver/router.go) 接收若干个注册函数：

```go
func NewRouter(registerRoutes ...func(*gin.RouterGroup)) *gin.Engine
```

`...` 表示可以传入多个这样的函数。`registration.RegisterRoutes` 是绑定到对象的方法值，作为函数传入；`NewRouter` 在启动时遍历调用它们，完成路由注册，不是在每个请求到来时重新组装 Service。

路径由不同层组合：

```text
NewRouter 中的 /api/v1
  + ticket.RegisterRoutes 中的 /organizations/:orgID/tickets
  + 具体方法注册的 /:ticketID/claim
  = POST /api/v1/organizations/:orgID/tickets/:ticketID/claim
```

`group.Group("")` 创建路径不变的子组，随后 `protected.Use(auth.RequireUser(tokens, users))` 给该组增加认证中间件。

| 路由范围 | 是否经过 RequireUser |
| --- | --- |
| `GET /healthz` | 否 |
| `POST /api/v1/auth/register` | 否 |
| `POST /api/v1/auth/login` | 否 |
| `/me`、组织、成员、工单、知识库、RAG、任务 | 是，均在 `/api/v1` 的受保护组中 |

[auth/middleware.go](../internal/auth/middleware.go) 验证 Bearer Token，再查询用户是否存在，把可信用户 ID 放入当前 Gin Context。它没有代替组织授权；Handler/Service 随后还要检查成员关系和对象权限。

新增受保护业务路由时，要在受保护组注册，不能为了测试方便挂到公开注册/登录旁边。已有模块内部新增路由，通常只需修改该模块 `RegisterRoutes`，不用在 `main.go` 再注册整个模块。

Gin 的 `*gin.Engine` 实现了标准库 `http.Handler`，因此可以直接传给 `runHTTP`。前者负责路由分发，后者负责网络监听、连接和服务生命周期。

## 六、跟着真实请求走一遍

### 例一：领取工单

按下面的文件与函数顺序阅读：

1. [TicketDetailView.vue](../web/src/views/TicketDetailView.vue) 的“领取工单”按钮调用 [desk.ts](../web/src/composables/desk.ts) 中的动作逻辑。
2. [client.ts](../web/src/lib/client.ts) 发出带 Bearer Token 和 `expected_version` 的请求。
3. `RequireUser` 验证身份；[ticket/handler.go](../internal/ticket/handler.go) 的 `scope` 与 `claim` 解析组织、工单、版本并调用业务方法。
4. [ticket/service.go](../internal/ticket/service.go) 的 `Claim` 通过 `write` 开始事务，在事务内复查有效成员关系。
5. [ticket/repository.go](../internal/ticket/repository.go) 的事务实现执行 `ClaimTicket`，以组织、状态、受理人和旧版本等条件更新。
6. 同一事务写入领取事件，成功提交后返回新工单。
7. Handler 返回 JSON，页面刷新详情；竞争失败按冲突处理。

这个例子把三个规则连在一起：权限从后端确认、并发由数据库条件约束、业务变化与事件同事务提交。修改领取逻辑时，仅修改按钮或仅在查询前加一个 `if` 都不足以替代这些约束。

### 例二：基于知识库回答

入口为 `POST /api/v1/organizations/:orgID/knowledge/ask`。

认证与组织授权后，`rag.Service.Ask` 调用 [retriever.go](../internal/rag/retriever.go) 中的检索逻辑：规范化问题、BGE-M3 向量化、查询有效索引、复查来源。得到片段后，Generator 调用当前模型，随后 Service 再次检查权限、来源及引用。

DeepSeek 适配器负责 HTTPS 请求和结构化回答解析；pgvector 检索 SQL 在 [index_repository.go](../internal/rag/index_repository.go)。正文不是直接拼进 SQL，模型返回的引用也不能直接当作可信来源。

改变分块策略，看 [chunker.go](../internal/knowledge/chunker.go)；改变检索阈值，看 `RAG_MIN_SCORE`；改变生成请求，看 `llm`；改变来源筛选，看检索 SQL 与 Service。它们影响链路的不同位置。

### 例三：上传并异步处理

这个按钮实际涉及上传文档和提交任务两个请求。上传成功不等于任务提交成功，任务提交成功也不等于索引已经完成。

API 的 `jobs.Service.Submit` 在 [jobs/repository.go](../internal/jobs/repository.go) 中保存持久任务；独立 Worker 扫描和投递任务，消费后调用 `Execute`。浏览器通过任务列表轮询展示进度。

只启动 `cmd/api` 时，系统可以接受任务，但没有 Worker 就不会执行文档处理。

## 七、Worker 与 API 的依赖为什么不同

看 [cmd/worker/main.go](../cmd/worker/main.go)：

| 对比项 | API | Worker |
| --- | --- | --- |
| 配置入口 | `config.Load()` | `config.LoadWorker()` |
| HTTP 监听与 Gin 路由 | 有 | 无 |
| JWT、注册、登录组件 | 有 | 不需要 |
| PostgreSQL、私有目录 | 需要 | 需要，指向同一份业务数据 |
| 文档/RAG/任务 Service | 组装后供 HTTP 使用 | 组装后供后台执行使用 |
| Redis 客户端与任务消费者 | 不创建 | 创建，并检查连接 |
| DeepSeek 生成器与 Key | 当前 Ask 使用 | 不需要 |
| 本地 BGE-M3 | 检索和手动索引时使用 | 后台索引时使用 |

Worker 的主要执行关系是：

```text
启动时：创建 Service → 创建队列客户端 → Ping Redis → 启动 Asynq 消费者
运行时：Dispatch 扫描 PostgreSQL 中可投递任务 → Redis 队列
        Asynq 收到消息 → jobs.Service.Execute
        → knowledge.Service.Process → rag.Service.Index → 持久化执行结果
```

[taskqueue/asynq.go](../internal/platform/taskqueue/asynq.go) 负责消息传输；[jobs/service.go](../internal/jobs/service.go) 负责租约、心跳、实际尝试次数、执行与恢复；PostgreSQL 中的任务记录是业务状态依据。

Worker 仍向 `rag.NewService` 传入一个本地 Generator 对象，这是当前 RAG Service 的构造及可用性检查需要。索引过程只调用 Embedder，不调用 `Generate`，所以并不因此要求本地 Qwen 实际在线。

[taskguard/guard.go](../internal/platform/taskguard/guard.go) 把任务身份放进操作 Context，并在数据库中核验 run version、租约、Token、成员和来源。它让文档/RAG 写入检查执行所有权，而不让这些领域模块反向依赖整个 `jobs` 包。Context 里携带标识本身不等于通过权限检查。

## 八、资源生命周期、Context 和超时

`runHTTP` 在协程中调用 `server.Serve(listener)`，通过缓冲长度为 1 的 `serveResult` 收集结束结果。主协程等待服务器结束，或等待进程 Context 取消。

收到停止信号后，代码从 `context.Background()` 创建新的 5 秒退出 Context，调用 `server.Shutdown`；超时则尝试 `server.Close`。退出等待不能复用已经取消的进程 Context。

| 对象/Context | 存活范围 | 修改时要注意 |
| --- | --- | --- |
| 连接池、密码组件、Service、Handler | 通常整个进程 | 被多个请求共享，不能随意加无保护的可变请求字段 |
| `c.Request.Context()` | 单次 HTTP 请求 | 传给业务和 SQL；客户端断开或请求结束会影响它 |
| SQL/模型操作 Context | 某个业务操作 | 在请求或任务 Context 上设置更短的截止时间 |
| Worker 任务 Context | 一次后台执行 | 不依赖最初提交任务的 HTTP 请求继续存活 |
| 清理/回滚 Context | 短暂的收尾阶段 | 操作已取消时仍需有限时间清理；保留必要的任务所有权信息 |

进程 Context 和 HTTP 请求 Context 不是同一个对象。当前服务器没有把进程 Context 设为所有请求的 `BaseContext`，停止流程通过 `Shutdown` 等待在途请求，而不能假设收到 SIGTERM 后所有请求 Context 同时取消。

当前主要时间限制：

| 位置 | 当前值 |
| --- | --- |
| HTTP 读头 / 读请求 / 写响应 / 空闲连接 | 5 / 15 / 75 / 60 秒 |
| HTTP 优雅退出等待 | 5 秒 |
| API RAG 索引 / 检索 / 问答操作 | 60 / 25 / 60 秒 |
| 单次向量 / 单次生成请求 | 20 / 35 秒 |
| Worker 索引 / 总任务 | 150 / 180 秒 |
| 任务租约 / 心跳间隔 | 30 / 5 秒 |

这些上限分属不同层。HTTP `WriteTimeout` 不会自动给所有 SQL 加超时；单独把浏览器等待时间调大，也不会改变后端模型的 35 秒限制。

`defer` 只在所属函数返回时执行，按后注册先执行。API 的存储关闭在连接池关闭之前执行；这些资源不会在刚创建后立即关闭。`Shutdown` 返回之前，资源仍供在途请求使用。

## 九、外部服务和配置分别影响哪些地方

| 配置或依赖 | 读取位置 | 影响和修改联动 |
| --- | --- | --- |
| `HTTP_ADDR` | [config.go](../internal/platform/config/config.go) | API 监听地址；改端口还需对齐前端代理目标 |
| `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE` | [async.go](../internal/platform/config/async.go) 的 `loadDatabase` | API、Worker、迁移工具应指向正确且一致的库 |
| `JWT_*` | `config.go`、`auth/token.go` | API 的 Token 签发/验证；改变密钥会影响已有 Token |
| `STORAGE_ROOT` 与上传/解析限制 | [storage.go](../internal/platform/config/storage.go) | API 与 Worker 必须访问同一私有目录；不要只改一端 |
| `RAG_ENABLED`、`RAG_MIN_SCORE` | [rag.go](../internal/platform/config/rag.go) | RAG 可用性与检索最低分数 |
| `OLLAMA_BASE_URL` | `rag.go`、`llm/client.go` | 本地模型端点，只接受当前代码允许的回环地址 |
| `EMBEDDING_MODEL`、`EMBEDDING_DIM` | `rag.go`、`rag/models.go`、索引 SQL | 当前固定 BGE-M3/1024，不能只改环境变量就换维度 |
| `CHAT_PROVIDER`、`CHAT_MODEL`、`DEEPSEEK_API_KEY` | `rag.go`、API `main.go`、模型适配器 | 选择回答生成器；Key 不进入前端或 Worker |
| `ASYNC_ENABLED`、`REDIS_*`、`WORKER_CONCURRENCY` | `async.go`、`taskqueue/asynq.go` | 任务启用及执行；Worker 并发当前支持 1～4 |

如果新增环境变量，除了配置结构、读取与校验，还要检查 [dev.py](../scripts/dev.py) 的 `COMMON_KEYS` / `API_KEYS` 白名单，以及 [.env.example](../.env.example)。否则“变量写进了 `.env`，Go 里却读不到”。前端启动分支不读取后端 `.env`。

Vite 代理读取进程变量 `NEXUSDESK_API_TARGET`。当前 `dev.py web` 固定使用默认前端启动方式，且会筛选环境；仅在根 `.env` 添加该变量或在外层随意 export，不能认为它一定传入 Vite。需要自定义端口时，应同时检查启动器和 [vite.config.ts](../web/vite.config.ts) 的传递链路。

### 依赖故障怎样影响程序

| 情况 | 当前行为 |
| --- | --- |
| 配置格式错误、必需变量缺失 | API 在监听前退出；即使相关外部服务在线也不会启动 |
| PostgreSQL 不可连接 | API 和 Worker 启动失败 |
| 私有目录初始化失败 | API 和 Worker 启动失败 |
| Redis 不可连接，但配置格式正确 | API 可启动和保存任务；Worker 启动时连接检查失败 |
| Ollama 或 DeepSeek 不在线 | 创建模型客户端时不联网；相关操作调用时才报错 |
| `RAG_ENABLED=false` | RAG 业务操作受限，路由和组件仍会组装，不能借此跳过全部配置校验 |
| `ASYNC_ENABLED=false` | API 仍注册任务路由但提交受限；Worker 明确拒绝启动 |

数据库和 Redis 由 Compose 管理，Go API 和 Worker 在宿主机运行。pgvector 是 PostgreSQL 扩展，不是独立模型服务。当前原文件由本地文件系统保存，DeepSeek 仅收到问答需要的问题及相关片段。

## 十、第三方库和工具不必全部背下来

下表版本来自当前仓库文件，后续以锁定文件为准。

| Go 直接依赖 | 当前版本 | 项目使用位置和用途 |
| --- | --- | --- |
| `gin-gonic/gin` | v1.12.0 | 路由、中间件、HTTP Handler |
| `jackc/pgx/v5` | v5.12.0 | PostgreSQL 连接池、SQL、事务、数据库错误 |
| `alexedwards/argon2id` | v1.0.0 | 密码散列与验证 |
| `golang-jwt/jwt/v5` | v5.3.1 | JWT 签发与校验 |
| `hibiken/asynq` | v0.26.0 | Redis 任务投递和消费者 |
| `redis/go-redis/v9` | v9.14.1 | 队列适配器中的 Redis 连接检查 |

`go.mod` 决定模块依赖和版本要求，`go.sum` 保存模块校验信息。`// indirect` 表示这项模块依赖通过其他依赖引入，不能按名字判断本项目已经实现相应功能。例如出现 `gin-contrib/sse` 不代表当前已经实现第八周流式对话；出现 MongoDB 相关间接模块，也不代表业务数据存入 MongoDB。

`main.go` 中的 `context`、`net/http`、`net`、`os/signal`、`syscall`、`time`、`errors`、`fmt`、`log/slog` 都属于 Go 标准库，不需要另外 `go get`。其中 `slog` 负责日志，`fmt.Errorf` 可包装错误，`errors.Is` 用来判断错误链中的已知原因。

前端常读的依赖是 Vue（页面）、Router（路由）、Pinia（会话状态）、`lossless-json`（避免 Go int64 ID 在 JavaScript 中损失精度）。构建与验证使用 Vite、TypeScript、Vitest、Playwright、ESLint 和 Prettier，版本见 [package.json](../web/package.json) 与 [package-lock.json](../web/package-lock.json)。

浏览器请求使用 `fetch`，项目统一封装在 [client.ts](../web/src/lib/client.ts)；不要新增绕开该封装的请求而遗漏 Token、取消、错误映射或无损 ID 处理。

Goose 用来执行 SQL 迁移，Python 用于启动及隔离验收，Docker Compose 管理依赖服务。这些都是运行或开发工具，不会因为 API 引用了 `internal/database` 就自动安装、启动或执行。

## 十一、想改某项功能时，应该打开哪里

| 修改目标 | 主要文件 | 需要一起核对的地方 |
| --- | --- | --- |
| 请求字段、响应字段、HTTP 状态码 | 相应模块 `handler.go` | Service 输入类型、API 文档、前端 `types.ts` 与调用方 |
| 工单谁能操作、何时能操作 | [policy.go](../internal/ticket/policy.go)、[state_machine.go](../internal/ticket/state_machine.go)、`service.go` | 组织授权、事务内检查、页面按钮和测试 |
| 工单查询条件或新增字段 | `ticket/model.go`、`repository.go`、新 migration | SQL 扫描顺序、返回 DTO、前端类型与表单 |
| 注册或密码规则 | `auth/service.go`、`password.go` | 登录是否兼容、前端限制、错误映射、测试 |
| 上传大小或支持类型 | `config/storage.go`、[upload.go](../internal/knowledge/upload.go)、`extractor.go` | 流式限制、请求总大小、前端提示与提取结果限制 |
| 分块和检索效果 | `knowledge/chunker.go`、`rag/retriever.go`、`index_repository.go` | chunker 版本、旧索引重建、固定评测 |
| DeepSeek 的请求与返回协议 | `llm/deepseek.go`、`chat_client.go` | 生成器接口、引用白名单、错误码、超时、模型允许列表 |
| 任务重试、取消、崩溃恢复 | `jobs/service.go`、`repository.go`、`errors.go`、`models.go` | 队列重试参数、taskguard、数据库状态转换、集成测试 |
| 新增模块或替换注入实现 | `cmd/api/main.go`；涉及后台处理时还有 `cmd/worker/main.go` | 资源初始化/释放、配置、受保护路由注册 |
| 页面交互 | `web/src/views/`、`components/`、`composables/` | API 契约与会话/组织切换后的旧响应处理 |

### 示例 A：给工单增加第四档优先级

这是理解跨层修改的例子，当前仍只有 1～3 档。

先找到 `validPriority` 与输入解析，再看 `00004_create_tickets.sql` 的 `CHECK (priority BETWEEN 1 AND 3)`，然后查创建、编辑、筛选及前端标签。正确修改需要新增迁移调整数据库约束，并同步后端校验、页面选项和测试；不能只在下拉框里加一个选项，也不改写已执行的旧迁移。

### 示例 B：增加一个已有模块的接口

先确定路径、输入、角色和成功/失败语义；在现有 Handler 注册路由，Service 实现规则，需要 SQL 时再扩展 Store/Repository。若已有 Service 的构造参数没有变化，通常不用修改 `main.go`。

只有新增模块、增加构造依赖或替换实现时，才需要回到启动入口进行组装。若是受保护接口，沿用当前认证组，Service 仍自己检查组织和对象权限。

### 示例 C：更换回答模型或增加模型供应商

若只从已支持的 DeepSeek 模型中选择，调整配置并重启 API即可。若添加全新供应商，需要实现 `rag.Generator`，添加供应商配置与校验，在 API 入口选择实现，并补充协议、取消、超时、错误和引用测试。

更换回答模型与更换向量模型是两类改动。后者还影响维度、索引元数据、过滤条件和已有向量，需要完整的迁移与重新索引方案。

### 示例 D：增加配置后没有生效

按“`.env` → 启动器白名单 → 进程环境 → `config.Load` → 构造参数 → 实际消费位置”逐层检查。若字段只读进了 `cfg`，却没有传给真正执行操作的组件，功能不会改变。任务配置还要确认 API 和 Worker 都已重启。

## 十二、修改后怎样验证

先检查本次修改范围，再执行与它相关的测试。下面是命令入口，不表示本文编写时重新执行过全部测试：

```bash
# 在项目根目录：例如修改认证或工单
go test -race ./internal/auth ./internal/organization ./internal/ticket

# 例如修改知识库、模型或任务
go test -race ./internal/knowledge ./internal/rag ./internal/jobs ./internal/platform/llm ./internal/platform/taskqueue

# Go 业务变更完成后的常规检查
go test -race ./...
go vet ./...
go build ./...
```

普通 `go test` 中有带环境门禁的数据库集成测试，显示跳过不能算真实数据库通过。需要实际数据库和队列回归时，使用仓库已有的隔离脚本，例如 `python3 scripts/week07-smoke.py --backend-only`，不要手动把测试环境指向日常数据库。

前端修改可按现有工具链执行：

```bash
export PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH"
npm --prefix web run test:unit -- --run
npm --prefix web run build
npm --prefix web run lint
npm --prefix web run format:check
```

上面的 Node 路径是本机已安装工具的位置；换机器应满足 `package.json` 的 Node 要求。启动器修改用 `python3 -m unittest discover -s scripts -p test_dev.py`。真实 DeepSeek 验收用 `python3 scripts/deepseek-smoke.py --live`，会调用云模型并可能产生费用。

只修改文档时，核对事实、路径、链接和格式即可，不需要为文案启动模型或重跑全部应用测试。

## 十三、建议按这个顺序读代码

| 轮次 | 阅读路径 | 读完应能解释的问题 |
| --- | --- | --- |
| 第一轮：启动 | `cmd/api/main.go` → `config` → `database/postgres.go` → `httpserver/router.go` | 谁读配置，谁监听，哪些失败会阻止启动？ |
| 第二轮：身份 | `auth/handler.go` → `service.go` → `repository.go` → `middleware.go` | Token 如何产生，当前用户是谁，组织权限在哪里查？ |
| 第三轮：工单 | `ticket/handler.go` → `service.go` → `policy.go/state_machine.go` → `repository.go` | 一次领取为什么只有一个人成功，事件何时提交？ |
| 第四轮：知识库 | `knowledge/handler.go` → `service.go` → `extractor.go` → `storage/local.go` | 原文件与元数据分别在哪，上传失败如何处理？ |
| 第五轮：问答 | `rag/service.go` → `retriever.go` → `index_repository.go` → `llm` | 片段如何选出，DeepSeek 看到什么，引用如何检查？ |
| 第六轮：任务 | `cmd/worker/main.go` → `jobs` → `taskqueue` → `taskguard` | API 为什么不直接执行，重复消息和过期任务怎么处理？ |

读每个 Service 时都问自己四件事：它接收什么依赖，在哪里检查权限，哪些操作必须同事务，失败后由谁收尾。对应测试通常能帮助你理解这些规则为什么存在。

相关文档：[当前开发阶段](项目阶段介绍_第七周及DeepSeek接入.md) · [启动与排错](demo/local-deepseek-run.md) · [工单状态机](ticket-state-machine.md) · [RAG 设计](rag-design.md) · [异步任务设计](jobs-design.md)。

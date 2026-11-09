# NexusDesk 企业级 AI 智能工单与知识库平台

**项目设计文档（PRD + 技术设计 + 验收规范）**  
版本：v1.0（开发基线）  
日期：2026-11-09  
定位：计算机硕士求职作品，Go 后端为核心，AI 应用工程为特色  
状态：计划文档，所有性能数值均为拟定目标，不代表已经实测

> 核心原则：先交付真实可用的工单系统，再叠加知识库、异步任务和 Agent；采用模块化单体起步；业务权限由后端控制，不能交给模型决定。

## 1. 项目概况与目标

### 1.1 背景与问题

企业内部的软件支持团队往往同时处理产品说明文档、常见故障和人工工单。知识分散在文档中，员工要反复搜索、复制回复并手工转交问题。NexusDesk 将结构化工单和非结构化知识库放到一个工作空间，提供可追溯的 AI 辅助解答，同时保留人工审核与业务操作控制。

### 1.2 目标用户与业务场景

| 角色 | 核心诉求 | 主要权限 |
| --- | --- | --- |
| 租户管理员 | 邀请成员、分配角色、查看审计记录 | 管理本组织成员和配置 |
| 一线支持人员 | 新建/接收/处理工单、查询知识 | 访问授权范围内的工单和知识库 |
| 技术处理人员 | 领取工单、回复处理记录、变更状态 | 处理被分配或可见的工单 |
| 普通业务用户 | 提问、查看自己的工单、申请人工处理 | 查看自己的数据 |

**典型场景 A：** 用户询问数据库连接失败原因。系统检索企业内部排障手册并提供可核验的引用；用户确认后，创建新的故障工单。

**典型场景 B：** 两个工程师同时领取工单，系统只允许一人成功，失败者收到明确冲突提示。

**典型场景 C：** 用户上传一份较大的技术文档，接口快速返回处理任务，Worker 异步解析、切分、向量化并回写进度。

### 1.3 产品目标与非目标

**产品目标**

- 提供可操作的 Vue 前端、Go API、PostgreSQL 持久化及本地可复现部署。
- 形成独立可用的工单业务，不依赖大模型服务是否在线。
- 完成带来源引用的企业知识库问答，并支持安全受控的工单工具调用。
- 对并发、事务、异步任务、多租户权限、RAG 检索开展可复现测试。
- 将项目建设成面试可以讲清技术决策、实现过程和测量结果的作品。

**第一阶段非目标**

- 不训练或微调基础模型，不自研向量数据库。
- 不在 V1 就上 Kubernetes、服务网格或十几个微服务。
- 不承诺生产级 SLA，也不处理真实企业敏感数据。
- 不让 Agent 在没有确认的情况下自动发送外部消息、删除记录或执行任意命令。

### 1.4 分阶段范围与优先级

| 功能 | P0 可演示业务版 | P1 AI 增强版 | P2 工程深化版 |
| --- | --- | --- | --- |
| 用户与组织 | 登录、成员关系、基础角色 | 权限审计 | 更精细 RBAC |
| 工单 | CRUD、状态流转、评论 | 幂等、操作事件 | 复杂 SLA、统计看板 |
| 知识库 | 文件元数据与上传 | 异步解析、向量检索 | 混合检索、重排序、评测 |
| AI | 无强依赖 | 带引用的 RAG、SSE | Agent、MCP、AI 网关 |
| 性能与运维 | 日志、迁移、Compose | Redis、任务队列、测试 | pprof、追踪、故障演练 |

**范围控制：** P0 优先级高于全部 AI 扩展；P0 未验收不得同时展开 Agent、MCP 或微服务拆分。

## 2. 核心业务流程

### 2.1 工单生命周期

状态集合：`OPEN`（待处理）、`IN_PROGRESS`（处理中）、`RESOLVED`（已解决）、`CLOSED`（已关闭）。允许 `OPEN -> IN_PROGRESS -> RESOLVED -> CLOSED`；允许管理员将 `RESOLVED -> IN_PROGRESS` 重新打开。所有转换需验证当前状态、角色及组织边界。

关键约束：每次状态变更写入 `ticket_events`；领取工单需使用数据库条件更新或乐观锁保证并发安全；创建工单可使用 `Idempotency-Key` 避免重试导致重复创建。

### 2.2 文档摄取流程

上传文件 -> 检查类型与大小 -> 保存原文件和元数据 -> 提交解析任务 -> 解析文本 -> 清洗与 Chunk -> 调用 Embedding -> 写入 `document_chunks` 与向量 -> 更新状态。失败要记录原因和可重试状态，Worker 重启后可继续处理。

### 2.3 RAG 问答流程

用户提问 -> 验证身份和租户 -> 查询重写（可选）-> 仅检索授权文档 -> 关键词/向量检索 -> 去重与重排序 -> 构建带来源片段的上下文 -> 调用 LLM -> 流式返回答案和引用。检索不到可靠材料时明确说明依据不足，不应伪造来源。

### 2.4 Agent 工具调用流程

用户请求 -> LLM 提出工具调用意图 -> Go 服务校验工具 schema 和用户权限 -> 对写操作返回待确认操作 -> 用户明确确认 -> 业务 Service 执行 -> 记录审计事件 -> 将结果反馈到对话。模型的工具调用建议不是授权凭据。

## 3. 总体技术架构

### 3.1 架构选型

采用 **模块化单体 + 独立 Worker 进程**。单个仓库、共享领域模型，`cmd/api` 提供 HTTP API，`cmd/worker` 处理耗时任务。外部依赖包括 PostgreSQL、Redis、模型 API、文件存储；后续只有在性能或团队分工确有需要时才考虑拆出独立服务。

```text
Vue 3 + TypeScript
      | HTTPS / REST / SSE
      v
Go Gin API (auth / rate limit / tenant guard / audit)
      |---- Ticket, Organization, Knowledge, Agent
      |---- PostgreSQL (业务数据 + pgvector)
      |---- Redis (缓存 + Asynq)
      |---- Local Storage / S3-compatible Storage
      |---- LLM Adapter (chat + embedding)
      |
      +---- enqueue tasks ---> Go Worker (parse / embedding / retry)
                                  |
                                  +---- DB / file / LLM
Metrics + Structured Logs + Traces + pprof (development only)
```

### 3.2 技术栈决策

| 层次 | 推荐技术 | 理由与取舍 |
| --- | --- | --- |
| 语言与 Web | Go、Gin | 主修 Go；低门槛交付 API，后续学习标准库 HTTP |
| 数据访问 | PostgreSQL + pgx；SQL 可配 sqlc | 亲自掌握 SQL、事务和执行计划；暂不以 ORM 代替 SQL 学习 |
| 缓存 | Redis | 登录辅助、热点读取、限流；不代替数据库事实来源 |
| 异步任务 | Asynq（Redis） | 便于学习任务重试和 Worker；应对至少一次投递做幂等 |
| 向量检索 | pgvector | 与权限、组织字段同库存储；先精确检索再评估 HNSW |
| AI 编排 | 原生模型 API -> Eino（后期） | 先写最小工具循环，再理解框架价值 |
| 工具协议 | 官方 MCP Go SDK（P2） | 以受控服务接口暴露少量工具，不开放任意执行 |
| 前端 | Vue 3 + TypeScript | 适合个人演示与调试，减少 UI 投入 |
| 运维 | Docker Compose + GitHub Actions | 本地可复现，后期再学 K8s |
| 可观测 | slog、Prometheus、OpenTelemetry | 日志先行，后续补指标和 trace |

**版本规则：** 初始化时选用适配当前依赖的稳定 Go 版本，提交 `go.mod/go.sum` 和 Compose 镜像版本；SDK 的接口、兼容性和许可证以当时官方文档为准。不要在设计文档中假定所有依赖永远兼容。

### 3.3 代码目录约定

```text
nexusdesk/
├── cmd/api/                 # Web 服务入口
├── cmd/worker/              # 异步任务入口
├── internal/
│   ├── auth/                # 认证
│   ├── organization/        # 组织和成员
│   ├── ticket/              # 工单领域
│   ├── knowledge/           # 文档元数据及摄取
│   ├── rag/                 # 检索与生成
│   ├── agent/               # 工具定义、确认与执行
│   ├── task/                # 异步任务及幂等
│   └── platform/            # 数据库、Redis、LLM、存储、日志
├── migrations/              # SQL 向前迁移
├── web/                     # Vue 前端
├── deploy/                  # Docker Compose 等
├── docs/                    # ADR、API、评测、学习日志
├── tests/                   # 集成/端到端测试
├── AGENTS.md
├── Makefile
└── README.md
```

原则：Handler 仅做协议适配；Service 承担业务规则和事务边界；Repository 负责 SQL。领域模块不能直接调用其他模块的私有存储实现；不要为了分层而创建只有一行转发的抽象。

## 4. 数据模型与数据库约束

### 4.1 表结构蓝图

| 表 | 主要字段 | 约束/用途 | 阶段 |
| --- | --- | --- | --- |
| `users` | id、email、password_hash、created_at | email 唯一；密码仅存安全散列 | P0 |
| `organizations` | id、name、created_at | 租户根实体 | P0 |
| `organization_members` | org_id、user_id、role、status | `(org_id,user_id)` 唯一；角色隶属组织 | P0 |
| `tickets` | id、org_id、creator_id、assignee_id、title、body、priority、status、version、timestamps | FK、状态约束、索引、乐观锁 | P0 |
| `ticket_comments` | id、org_id、ticket_id、author_id、content | 不允许跨租户引用 | P0 |
| `ticket_events` | id、org_id、ticket_id、actor_id、event_type、before/after、created_at | 留存完整操作事件 | P0 |
| `documents` | id、org_id、uploader_id、object_key、filename、mime、status、checksum、created_at | 元数据与解析状态 | P1 |
| `document_chunks` | id、org_id、document_id、chunk_index、text、embedding、embedding_model | `(document_id,chunk_index)` 唯一 | P1 |
| `ingestion_jobs` | id、org_id、document_id、state、attempts、last_error、timestamps | 任务可观测性与恢复 | P1 |
| `ai_conversations` | id、org_id、user_id、created_at | 绑定用户与租户 | P1 |
| `ai_messages` | id、conversation_id、role、content、model、token_usage、created_at | 保存受控对话记录 | P1 |
| `ai_tool_calls` | id、org_id、conversation_id、tool_name、args_digest、state、confirmed_by、timestamps | 工具审批和执行审计 | P2 |
| `idempotency_keys` | org_id、actor_id、key、request_hash、result_ref、expires_at | 保证同键同请求；过期清理 | P1 |
| `audit_logs` | id、org_id、actor_id、action、target、result、created_at | 关键管理与 AI 写操作审计 | P1 |
| `outbox_events` | id、aggregate_id、event_type、payload、published_at、created_at | DB 与队列一致性增强（P2） | P2 |

### 4.2 `tickets` 示例 SQL 结构草案

以下为讨论用骨架，不是完整迁移脚本。实现前需补充外键、枚举约束、组织内关联校验及迁移回滚方案。

```sql
CREATE TABLE tickets (
    id            BIGSERIAL PRIMARY KEY,
    org_id        BIGINT NOT NULL REFERENCES organizations(id),
    creator_id    BIGINT NOT NULL REFERENCES users(id),
    assignee_id   BIGINT NULL REFERENCES users(id),
    title         VARCHAR(200) NOT NULL,
    body          TEXT NOT NULL,
    priority      SMALLINT NOT NULL DEFAULT 2,
    status        VARCHAR(20) NOT NULL DEFAULT 'OPEN',
    version       BIGINT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_tickets_org_status_created
    ON tickets(org_id, status, created_at DESC);
```

并发领取核心思路：`UPDATE tickets SET assignee_id=$actor, status='IN_PROGRESS', version=version+1 WHERE id=$id AND org_id=$org AND status='OPEN' AND assignee_id IS NULL RETURNING id;`，影响行数为 0 时区分不存在、无权限与并发冲突。涉及 `ticket_events` 时，在一个数据库事务里完成。

### 4.3 多租户设计

- 业务表必须携带 `org_id`，请求从认证身份和组织成员关系取得有效组织上下文；绝不直接信任客户端提交的 `org_id`。
- Repository 查询必须有组织过滤条件。聊天、缓存 Key、文档检索、向量索引查询、工具调用均需执行相同隔离规则。
- 文件对象键不使用原始文件名拼接目录；下载由后端鉴权后下发受控访问。
- 设计越权测试：组织 A 成员无法访问组织 B 工单、文档、聊天、工具调用结果。

### 4.4 索引与迁移策略

索引以真实查询为依据，优先为 `org_id + status + created_at`、`org_id + assignee_id`、`ticket_id + created_at` 建立合适索引。每次增加索引，记录 `EXPLAIN (ANALYZE, BUFFERS)` 前后对比。向量检索先使用精确距离检索建立基线；当数据量与查询需求证明有价值时再尝试 HNSW，并衡量召回率差异及租户过滤影响。

## 5. HTTP API 契约（初始建议）

统一前缀 `/api/v1`；JSON 响应包含 `data`、`error`、`request_id` 等字段。错误应使用稳定业务错误码，并与 HTTP 状态码匹配。分页建议使用 `limit + cursor` 或初期 `page + page_size`，选定后保持一致。

| 方法与路径 | 作用 | 权限/要点 |
| --- | --- | --- |
| `POST /auth/register` | 注册 | 频率限制、密码安全存储 |
| `POST /auth/login` | 登录 | 统一错误提示、失败限制 |
| `GET /me` | 当前身份 | 校验 token |
| `GET /organizations/current` | 当前组织 | 验证成员关系 |
| `GET /members` | 成员列表 | 管理员或授权成员 |
| `POST /tickets` | 创建工单 | 幂等键可选/后续强制 |
| `GET /tickets` | 工单分页查询 | 租户范围与过滤 |
| `GET /tickets/{id}` | 工单详情 | 单条数据权限 |
| `PATCH /tickets/{id}` | 编辑标题、优先级等 | 角色和字段白名单 |
| `POST /tickets/{id}/claim` | 领取工单 | 并发条件更新 |
| `POST /tickets/{id}/transition` | 状态流转 | 状态机校验 |
| `POST /tickets/{id}/comments` | 新增评论 | 事务和审计 |
| `POST /documents` | 上传文档 | 类型、大小、配额校验 |
| `GET /documents` | 文档列表 | 租户隔离 |
| `GET /documents/{id}/status` | 解析进度 | 任务状态与错误摘要 |
| `POST /knowledge/search` | 检索文档 | 仅返回授权结果 |
| `POST /ai/conversations` | 创建对话 | 记录 owner 与 org |
| `POST /ai/conversations/{id}/messages` | AI 流式问答 | SSE、超时、取消 |
| `POST /ai/tool-calls/{id}/confirm` | 确认写操作 | 绑定发起人、有效期和参数摘要 |
| `GET /healthz`、`GET /readyz` | 健康与就绪 | 不暴露凭据 |

**示例错误语义：** `400` 参数错误；`401` 未认证；`403` 禁止访问；`404` 不可见或不存在；`409` 状态冲突/幂等键不匹配；`429` 限流；`503` 外部模型或必要基础服务暂不可用。`404` 与 `403` 的使用应兼顾防止跨组织对象探测。

## 6. AI 子系统详细设计

### 6.1 文档处理规范

V1 文档格式从 TXT、Markdown、文本型 PDF 开始。扫描 PDF OCR 是扩展需求，不在 P1 承诺范围。验证 MIME 与扩展名、限制大小和数量；文件内容作为不可信输入处理；任何文档文本都不能变成系统指令。

分段初始策略可采用目标长度与重叠窗口，保留文档名、页码或段落位置等来源元数据。Chunk 参数作为可调配置，不能声称某个固定值普遍最优。

### 6.2 RAG 检索和评测

**最小链条：** embedding 查询 -> 租户范围内向量 Top-K -> 生成答案 -> 返回引用（文档 ID、标题、片段位置）。

**优化链条：** 查询预处理 -> 关键词与向量混合召回 -> 归并去重 -> Rerank -> 引用校验 -> 生成。中文关键词检索需要专门处理分词，不能假定 PostgreSQL 默认英文全文检索就足够。

准备人工标注的小规模测试集，初始建议 50～100 道问题，覆盖专业术语、产品编号、跨段落问题、无答案问题和跨租户访问。记录 `Recall@5`、`MRR`、带来源答案占比、引用是否匹配、P50/P95 延迟及 token 成本。指标必须来自实际运行的日志和评测代码。

### 6.3 AI 网关与流式响应

为聊天和 Embedding 定义 `ModelClient` 适配接口，隔离外部模型服务。实现请求超时、上下文取消、有限重试、重试退避、并发上限、用量统计和错误分类。避免对已经产生副作用的调用做无条件重试。

SSE 建议事件：`message_start`、`content_delta`、`citation`、`tool_approval_required`、`message_end`、`error`。每个事件要有清晰 schema；断开连接后及时取消无须继续运行的上游生成任务。

### 6.4 Agent 与 MCP 的边界

先实现原生 Function Calling，理解工具注册、参数校验、执行、结果回传和终止条件。再引入 Eino 编排。MCP 放在 P2，用官方 Go SDK 将指定只读工具对外暴露；写操作仅在认证与确认机制完备后开放。

安全要求：工具白名单、参数 schema 校验、组织与资源授权、调用次数/成本上限、审计记录。写工具需采用待确认态，确认必须绑定用户身份、请求参数摘要与过期时间。禁止模型自由构造 SQL、Shell 命令、内部网络地址或任意文件路径。

## 7. 可靠性与性能设计

### 7.1 异步任务

采用至少一次交付语义，Worker 必须幂等。以 `document_id + parser_version + embedding_model` 等稳定标识定义业务去重维度。任务状态：`PENDING / RUNNING / SUCCEEDED / FAILED / RETRYING`。失败要保存可追踪错误码，按可重试和不可重试分类；达到最大次数后进入人工可见的失败状态。

需要明确处理：Worker 在写入向量后崩溃、消息重复投递、任务已取消、文档被删除、模型 API 超时、Redis 不可用。P2 可加入 Outbox 来减少数据库提交成功但消息未入队的不一致窗口。

### 7.2 缓存与限流

初期不缓存频繁变动的关键工单状态。优先缓存低风险的组织配置、公开配置或昂贵的读取结果。Key 必须包含租户标识；明确 TTL、失效时机与降级行为。限流至少覆盖登录、上传、AI 问答和工具调用，不同接口采用不同配额。

### 7.3 性能验证（拟定目标，非实测）

| 维度 | 初步目标 | 评测条件 |
| --- | --- | --- |
| 纯业务列表/详情 | P95 小于 200 ms | 本地固定数据量、固定硬件、无外部模型 |
| 工单创建 | 无重复写入与状态异常 | 并发压测与幂等测试 |
| 文档异步上传 | HTTP 快速返回任务 ID | 记录文件大小、网络、存储环境 |
| RAG 检索 | 有评测基线并能比较优化 | 固定语料、查询集、模型与 Top-K |
| AI 问答 | 记录首 token 延迟与总延迟 | 模型与网络条件单独记录 |
| 任务系统 | 重复投递后最终结果一致 | 重启 Worker、超时、失败注入 |

压测工具建议使用 `k6` 或 `vegeta`；Go 性能分析使用 `pprof`、基准测试与执行计划。不得把第三方模型服务耗时混成纯 Go API 的性能结论。

## 8. 安全、隐私与治理

- 密码使用成熟算法安全散列（如 Argon2id 或经充分配置的 bcrypt），不存明文；密钥只从环境或密钥管理中读取。
- JWT 签名密钥独立配置，验证算法、签发方、有效期；刷新机制按需实现，记录登出/失效策略。
- API 必须有对象级权限检查和组织隔离；前端隐藏按钮不能代替后端鉴权。
- 防范 Prompt Injection：文档、用户文本、模型结果均不可信；模型不能提升权限；限制工具能力并记录审批。
- 上传实施文件大小限制、类型检查、安全文件名生成；对非可信文档解析进程使用资源与网络限制。
- 日志不能写入 access token、密码、完整 API Key；对敏感文档和 AI 对话采用最小化保存策略。
- 提供演示数据集，避免使用真实客户、学校、单位的私有数据；第三方模型数据出境、保留政策应按选用服务核查。
- Codex 与项目 AI 的权限边界应分开。用于开发的编码助手不应获取生产凭据；平台内部 Agent 也不得继承开发工具权限。

## 9. 测试、验收与演示

### 9.1 测试矩阵

| 测试层 | 必测案例 | 证据 |
| --- | --- | --- |
| 单元测试 | 状态流转、业务校验、工具参数校验 | `go test` 报告 |
| 数据库集成测试 | 事务回滚、并发领取、租户过滤 | 测试脚本、日志 |
| HTTP 集成测试 | 登录、创建工单、分页、错误码 | 自动化 API 测试 |
| Worker 测试 | 重试、重复任务、超时与重启恢复 | 任务状态与执行记录 |
| AI 评测 | Recall@K、引用匹配、无答案处理 | 问题集与评测结果 |
| 安全测试 | 越权请求、恶意文件、提示注入、错误工具参数 | 测试清单与结果 |
| UI E2E | 登录、建单、处理、知识问答 | 演示视频/自动化脚本 |

### 9.2 里程碑验收

**G1 业务 MVP：** 用户能登录，在组织内创建、查看、领取、流转工单；并发领取正确；README 可在新环境复现；关键测试通过。

**G2 AI MVP：** 用户能上传文档并查看解析状态；在租户权限下得到带真实来源的知识问答；Worker 失败可恢复；外部模型失败不影响工单核心业务。

**G3 求职展示版：** 工具调用需要确认并留下审计；至少两项有实测数据的性能/可靠性改进；有架构决策、技术复盘、压测与 RAG 评测报告、演示视频。

### 9.3 演示脚本（5～8 分钟）

1. 使用管理员账号创建组织和成员，展示两个不同角色。
2. 通过用户账号创建工单，同时模拟两个工程师领取同一工单，展示只有一个成功。
3. 上传产品排障文档，查看异步处理进度，展示 Worker 日志或任务状态。
4. 在 AI 页面询问排障问题，查看引用来源，并演示资料不足时不编造答案。
5. 让 Agent 提议创建工单，显示确认弹窗，确认后展示新工单及审计记录。
6. 用一页实验结果说明自己做过的性能或可靠性优化，而非只展示功能界面。

## 10. 研发决策与风险

### 10.1 关键 ADR（架构决策记录）

| 编号 | 决策 | 原因 | 触发重评条件 |
| --- | --- | --- | --- |
| ADR-001 | 模块化单体优先 | 个人开发阶段成本可控 | 负载、独立扩缩容或组织边界明确 |
| ADR-002 | PostgreSQL 为事实来源 | 事务与数据关系清晰 | 真实规模与访问模式改变 |
| ADR-003 | 先精确检索，再引入 HNSW | 建立可比较基线 | 数据量和性能证据支持 |
| ADR-004 | 原生工具调用先于 Eino | 掌握底层原理 | 工具流程变复杂后评估 |
| ADR-005 | Agent 写操作需二次确认 | 防越权和错误执行 | 不得以便利性绕过 |

### 10.2 主要风险

| 风险 | 表现 | 应对 |
| --- | --- | --- |
| 范围失控 | 一直扩展功能，基础业务不完成 | P0 冻结，新增需求先排优先级 |
| AI SDK 变化 | 接口更新、版本冲突 | 固定依赖版本，阅读官方迁移说明 |
| AI 费用或网络限制 | Embedding/聊天调用失败 | Mock Provider、预算上限、有限重试 |
| 异步数据重复 | 多次解析、重复向量写入 | 业务幂等键、唯一约束、重放测试 |
| 权限泄露 | 跨组织查看文档/工单 | 数据层约束、对象级授权、负向测试 |
| 前端时间投入过多 | UI 美化影响后端学习 | 选用成熟组件库，优先可用性 |
| 研二求职冲突 | 实习/论文打断进度 | 保留最低可演示版本与减配方案 |

## 11. 交付清单与完成定义

交付 GitHub 仓库（敏感信息已剔除）、一键部署说明、数据库迁移、API 文档、测试与评测脚本、架构图、ADR、故障案例记录、演示视频和真实测量结果。每个完成的功能必须同时满足“代码在库、可运行、测试通过、文档同步、本人能解释”五个条件。

## 12. 参考资料与使用说明

以下是技术选型及实践资料，不代表要求使用最新版本；正式编码时再次核对兼容性。

- CloudWeGo Eino: https://github.com/cloudwego/eino
- MCP 官方 Go SDK: https://github.com/modelcontextprotocol/go-sdk
- pgvector: https://github.com/pgvector/pgvector
- Asynq: https://github.com/hibiken/asynq （留意其 v0.x API 稳定性说明）
- OpenTelemetry Go: https://opentelemetry.io/docs/languages/go/
- OWASP GenAI Security: https://genai.owasp.org/llm-top-10/

**使用原则：** 本文为开发基线。每个阶段的技术决策、目标值和 API 契约经过实际验证后，应通过 ADR 或版本提交更新，避免文档与代码分离。

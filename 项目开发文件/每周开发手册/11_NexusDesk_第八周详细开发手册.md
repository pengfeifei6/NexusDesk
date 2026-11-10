# NexusDesk 第八周详细开发手册

**版本**：W08 · v1.0  
**主题**：SSE 流式问答、原生 Function Calling、受控 Agent 工具调用与人工确认  
**适用对象**：计算机科班本硕、研二；已完成 Go/Gin/PostgreSQL、组织权限、工单、文件知识库、RAG、Redis + Asynq Worker 基础  
**建议投入**：22～28 小时；如果模型 API、前端流式解析或权限测试耗时较多，可以拆成两周  
**个人学习重点**：你亲自实现 SSE 服务端协议、模型适配接口、工具注册与执行边界、参数校验、待确认操作和数据库幂等；Codex 负责受限的代码审查、测试补充、前端辅助和排障分析。

> 本手册是 W08 开发计划与设计模板，不表示功能已经实现或测试通过。必须在前七周的 NexusDesk 仓库上增量开发。W06 的非流式 RAG Ask 和 W07 的异步索引必须继续可用。本周只实现一个范围很小的 Agent：两个只读工具，以及一个需要人工确认的创建工单提议。**不允许模型直接创建工单，不做任意 SQL、Shell、文件系统或 URL 工具，也不引入 MCP/Eino 作为本周前置条件。**

---

## 0. 本周目标、技术边界与验收定义

### 0.1 P0 必须完成

1. 新增 AI 对话与消息持久化。对话必须绑定 `org_id` 和创建者 `owner_user_id`；第一版仅允许创建者访问自己的对话。
2. 新增流式消息接口，使用 HTTP SSE 返回模型输出。建议使用 `POST + fetch + ReadableStream`，不要直接依赖浏览器 `EventSource`，因为本项目需要携带 Bearer Token 和 JSON 请求体。
3. 定义稳定的 Go 模型适配接口，将具体模型供应商的 SDK、请求结构、流式事件和工具调用格式隔离在 Adapter 内。
4. 将模型流转换为项目自己的事件类型，例如 `TextDelta`、`ToolCallReady`、`Usage`、`Completed`、`Failed`。工具参数必须等到完整事件到达后再解析，不能执行尚未接收完的 JSON 片段。
5. 实现原生 Function Calling 的最小工具循环。模型只能选择服务端注册的工具，工具执行由 Go 代码完成，不能把模型返回的函数名或参数直接当作授权结果。
6. 注册两个只读工具：`search_knowledge` 和 `get_ticket`。工具内部必须复用现有 RAG/Ticket Service，并从已认证请求上下文取得用户与组织，不接受模型传入 `org_id`、`user_id` 或角色。
7. 注册一个写操作提议工具：`propose_create_ticket`。它只能生成待确认的工单创建提议，不得直接创建工单。
8. 新增待确认操作记录。提议需绑定组织、对话、发起用户、工具名称、规范化参数摘要、过期时间和状态。
9. 新增确认与拒绝接口。确认时必须重新检查用户身份、组织成员关系、角色权限、提议状态、过期时间和参数摘要。确认后通过现有 Ticket Service 创建工单，并确保重复确认不会重复创建。
10. 前端可以流式显示回答、来源引用、工具提议和确认/拒绝按钮；用户中止生成后，后端应取消模型请求。
11. 建立模型不可用、流中断、非法工具参数、跨组织读取、未确认写操作、重复确认、过期提议等测试。
12. 更新 `AGENTS.md`、README、API 文档、测试记录和每周复盘。

### 0.2 本周不做

- 不让模型直接执行 `create_ticket`。模型只允许调用 `propose_create_ticket`，真实写入必须由用户确认接口完成。
- 不提供任意 SQL、Shell、命令执行、任意 URL 请求、任意文件路径访问工具。
- 不允许模型自行指定租户、用户身份、权限角色或数据库连接。
- 不做自动审批、自动发消息、删除工单、修改权限等高风险动作。
- 不引入 MCP、Eino、LangGraph 等编排框架。先把底层工具调用流程写清楚，后续再评估框架价值。
- 不实现多 Agent、长期记忆、复杂计划器、反思循环或无限工具调用。
- 不把模型输出的引用 ID 当作可信引用。来源必须由后端根据本次实际检索结果构造。
- 不把“提示词写了不能越权”当作安全措施。权限必须在每个工具执行函数和业务 Service 中验证。
- 不因 SSE 接口成功建立连接就认定模型回答成功。必须区分完成、失败、中断和等待确认。

### 0.3 本周演示场景

用户 Alice 登录组织 A，在对话中问：“根据知识库说明，什么情况下需要升级工单？另外帮我创建一个高优先级工单，标题是‘检查文档索引失败’。”

系统先通过 `search_knowledge` 查询组织 A 的知识库，并通过 SSE 流式返回回答和真实来源。模型随后提出 `propose_create_ticket`，服务端创建待确认提议，前端展示工单标题、描述、优先级和有效期。Alice 点击确认后，后端再次检查她的权限，调用既有 Ticket Service 创建工单并写入审计事件。若用户没有点击确认，工单不得创建。组织 B 用户不能查看 Alice 的对话、提议或工单。

---

## 1. Day 0：检查前七周成果（30～60 分钟）

在 PowerShell 中进入项目目录，路径按本机实际情况修改：

```powershell
Set-Location 'D:\workspace\nexusdesk'

git status --short
go version
go test ./...
go vet ./...
go build ./...
docker compose --env-file .env -f deploy/compose.yaml ps
```

确认以下内容：

- W03/W04 工单创建、详情、状态流转、并发领取仍可用。
- W05 文档上传、解析和组织隔离仍可用。
- W06 Search/Ask 能返回真实来源，不能伪造引用。
- W07 异步索引任务能提交、执行、查询状态，Worker 可独立启动。
- 当前模型配置能完成一次非流式 RAG Ask，且密钥仅来自环境变量。
- 前端登录态和组织选择逻辑正常。

如果前七周的功能没有通过回归测试，先修复再进入 W08。保存基线提交：

```powershell
git switch -c feat/week08-sse-agent-tools
```

检查现有模型供应商、Go SDK 和 API 版本。不要因为本周需要流式输出，就直接把业务层改成某个供应商的专有结构。供应商差异应留在 `internal/platform/llm` 或现有模型适配目录。

---

## 2. 本周目录规划

下面是增量建议，必须结合仓库现有结构调整，不要为了匹配树形图而大规模搬动已有代码。

```text
nexusdesk/
├── cmd/
│   ├── api/main.go
│   └── worker/main.go
├── internal/
│   ├── ai/
│   │   ├── conversation/
│   │   │   ├── service.go
│   │   │   └── repository.go
│   │   ├── model/
│   │   │   ├── client.go
│   │   │   └── types.go
│   │   ├── agent/
│   │   │   ├── loop.go
│   │   │   ├── registry.go
│   │   │   ├── executor.go
│   │   │   └── actor.go
│   │   ├── tools/
│   │   │   ├── search_knowledge.go
│   │   │   ├── get_ticket.go
│   │   │   └── propose_create_ticket.go
│   │   └── approval/
│   │       ├── service.go
│   │       └── repository.go
│   └── transport/http/
│       └── ai/
│           ├── handler.go
│           └── sse.go
├── migrations/
│   └── 00012_create_ai_conversations_messages_actions.sql
├── web/src/
│   └── features/ai/
│       ├── api.ts
│       ├── useSSE.ts
│       ├── ChatPage.vue
│       └── components/
│           ├── ChatMessage.vue
│           ├── SourceList.vue
│           └── ToolApprovalCard.vue
├── tests/integration/
│   ├── ai_stream_test.go
│   └── ai_tool_approval_test.go
├── docs/
│   ├── ai/
│   │   ├── agent-flow.md
│   │   ├── tool-registry.md
│   │   ├── sse-protocol.md
│   │   └── approval-security.md
│   ├── api/ai-conversation.md
│   ├── decisions/0008-native-function-calling-first.md
│   ├── testing/week08-test-plan.md
│   └── weekly/week08.md
└── requests/week08.http
```

### 2.1 创建目录和空文件

```powershell
$dirs = @(
  'internal/ai/conversation',
  'internal/ai/model',
  'internal/ai/agent',
  'internal/ai/tools',
  'internal/ai/approval',
  'internal/transport/http/ai',
  'web/src/features/ai/components',
  'tests/integration',
  'docs/ai',
  'docs/api',
  'docs/decisions',
  'docs/testing',
  'docs/weekly',
  'requests'
)

foreach ($d in $dirs) {
  New-Item -ItemType Directory -Force $d | Out-Null
}

$files = @(
  'docs/ai/agent-flow.md',
  'docs/ai/tool-registry.md',
  'docs/ai/sse-protocol.md',
  'docs/ai/approval-security.md',
  'docs/api/ai-conversation.md',
  'docs/decisions/0008-native-function-calling-first.md',
  'docs/testing/week08-test-plan.md',
  'docs/weekly/week08.md',
  'requests/week08.http'
)

foreach ($f in $files) {
  if (-not (Test-Path $f)) {
    New-Item -ItemType File $f | Out-Null
  }
}
```

不要覆盖已有目录和文件。`AGENTS.week08.append.md` 要合并到根目录现有 `AGENTS.md`，不能替换前七周规则。

---

## 3. Day 1：Agent 流程、模型接口与工具边界设计（2.5～3.5 小时）

### 3.1 先画出最小 Agent 流程

建议流程：

```text
用户消息
  |
  v
Gin Handler：验证 JWT、组织成员、对话归属
  |
  v
Conversation Service：保存 user message
  |
  v
Agent Loop：构造历史消息、RAG 上下文、工具定义
  |
  v
Chat Model Adapter：接收流式事件
  |
  +---- Text Delta --------------------> SSE content_delta
  |
  +---- Tool Call Ready
           |
           v
      Tool Registry 白名单
           |
           v
      参数 JSON 解析 + Go 侧校验
           |
           +---- search_knowledge ----> 复用 RAG Service
           |
           +---- get_ticket ----------> 复用 Ticket Service
           |
           +---- propose_create_ticket -> 保存待确认提议
                                               |
                                               v
                                   SSE tool_approval_required
                                               |
                                               v
                                         用户确认
                                               |
                                               v
                                   Approval Service 二次鉴权
                                               |
                                               v
                                      Ticket Service 创建
```

**必须理解**：模型只负责提出工具调用意图。工具是否存在、参数是否合法、当前用户是否有权访问目标资源、写操作是否已经得到确认，全部由服务端代码决定。

### 3.2 设计模型适配接口

你先写接口草案，再让 Codex 只读审查。建议业务层不要直接依赖某个 SDK：

```go
type ChatModel interface {
    Stream(
        ctx context.Context,
        req ChatRequest,
        emit func(ModelEvent) error,
    ) error
}
```

`ChatRequest` 至少包含：

- 模型标识（由服务端配置，不由用户任意指定）
- 系统指令和历史消息
- 本轮可用工具定义
- 最大输出 Token 或等价预算
- 是否启用工具调用
- 温度等可选生成参数（本周可以固定）

`ModelEvent` 使用项目自己的类型表达：

- `TextDelta`：新增文本片段
- `ToolCallReady`：完整工具名称与完整参数 JSON
- `Usage`：输入/输出 Token 用量（供应商支持时）
- `Completed`：本轮模型输出结束
- `Failed`：经过分类的模型错误

不要把供应商的原始 SSE JSON 直接传到 Handler。Adapter 应完成协议转换，并负责把分片到达的工具参数拼接成完整 JSON。**未收到完整工具调用前绝不能执行工具。**

### 3.3 定义工具清单

| 工具名 | 类型 | 模型可调用 | 真实副作用 | 权限规则 |
|---|---|---:|---|---|
| `search_knowledge` | 只读 | 是 | 无 | 仅当前组织可检索 |
| `get_ticket` | 只读 | 是 | 无 | 仅当前组织且当前用户可见 |
| `propose_create_ticket` | 写操作提议 | 是 | 只创建待确认记录 | 用户必须有创建工单权限 |
| `create_ticket` | 真实写入 | **否** | 创建工单 | 仅确认接口可调用 |

模型工具参数中不提供 `org_id`、`user_id`、`role`、`created_by`。这些值从经过认证的 Actor 上下文取得。

### 3.4 今天自己完成的文件

- `docs/ai/agent-flow.md`
- `docs/ai/tool-registry.md`
- `docs/decisions/0008-native-function-calling-first.md`
- `docs/api/ai-conversation.md` 的初版

### 3.5 Codex 只读审查提示词

```text
请只读审查 NexusDesk 当前 W06 RAG Service、W03/W04 Ticket Service、
组织鉴权中间件和我刚写的 docs/ai/agent-flow.md。

请检查：
1. Agent 是否绕过现有业务 Service 直接访问数据库；
2. 工具执行是否能从认证上下文取得 org_id/user_id；
3. 写工具是否可能被模型直接执行；
4. RAG 来源是否由后端构造；
5. 模型 SDK 是否会侵入业务层；
6. 工具调用循环是否有次数上限。

按严重程度列出问题和代码位置。只给建议，不修改文件。
```

**Day 1 验收**：你能不看文档讲清楚模型、Agent Loop、Tool Registry、Tool Executor、Approval Service 的职责区别。

---

## 4. Day 2：对话、消息与待确认操作的数据模型（3～4 小时）

### 4.1 建议新增三张表

**`ai_conversations`**

| 字段 | 说明 |
|---|---|
| `id` | 对话 UUID |
| `org_id` | 所属组织 |
| `owner_user_id` | 创建者 |
| `title` | 对话标题，可后续生成 |
| `status` | ACTIVE / ARCHIVED |
| `created_at`、`updated_at` | 时间 |

**`ai_messages`**

| 字段 | 说明 |
|---|---|
| `id` | 消息 UUID |
| `org_id` | 冗余租户字段，查询仍要校验 |
| `conversation_id` | 所属对话 |
| `client_request_id` | 客户端幂等 ID，防止重复提交 |
| `role` | USER / ASSISTANT / TOOL |
| `content` | 消息文本 |
| `status` | STREAMING / COMPLETED / FAILED / INTERRUPTED |
| `model_name` | 实际模型标识 |
| `input_tokens`、`output_tokens` | 可用时记录 |
| `created_at`、`completed_at` | 时间 |

建议唯一约束：`(conversation_id, client_request_id)`。如果数据库中已有消息表或项目已采用其他消息模型，优先复用，不要重复建模。

**`ai_tool_actions`**

| 字段 | 说明 |
|---|---|
| `id` | 提议 ID |
| `org_id`、`conversation_id` | 租户与对话 |
| `assistant_message_id` | 产生提议的助手消息 |
| `tool_name` | 本周固定为 `propose_create_ticket` |
| `arguments_json` | 服务端校验并规范化后的参数 |
| `arguments_sha256` | 规范化参数摘要 |
| `status` | PROPOSED / EXECUTED / REJECTED / EXPIRED / FAILED |
| `proposed_by_user_id` | 提议关联用户 |
| `confirmed_by_user_id` | 实际确认人 |
| `expires_at` | 提议有效期 |
| `ticket_id` | 创建成功后关联的工单 |
| `created_at`、`confirmed_at` | 时间 |

Migration 序号 `00012` 只是示例，必须检查本地实际迁移编号。不要修改已执行过的 W01～W07 Migration。

### 4.2 写操作确认的关键约束

1. 参数先经 Go 结构体解析和业务校验，再序列化成规范化 JSON。
2. 对规范化参数计算 SHA-256，存储摘要和完整参数。
3. 提议绑定 `org_id`、`conversation_id`、`proposed_by_user_id` 和过期时间。
4. 确认时从 JWT/服务端 Actor 取得当前用户，不能信任请求体里的 `confirmed_by_user_id`。
5. 在数据库事务内锁定提议行，验证状态仍为 PROPOSED、未过期、组织一致、确认者有权限。
6. 通过已有 Ticket Service 创建工单，写入工单事件/审计记录，并把提议状态更新为 EXECUTED。
7. 重复确认时返回同一个 `ticket_id`，不得创建第二张工单。
8. 如果 Ticket Service 目前无法复用同一个事务，先设计事务边界或事务接口，不要用“先建工单、再更新提议”的无保护双写。

### 4.3 今天由你亲自完成

- 编写 Migration 草稿。
- 检查主键、外键、时间字段和索引类型是否与已有表一致。
- 执行 Up/Down，并在隔离测试库验证约束。
- 编写 `ConversationRepository` 的创建和归属查询。
- 编写 `ApprovalRepository` 的提议读取和状态条件更新。

**Day 2 验收**：对话只能由所有者访问；同一 `client_request_id` 不能重复创建消息；待确认提议能够过期；Migration 可回滚。

---

## 5. Day 3：实现 SSE 流式问答（3～4 小时）

### 5.1 为什么使用 SSE

SSE 是服务端向浏览器持续发送文本事件的 HTTP 机制，适合本周单向的模型输出流。客户端提交消息后，服务端逐段返回文本、引用和状态事件。

本项目使用 `POST` 提交消息，因此前端建议采用 `fetch` 读取 `ReadableStream`，而不是 `EventSource`。`EventSource` 更适合浏览器原生 GET 订阅，无法方便地携带当前项目所需的 JSON Body 和自定义 Bearer Header。

### 5.2 SSE 响应头

在开始写入响应体前设置：

```http
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache, no-transform
X-Accel-Buffering: no
```

如果使用 Gin，必须确认当前 `ResponseWriter` 支持 Flush。底层 Go `net/http` 使用 `http.Flusher` 时应在运行时检查，不要假设所有包装器都支持。代理或网关也可能缓冲响应，需要通过实际环境验证。

### 5.3 项目事件协议

每条 SSE 事件使用：

```text
event: content_delta
data: {"message_id":"...","delta":"新增文本"}

```

建议事件如下：

| 事件 | 触发时机 | 主要字段 |
|---|---|---|
| `message_start` | 消息开始 | `message_id`、`conversation_id` |
| `content_delta` | 收到文本片段 | `message_id`、`delta` |
| `citation` | 后端确认来源 | `document_id`、`title`、`chunk_id`、`position` |
| `tool_approval_required` | 产生写操作提议 | `proposal_id`、`tool_name`、`preview`、`expires_at` |
| `message_end` | 正常结束或等待确认 | `message_id`、`status`、`usage` |
| `error` | 流开始后发生错误 | `code`、`message`、`retryable` |

要求：

- 事件名和 JSON 字段固定并写入 `docs/ai/sse-protocol.md`。
- JSON 内容必须使用 `json.Marshal`，不要手工拼接字符串。
- 文本中的换行由 JSON 编码处理，不要破坏 SSE 的空行分隔。
- 开始流式响应前发生的鉴权/参数错误，使用正常 HTTP 状态码返回。
- 已经开始流式响应后发生错误，不能再修改 HTTP 状态码，应发送 `error` 事件并结束。
- 发送心跳时使用 SSE 注释行，例如 `: ping\n\n`，不要把心跳当作模型文本。
- 每个事件后及时 Flush，并用 `curl -N` 或前端实际页面检查是否逐段到达。
- 不能把模型的原始错误、内部 Prompt、API Key、SQL 或文件路径发送给浏览器。

### 5.4 请求取消

Handler 使用 `c.Request.Context()` 向模型 Adapter 和 Agent Loop 传递取消信号。浏览器点击停止或关闭页面时，前端调用 `AbortController.abort()`；服务端检测到 Context 取消后，停止上游模型请求并把助手消息标记为 INTERRUPTED。

不要为普通流式请求使用 `context.Background()` 启动一个脱离用户连接的无限任务。若需要在取消后保存中断状态，只允许使用有严格超时的清理上下文完成一次短暂数据库写入。

### 5.5 今天自己实现

- `ChatModel` 接口和项目内 `ModelEvent`。
- SSE Writer、事件序列化、Flush 和错误处理。
- 纯文本流式输出，不带工具调用。
- 请求取消和消息状态更新。
- 一个 Mock ChatModel，用于不依赖真实模型 API 的自动化测试。

**Day 3 验收**：浏览器可以看到逐段输出；停止按钮能取消请求；模型超时能收到 `error` 事件；非流式 W06 Ask 仍正常。

---

## 6. Day 4：只读工具注册、参数校验与 Agent Loop（3～4 小时）

### 6.1 先实现 Tool Registry

工具定义由服务端注册，不允许模型传入新工具代码。每个工具包含：

- 固定名称
- 工具用途说明
- JSON Schema
- 风险等级（READ_ONLY / WRITE_PROPOSAL）
- 服务端执行函数

建议先定义最小接口：

```go
type Tool interface {
    Name() string
    Definition() ToolDefinition
    Execute(
        ctx context.Context,
        actor Actor,
        args json.RawMessage,
    ) (ToolResult, error)
}
```

`Actor` 是服务端从认证上下文构造的身份信息，至少包括 `UserID`、`OrgID` 和必要角色/权限。不能由模型参数构造。

### 6.2 两个只读工具

**`search_knowledge` 参数建议**

```json
{
  "query": "如何处理索引失败？",
  "top_k": 5
}
```

服务端验证 `query` 长度、`top_k` 范围，并调用 W06 现有 Search Service。组织 ID 从 Actor 取得。结果只返回当前组织已授权的片段和来源元数据。

**`get_ticket` 参数建议**

```json
{
  "ticket_id": "服务端实际使用的工单 ID"
}
```

服务端必须调用现有 Ticket Service 或 Repository 的组织范围查询。不要先按 ID 查全局数据，再在应用层忘记校验组织。

### 6.3 参数校验不能只依赖 JSON Schema

即使模型提供商支持 Strict JSON Schema，也要在 Go 服务端重新验证：

- 工具名是否在白名单；
- JSON 是否完整、可解析；
- 是否存在未知字段；
- 字符串长度、枚举、数值范围；
- ID 格式是否符合项目实际类型；
- 用户是否有权访问对应资源；
- 结果是否需要脱敏。

对于不支持 Strict Mode 的模型供应商，Go 侧验证仍然必须存在。

### 6.4 Agent Loop 必须有限

建议本周约束：

- 单轮最多 3 次工具调用轮次；
- 单轮最多 4 个工具调用；
- 工具调用顺序执行，暂不启用并行工具；
- 模型最多生成一个待确认写操作；
- 未知工具直接拒绝；
- 工具执行失败只向模型返回脱敏错误；
- 达到上限后结束本轮并说明无法继续。

模型调用流程是：发送对话与工具定义 → 收到完整 Tool Call → Go 验证并执行 → 把工具结果回传模型 → 模型继续生成最终文本。不要把工具调用结果当作最终答案，也不要无限循环。

### 6.5 今天自己完成

- Tool Registry。
- JSON 参数解析和验证。
- `search_knowledge` 与 `get_ticket` 两个工具。
- 有最大轮数的 Agent Loop。
- 来源 ID 收集与后端引用构造。

**Day 4 验收**：模型可以调用两个只读工具；工具结果确实来自当前组织；传入其他组织的工单 ID 不会泄漏信息；循环达到上限会终止。

---

## 7. Day 5：写操作提议与人工确认（3～4 小时）

### 7.1 只暴露提议工具

模型可调用：

```json
{
  "title": "检查文档索引失败",
  "description": "知识库文档索引多次失败，需要排查。",
  "priority": "HIGH"
}
```

工具名称：`propose_create_ticket`。

它的唯一职责是：

1. 解析参数；
2. 验证标题、描述和优先级；
3. 从 Actor 取得组织与发起用户；
4. 计算规范化参数 JSON 和 SHA-256；
5. 写入 `ai_tool_actions`，状态为 PROPOSED；
6. 返回提议 ID 和安全展示摘要。

它**不调用 TicketService.Create**，也不直接写入 tickets 表。

### 7.2 前端确认卡片

确认卡片至少展示：

- 将要创建的工单标题；
- 描述；
- 优先级；
- 所属组织；
- 提议创建者；
- 提议失效时间；
- 确认与拒绝按钮。

不能只显示“AI 想执行一个操作”。用户需要看清楚将发生什么。

### 7.3 确认接口

建议：

```http
POST /api/v1/ai/tool-calls/{proposalID}/confirm
Authorization: Bearer <token>
Content-Type: application/json
```

拒绝接口：

```http
POST /api/v1/ai/tool-calls/{proposalID}/reject
Authorization: Bearer <token>
```

确认请求不接受客户端提交完整工单参数，也不接受客户端指定 `org_id` 或 `confirmed_by_user_id`。服务端从数据库读取已保存的规范化提议。

### 7.4 确认服务事务边界

你需要亲自设计以下流程：

1. 开启 PostgreSQL 事务。
2. `SELECT ... FOR UPDATE` 锁定待确认操作。
3. 验证当前用户与提议的组织、对话和发起人关系。
4. 验证状态为 PROPOSED 且尚未过期。
5. 重新检查当前用户的创建工单权限。
6. 使用事务内的 Ticket Service 方法创建工单。
7. 写入工单事件和 AI 操作审计信息。
8. 将提议更新为 EXECUTED，并保存 `ticket_id`。
9. 提交事务。

如果同一确认请求重复到达，服务端读取已完成提议并返回原来的 `ticket_id`。不能重复创建。

如果 Ticket Service 目前只支持自己开事务，先重构出可接收事务句柄的内部方法，或提供统一 Unit of Work；不要复制一套创建工单 SQL。

### 7.5 今天自己完成

- `propose_create_ticket` 工具。
- Approval Service。
- 确认/拒绝接口。
- 事务、锁、过期和重复确认逻辑。
- 工单创建审计记录。

**Day 5 验收**：没有确认不会创建工单；确认后只创建一次；无权限用户不能确认；提议过期后不能执行；拒绝后不能再确认。

---

## 8. Day 6：前端联调、安全测试与故障排查（3～4 小时）

### 8.1 前端页面最小范围

- 对话列表与新建对话；
- 消息输入框；
- 流式回答区域；
- 停止生成按钮；
- 来源引用列表；
- 工具提议确认卡片；
- 错误、超时、取消和空状态。

前端不要把原始模型 Tool Call JSON 当作可信数据展示。确认卡片内容来自后端持久化提议。

### 8.2 Fetch Stream 解析

`fetch` 返回的网络分块不保证刚好对应一条 SSE 事件。一个 SSE 事件可能跨多个 chunk，一个 chunk 也可能包含多条事件。因此你需要自己维护文本缓冲区，按空行边界拆分完整事件，再解析 `event:` 和 `data:`。

不能假设一次 `reader.read()` 就得到一条完整 JSON。前端应处理 UTF-8 多字节字符被拆分的情况，建议使用 `TextDecoder` 的流式解码模式。

### 8.3 安全测试重点

- 组织 A 的用户不能读取组织 B 的对话和提议。
- 模型不能通过参数伪造 `org_id` 或 `user_id`。
- 工具调用必须命中服务端白名单。
- 工具参数中的未知字段和超长内容被拒绝。
- 恶意知识库文本不能让模型获得额外权限。
- 工具返回值作为不可信数据，不直接拼接为系统指令。
- 模型请求不能访问任意 URL、Shell、SQL 或文件路径。
- 没有确认时，不产生工单写入。
- 确认时再次检查角色权限，不能只在生成提议时检查一次。
- 前端以安全文本/受限 Markdown 渲染，禁止直接 `v-html` 渲染模型原文。
- 日志不得记录 Bearer Token、模型 API Key、完整私有文档和敏感 Prompt。

### 8.4 故障演练

至少执行：

1. 模型 API 连接超时；
2. 模型流输出一半后断开；
3. 浏览器点击停止；
4. 模型返回未知工具名；
5. 模型返回不完整 JSON 参数；
6. 用户在提议过期后点击确认；
7. 两个确认请求同时到达；
8. Ticket Service 创建失败；
9. 数据库在流式响应过程中短暂不可用；
10. SSE 经过本地代理后被缓冲。

**Day 6 验收**：完成至少 25 项测试中的核心用例；前端可以从提问到引用展示；待确认提议可以安全地确认或拒绝。

---

## 9. Day 7：Codex Review、演示与独立复盘（2～3 小时）

### 9.1 代码审查顺序

先自己检查：

```powershell
git status
git diff --check
git diff
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

`go test -race` 主要检查 Go 进程内数据竞争，不等于验证 PostgreSQL 事务并发正确性。数据库重复确认还必须用真实 PostgreSQL 集成测试。

然后让 Codex 分三轮审查：

**第一轮：SSE 与取消**

```text
只读审查本次 SSE 实现。重点检查 ResponseWriter Flush、事件格式、
错误发生时的 HTTP/SSE 语义、请求 Context 取消传播、消息状态收尾、
代理缓冲、并发写入和 goroutine 泄漏。先报告问题，不修改文件。
```

**第二轮：Agent 与工具安全**

```text
只读审查 Tool Registry、Agent Loop 和工具执行器。
重点检查白名单、JSON 参数校验、工具调用轮数、租户上下文来源、
是否存在模型直接执行写操作、越权访问、任意 URL/SQL/Shell 能力。
请给出严重程度、文件位置、复现步骤和建议。
```

**第三轮：确认操作与事务**

```text
只读审查 ai_tool_actions 的确认流程。
重点检查重复确认、并发确认、提议过期、参数摘要、组织与用户绑定、
权限二次检查、Ticket Service 事务边界和审计事件。
不要直接修改代码。
```

### 9.2 本周演示脚本

1. 登录 Alice，打开组织 A 的 AI 对话。
2. 提问知识库问题，展示 SSE 逐段输出和真实来源。
3. 询问某张工单，展示 `get_ticket` 只读工具。
4. 要求 AI 创建工单，展示待确认卡片。
5. 不确认时检查数据库，证明工单没有创建。
6. 点击确认，展示新工单 ID 和审计记录。
7. 重复点击或并发发送确认请求，证明只创建一张工单。
8. 登录组织 B，尝试读取组织 A 的对话和提议，确认被拒绝。
9. 点击停止生成，确认上游请求被取消。

### 9.3 独立复盘问题

不看 AI 对话，尝试回答：

1. SSE 与普通 JSON 响应的区别是什么？
2. 为什么这里选择 `POST + fetch`，而不是原生 EventSource？
3. 一个网络分块为什么不一定是一条 SSE 事件？
4. 模型工具调用与工具执行有什么区别？
5. 为什么模型返回合法 JSON 也不能直接执行？
6. 为什么工具执行必须从 Actor 读取 org_id？
7. 为什么写工具要先创建提议？
8. 如何防止重复确认创建两张工单？
9. 确认时为什么还要重新检查权限？
10. 文档中的 Prompt Injection 为什么不能靠系统 Prompt 完全解决？

**Day 7 验收**：完成演示、测试报告、架构决策记录和个人复盘；至少能独立解释 SSE、工具调用和人工确认三项核心机制。

---

## 10. API 契约建议

实际路径需要与你当前项目统一。下面是建议版本：

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/v1/ai/conversations` | 创建对话 |
| GET | `/api/v1/ai/conversations` | 当前用户对话列表 |
| GET | `/api/v1/ai/conversations/{id}/messages` | 查看对话消息 |
| POST | `/api/v1/ai/conversations/{id}/messages` | 发送消息，返回 SSE |
| POST | `/api/v1/ai/tool-calls/{id}/confirm` | 确认待执行提议 |
| POST | `/api/v1/ai/tool-calls/{id}/reject` | 拒绝待执行提议 |

发送消息请求：

```json
{
  "content": "请根据知识库说明索引失败怎么排查。",
  "client_request_id": "客户端生成的唯一请求 ID"
}
```

确认接口不需要提交工单字段，只需调用确认动作。所有数据从已保存的提议中读取。

建议错误语义：

| HTTP | 含义 |
|---|---|
| 400 | 请求格式或参数错误 |
| 401 | 未认证 |
| 403/404 | 无权限或资源不可见，按防止对象探测的策略选择 |
| 409 | 对话/提议状态冲突、重复请求参数不一致 |
| 410 | 提议已过期（也可按项目统一映射为 409） |
| 429 | AI 请求频率或并发额度超限 |
| 502/503 | 上游模型或必要依赖不可用 |
| 504 | 模型请求超时 |

一旦 SSE 响应已经开始，后续错误用 `error` 事件表达，不能试图再返回新的 HTTP 状态码。

---

## 11. 本周测试清单（至少 30 项）

| 编号 | 测试场景 | 预期结果 |
|---|---|---|
| T01 | 未登录创建对话 | 401 |
| T02 | 用户访问其他人的对话 | 拒绝访问 |
| T03 | 用户访问其他组织对话 | 拒绝访问 |
| T04 | 重复 `client_request_id` | 不重复创建消息 |
| T05 | 正常 SSE 文本流 | 多个 `content_delta`，最后 `message_end` |
| T06 | SSE 中途模型报错 | 发 `error`，消息标记失败 |
| T07 | 浏览器主动取消 | 上游 Context 取消 |
| T08 | 模型超时 | 有界结束，不泄漏 goroutine |
| T09 | Writer 不支持 Flush | 返回明确错误或使用受支持的实现 |
| T10 | SSE 经过代理 | 事件能及时到达，不被整体缓冲 |
| T11 | 工具参数 JSON 不完整 | 不执行工具 |
| T12 | 未知工具名 | 拒绝执行 |
| T13 | 未注册工具被模型调用 | 拒绝执行并记录安全事件 |
| T14 | 参数包含额外 `org_id` | 严格拒绝或忽略并记录；绝不改变租户 |
| T15 | `search_knowledge` 正常查询 | 仅返回当前组织来源 |
| T16 | `get_ticket` 查询本组织工单 | 返回授权字段 |
| T17 | `get_ticket` 查询其他组织工单 | 不泄漏数据 |
| T18 | 工具调用达到轮数上限 | Agent Loop 终止 |
| T19 | 模型尝试调用 `create_ticket` | 不在工具白名单，拒绝 |
| T20 | `propose_create_ticket` 参数非法 | 不创建提议 |
| T21 | 正常创建工单提议 | 状态 PROPOSED |
| T22 | 未确认时检查 tickets 表 | 不存在新工单 |
| T23 | 合法用户确认 | 只创建一张工单 |
| T24 | 同一用户重复确认 | 返回同一个 ticket_id |
| T25 | 两个请求并发确认 | 最多创建一张工单 |
| T26 | 提议已过期 | 拒绝执行 |
| T27 | 提议已拒绝后确认 | 拒绝执行 |
| T28 | 确认者权限已被撤销 | 二次鉴权失败 |
| T29 | 工单创建事务失败 | 提议不能错误标记为 EXECUTED |
| T30 | Prompt Injection 要求越权读取 | 工具层仍拒绝 |
| T31 | 模型输出 HTML/脚本 | 前端按安全文本处理 |
| T32 | 日志检查 | 不含 Token、Key、私有全文 |
| T33 | 模型供应商不可用 | 工单业务仍可用 |
| T34 | 工具结果含恶意指令文本 | 作为不可信数据，不改变工具权限 |
| T35 | 对话历史超过限制 | 按策略截断/摘要，不无限增长 |

至少将 T01～T08、T11～T18、T19～T30 做成自动化测试。其余安全用例可以先形成可复现的人工测试记录，但不能只写“测试通过”而没有请求、日志或数据库证据。

---

## 12. 你与 ChatGPT、Codex 的任务分工

| 工作 | 你亲自负责 | ChatGPT 负责 | Codex 负责 |
|---|---|---|---|
| Agent 流程设计 | 决定边界和业务规则 | 讲解原理、比较方案 | 只读检查现有代码依赖 |
| 模型 Adapter | 编写接口和理解事件 | 解释流式协议和接口设计 | 检查适配器、生成 Mock 测试建议 |
| SSE | 亲自写 Writer、取消和错误处理 | 讲解 HTTP/SSE、定位思路 | Review、补充边界测试 |
| 工具系统 | 写 Registry、参数验证、Actor | 解释 Function Calling 机制 | 检查白名单和参数校验 |
| 只读工具 | 亲自接现有业务 Service | 分析权限边界 | 补测试、检查 SQL |
| 写操作确认 | 亲自写事务、锁和幂等 | 解释并发确认风险 | Review、构造并发测试 |
| Vue 页面 | 负责验收交互和事件状态 | 讲解 Fetch Stream | 可在限定目录生成基础组件 |
| 安全测试 | 决定安全标准并验证 | 提供攻击场景 | 生成测试用例、只读审查 |
| 文档与复盘 | 写真实结果和个人理解 | 追问薄弱点 | 整理文档格式，不编造结果 |

### 12.1 Codex 授权规则

默认只读。每次授权修改都要写清楚：

- 允许修改的具体文件；
- 允许新增的文件；
- 不允许改动的模块；
- 验收命令；
- 是否允许新增依赖；
- 是否允许执行 Migration。

禁止让 Codex 一次性完成“整个 Agent”。你需要把任务拆成模型接口、SSE、只读工具、写操作提议、确认服务和前端组件等小任务。

---

## 13. 本周 Git 与验收命令

```powershell
git status
git diff --check
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

建议分阶段提交：

```text
docs: add week08 agent and sse design
feat(ai): add conversation persistence
feat(ai): add sse streaming endpoint
feat(ai): add read-only tool registry
feat(ai): add ticket action proposal flow
feat(ai): add explicit action confirmation
test(ai): cover stream cancellation and tool authorization
```

提交前必须人工检查 `git diff`。不要把 `.env`、模型密钥、真实用户文档或调试日志中的敏感内容提交到 Git。

### 本周最终交付物

- [ ] SSE 流式问答接口可运行
- [ ] ChatModel Adapter 与 Mock 可运行
- [ ] 对话和消息持久化
- [ ] 两个只读工具可运行
- [ ] 一个写操作提议工具可运行
- [ ] 确认/拒绝接口有事务与幂等保障
- [ ] 前端可展示流式回答、引用和确认卡片
- [ ] 至少 30 项测试场景有记录
- [ ] 关键安全测试通过
- [ ] README、AGENTS、ADR 和周复盘已更新
- [ ] 录制 3～5 分钟演示

**W08 完成标准**：用户能够通过 SSE 与知识库对话，模型能够调用限定的只读工具；当模型提出创建工单时，系统先展示待确认操作，只有经过身份和权限二次验证的用户明确确认后，后端才调用既有业务 Service 创建工单。整个过程可测试、可审计、可解释。

---

## 14. 面试准备：本周必须能讲清楚的技术问题

1. SSE 的协议格式是什么？为什么事件以空行分隔？
2. `http.Flusher` 的作用是什么？为什么代理可能造成缓冲？
3. 客户端断开连接后，如何取消模型请求？
4. Function Calling 为什么不是模型直接执行函数？
5. JSON Schema 能解决什么问题，不能解决什么问题？
6. 工具调用为什么必须复用已有业务 Service？
7. 如何防止模型伪造租户 ID？
8. 什么是 Prompt Injection？为什么提示词不是安全边界？
9. 如何设计需要人工确认的写操作？
10. 如何保证重复确认不会重复创建工单？
11. 为什么确认时要重新检查权限？
12. Agent Loop 为什么必须有最大调用轮数和预算？

建议每道题都用 NexusDesk 的代码、测试和实际日志回答，不要只背概念。

---

## 15. 官方资料

1. OpenAI Function Calling 指南：<https://developers.openai.com/api/docs/guides/function-calling>。了解工具定义、JSON Schema、模型提出调用、应用执行和结果回传的多步流程。不同供应商的字段和事件并不完全一致，必须通过 Adapter 隔离。
2. Go `net/http` 文档：<https://pkg.go.dev/net/http#Flusher>。了解 `http.Flusher` 和响应缓冲行为。
3. OWASP Prompt Injection Prevention Cheat Sheet：<https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html>。重点学习工具参数校验、最小权限、人工确认、对检索文档和工具结果保持不信任。
4. WHATWG Server-Sent Events：<https://html.spec.whatwg.org/multipage/server-sent-events.html>。了解 SSE 事件流格式和浏览器处理规则。

**版本说明**：本手册不固定具体模型供应商或 SDK 版本。实际编码前请核对当前模型 API 的工具调用与流式事件文档，并记录你实际使用的模型、SDK 版本和测试条件。本文接口、Migration 和代码片段是设计模板，需要按现有仓库真实表结构和代码调整。

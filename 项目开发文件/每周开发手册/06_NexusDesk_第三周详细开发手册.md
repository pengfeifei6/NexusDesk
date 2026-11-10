# NexusDesk 第三周详细开发手册

**版本：W03 · v1.0**  
**主题：工单核心业务、CRUD、状态机、SQL 事务与操作事件**  
**适用对象：计算机科班本硕、研二，已经能够使用 Go/Gin/PostgreSQL 编写基础接口**  
**建议投入：18～24 小时；以验证结果为准，不为赶进度跳过跨组织测试。**

> 本周直接延续 W01 的模块化单体工程和 W02 的 JWT、组织成员表及权限策略。继续使用同一个 `nexusdesk` 仓库，**只新增本周文件与迁移，不重新初始化、不覆盖已执行的 SQL migration**。这是一份操作与学习手册，而非可直接复制完成的工单模块。核心业务规则、SQL 和 Go Service 由你自己编写，AI 负责解释、审查、提示与受限补测试。

---

## 0. 本周目标、范围与最终演示

### 0.1 本周必须做成的内容

1. 设计并落地 `tickets`、`ticket_comments`、`ticket_events` 三张表，使用新增迁移 `00004`、`00005`、`00006`。
2. 实现组织维度的工单创建、列表与筛选、详情、部分更新、**软删除**，以及评论的新增与查询。
3. 设计可明确验证的状态机：`OPEN`、`IN_PROGRESS`、`RESOLVED`、`CLOSED`；非法转换必须被拒绝。
4. 工单修改、状态变更、删除及评论新增时，同时写入 `ticket_events`；**业务写入与事件写入处于同一数据库事务**。
5. 通过服务端验证 JWT、`org_id` 和有效成员身份，避免跨组织读取或修改；角色控制复用 W02 的 `admin/member`。
6. 实现请求参数检查、分页、数据库约束和稳定的错误响应，完成自动化集成测试与一轮 Codex 只读 Review。
7. 在 `docs/weekly/week03.md` 中记录实际测试、个人完成代码、真实问题和未完成项目。

### 0.2 本周有意不做的内容

- **不做多用户抢单 API `/claim`，也不以它作为 W03 验收项。** 该内容安排在 **W04**，重点训练数据库条件更新、并发测试和前端联调。
- 不做 Vue 前端、Redis 缓存、消息队列、AI、RAG、Agent、MCP 或微服务拆分。
- 不做全局工单搜索、附件、SLA 自动升级、提醒、复杂审批以及公开部署。
- `Idempotency-Key` 的正式持久化与去重保护放到后续增强；本周先通过测试理解重复 POST 的结果，并在 API 文档标注当前边界，**不能声称具有请求级幂等性**。

### 0.3 贯穿全周的三个技术难点

**难点 A：租户隔离。** JWT 证明用户身份，`organization_members` 决定用户能否访问某个组织，工单 SQL 必须始终包含组织维度。任何只用 `ticket_id` 查详情、修改或评论的代码都要优先审查。

**难点 B：事务一致性。** 更改工单状态和写操作事件必须同时成功或同时失败。单次 API 正常返回不是充分证明，需要构造失败注入或真实数据库约束错误做回滚测试。

**难点 C：状态规则。** 状态不是客户端随意提交的字符串。由 Service 校验当前状态、目标状态、操作者权限、受理人条件，再执行持久化。W04 的并发领取要建立在这套规则之上。

### 0.4 周末必须可演示

1. Alice 已登录并属于组织 A，创建一张 OPEN 工单，获取详情和列表。
2. Alice 将工单标题或优先级更新，查询事件列表能看到更新记录。
3. 组织 A 的 admin 将工单指派给组织 A 的 active 成员 Bob；Bob 可将其从 OPEN 变为 IN_PROGRESS，并添加评论。
4. Bob 将 IN_PROGRESS 工单改为 RESOLVED，admin 将其改为 CLOSED；不允许 CLOSED 再转回 OPEN。
5. 组织 B 的成员访问 A 的任意工单详情、评论、事件或写接口，均不能拿到 A 的数据。
6. 人为制造写事件失败，验证工单状态/评论新增没有被部分提交。
7. `go test ./...`、`go vet ./...`、`go build ./...` 等实际运行；另用专用测试数据库跑集成测试，保存命令与结果。

---

## 1. 开始之前：核对 W02 交付状态

在已经存在的 `nexusdesk` 项目根目录执行。路径按你的环境修改。

```powershell
Set-Location "D:\workspace\nexusdesk"
git status --short
go version
go test ./...
go build ./...
docker compose --env-file .env -f deploy/compose.yaml ps
```

必须确认以下能力真实可用：`GET /healthz`、注册、登录、`GET /me`、创建组织、列组织、列组织成员、管理员添加已有成员；且 Alice/Bob 的跨组织访问可以被拒绝。

在本地使用 W02 已验证过的安全方式加载数据库 DSN，确认迁移状态。命令中的 `$dbConn` 代表**你本机已经正确设置的数据库连接变量**，不是本手册要求你将密码明文粘贴到命令或 AI 对话中。

```powershell
goose -dir migrations postgres $dbConn status
```

确认 W01～W02 修改已经提交，创建独立分支。

```powershell
git switch -c feat/week03-ticket-core
```

如果 `organization_members` 尚未实现，先完成 W02。不能通过在工单 Handler 里临时跳过成员检查来推进进度。

---

## 2. 第三周要新增的目录与文件

```text
nexusdesk/
├── AGENTS.md                               # 合并 W03 追加规则，不覆盖 W01/W02
├── cmd/api/main.go                         # 修改：组装 ticket 依赖
├── internal/
│   ├── auth/                               # 保留 W02
│   ├── organization/                       # 复用 W02 的成员身份检查
│   ├── ticket/
│   │   ├── model.go                        # 工单、评论、事件模型
│   │   ├── handler.go                      # HTTP 参数和状态码
│   │   ├── service.go                      # 业务规则和事务边界（自己写）
│   │   ├── repository.go                   # SQL 查询与更新（自己写）
│   │   ├── policy.go                       # 角色与对象级授权（自己写）
│   │   ├── state_machine.go                # 状态转换规则（自己写）
│   │   ├── state_machine_test.go           # 表驱动单元测试
│   │   └── service_test.go                 # 事务/异常逻辑测试
│   └── platform/httpserver/router.go       # 修改：注册工单路由
├── migrations/
│   ├── 00001_create_organizations.sql      # 保持原样
│   ├── 00002_create_users.sql              # 保持原样
│   ├── 00003_create_organization_members.sql # 保持原样
│   ├── 00004_create_tickets.sql            # 新建
│   ├── 00005_create_ticket_comments.sql    # 新建
│   └── 00006_create_ticket_events.sql      # 新建
├── docs/
│   ├── ticket-design.md                    # 业务规则与实体关系
│   ├── ticket-state-machine.md             # 转换表和身份限制
│   ├── api/ticket.md                       # API 契约
│   ├── decisions/0003-ticket-transaction.md # ADR
│   ├── testing/ticket-test-matrix.md       # 必做测试清单
│   ├── weekly/week03.md                    # 每日工作与复盘
│   └── database.md                         # 更新：本周新增表结构
├── tests/integration/ticket_test.go        # 显式运行的 PostgreSQL 集成测试
└── requests/week03.http                    # HTTP 客户端请求示例
```

**目录原则：** `Handler` 只处理 HTTP；`Service` 控制权限、状态规则及事务；`Repository` 负责 SQL。不为了套架构而引入几十个空接口。组织成员查询尽量复用 W02 已有的能力，不复制第二套认证代码。

### 2.1 Day 1 先创建文档，不一次性创建全部 Go 文件

```powershell
$dirs = @('internal/ticket','docs/api','docs/testing','docs/weekly','docs/decisions','requests','tests/integration')
foreach ($d in $dirs) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
$files = @(
  'docs/ticket-design.md', 'docs/ticket-state-machine.md',
  'docs/api/ticket.md', 'docs/decisions/0003-ticket-transaction.md',
  'docs/testing/ticket-test-matrix.md', 'docs/weekly/week03.md'
)
foreach ($f in $files) {
  if (-not (Test-Path $f)) { New-Item -ItemType File -Path $f | Out-Null }
}
```

资料包提供上述文档的初稿模板。**先自己补充并审查内容**，不能把模板里未验证的设计直接当成实现结果。

### 2.2 更新 AGENTS.md 的正确方式

使用资料包中的 `AGENTS.week03.append.md`，只把 W03 追加段落合并到根目录既有 `AGENTS.md`。要保留前两周对 `.env`、密码、Token、数据库破坏性命令、自动提交的禁止约定。合并后让 Codex 在只读模式检查文件是否有相互矛盾的规则。

---

## 3. 先统一工单 API、状态和权限规则

### 3.1 API 路径设计决策

本周采用**显式组织作用域路由**，避免客户端通过隐藏 Header 或 JSON 随意切换组织：

`/api/v1/organizations/:orgID/tickets`

前期项目蓝图曾简写为 `/api/v1/tickets`。本周把嵌套组织路径作为**W03 正式契约**，更新 `docs/api/ticket.md` 和 `docs/architecture.md`（如存在对应路由描述）。路由里的 `orgID` 仍然是**不可信输入**，服务端必须用 W02 的 active 成员关系验证当前用户，并在所有数据库查询中使用 `org_id` 过滤。

### 3.2 API 目录与行为

| 方法 | 路径（统一前缀 `/api/v1`） | 返回与权限 |
| --- | --- | --- |
| `POST` | `/organizations/:orgID/tickets` | active 成员可创建；`201` |
| `GET` | `/organizations/:orgID/tickets` | active 成员按页查询；`200` |
| `GET` | `/organizations/:orgID/tickets/:ticketID` | active 成员查看本组织工单；`200` |
| `PATCH` | `/organizations/:orgID/tickets/:ticketID` | 创建者可编辑自己的 OPEN 工单；admin 可编辑本组织 OPEN 工单并指派；`200` |
| `DELETE` | `/organizations/:orgID/tickets/:ticketID` | 本组织 admin 执行软删除；`204` |
| `POST` | `/organizations/:orgID/tickets/:ticketID/transition` | 符合状态与角色规则；`200` |
| `POST` | `/organizations/:orgID/tickets/:ticketID/comments` | active 成员发表评论；`201` |
| `GET` | `/organizations/:orgID/tickets/:ticketID/comments` | active 成员查看评论；`200` |
| `GET` | `/organizations/:orgID/tickets/:ticketID/events` | active 成员查看工单操作记录；`200` |

本周的 `PATCH` **不允许直接传 `status`**，工单状态只能通过 `/transition` 修改；否则客户端可以绕过状态机。允许字段建议为 `title`、`body`、`priority`，只有 admin 可以更改 `assignee_id`，且只允许指派给**当前组织的 active 成员**。所有编辑在工单 `OPEN` 阶段进行；其他状态编辑规则留待后续按需求扩展。

### 3.3 状态机（本周实现）

```text
OPEN ------> IN_PROGRESS ------> RESOLVED ------> CLOSED
                   ^                 |
                   |                 |
                   +---- admin ------+
```

| 当前状态 | 目标状态 | 操作者与前置条件 | 允许 |
| --- | --- | --- | --- |
| `OPEN` | `IN_PROGRESS` | 该工单已指派 active assignee；assignee 或 admin | 是 |
| `IN_PROGRESS` | `RESOLVED` | assignee 或 admin，当前 assignee 仍 active | 是 |
| `RESOLVED` | `CLOSED` | admin | 是 |
| `RESOLVED` | `IN_PROGRESS` | admin；已有 active assignee | 是 |
| `OPEN` | `CLOSED` | 任意 | 否 |
| `CLOSED` | `OPEN` | 任意 | 否 |
| 任意 | 原状态 | 任意 | 否，返回无效转换 |

**W03 与 W04 的衔接：** 本周先由 admin 使用 PATCH 指派工单，assignee 再调用状态转换接口启动处理。W04 加入 `/claim` 时，允许尚未指派的 OPEN 工单由多名成员竞争领取，必须通过数据库条件更新保证至多一人成功。不要在 W03 提前写一个有竞态的抢单接口。

### 3.4 权限矩阵（明确区分“组织成员”和“工单对象”）

| 操作 | 非 active 成员 | active member | active admin |
| --- | --- | --- | --- |
| 创建本组织工单 | 拒绝 | 可以 | 可以 |
| 读取本组织工单、评论、事件 | 拒绝 | 可以 | 可以 |
| 编辑 OPEN 工单基础字段 | 拒绝 | 仅本人创建 | 可以 |
| 指派 OPEN 工单给 active 成员 | 拒绝 | 不可以 | 可以 |
| 状态转换 | 拒绝 | 仅在规则允许且本人为 assignee 时 | 按状态机允许 |
| 添加评论 | 拒绝 | 可以 | 可以 |
| 软删除工单 | 拒绝 | 不可以 | 可以 |

未加入组织或访问非本组织对象时建议使用 `404`，减少资源枚举；已是组织成员但角色不足返回 `403`；无效 JWT 返回 `401`。状态冲突和版本过期返回 `409`；输入格式错误返回 `400`。错误码要在 API 文档中固定，Handler 不应返回原始数据库错误或 DSN。

---

## 4. 数据模型与迁移方案

### 4.1 表一：tickets

建议字段：

| 字段 | 类型建议 | 规则 |
| --- | --- | --- |
| `id` | `BIGINT GENERATED ... AS IDENTITY` | 主键 |
| `org_id` | `BIGINT` | NOT NULL，外键到 organizations |
| `creator_id` | `BIGINT` | NOT NULL，创建人，在组织中有成员关系 |
| `assignee_id` | `BIGINT NULL` | 当前受理人，必须是同一组织成员且 active 由 Service 验证 |
| `title` | `VARCHAR(200)` | 非空且去除首尾空白后非空 |
| `body` | `TEXT` | 非空，长度上限由应用层统一校验 |
| `priority` | `SMALLINT` | 例如 1 高、2 中、3 低；CHECK 约束 |
| `status` | `VARCHAR(20)` | 默认 OPEN；CHECK 限制 4 个值 |
| `version` | `BIGINT` | 默认 0，更新工单可进行版本校验 |
| `created_at` / `updated_at` | `TIMESTAMPTZ` | NOT NULL |
| `deleted_at` | `TIMESTAMPTZ NULL` | 非空代表软删除 |

推荐设置 `UNIQUE(org_id, id)`，以便评论和事件表使用 `(org_id, ticket_id)` 的**组合外键**，避免只检查 ticket ID 造成跨组织关联。`creator_id` 和 `assignee_id` 可以考虑 `(org_id,user_id)` 组合外键到 `organization_members`（W02 已有复合键），但**active 状态仍必须由服务端检查**，外键无法表达“状态为 active”。

建议索引：`(org_id, status, created_at DESC, id DESC)` 与 `(org_id, assignee_id)`。要能解释哪个实际查询使用哪个索引，不要添加没有用途的索引。

### 4.2 表二：ticket_comments

字段建议：`id, org_id, ticket_id, author_id, content, created_at`。`ticket_id` 与 `org_id` 必须对应同一张工单。评论文本不允许空字符串，建议限制最大长度。对 `(org_id,ticket_id,created_at,id)` 建索引以支持稳定排序。

评论是一条独立记录，**新增评论不会改变本周 `tickets.version`**，但会在同一事务中新增 `ticket_events`。这一决定应写进设计文档，避免把普通评论误当作工单状态冲突。

### 4.3 表三：ticket_events

字段建议：`id, org_id, ticket_id, actor_id, event_type, before_data JSONB, after_data JSONB, created_at`。推荐 event_type 包括：

- `TICKET_CREATED`
- `TICKET_UPDATED`
- `TICKET_ASSIGNED`
- `TICKET_STATUS_CHANGED`
- `TICKET_COMMENTED`
- `TICKET_DELETED`

记录变更前后必要的**结构化字段**，例如状态、优先级、受理人 ID；不要复制 JWT、密码、数据库错误、整份请求头等敏感信息。评论事件可只保存 `comment_id`，避免重复存储正文。事件记录默认追加，不通过业务 API 修改或删除。

### 4.4 Migration 顺序和编写原则

从 W02 的 `00003_create_organization_members.sql` 之后新增：

```powershell
$files = @(
  'migrations/00004_create_tickets.sql',
  'migrations/00005_create_ticket_comments.sql',
  'migrations/00006_create_ticket_events.sql'
)
foreach ($f in $files) { New-Item -ItemType File -Force -Path $f | Out-Null }
```

迁移每个文件都包含 `-- +goose Up` 和 `-- +goose Down`。**由你本人编写**建表、约束、索引和回滚 SQL，先用全新的本地测试库运行，再检查是否能正确读取表结构。

```powershell
# $dbConn 是从你本机安全加载的开发数据库连接，禁止泄漏凭据
goose -dir migrations postgres $dbConn status
goose -dir migrations postgres $dbConn up
goose -dir migrations postgres $dbConn status
```

原则：不编辑已执行的旧 migration；不在有业务数据的开发库随意执行 `down`；不能直接 `docker compose down -v` 重建全部数据。回滚练习只在你确认可销毁的临时测试数据库执行。

**DAY 2 的主动实验：** 手工尝试向数据库插入非法 `priority`、非法 `status`、不存在的 `org_id`、跨组织错误的 `(org_id,ticket_id)` 关联，确认数据库能拒绝相应错误。需使用测试数据，不破坏真实开发数据。

---

## 5. Day 1：业务分析、API 设计与状态机（2～3 小时）

**第一步，自己分析三类用户故事。** 至少写出“创建人提交问题”“工程师处理工单”“管理员关闭工单”，分别列出业务前置条件、成功结果、错误结果和谁有权限。

**第二步，完成四份初稿。** 编写 `docs/ticket-design.md`（范围、角色和字段规则）、`docs/ticket-state-machine.md`（合法/非法状态转换）、`docs/api/ticket.md`（接口契约）、`docs/decisions/0003-ticket-transaction.md`（事务边界方案）。先写自己的版本，再让 ChatGPT 按边界条件审查。

**第三步，回答这些问题，不查 AI 生成代码。** 为什么 URL 有 orgID 仍要检查成员身份？为什么不能直接 PATCH status？为什么事件要和状态更新同事务？“工单评论”和“工单状态变化”是否属于同一版本号？为什么 W03 不能声称已经解决并发抢单？

**第四步，Codex 只读复核。** 明确给出 `PLAN_ONLY` 或 `REVIEW_ONLY`，让它检查新接口与 W02 权限矩阵是否冲突，不改文件。

**当日交付：** 四份文档初稿、API 范围冻结、状态机表完成、明确 W04 抢单接口的预留边界。

---

## 6. Day 2：数据库设计、Migration 与约束验证（3～4 小时）

**第一步，画 ER 图。** 包含 users、organizations、organization_members、tickets、ticket_comments、ticket_events 六张表。标清工单和组织关系、受理人与创建人关系、事件和评论关联。

**第二步，自己编写三条迁移。** 完成主键、外键、CHECK、唯一约束、时间字段和索引。重点思考：为什么评论与事件表应该保存 org_id？复合外键带来什么保护？哪个约束依赖 Service 校验？

**第三步，自己定义 Repository 需要的主要方法。** 建议有 `CreateTicket`、`GetTicketInOrg`、`ListTicketsInOrg`、`UpdateTicket`、`SoftDeleteTicket`、`InsertComment`、`ListComments`、`InsertEvent`、`ListEvents`。不用一开始为每个方法创建独立接口；先决定输入输出和错误分类。

**第四步，验证数据库约束。** 在独立测试库试出正常与异常插入，记下实际数据库错误类别，区分唯一冲突、外键失败、CHECK 失败。索引通过 `\d` 或数据库工具检查，并能解释查询计划的大致含义。

**第五步，让 Codex 只审查 SQL。** 特别检查 `deleted_at`、`org_id`、索引、组合外键、Down 顺序是否存在问题。不要让它自行运行危险迁移命令。

**当日交付：** 3 条可用新迁移、更新 `docs/database.md`、ER 图、数据库约束验证结果。

---

## 7. Day 3：创建、列表、详情与分页（3～4 小时）

### 7.1 建立最小模块文件

```powershell
$files = @(
  'internal/ticket/model.go',
  'internal/ticket/handler.go',
  'internal/ticket/service.go',
  'internal/ticket/repository.go',
  'internal/ticket/policy.go',
  'internal/ticket/state_machine.go'
)
foreach ($f in $files) { if (-not (Test-Path $f)) { New-Item -ItemType File -Path $f | Out-Null } }
```

### 7.2 由你亲自实现核心接口

- `POST /api/v1/organizations/:orgID/tickets`：从验证过的身份取得 `actor_id`；检查组织 active 成员；Service 生成 OPEN、version=0；写入工单与 `TICKET_CREATED` 事件，同一事务提交。
- `GET /api/v1/organizations/:orgID/tickets`：只读本组织未删除工单；支持 `page`、`page_size`、`status`、`priority`；默认 `page=1`，`page_size=20`，最大 100，固定按 `created_at DESC, id DESC` 排序。
- `GET /api/v1/organizations/:orgID/tickets/:ticketID`：先验证成员关系，查询条件包含 `org_id`、`id` 和 `deleted_at IS NULL`，返回明确的字段白名单。

创建请求建议包含 `title`、`body`、`priority`，**不接受** `creator_id`、`org_id`、`status`、`version`、`deleted_at` 的客户端覆盖。即使客户端 JSON 含这些字段，也不能把它们赋值到数据库。

### 7.3 分页与错误分类

采用 offset 分页，先把 `limit`、`offset` 和总数/是否有下一页的约定写清楚。对于 `page` 极大导致乘法溢出、`page_size` 为 0、负数、非法状态过滤要返回稳定错误。后期再升级游标分页，本周先把正确性做好。

创建与读取都写最小自动化测试。注意 `COUNT(*)` 和查询列表都要包含组织与软删除过滤条件。

**当日交付：** 三个接口可运行，组织 A 看不到组织 B 工单，列表分页与过滤可验证，创建事件与工单同步落库。

---

## 8. Day 4：工单编辑、指派和软删除（2～3 小时）

**第一步，编写最小更新规则。** `PATCH` 允许更新 `title`、`body`、`priority`；`assignee_id` 仅 admin 可修改且只指派给 active 成员。只有 OPEN 工单可以编辑，已关闭或软删除工单不能更新。

**第二步，设计版本约束。** 读取详情时返回 `version`。客户端在 PATCH 请求中携带 `expected_version`。更新时用带 `WHERE org_id=? AND id=? AND version=? AND deleted_at IS NULL` 的 SQL 进行条件更新，并在成功后版本加一；没有匹配行时区分不可见、非法状态和版本冲突。

**第三步，写业务事件。** 当标题、优先级或受理人发生变更，写入相应 `TICKET_UPDATED` 或 `TICKET_ASSIGNED` 事件；必须与工单更新在同一个事务中。

**第四步，实现软删除。** `DELETE` 仅 admin 可调用，设置 `deleted_at`，不直接执行物理删除，事件写 `TICKET_DELETED`。此后普通列表、详情、评论和事件 API 不再返回该工单，数据库仍保留审计记录。

**自己故意测试：** member 指派工单、指派外组织用户、重复提交旧 version、编辑已删除工单、提交不允许的 JSON 字段。均不能绕过限制。

**当日交付：** PATCH、DELETE 可运行，版本冲突与权限错误正确，操作事件完整。

---

## 9. Day 5：状态机、评论与操作事件事务（3～4 小时）

### 9.1 状态机应是可独立测试的函数

先在 `state_machine.go` 中实现纯业务判断：输入当前状态、目标状态、角色、是否为 active assignee 等信息，返回允许/拒绝的业务错误。不要把全部判断埋在 Handler 里。

在 `state_machine_test.go` 中采用表驱动测试：覆盖全部合法边与几个非法边，包括 CLOSED 不能重开、member 不能关闭、未指派不能进入 IN_PROGRESS、旧状态不能被跳过。

### 9.2 状态变更必须更新工单并落事件

`POST .../transition` 建议请求 `to_status`、`expected_version`。Service 在**同一个数据库事务**里完成：

1. 验证 actor 仍是目标组织 active 成员，并确认角色。
2. 按组织和工单 ID 读取当前状态、assignee 与 version；必要时使用行锁或条件更新以避免读写竞态。
3. 调用状态机校验状态转换及操作者权限。
4. 通过带当前状态与版本条件的 UPDATE 写入目标状态并递增 version。
5. 插入一条 `TICKET_STATUS_CHANGED` 事件；任何一步失败则回滚。
6. 提交后返回最新状态和版本。

**关键原则：** “先 UPDATE 事务外提交，再 INSERT 事件”是错误实现。并发更新也不能只在 Go 内存里做判断，必须让数据库条件或行锁参与保证。

### 9.3 新增评论与事件

`POST .../comments` 验证 active 成员与未删除工单、评论内容长度；事务内插入评论和 `TICKET_COMMENTED` 事件。`GET .../comments` 读取同一组织、同一未删除工单下的评论并按时间和 ID 稳定排序。`GET .../events` 返回对外可披露的事件结构，不暴露内部错误和机密字段。

**Day 5 特别实验：** 在测试环境让写入 `ticket_events` 故意失败，再验证工单状态、版本或新增评论没有部分提交。可以通过测试专用的受限失败注入、Repository stub 或违反测试表上的 CHECK 约束触发错误。**不要为制造故障而破坏日常开发数据库。**

**当日交付：** 状态流转、评论与事件 API 可运行，事务回滚证据存在，状态机单元测试通过。

---

## 10. Day 6：集成测试、安全与失败排查（3～4 小时）

### 10.1 专用测试库

建议使用独立的测试数据库 `nexusdesk_test`，且最好通过独立 Compose Project 或数据库容器隔离。测试 DSN 不写入仓库；测试初始化按 migration 建表，必要时清理自己的临时数据，禁止操作开发库和共享数据库。

若测试文件采用 Go build tag，在文件顶部使用 `//go:build integration` 并用明确命令执行：

```powershell
go test ./...
go test -tags=integration ./tests/integration/...
go test -race ./internal/ticket/...
go vet ./...
go build ./...
```

并发 `-race` 发现的是 Go 进程中的共享内存数据竞争，**不是数据库事务正确性证明**。W04 的多用户同时抢单须另外用 PostgreSQL 并发集成测试验证。

### 10.2 第三周必须完成的测试矩阵

| 编号 | 测试场景 | 核心预期 |
| --- | --- | --- |
| T01 | active 成员创建工单 | 201，OPEN，版本 0，creator 正确 |
| T02 | 创建请求伪造 `creator_id`/`org_id` | 无法以其他身份或组织创建 |
| T03 | 非成员向目标组织创建工单 | 拒绝，不新增数据 |
| T04 | 缺失/过期 JWT 访问工单 | 401 |
| T05 | 列表按 `org_id`、状态、优先级分页 | 无跨组织数据，排序稳定 |
| T06 | 跨组织读取工单详情 | 404，不泄漏对象信息 |
| T07 | 使用错误 `orgID` 与真实 `ticketID` | 404 |
| T08 | 创建时标题为空/优先级非法 | 400 或数据库校验失败，不能创建 |
| T09 | 创建事件失败 | 工单创建事务回滚 |
| T10 | 创建人编辑自己 OPEN 工单 | 成功，version 增加 |
| T11 | member 编辑别人 OPEN 工单 | 403 |
| T12 | member 为工单指派 assignee | 403 |
| T13 | admin 指派外组织或 disabled 用户 | 被拒绝 |
| T14 | 旧 `expected_version` 修改 | 409，数据不被覆盖 |
| T15 | OPEN 未分配 assignee 直接流转 | 拒绝 |
| T16 | 指派后 OPEN→IN_PROGRESS | 允许并记录事件 |
| T17 | IN_PROGRESS→RESOLVED→CLOSED | 符合角色策略时成功 |
| T18 | OPEN→CLOSED / CLOSED→OPEN | 拒绝 |
| T19 | RESOLVED→IN_PROGRESS | 仅 active admin 可重开 |
| T20 | 状态事件失败 | 状态与 version 均回滚 |
| T21 | 本组织 active 成员新增评论 | 201，评论和事件均存在 |
| T22 | 评论事件失败 | 评论新增回滚 |
| T23 | 非成员读取/新增他组织评论或事件 | 拒绝 |
| T24 | admin 软删除工单 | 204，普通查询不可见，事件留存 |
| T25 | member 删除 / 重复删除 | 拒绝或明确约定的不可见响应 |
| T26 | 数据库连接异常 | 安全错误，不暴露 SQL/DSN |

建议优先自动化 T01～T24，并为剩余项保留明确的人工验证记录或自动测试。**禁止为了让测试通过降低权限检查、删掉断言或使用生产数据。**

### 10.3 自己练习的诊断方法

遇到故障时依次检查：HTTP 请求/响应 → Router 是否正确注册 → JWT 与成员身份 → Service 权限/状态判断 → Repository SQL 条件 → 数据库事务与约束 → 操作事件。

先用日志和测试定位。只有在可以复现、能提出自己假设之后，才让 ChatGPT 或 Codex 提供证据驱动的排查建议。

**当日交付：** 测试结果、至少一条真正有价值的 Bug 排查记录、Codex 安全 Review 报告。

---

## 11. Day 7：验收、Codex Review、提交与技术复盘（2～3 小时）

### 11.1 最终质量检查

```powershell
go fmt ./...
go test ./...
go test -tags=integration ./tests/integration/...
go vet ./...
go build ./...
git diff --check
git status --short
```

`go test` 等命令必须根据你实际仓库路径调整，若没有运行某一项，记录“未运行”，不能写“已通过”。检查 `.env`、数据库密码、JWT、个人信息不在提交内容中。

### 11.2 完整演示剧本

1. Alice 登录 A 组织并创建工单，读取详情；验证初始事件。
2. A 组织 admin 更新优先级、指派 Bob，查看两个操作事件。
3. Bob 将工单转为 IN_PROGRESS 并发表评论，再转为 RESOLVED。
4. admin 关闭工单，尝试非法再打开，确认拒绝。
5. B 组织用户尝试访问 A 的工单、评论、事件，确认全部拒绝。
6. admin 软删除一张测试工单，确认正常列表不显示，但事件还在数据库。
7. 用集成测试触发事件写入失败，查看事务回滚结果。

### 11.3 建议 Git 提交节奏

```text
docs(ticket): define W03 scope, API and state machine
feat(db): add ticket, comment and event migrations
feat(ticket): create and query organization-scoped tickets
feat(ticket): add updates, assignment and soft delete
feat(ticket): implement transitions, comments and event transactions
test(ticket): cover permissions, rollback and state machine
docs: record W03 QA and W04 handoff
```

这里只是提交建议，不必为了凑次数制造空提交。每次提交前检查 `git diff` 和暂存的修改，确保没有顺手提交 W02 Token、真实数据库配置或超出范围的文件。

### 11.4 个人复盘提纲

- 我亲自完成了哪几段 Service / SQL？哪些代码由 Codex 编写或调整？
- 为什么 `org_id` 既出现在路径，也要出现在 SQL 条件中？
- 为什么更新状态、写入事件应该放在同一事务？
- `version`、乐观锁、`SELECT ... FOR UPDATE` 分别解决什么问题？
- `go test -race` 能证明什么、不能证明什么？
- 我实际发现哪些缺陷，如何定位，又如何用测试确认修复？
- W04 的并发领取需要在现有代码基础上修改哪些 SQL 与测试？

**周末输出：** `docs/weekly/week03.md`、功能演示步骤、测试命令结果、1 个明确的技术复盘案例，以及 W04 待办清单。

---

## 12. W03 的 AI 协作约束

### 12.1 人、ChatGPT、Codex 的分工

| 任务 | 你必须完成 | ChatGPT 适合做 | Codex 允许做 |
| --- | --- | --- | --- |
| 工单业务与权限 | 决定角色、状态和编辑限制 | 提出反例与遗漏 | `PLAN_ONLY`/`REVIEW_ONLY` |
| 三张表与 SQL | 写迁移、定义约束、验证结果 | 解释外键、索引、事务 | 只读审查 SQL |
| Handler/Service/Repository | **亲手写关键业务逻辑** | 分步骤讲解与答疑 | 只读审查、经授权才改指定文件 |
| 状态机和版本控制 | 亲手实现并解释 | 提出边界案例 | 补充测试（授权后） |
| 评论/操作事件事务 | **亲手写事务边界与异常处理** | 提出回滚实验 | 审查事务缺陷 |
| 集成测试 | 自己决定核心验证场景并运行 | 提供反例和故障排查思路 | `TEST_SCOPED` 写指定测试 |
| README、HTTP 示例、重复文档 | 复核准确性 | 整理说明 | `DOC_SCOPED`/受限编辑 |
| Git 与数据库执行 | 亲自审核、执行危险操作 | 帮助复盘 | 未授权不得 commit/push/回滚 |

**本周学习红线：** `ticket/service.go` 的事务与权限判断、`ticket/state_machine.go`、Repository 的组织过滤 SQL、`ticket/policy.go`、新迁移文件应由你先独立实现，再让 AI 审查。AI 可以在你卡住时给最小示例、定位线索和分段提示，不直接替换完整业务模块。

### 12.2 推荐给 ChatGPT 的教学提示词

```text
我正在自己实现 NexusDesk W03 的工单状态机。
我已有设计：[贴上状态表和经过脱敏的代码]。
请先以 Go 后端导师身份检查我的理解，不要生成整个 Service。
请指出 3 个最容易遗漏的边界条件，让我先提出解决方案。
如果我的方案有问题，先解释原理和验证方法，再给必要的最小代码示例。
```

### 12.3 推荐给 Codex 的只读审查提示词

```text
任务：W03-TICKET-REVIEW
模式：REVIEW_ONLY。先读取 AGENTS.md、docs/ticket-design.md、
docs/ticket-state-machine.md、docs/api/ticket.md。
只审查 internal/ticket/**、新增三条 migration、相关 router/main 修改。
重点：org_id 是否贯穿 SQL、权限是否由服务端判断、状态机是否被绕过、
UPDATE/INSERT event 是否同事务、版本冲突、错误映射、软删除过滤、索引。
按照严重程度报告：文件位置、触发路径、影响、证据、复现方法、最小修复建议。
禁止编辑、运行 Down/DROP/TRUNCATE、读取 .env、提交或推送。
```

### 12.4 推荐给 Codex 的受限测试提示词

```text
任务：W03-TEST-ONLY
模式：TEST_SCOPED。
只允许修改 internal/ticket/*_test.go 与 tests/integration/ticket_test.go。
基于已有业务代码补充状态机非法边、跨组织查询、事务回滚与版本冲突测试。
不得修改 service.go、repository.go、policy.go、迁移、认证代码、密钥及配置。
不得删除已有断言或将失败测试标记跳过。
列出实际执行过的命令、结果、未运行原因和可能缺少的测试前置条件。
```

### 12.5 标准 Bug 排查提示词

```text
问题：[一句话说明]
预期：[期待的 HTTP 和数据库结果]
实际：[实际响应/日志，已脱敏]
已完成排查：[我看了哪些函数、SQL、事务及日志]
相关代码：[最小相关片段]
请不要立即改代码。先提出最可能的 3 个原因与可执行的验证实验。
等我自己确认根因，再讨论最小修复和回归测试。
```

**工具使用规则：** Codex 的仓库指导不能替代真实的执行权限和审批设置。遇到危险命令、密钥、数据库修改、网络发送、提交或推送，应由你直接确认。AI 不应该自主改变 W02 的认证方式或跨组织权限策略。

---

## 13. 学习检查：周末无需 AI 能否回答

1. Handler、Service、Repository 的边界分别在哪里？事务到底应该在哪一层开始与提交？
2. `orgID` 来自 URL 为什么不等于已授权？怎样避免只按 ticket ID 查错数据？
3. `UNIQUE(org_id,id)` 与组合外键对多租户有什么实际帮助？
4. 为什么数据库 CHECK 和应用层状态机都需要？它们的关注点有何不同？
5. 为什么两次写入必须同一事务？怎样人为制造第二步失败并证明回滚？
6. 什么情况下使用乐观锁 `version`？它不能自动防止哪些并发问题？
7. 为什么软删除条件必须加在列表、详情、评论、事件查询中？
8. 为什么 `go test -race` 不能证明 PostgreSQL 中没有并发竞争？
9. 如何解释状态 `RESOLVED→IN_PROGRESS` 的权限与审计意义？
10. 如果要新增 W04 抢单接口，为什么不能先 SELECT 未领取，再单独 UPDATE？

**能力目标：** 状态机、权限策略和 SQL 事务至少达到“独立实现与排障”，其中一个真实 Bug 争取达到“能解释工程取舍”。本周暂不要求掌握所有并发隔离级别的理论细节，W04 会继续深化。

---

## 14. W03 完成判定与 W04 交接

### 14.1 完成标准（DoD）

- [ ] W02 认证与组织接口回归通过，无破坏性改动。
- [ ] 已新增且成功执行三条 migration，SQL 约束在测试库验证过。
- [ ] 工单创建/列表/详情/更新/软删除 API 按文档运行。
- [ ] 状态机、评论、事件可工作，非法状态转换被拒绝。
- [ ] 工单和事件、评论和事件写入同一事务，失败可回滚。
- [ ] 读取和写入全部经过 active 成员检查、角色检查及 org 过滤。
- [ ] 已有覆盖关键负向场景的测试，记录实际运行结果。
- [ ] Codex Review 已由本人阅读，重要问题有复现或处理记录。
- [ ] `docs/weekly/week03.md` 已记录自己的贡献、AI 贡献、真实 Bug 和待办。
- [ ] 对核心函数和 SQL 能在不看 AI 答案时解释；工作区无泄露凭据。

### 14.2 给 W04 的明确接口与待办

- 追加 `POST /api/v1/organizations/:orgID/tickets/:ticketID/claim`，仅对未分配 OPEN 工单操作。
- 设计由数据库保证的原子领取条件，并在**同一事务**写入 `TICKET_ASSIGNED`/状态变更事件。
- 编写两位成员同时领取的真实并发集成测试，断言至多一人成功，其他获得明确冲突响应。
- 引入 Vue 3 的登录、工单列表、详情和处理界面，完成 G1 业务 MVP。
- 明确是否需要新增“未分配工单”列表，重新评估索引并用真实 SQL 查询验证。

**最重要的交接条件：** W03 的权限、状态、事件和 SQL 事务稳定，才进入 W04 并发领取。否则 W04 很容易在未稳定的代码上添加更多竞态。

---

## 15. 官方学习参考

- Go 数据库事务：https://go.dev/doc/database/execute-transactions
- Go 表驱动测试与测试基础：https://go.dev/doc/tutorial/add-a-test
- Go 竞态检测器：https://go.dev/doc/articles/race_detector
- PostgreSQL 事务隔离：https://www.postgresql.org/docs/current/transaction-iso.html
- PostgreSQL 约束：https://www.postgresql.org/docs/current/ddl-constraints.html
- PostgreSQL 显式锁：https://www.postgresql.org/docs/current/explicit-locking.html

> 学习顺序：先自己实现小功能，再查官方文档解决具体疑问，最后让 AI 对真实代码做审查。以上参考适用于学习原理；项目实际用法以安装的 Go、PostgreSQL 和驱动版本为准。

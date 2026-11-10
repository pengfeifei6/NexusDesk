# NexusDesk 第四周详细开发手册

**版本：W04 · v1.0**  
**主题：数据库原子领取、并发验证、Vue 3 前端联调与 G1 业务 MVP**  
**适用对象：计算机科班本硕、研二，掌握 Go/Gin/PostgreSQL 基本接口开发，已按 W01～W03 实现认证、组织与工单模块**  
**建议投入：20～26 小时。如前端基础较弱，可延长 2～4 天，不允许通过跳过测试缩短周期。**

> 这是前面三周同一个 `nexusdesk` 仓库的增量开发手册。**W04 同时兑现两个目标：Go 后端多用户并发领取；最小 Vue 前端业务展示**。本手册不是能一键生成成品的代码包。你亲自完成并发核心 SQL、事务、集成测试、接口错误语义和前后端数据流；Codex 可协助搭建 Vue 页面骨架、测试脚手架及审查，但不得代替你的关键技术决策。

---

## 0. 本周边界、完成标准与演示结果

### 0.1 必须实现的成果

1. 在 W03 工单 API 基础上新增 `POST /api/v1/organizations/:orgID/tickets/:ticketID/claim`，允许当前组织 active 成员领取**未分配且为 OPEN** 的工单。
2. 领取由 PostgreSQL 条件更新保证原子性。领取成功只设置 `assignee_id` 并递增 `version`，**工单状态仍为 OPEN**；之后由领取人调用既有 `/transition` 进入 `IN_PROGRESS`。这两个行为故意分开，便于学习业务状态机与并发更新。
3. 领取更新与 `TICKET_ASSIGNED` 操作事件写入必须在**同一数据库事务**；事件写入失败必须回滚领取。
4. 完成真实 PostgreSQL 并发集成测试。多位用户在同一时刻尝试领取同一工单，最终恰好一人成功，失败者得到清晰的 `409`，不会产生两个有效受理人或多余领取事件。
5. 创建 `web/` Vue 3 + TypeScript 工程，完成登录、组织选择、工单列表、创建工单、工单详情、领取与状态推进的最小操作界面。
6. 通过浏览器发起真实 API 请求，不使用假数据冒充联调成功。完成错误提示、加载状态、Token 处理、基础权限展示与刷新行为说明。
7. 发布 **G1 业务 MVP** 的本地演示说明，录制约 1～2 分钟演示视频，保存一份真实的测试和问题复盘记录。

### 0.2 刻意不做的内容

- 不引入 Redis、消息队列、文件上传、RAG、Agent、MCP、WebSocket、微服务、Kubernetes。
- 不增加密码重置、刷新 Token、SSO；W02 的短期 Access Token 策略保持不变。
- 不制作复杂组件库、移动端适配、UI 动画或正式互联网部署。前端以业务完整和正确联调为标准。
- 不做自动指派、批量领取、锁等待队列、抢单公平性保证、撤销领取、重新分配规则扩展。
- 不把 `go test -race` 作为数据库领取正确性的证明，也不以“并发 20 次看似没问题”代替可重复的集成测试。

### 0.3 周末验收情景

Alice 为组织 A 管理员，Bob、Charlie 为 A 的 active 成员，David 为组织 B 的成员。Alice 登录前端，在组织 A 创建 OPEN 工单；Bob 和 Charlie 同时点击领取，后台保证只有一个领取成功，另一人得到 `409` 并提示刷新。成功者查看工单详情，点击开始处理，使状态变为 IN_PROGRESS，并发表评论。David 不能查看或领取组织 A 的工单。管理员可以看到事件历史，数据库中领取事件与受理人一致。

**G1 完成口径：** 后端、数据库与前端在本机可启动；前端能走通登录、组织选择、创建、查看、领取、状态流转；API 与数据库测试通过；有简短演示与复盘文件。没有真实运行过的功能不能标为完成。

---

## 1. 开始之前检查 W03 基线

在仓库根目录执行。修改示例路径为你电脑上的实际路径。

```powershell
Set-Location 'D:\workspace\nexusdesk'
git status --short
go version
go test ./...
go build ./...
docker compose --env-file .env -f deploy/compose.yaml ps
```

确保 W03 的登录、组织成员、工单 CRUD、工单评论、状态机、事件事务均可运行。确认 `tickets` 已有 `org_id`、`status`、`assignee_id`、`version`、`deleted_at` 等字段，`ticket_events` 可记录 `TICKET_ASSIGNED`。本周通常**不需要新 migration**。如发现 W03 实际表结构与约定不一致，先记录差异并创建增量迁移，不得编辑已执行的 `00004～00006`。

确认工作区干净，另开分支：

```powershell
git switch -c feat/week04-claim-web-mvp
```

如果你尚未完成 W03，不要为了赶上 W04 而暂时取消组织权限校验或状态事务。应先补齐基线。**本手册中的测试和示例不是已经运行过的项目结果。**

### 1.1 环境核对

- Go、Docker Desktop（或兼容 Docker 环境）、PostgreSQL 和 Goose：沿用前三周已验证版本。
- Node.js：使用与当前 `create-vue` 官方要求兼容的受支持版本。写本文时官方指南要求 Node.js `^22.18.0` 或 `>=24.12.0`；版本升级后以官方文档为准。来源：https://vuejs.org/guide/quick-start
- npm 和 IDE：建议 VS Code + Vue 官方扩展；Codex 在已存在的 Git 仓库中运行。
- 不在开发机保存正式密钥；`web/.env.local`、根目录 `.env` 均不得提交。

检查前端环境：

```powershell
node --version
npm --version
git --version
```

---

## 2. 本周预计新增和修改的文件

```text
nexusdesk/
├── AGENTS.md                              # 合并 W04 增量要求，不覆盖 W01～W03
├── cmd/api/main.go                        # 如有需要，更新 Claim 依赖组装
├── internal/
│   └── ticket/
│       ├── handler.go                     # 增加 ClaimTicket HTTP 入口
│       ├── service.go                     # 自己写：领取业务事务边界
│       ├── repository.go                  # 自己写：条件更新及返回值
│       ├── policy.go                      # 复用活跃成员判定
│       ├── claim_test.go                  # 自己写：领取规则单测
│       └── state_machine.go               # 复用旧逻辑，不改 W03 状态规则
├── internal/platform/httpserver/router.go # 注册 /claim 路由
├── tests/integration/
│   └── ticket_claim_test.go               # 自己写：并发 + 事务回滚 + 租户隔离
├── web/                                   # Vue 官方脚手架生成的完整工程
│   ├── src/
│   │   ├── api/http.ts                    # Token、错误分类、HTTP 客户端
│   │   ├── api/auth.ts
│   │   ├── api/organizations.ts
│   │   ├── api/tickets.ts
│   │   ├── router/index.ts
│   │   ├── stores/auth.ts
│   │   ├── types/api.ts
│   │   ├── views/LoginView.vue
│   │   ├── views/TicketListView.vue
│   │   ├── views/TicketDetailView.vue
│   │   └── components/TicketForm.vue
│   ├── vite.config.ts                     # /api 代理，避免本地 CORS 误区
│   └── package.json
├── docs/
│   ├── api/ticket-claim.md                # W04 新接口契约
│   ├── decisions/0004-atomic-ticket-claim.md
│   ├── frontend/web-mvp.md                # 页面、API 映射与状态说明
│   ├── testing/claim-concurrency.md       # 并发测试设计和证据
│   ├── demo/g1-demo-script.md             # 演示步骤与视频链接占位
│   └── weekly/week04.md                   # 每日学习与 Bug 复盘
└── requests/week04.http                  # Claim 与联调 HTTP 示例
```

注意：`claim_test.go`、`ticket_claim_test.go` 是需要你自己编写的测试文件名示例，实际可以沿用项目原有命名；**不要为了符合目录树而复制同一份测试代码**。`web/` 由官方脚手架创建，生成的文件不需要再手工创建一遍。Vue Router、Pinia 和测试依赖应一次性在脚手架里按需选择。

### 2.1 Day 1 先建文档目录和文件

```powershell
$dirs = @('docs/api','docs/decisions','docs/frontend','docs/testing','docs/demo','docs/weekly','requests')
foreach ($d in $dirs) { New-Item -ItemType Directory -Force $d | Out-Null }
$files = @(
  'docs/api/ticket-claim.md',
  'docs/decisions/0004-atomic-ticket-claim.md',
  'docs/frontend/web-mvp.md',
  'docs/testing/claim-concurrency.md',
  'docs/demo/g1-demo-script.md',
  'docs/weekly/week04.md',
  'requests/week04.http'
)
foreach ($f in $files) { if (-not (Test-Path $f)) { New-Item -ItemType File -Force $f | Out-Null } }
```

如果你使用 ZIP 中的模板，请把内容复制到这些新文件，再逐项修改。模板中的 TODO 和示例值不得作为已经确认的实现结果。

### 2.2 更新 AGENTS.md

将 `AGENTS.week04.append.md` 的增量条款追加到已有根目录 `AGENTS.md`。保留前三周对敏感信息、受控修改、SQL 事务与组织隔离的要求。**不要在 `web/` 下另外创建互相矛盾的 AGENTS 文件。**

---

## 3. Day 1：冻结领取语义、API 和前端范围（2～3 小时）

**你亲自完成：** 阅读 W03 的 `state_machine.go`、`service.go`、`repository.go`、`ticket_events` 设计；从业务角度确定 Claim 的输入、输出与状态影响。

### 3.1 W04 正式的领取业务规则

- `POST /api/v1/organizations/:orgID/tickets/:ticketID/claim` 仅允许有效 JWT 的本组织 active 成员调用；`member/admin` 均可领取。
- 只能领取 `status='OPEN'`、`assignee_id IS NULL`、`deleted_at IS NULL` 的工单。
- 成功后：受理人变成当前用户，`version + 1`，写入 `TICKET_ASSIGNED`；状态**保持 OPEN**。
- 用户随后使用 W03 `/transition` 把 OPEN 变成 IN_PROGRESS，必须仍满足 W03 的角色与版本规则。
- 每次领取请求建议携带 `expected_version`，其值来自最新的工单详情。旧版本、已经有人领取或已关闭，统一以业务冲突 `409` 呈现；不存在或跨租户对象遵循 W03 的 `404` 约定。
- 重复点击领取不视为幂等成功，同一人再次领取也返回 `409`。不允许客户端指定 `assignee_id`。

这套语义的好处是：**一次请求只做一项可证明原子性的资源分配**，不和复杂状态转换混在一起。后期如果改变为领取即开始处理，需另立 ADR 并更新测试，不能悄悄修改 API 行为。

### 3.2 请求与响应约定

```http
POST /api/v1/organizations/1/tickets/42/claim
Authorization: Bearer <ACCESS_TOKEN>
Content-Type: application/json

{"expected_version": 3}
```

成功 `200` 返回工单至少包含 `id`、`org_id`、`assignee_id`、`status`、`version`、`updated_at`，且 `status` 为 OPEN。冲突返回统一错误码 `TICKET_CLAIM_CONFLICT`，HTTP `409`；客户端收到 `409` 后重新读取工单详情。

**安全说明：** `orgID` 与 `ticketID` 来自 URL，并不可信。必须校验 JWT 的 `actor_id`、当前组织 active 成员身份，并确保 SQL 在同一组织内更新。跨组织的 404，不得返回他组织工单存在或当前受理人是谁。

### 3.3 前端 P0 页面

本周只做五种可见页面或界面状态：登录页、组织选择、工单列表、工单创建表单、工单详情（领取和状态操作）。评论、事件可以在详情中以简洁列表展示。不要做组织管理员 UI，也不要做复杂权限编辑表单；组织与成员可以使用 W02 API 请求脚本预置。

**当天输出：** `docs/api/ticket-claim.md`、`docs/decisions/0004-atomic-ticket-claim.md`、`docs/frontend/web-mvp.md` 三份初稿。先自己写，再让 Codex 只读审查设计冲突。

---

## 4. Day 2：自己实现 PostgreSQL 原子领取（3～4 小时）

### 4.1 为什么不能先 SELECT 再直接 UPDATE

不安全的思路：先读取 `assignee_id=NULL`，两个并发请求都看到空值；随后分别执行不带领取条件的 `UPDATE`，后写入的用户覆盖前一位受理人。即使使用 Go 互斥锁也只保护**同一进程**，多实例部署无法保证正确性。

推荐使用 PostgreSQL 条件更新。以下是**学习用 SQL 骨架**，你需要根据实际字段、绑定参数和错误模型亲自改写到 Repository，不要把此骨架当作已测试的生产实现。

```sql
UPDATE tickets
SET assignee_id = $1,
    version = version + 1,
    updated_at = NOW()
WHERE org_id = $2
  AND id = $3
  AND status = 'OPEN'
  AND assignee_id IS NULL
  AND deleted_at IS NULL
  AND version = $4
RETURNING id, org_id, assignee_id, status, version, updated_at;
```

该条件更新在同一个 PostgreSQL 数据库中受到行级并发控制。一个并发更新成功后，其他等待中的更新会重新检查 `WHERE`，不能再同时满足 `assignee_id IS NULL`。PostgreSQL 官方文档可核对此行为：https://www.postgresql.org/docs/current/transaction-iso.html

**必须理解：** 这是数据库层面的原子更新，不是 `go test -race` 检测到的 Go 内存数据竞争。`RETURNING` 用于取得已更新的行，免去一次额外查询：https://www.postgresql.org/docs/current/dml-returning.html

### 4.2 Service 的事务边界

用你 W03 已有的事务封装，不新建第二套事务框架。目标步骤如下：

1. 从服务端 JWT 上下文取 `actor_id`，核实其对目标组织的 active 成员权限。
2. 开启数据库事务。成员资格与领取更新之间的时序风险应记录在 ADR；如果系统支持成员并发禁用，可考虑在同一事务中读取并适度锁定成员记录，避免成员被撤销后仍完成领取。注意保持锁顺序稳定。
3. 在事务内执行带 `org_id`、`status='OPEN'`、`assignee_id IS NULL`、`deleted_at IS NULL`、`expected_version` 的条件更新。
4. 如果没有返回记录，检查属于哪种安全可披露的失败，并映射为 `404` 或 `409`；不能把 SQL 的 0 行当作成功。
5. 成功时使用 `ticket_events` 记录 `TICKET_ASSIGNED`，`actor_id` 为真实操作者，记录必要的 before/after 信息，和 UPDATE 同事务提交。
6. 如果事件记录失败，事务回滚。只有事务成功提交后，HTTP 才返回领取成功。

W03 已明确普通评论不变更工单版本。本周领取**必须**使版本加一；成功领取事件只写一次。对失败请求不写成功事件。

### 4.3 今日手写任务

你要亲自修改 `internal/ticket/repository.go`、`service.go`、`handler.go`，并注册路由。不要把核心 Claim 逻辑委托给 Codex 一次性生成。自己编写最少三个测试：正常领取、已领取再次领取、旧版本请求冲突。

让 Codex 只读审查是否存在 SQL 漏掉 `org_id`、事务外写事件、传入客户端伪造受理人、把 0 行更新当成功等问题。

**当天输出：** 可用 `/claim` 接口、事务事件记录、基本单元测试和 `docs/api/ticket-claim.md` 更新。

---

## 5. Day 3：并发集成测试与故障实验（3～4 小时）

### 5.1 测试环境

使用独立测试数据库，例如 `nexusdesk_test`。测试库和开发库必须可区分，测试自动清理只能作用于**明确的测试资源**。如果采用 Go build tag，单独执行带标签的测试。确保 PostgreSQL 连接池允许并发连接，避免测试完全被单连接串行化。

### 5.2 必须自己编写的并发测试

先创建组织 A、至少两名 active 成员和一张尚未领取的 OPEN 工单。启动多个 Goroutine，使用同一个 `expected_version`，通过一个同步起跑信号让它们几乎同时调用 Service 或真实 HTTP `/claim`。等待全部返回后断言：

- 所有请求结束且无意外超时、panic。
- 成功结果**恰好一个**；其他请求为预期的 `409`，而不是错误地返回成功。
- 最终工单的 `assignee_id` 与成功返回的操作者一致。
- 工单 `version` 恰好加一，`status` 仍然为 OPEN。
- 本次领取对应的 `TICKET_ASSIGNED` 事件**恰好一条**。
- 不存在来自组织 B 的跨租户修改；组织 B 无法领取组织 A 工单。

建议至少重复执行 20 次这个测试，期间使用不同操作者组合，并记录迭代次数、请求并发数与失败输出。次数是本地训练建议，不是性能基准；通过 20 次也不能证明所有并发情况一定正确。

### 5.3 事务失败实验

在独立测试库中，让领取成功更新之后的事件插入返回错误。验证事务回滚后 `assignee_id` 仍为空、version 未增加、没有成功事件。**不要故意损坏日常开发库的事件表或修改正式 migration。**

### 5.4 `-race` 的正确使用方式

```powershell
go test ./...
go test -race ./internal/ticket/...
go test -tags=integration ./tests/integration/...
go test -tags=integration -count=20 ./tests/integration/...
```

`-race` 可以发现测试执行路径上的 Go 内存数据竞争，**不能检查 PostgreSQL 中是否发生业务竞争**。数据库竞争的证据来自真实并发请求、最终行状态与事件记录。参考 Go 官方：https://go.dev/doc/security/best-practices

**当天输出：** `tests/integration/ticket_claim_test.go`、`docs/testing/claim-concurrency.md` 与至少一次真实问题或失败假设的分析。没有实际报错时，可以记录一次事先提出的风险与反证实验，不要捏造 Bug。

---

## 6. Day 4：创建 Vue 3 + TypeScript 工程（3～4 小时）

这一天开始做前端，但核心目标是理解浏览器怎样真实调用你的 Go API，而不是追求 UI 风格。

### 6.1 使用 Vue 官方脚手架

在仓库根目录执行：

```powershell
node --version
npm --version
npm create vue@latest
```

交互式选择建议：

| 选项 | 建议 | 原因 |
| --- | --- | --- |
| 项目名 | `web` | 放在现有仓库下 |
| TypeScript | Yes | 熟悉类型化接口与请求响应 |
| Vue Router | Yes | 登录/列表/详情页面跳转 |
| Pinia | Yes | 保存当前登录态与组织选择 |
| Vitest | Yes | 给基础逻辑增加测试 |
| ESLint / Prettier | Yes | 保证团队级代码格式一致 |
| E2E 测试框架 | 暂选 No | 本周先完成功能；后续可补 Playwright |

随后执行：

```powershell
Set-Location web
npm install
npm run dev
```

如果官方脚手架的选项或 npm scripts 与本文不同，以当时生成的项目为准，不要假定 `npm run lint`、`npm run test:unit` 一定存在。官方来源：https://vuejs.org/guide/quick-start

### 6.2 定义 HTTP 客户端与开发代理

自己先阅读 `fetch`、HTTP 状态码、`Authorization` Header、JSON 处理和 `Promise` 的基础行为。之后实现：

- API base 建议为相对路径 `/api/v1`；`vite.config.ts` 在**开发环境**把 `/api` 转发到实际 Go 服务地址，例如 `http://localhost:8080`，具体端口以你 W01 配置为准。
- Token 只由客户端统一模块附带到受保护请求，不在每个 Vue 页面复制逻辑。
- 统一识别 `401`、`403`、`404`、`409`、`5xx`，展示安全的用户提示。`409` 抢单冲突时刷新详情。
- 取消请求、加载状态、网络错误和非 JSON 响应需要明确处理，不把每个错误都显示为“网络错误”。

### 6.3 本周 MVP 的 Token 策略

本周建议先把 Access Token **只放在内存状态**（例如 Pinia Store），不写 `localStorage`。刷新页面会丢失登录态，直接跳转登录页；这是一项明确、可接受的本地 MVP 取舍。它不是完整的生产级认证解决方案。后续若采用 Cookie 会话或刷新 Token，应补充 CSRF、XSS 和会话撤销设计。

Vite 的 `VITE_` 环境变量会暴露给前端打包代码，**绝对不能把 JWT 私钥、数据库密码或模型 API Key 放在前端变量中**。

**当天输出：** `web/` 可以通过 `npm run dev` 启动，前端 API 客户端能调用 Go `/healthz` 或其他公开接口，开发代理正确。让 Codex 帮助检查 Vue 目录、类型定义和样式，不要让它修改 Go 并发逻辑。

---

## 7. Day 5：登录、组织选择、工单列表与创建（3～4 小时）

建议开发顺序由你决定并记录在 `docs/frontend/web-mvp.md`。推荐按下面的最短业务路径推进。

### 7.1 登录页

- 用户输入账号与密码，调用 W02 `POST /api/v1/auth/login`。
- 成功后保存本次会话 Token 到内存状态，通过 `GET /api/v1/me` 获取当前用户信息。
- 登录失败显示非敏感错误，不打印密码、Token 或完整登录响应到控制台。
- 未登录进入受保护页面，路由可以跳转登录页；**前端路由守卫只是用户体验，权限仍由 Go 服务端强制校验**。

### 7.2 组织选择

使用 W02 `GET /api/v1/organizations` 读取当前用户可访问的组织。选择当前组织后保存 `orgID`，切换组织时清除旧组织的工单列表和详情缓存，重新请求数据。

注意：后端必须每次校验成员身份，不能因为前端显示了组织 A 就假定所有请求具有组织 A 的权限。

### 7.3 工单列表与创建

- 列表按 W03 的 `page`、`page_size`、`status`、`priority` 使用真实后端分页结果。
- 创建表单最少包含标题、问题描述、优先级；不能让客户端提交 `creator_id`、`org_id`、`assignee_id` 等服务端身份字段。
- 创建成功后跳转详情或刷新列表，失败时保留用户输入并给出合理错误信息。
- 列表展示 `status`、`priority`、`assignee_id`、更新时间，使用简洁表格即可。

### 7.4 Codex 的正确使用范围

允许 Codex 在**指定的 `web/src/views` 和 `web/src/components` 文件**里生成布局和基础表单，但 API 协议、错误分类、组织切换与 Token 策略由你先决定。审核它是否引入 Mock 数据、绕过权限、使用未经确认的字段名或把密钥放到前端。

**当天输出：** 浏览器真实完成登录、选择组织、查看工单列表、创建工单；能够在浏览器 Network 面板识别对应的 HTTP 请求。

---

## 8. Day 6：工单详情、领取与状态处理联调（3～4 小时）

### 8.1 页面行为

工单详情展示标题、描述、状态、优先级、当前受理人和事件（可简化）。只有在当前工单为 OPEN 且未指派时，界面才显示领取按钮；即使按钮被隐藏，后端**仍必须进行全部权限和状态校验**。

领取按钮调用 `/claim`，请求体携带页面刚读到的 `expected_version`。成功后刷新详情并显示受理人。失败为 `409` 时显示“工单已被领取或版本发生变化，请刷新后重试”，随后刷新详情，不要覆盖成页面旧数据。

若当前用户为受理人，使用 W03 `/transition` 的 `expected_version` 将 OPEN 变为 IN_PROGRESS，并在页面更新展示。评论和 RESOLVED 转换可以作为可选页面扩展，但后端能力必须仍存在。

### 8.2 必须主动制造的联调问题

1. Bob 和 Charlie 同时打开同一工单，先后或同时点击领取。最终只能一人成功。
2. Bob 刷新后 Token 丢失，跳转登录页，不携带旧令牌调用接口。
3. 修改前端代码，尝试使用组织 B 的 orgID 访问组织 A 的 ticketID，后端拒绝。
4. 模拟服务器离线，前端展示网络错误而不是无响应卡死。
5. 接口发生 `409`，前端重新获取最新版本而不是强制覆盖。
6. 组织切换后旧组织的数据不会在新组织页面误显示。

### 8.3 G1 演示数据

可以使用 W02 的注册/组织 API 和 W03 的创建/指派 API 生成**专用演示用户和工单**，但不应在文档或 Git 中存储演示账号的真实密码。需要复现脚本时，采用本地环境变量与占位符。不要使用个人真实信息。

**当天输出：** 端到端业务可以在浏览器操作，记录至少六项联调结果及截图路径或视频素材。

---

## 9. Day 7：回归测试、Codex Review、G1 验收与复盘（2～3 小时）

### 9.1 完整验证

在仓库根目录：

```powershell
go fmt ./...
go test ./...
go test -tags=integration ./tests/integration/...
go test -race ./internal/ticket/...
go vet ./...
go build ./...
git diff --check
```

在 `web/` 目录：

```powershell
npm run build
npm run lint       # 如果实际 package.json 提供该脚本
npm run test:unit  # 如果实际 package.json 提供该脚本
```

你需要根据 `package.json` 的真实 scripts 判断命令。命令没运行就写未运行，失败要保留失败记录和原因，不能让 AI 删除断言或偷偷改期望值。

### 9.2 G1 最终演示脚本（1～2 分钟）

1. 启动 PostgreSQL 和 Go API，打开 Vue 页面。
2. Alice 登录并选择组织 A，查看工单列表并创建一张 OPEN 工单。
3. Bob 和 Charlie 各开一个独立浏览器会话，访问同一张工单，同时领取。
4. 展示恰好一位领取成功，另一位收到 409 并刷新后看到受理人。
5. 成功者将工单从 OPEN 推进到 IN_PROGRESS。
6. 展示后台事件记录，并切换到组织 B 用户尝试越权访问，后端拒绝。
7. 说明这是 G1 版本，后续将在第 5 周开始文档上传与知识库。

录屏不需要豪华剪辑，重点是证明真实功能，不要在视频中露出明文密码、JWT 或数据库连接串。

### 9.3 建议提交记录

```text
docs(ticket): specify atomic claim contract and conflict semantics
feat(ticket): implement transactional conditional claim
test(ticket): verify concurrent claims and rollback
chore(web): bootstrap Vue 3 TypeScript workspace
feat(web): add auth, organization and ticket pages
feat(web): integrate claim and transition actions
docs: publish G1 demo and W04 retrospective
```

先检查 `git status`、`git diff` 和暂存内容，再由你自己决定何时 commit/push。不得授权 Codex 在未审查的情况下自动提交或强推分支。

### 9.4 七道你应当脱离 AI 回答的问题

1. `SELECT ... WHERE assignee_id IS NULL` 再执行无条件 UPDATE 为什么不安全？
2. W04 的条件更新为什么可以避免同一张工单出现两个有效受理人？
3. `version` 解决了什么问题？只靠版本号是否已经完整解决所有事务问题？
4. 领取 UPDATE 和操作事件为什么必须同事务？
5. `go test -race` 能做什么，为什么不能代替数据库并发集成测试？
6. Vite 开发代理、浏览器 CORS、后端 JWT 鉴权分别解决什么问题？
7. 为什么前端隐藏领取按钮不能代替后端权限校验？

建议把答案写入 `docs/weekly/week04.md`，隔一周不看 AI 再口头重述一次。如果只能照着 AI 生成的代码解释，暂时不要将该模块标为已经掌握。

---

## 10. 本周测试矩阵（20 项，建议至少 15 项自动化）

| 编号 | 场景 | 预期 |
| --- | --- | --- |
| C01 | active 成员领取空闲 OPEN 工单 | 200，受理人为当前用户 |
| C02 | 非成员领取其他组织工单 | 404，不改数据 |
| C03 | 无 JWT 调用领取 | 401 |
| C04 | 领取 CLOSED / RESOLVED 工单 | 409 |
| C05 | 领取已删除工单 | 404 |
| C06 | 领取已有受理人的工单 | 409 |
| C07 | 用旧 expected_version 领取 | 409 |
| C08 | 请求体伪造 assignee_id | 不允许客户端指定受理人 |
| C09 | 同一人重复领取 | 第二次 409 |
| C10 | Bob、Charlie 同时领取 | 恰好一人成功 |
| C11 | 20 个请求同时领取 | 恰好一个成功，其他冲突 |
| C12 | 多轮并发后检查 version | 每张工单领取只增加一次 |
| C13 | 多轮并发后检查事件 | 一张工单只有一次成功领取事件 |
| C14 | 事件插入失败 | 领取 UPDATE 回滚 |
| C15 | 领取后状态仍 OPEN | 必须调用 transition 才开始处理 |
| C16 | 只有当前合法受理人开始处理 | 状态转移与权限均符合 W03 |
| C17 | 组织切换 | 前端不显示旧组织残留数据 |
| C18 | 401/409/网络错误 | 前端能区分并给出反馈 |
| C19 | 浏览器刷新后令牌丢失 | 重新登录；不暴露旧 Token |
| C20 | 创建工单到领取的浏览器流程 | 请求都走真实 API |

**排除伪通过：** 禁止通过延长 `sleep`、将所有请求串行化、直接人工改数据库受理人、将服务端错误统一吞掉等方式满足测试数字。每个自动测试应该清楚标识是在单元层还是 PostgreSQL 集成层验证。

---

## 11. 你与 AI 的本周分工

| 工作 | 你亲自负责 | ChatGPT / Codex 可协助 |
| --- | --- | --- |
| Claim API 语义 | 决定领取与状态转换是否分离 | 找出业务边界与冲突场景 |
| SQL 条件更新 | 亲手写、理解行级并发行为 | 只读 Review、解释官方文档 |
| Service 事务与事件 | 亲自实现、故障注入验证 | 查找遗漏的回滚和错误映射 |
| 并发集成测试 | 亲自写核心断言、运行与复盘 | 讨论测试设计、补充边缘测试 |
| Vue 工程初始化 | 自己执行命令并选择功能 | 给出命令解释和环境排查建议 |
| Vue 组件布局 | 确定页面流程并审查效果 | Codex 可生成限定范围内的 UI 代码 |
| HTTP 客户端与登录态 | 自己理解 Header、状态码和 Token | 审查安全、建议类型定义 |
| 联调与验收 | 自己操作浏览器和验证请求 | 辅助复现错误与写测试脚本 |
| Git 提交与复盘 | 本人负责 | 生成建议的提交描述、复盘提问 |

### 11.1 Codex 权限模式建议

- `PLAN_ONLY`：只读取允许范围的文档和源码，输出设计风险与开发计划，不修改。
- `REVIEW_ONLY`：只读审查，输出带文件位置、影响和复现方法的问题。
- `TEST_SCOPED`：仅在指定测试文件新增测试；禁止降低权限断言或使用生产数据。
- `UI_SCOPED`：仅修改已批准的 `web/src/views`、`components` 与样式文件；不编辑 Go 后端权限规则。
- `IMPLEMENT_SCOPED`：只有在你已经理解方案并明确列出允许修改的文件后才可使用；禁止隐藏改动。

这些模式是**团队约定的提示词标签，不是 Codex 平台内置的权限开关**。真正的文件写入、命令执行和网络访问限制仍然需要在工具的实际审批、沙箱和操作界面中配置。

### 11.2 三条可以直接使用的 Codex 提示词

**设计审查（只读）**

> REVIEW_ONLY。阅读 `docs/api/ticket-claim.md`、`docs/decisions/0004-atomic-ticket-claim.md` 和 `internal/ticket`。不得修改文件或执行有副作用的命令。请检查组织隔离、事务一致性、版本冲突、已分配工单、软删除、事件记录等风险，并按严重程度输出文件位置与建议验证方法。

**并发测试审查（只读）**

> REVIEW_ONLY。检查 `tests/integration/ticket_claim_test.go` 是否真的通过独立 PostgreSQL 连接并发调用同一领取逻辑。请核对成功次数、最终 assignee、version、事件数量、事务回滚和租户隔离断言。不要修改实现或测试文件，不能把 `go test -race` 解释为数据库一致性证明。

**前端 UI 授权（限制修改范围）**

> UI_SCOPED。仅允许修改 `web/src/views/TicketListView.vue` 和 `web/src/components/TicketForm.vue`。请根据 `docs/frontend/web-mvp.md` 完成列表和表单的基础布局，不增加依赖，不使用 Mock 数据，不修改 HTTP 客户端、Token 存储、Go 代码或其他文件。先提出修改计划，待我确认后执行。完成后报告 diff 与实际运行的检查命令。

---

## 12. G1 验收清单与下一周交接

- [ ] `/claim` 使用数据库条件更新，受理人由 JWT 上下文确定。
- [ ] 成功领取保持 OPEN，version 增加一次，事件只有一条。
- [ ] 事件写入失败会回滚领取。
- [ ] 真实 PostgreSQL 并发测试通过，跨组织访问被拒绝。
- [ ] `web/` 可以启动、构建，并和真实后端联调。
- [ ] 浏览器能完成登录、组织选择、建单、查看、领取、开始处理。
- [ ] 401、403/404、409、网络错误有明确处理，页面不泄露敏感信息。
- [ ] 后端与前端相关测试已实际运行并记录结果。
- [ ] `AGENTS.md` 规则保留 W01～W03 条款，新增 W04 边界。
- [ ] `docs/weekly/week04.md` 包含个人实现比例、Codex 使用记录、技术复盘和未解决问题。
- [ ] G1 演示视频真实录制，README 有明确本地启动步骤。

**进入第 5 周的条件：** 工单业务主线可用，权限与并发领取核心测试通过。下一周才引入文档上传、存储抽象和知识库文件管理。不要在 W04 把 AI/RAG 提前堆进项目。

---

## 13. 官方参考资料

1. Vue 官方快速开始：https://vuejs.org/guide/quick-start
2. PostgreSQL 事务隔离与并发更新：https://www.postgresql.org/docs/current/transaction-iso.html
3. PostgreSQL `RETURNING`：https://www.postgresql.org/docs/current/dml-returning.html
4. Go 官方安全实践与 Race Detector：https://go.dev/doc/security/best-practices
5. OpenAI Codex `AGENTS.md` 用法：https://learn.chatgpt.com/docs/agent-configuration/agents-md


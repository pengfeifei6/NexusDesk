# NexusDesk 第一周详细开发手册

**版本**：W01 · v1.0  
**适用对象**：计算机科班本硕、研二、能用 Go + Gin + SQL 写基础接口  
**定位**：12 周项目计划的第 1 周，重点是亲自搭建可复现的 Go 工程基础，并形成正确的 AI 协作习惯  
**建议投入**：约 14～18 小时。可按实际课业拆分，完成情况以验收结果为准。

> 本手册给出可复制的命令、文件模板和验收标准，但**不会替你生成核心 Go 业务实现**。你本人需要动手写 `main.go`、配置加载、健康检查、数据库连接和第一次 migration。Codex 先做审查与解释，之后再逐步授权它完成有限任务。

---

## 0. 本周边界与最终成果

### 本周只做什么

1. 确定 P0 产品范围，独立写 3 个用户故事和验收条件。
2. 明确模块化单体结构与 6 张未来的核心业务表关系。
3. 创建 Git 仓库、Go Module、项目文件夹、`AGENTS.md`、`.gitignore`、环境变量模板。
4. 自己写出一个可启动的 Gin API 和 `GET /healthz`。
5. 使用 Docker Compose 运行 PostgreSQL，独立写并执行第一条 Goose migration。
6. 完成单元测试、构建、手动验证、代码审查与 Week 1 复盘。

### 本周明确不做

不开发登录注册、完整工单 CRUD、Vue 前端、Redis、消息队列、RAG、Agent、MCP、微服务或 Kubernetes。登录放在第 2 周，工单业务放在第 3 周，以免基础工程没有验收就持续加功能。

### 周末必须可以演示

- 打开项目，按 README 在本机运行服务与 PostgreSQL。
- 访问 `http://127.0.0.1:8080/healthz`，收到 HTTP 200 与可预期的 JSON。
- 查看 PostgreSQL 中的 `organizations` 表和 Goose 迁移版本。
- 实际执行 `go test ./...`、`go vet ./...`、`go build ./...`。
- 说明每个目录为何存在、哪些目录尚未创建、为何先选模块化单体。

---

## 1. 事先确认技术与开发环境

### 1.1 推荐第一周工具

| 工具 | 用途 | 安装或检查 |
| --- | --- | --- |
| Go 稳定版本 | 编译、依赖、测试 | `go version` |
| Git | 版本管理与回退 | `git --version` |
| VS Code + Go 扩展或 GoLand | IDE 与调试器 | 能打开项目并运行 Go |
| Docker Desktop / Engine + Compose | 本地 PostgreSQL | `docker version`、`docker compose version` |
| PowerShell / Terminal | 执行命令 | 能调用 Go、Git、Docker |
| ChatGPT | 解释、方案审查、复盘 | 不需要仓库读写权限 |
| Codex IDE 扩展或 CLI | 只读代码审查、限定范围实施 | 能进入本项目并读取 `AGENTS.md` |
| Goose CLI（第 5 天安装） | SQL schema migration | `goose -version` |

**环境选择**：以下终端命令优先按 Windows PowerShell 编写，因为很多个人开发环境在 Windows 上。Linux/macOS 可以把目录创建命令换成 `mkdir -p`，其余 Go、Git、Docker 相关命令大多相同。可选 WSL2，但不要在同一周里频繁切换 Windows 和 WSL 项目目录。

**版本策略**：项目选择与你本机 Go 版本、Gin 和 pgx 兼容的稳定版本，提交 `go.mod/go.sum`。不要只因为 AI 推荐就任意升级。PostgreSQL 示例固定主版本 17，后续升级按迁移计划操作。

### 1.2 第一项自检（10～15 分钟）

在 PowerShell 中运行：

```powershell
go version
git --version
docker version
docker compose version
```

如果 Docker 无法连接守护进程，先启动 Docker Desktop；如果 Go 无法识别命令，检查安装和 `PATH`。把实际输出记录在 `docs/weekly/week01.md`，不要写自己没有执行过的版本信息。

---

## 2. 第一周目标项目目录

**约定**：最终完整项目会有 `cmd/worker/`、`internal/agent/`、`internal/rag/`、`web/` 等目录，但第 1 周先不创建没有用途的空模块。只创建本周真正需要的代码和文档。

```text
nexusdesk/
├── .github/
│   └── workflows/
│       └── ci.yml                 # D6，可选但推荐
├── cmd/
│   └── api/
│       └── main.go                # D4，你亲自写
├── internal/
│   └── platform/
│       ├── config/
│       │   └── config.go          # D4，你亲自写
│       ├── httpserver/
│       │   ├── router.go          # D4，你亲自写
│       │   └── router_test.go     # D6，你先设计断言
│       └── database/
│           └── postgres.go        # D5，你亲自写
├── deploy/
│   └── compose.yaml               # D5，你配置并解释
├── migrations/
│   └── 00001_create_organizations.sql  # D5，你亲自写
├── docs/
│   ├── requirements.md            # D1
│   ├── architecture.md            # D2
│   ├── database.md                # D2
│   ├── decisions/
│   │   └── 0001-modular-monolith.md
│   └── weekly/
│       └── week01.md              # 每日更新
├── .env.example                   # D3，仅示例，不是真实密码
├── .env                           # D3，本地创建，禁止提交
├── .gitignore
├── AGENTS.md                      # 注意是 AGENTS.md，不是 agent.md
├── README.md
├── go.mod                         # go mod init 自动生成
└── go.sum                         # 下载依赖后生成
```

### 2.1 目录职责需要你自己讲得清

- `cmd/api` 是应用程序启动入口，负责组合配置、数据库和路由；不应堆放业务规则。
- `internal/platform/config` 负责读取、校验环境变量；不允许代码中写死真实凭据。
- `internal/platform/httpserver` 负责 Gin 引擎、路由和 HTTP 协议适配。
- `internal/platform/database` 负责连接池与生命周期管理；暂不放工单 SQL。
- `migrations` 保存数据库结构演进的事实记录。
- `docs` 是需求与技术决策依据，也是 Codex 的长期上下文来源。
- `deploy` 是本地基础设施配置；本周 Go 程序仍在宿主机运行，PostgreSQL 在容器中。

---

## Day 1：确定 P0 范围与用户故事（建议 2 小时）

### 第 1 步：先创建工作目录，再开始需求分析

第一天只创建基础文件夹，**暂时不执行** `git init` 和 `go mod init`，它们留到 Day 3。

```powershell
mkdir nexusdesk
cd nexusdesk
New-Item -ItemType Directory -Force docs, docs/decisions, docs/weekly | Out-Null
New-Item -ItemType File -Force docs/requirements.md | Out-Null
```

以后每一天都在这个 `nexusdesk` 根目录中工作。如果已经创建过，不要重复嵌套生成 `nexusdesk/nexusdesk`。

在 `docs/requirements.md` 写出 4 个小节。

1. 目标用户：软件支持团队的普通用户、支持人员和管理员。
2. 真实问题：文档分散、重复答疑、工单流转难以追溯。
3. 核心使用流程：提交工单、分配/处理工单、查看工单状态。
4. P0 和 P1 的边界：P0 先把业务做通；P1 才引入文档摄取与 AI 问答。

先用**你自己的语言**写，不要让 AI 替你生成整段 PRD。

### 第 2 步：写 3 个用户故事

建议覆盖不同角色：

- 普通用户能够创建工单并查看自己提交的记录。
- 支持人员能够领取、处理并更新被授权的工单。
- 组织管理员能够管理成员角色并查看本组织工单。

每个故事都补上 2～3 条**可测试的验收标准**。例如：非本组织用户不能读取另一个组织的工单；状态转换不合法时返回可识别的错误。

### 第 3 步：输出 P0 功能表

建议字段：`模块 / 用户故事 / 优先级 / 输入 / 输出 / 成功标准 / 暂不实现`。不要把 Agent、MCP、Redis 作为本周必须完成的功能。

### 第 4 步：ChatGPT 只审查，不代写

提示词：

```text
你是 NexusDesk 项目的需求评审员。下面是我自己写的 P0 需求。
请只指出业务流程、角色权限、异常情况和验收标准中容易遗漏的地方。
不要重写整份 PRD，不要增加 RAG、Agent 等第 1 周范围外的功能。
请按“确定的问题 / 待我决定的取舍 / 建议新增的测试”组织反馈。
```

**你自己的责任**：判断哪些建议有意义，亲自调整 PRD，写清采纳或不采纳的理由。

### Day 1 验收

- [ ] `docs/requirements.md` 至少包含 3 个用户故事及各自验收标准。
- [ ] 能用 1 分钟解释 NexusDesk 和普通 CRUD 管理系统的区别。
- [ ] 明确说明 AI 功能为什么不在第一周实现。
- [ ] 记录本次 AI 建议和你亲自决定的修改。

---

## Day 2：架构与数据库设计（建议 2～3 小时）

### 第 1 步：自己画出最小架构

可以用 Mermaid、Excalidraw 或手绘，要求体现：

```text
将来的 Vue 前端
      |
      v
Gin HTTP API（本周实现）
      |
      v
Service（第 2～3 周增加）
      |
      v
Repository（第 2～3 周增加）
      |
      v
PostgreSQL（本周启动）
```

这里的 Handler、Service、Repository 是职责划分，不必为了目录好看预先生成一堆空接口。

### 第 2 步：画出未来核心 ER 图（先设计，不全部建表）

核心表：

| 表 | 本周处理程度 | 设计核心问题 |
| --- | --- | --- |
| `organizations` | **本周实际建表** | 如何唯一标识租户？ |
| `users` | 第 2 周建表 | 邮箱唯一与密码存储 |
| `organization_members` | 第 2 周建表 | 一人可以加入多个组织吗？ |
| `tickets` | 第 3 周建表 | 工单归属、状态、版本字段 |
| `ticket_comments` | 第 3 周建表 | 作者和工单权限 |
| `ticket_events` | 第 3 周建表 | 状态变更审计与事务 |

至少想清 4 个点：主键、外键、多租户字段 `org_id`、时间字段和约束。此时只设计，不提前落地全部 SQL。

### 第 3 步：写第一个架构决策 ADR

文件 `docs/decisions/0001-modular-monolith.md`，内容需要回答：

- 为什么采用模块化单体，而不是立即拆分微服务？
- 如果只有一个开发者，微服务会引入哪些成本？
- 未来什么证据出现时才值得拆分（独立扩缩容、故障边界、部署节奏等）？

### 第 4 步：Codex 进行设计只读评审

如果目前仓库还没有足够的代码，可以先让 ChatGPT 审查文档。Codex 进入仓库后，只要求它检查文档之间是否矛盾，不修改文件。

### Day 2 验收

- [ ] `docs/architecture.md` 说明模块职责和数据流。
- [ ] `docs/database.md` 列出 6 张核心表及关系。
- [ ] ADR 记录了方案、替代方案、理由与代价。
- [ ] 可以脱离 AI 解释 `Handler → Service → Repository` 的职责边界。

---

## Day 3：创建仓库、目录和 AGENTS.md（建议 2 小时）

### 第 1 步：初始化项目

**以下以 PowerShell 为例**。先进入 Day 1 已创建的 `nexusdesk` 根目录；如果终端原本就在该目录，不需要再 `cd`。将 `YOUR_GITHUB_USERNAME` 换成自己的 GitHub 用户名；路径务必和未来仓库一致。

```powershell
# 从 nexusdesk 目录内执行
git init
go mod init github.com/YOUR_GITHUB_USERNAME/nexusdesk
```

检查 `go.mod` 中的 `module` 行正确。如果之后修改 GitHub 仓库位置，应同步考虑 Go import 路径。

### 第 2 步：创建本周需要的目录

```powershell
$dirs = @(
  'cmd/api',
  'internal/platform/config',
  'internal/platform/httpserver',
  'internal/platform/database',
  'deploy',
  'migrations',
  'docs/decisions',
  'docs/weekly',
  '.github/workflows'
)
foreach ($d in $dirs) {
  New-Item -ItemType Directory -Force -Path $d | Out-Null
}
```

注意 Git 不会记录真正的空目录。请先按开发顺序创建要用的文件，不必为未来所有领域模块建 `.gitkeep`。

### 第 3 步：创建配置与文档文件

```powershell
$files = @(
  'AGENTS.md', '.gitignore', '.env.example', 'README.md',
  'docs/requirements.md', 'docs/architecture.md',
  'docs/database.md',
  'docs/decisions/0001-modular-monolith.md',
  'docs/weekly/week01.md'
)
foreach ($f in $files) {
  if (-not (Test-Path $f)) {
    New-Item -ItemType File -Path $f | Out-Null
  }
}
```

随后把本资料包内的模板内容放入同名文件，并基于自己的决策修改。**模板是帮助你起步，不意味着设计已经经过你确认。**

### 第 4 步：确认 `AGENTS.md` 的正确名称和作用

文件名使用**大写复数** `AGENTS.md`，放在仓库根目录。这是 Codex 识别的可选仓库说明文件，不是 `agent.md`、`.env` 或某个专用 Agent 程序。它主要声明项目目标、代码约定、当前开发边界、测试方式和禁止自动执行的操作。Codex 官方介绍了这种仓库指导方式。见文末参考资料。

**第一周必须有 6 条实质约束**：

1. 默认仅审查或先给方案，不可擅自修改代码。
2. `main.go`、配置读取、健康检查、数据库连接及首个 migration 必须先由你本人实现。
3. 单个任务必须提供可修改文件范围，禁止顺手跨模块重构。
4. 禁止读取、提交或打印 `.env`、Token 和真实密钥。
5. 禁止未授权的数据库破坏性操作、`git push`、自动合并或部署。
6. 报告实际执行的测试及结果，不能编造“测试通过”。

资料包的 `项目起步文件/AGENTS.md` 已包含可直接使用的第一版。注意它是协作规范，**不能代替 Codex 产品实际的隔离和权限设置**。

### 第 5 步：创建 `.env` 并检查 `.gitignore`

```powershell
Copy-Item .env.example .env
```

手动修改 `.env` 的 `DB_PASSWORD` 为仅在本地使用的密码；`.env.example` 只能保留占位值。检查：

```powershell
git status --short
git check-ignore .env
```

`git check-ignore .env` 应显示 `.env`。不要在聊天、截图或 AI 提示词里提供实际密码。

### 第 6 步：第一笔 Git 提交

先检查要提交的文件，然后按实际完成情况提交：

```powershell
git status
git add AGENTS.md .gitignore .env.example README.md docs/ go.mod
git diff --cached
git commit -m 'chore: initialize nexusdesk repository and docs'
```

**不要使用 `git add -f .env`。** 如果暂时没有配置 Git 身份，先设置 `git config user.name` 与 `git config user.email`，不必开启自动推送。

### Day 3 验收

- [ ] `go.mod` 中的 module 路径正确。
- [ ] 根目录存在 `AGENTS.md`，而不是 `agent.md`。
- [ ] `.env` 真实存在，但 Git 忽略它。
- [ ] 第一个 Commit 只包含已审核文件。
- [ ] 你能向 Codex 解释本周为什么默认采用 `REVIEW_ONLY`。

---

## Day 4：亲自写 Go 服务和健康检查（建议 2～3 小时）

### 第 1 步：引入 Gin

```powershell
go get github.com/gin-gonic/gin
# 写好引用 Gin 的 Go 文件之后，再执行 go mod tidy
```

用 IDE 查看下载的依赖，理解直接依赖与间接依赖的区别。不要让 Codex 自动创建完整 Web 模板。

### 第 2 步：你亲自写 3 个文件

**`cmd/api/main.go`**：只处理启动顺序、加载配置、创建 Router、启动 HTTP 服务与必要的错误处理。第一周不放业务逻辑，也不要写大量全局变量。

**`internal/platform/config/config.go`**：定义与读取 `APP_ENV`、`HTTP_ADDR`、数据库配置（`DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`、`DB_SSLMODE`）。不在代码中写密码。对缺失的必要变量返回明确错误，不打印密码内容。

**`internal/platform/httpserver/router.go`**：创建 Gin Engine，注册 `GET /healthz`，返回 HTTP 200 与 JSON，例如 `{"status":"ok"}`。先使用最小实现，不必提前加入 JWT 中间件、Swagger 或 Prometheus。

### 第 3 步：加载本地环境变量，再运行

**重要**：Go 的 `os.Getenv` 不会自动读取磁盘里的 `.env`。本手册暂时不引入新的 dotenv 库，先在 PowerShell 中将 `.env` 的简单 `KEY=VALUE` 行加载到当前终端进程。这个简单脚本只适合本周 `.env.example` 中不含复杂引号、空格等特殊语法的变量值。

```powershell
Get-Content .env | ForEach-Object {
  if ($_ -match '^([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
    [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
  }
}
```

如果在新终端重新运行 Go 应用，要重新加载环境变量。后期可以选择专门的本地开发启动脚本或 dotenv 工具，但生产环境应通过部署平台注入必要变量。

完成三个文件并实际 import Gin 后，执行：

```powershell
go mod tidy
go fmt ./...
go run ./cmd/api
```

在另一个 PowerShell 窗口执行：

```powershell
curl.exe -i http://127.0.0.1:8080/healthz
```

预期至少有 HTTP 200 与 `status=ok`。如果返回了 `404`、`500` 或端口占用，不要马上让 Codex 改代码，先自己确认 Gin 路由、监听地址、服务日志以及端口情况。

### 第 4 步：设置一个故意的错误实验

把 `HTTP_ADDR` 改成已被占用的端口或错误格式，观察程序的真实报错及退出方式；再恢复正常配置。目标是搞清**启动失败应该如何被感知**，而不是把错误隐藏在 `panic` 或没有解释的日志里。

### Day 4 验收

- [ ] 你亲自写完三个 Go 文件。
- [ ] `GET /healthz` 返回 200 和固定 JSON。
- [ ] 关键参数来自环境变量而不是硬编码。
- [ ] 能自己解释 `main`、Router、Handler 三者的责任。
- [ ] 记录一次真实的启动错误及排查过程。

---

## Day 5：Docker PostgreSQL、连接池与第一条 migration（建议 2～3 小时）

### 第 1 步：亲自配置 `deploy/compose.yaml`

以下是**需要你理解后再手工输入**的参考骨架。这里只启 PostgreSQL，不启动 API/Redis。

```yaml
services:
  db:
    image: postgres:17
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${DB_USER:?set DB_USER}
      POSTGRES_PASSWORD: ${DB_PASSWORD:?set DB_PASSWORD}
      POSTGRES_DB: ${DB_NAME:?set DB_NAME}
    ports:
      - "127.0.0.1:${DB_PORT:-5432}:5432"
    volumes:
      - nexusdesk_pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U \"$${POSTGRES_USER}\" -d \"$${POSTGRES_DB}\""]
      interval: 5s
      timeout: 3s
      retries: 10

volumes:
  nexusdesk_pgdata:
```

说明：`${DB_USER}` 等值由本地 `.env` 提供，Compose 文件本身不保存真实凭据。端口只绑定到 `127.0.0.1`。生产环境的密码管理、网络与备份不能照搬这份本地配置。

### 第 2 步：检查并启动数据库

从项目根目录执行：

```powershell
docker compose --env-file .env -f deploy/compose.yaml config
docker compose --env-file .env -f deploy/compose.yaml up -d db
docker compose --env-file .env -f deploy/compose.yaml ps
```

**注意**：`docker compose config` 可能输出展开后的数据库密码，不要将其输出复制到 AI 聊天或公开 Issue。可只在本机查看。

如果本机 5432 已经被占用，把 `.env` 的 `DB_PORT` 改为 5433，然后再次启动；Go 数据库连接也应使用同一个配置。

### 第 3 步：安装 Goose 数据库迁移工具

```powershell
go install github.com/pressly/goose/v3/cmd/goose@latest
```

如 CLI 不在 `PATH` 中，Windows 可调用 `$(go env GOPATH)\bin\goose.exe` 的完整路径。不要在团队或长期 CI 中永远固定 `@latest`，本周确认可用版本后应在 README 中记录版本，后续采用可复现的安装方式。

### 第 4 步：自己创建第一个 migration

文件 `migrations/00001_create_organizations.sql`。先手写，再对照下面的**学习性参考结构**检查：

```sql
-- +goose Up
CREATE TABLE organizations (
    id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE organizations;
```

你需要独立理解：为什么使用 `BIGINT`？什么是 Identity？`NOT NULL` 的意义是什么？为什么时间字段使用 `TIMESTAMPTZ`？为什么 Down 有数据丢失风险？

**安全约束**：Down 仅用于本地测试库，且确认无需要保留的数据。后期已被其他人使用的迁移不能随意重写或回滚。

### 第 5 步：执行 Up、Status，并在空的本地库验证 Down

在已执行上面 PowerShell 环境变量加载步骤的**同一终端**中，构造一个仅用于本地开发的连接字符串。不要把连接字符串打印、截图或复制到 AI 对话中。你的实际密码如果含有空格、反斜杠等特殊字符，应查阅 pgx 的连接字符串转义规则；本周建议先使用长度足够的随机字母数字密码。

```powershell
$dbConn = "host=$env:DB_HOST port=$env:DB_PORT user=$env:DB_USER password=$env:DB_PASSWORD dbname=$env:DB_NAME sslmode=$env:DB_SSLMODE"

goose -dir migrations postgres $dbConn up
goose -dir migrations postgres $dbConn status

# 仅在可丢弃的本地测试库中，确认后再执行回滚实验
goose -dir migrations postgres $dbConn down
goose -dir migrations postgres $dbConn up
```

如果 `goose` 命令未加入 PATH，可将命令中的 `goose` 换成 `& "$(go env GOPATH)\bin\goose.exe"`，其余参数保持不变。这个示例将密码作为本机进程启动参数传入，因此只适合个人本地开发，不应照搬到生产和共享环境。

可以通过 `docker compose ... exec db psql -U nexusdesk -d nexusdesk` 查询 `organizations` 是否存在。记录实际截图或输出时，不包含密码。

### 第 6 步：你亲自写数据库连接文件

`internal/platform/database/postgres.go`：使用 `pgxpool`，接收 Context，创建连接池并在启动阶段做有超时的 Ping；应用退出时关闭连接池。所需依赖可使用：

```powershell
go get github.com/jackc/pgx/v5/pgxpool
# 写好引用 pgxpool 的代码后，再运行 go mod tidy
```

**重要知识点**：连接池不是单个永久连接；超时控制要用 Context；不能在日志中输出包含密码的 DSN。第一周可以先把数据库连通，但不必实现业务 Repository。

### Day 5 验收

- [ ] PostgreSQL 容器显示 healthy。
- [ ] migration Up 后存在 `organizations` 表。
- [ ] 可以解释 migration 的 Up/Down 和版本记录。
- [ ] Go 程序能在本地验证 DB 连接成功；失败时有清晰错误。
- [ ] `.env` 和真实密码没有进入 Git。

---

## Day 6：测试、错误复现和 Codex Review（建议 2 小时）

### 第 1 步：自己决定测试什么

为 `GET /healthz` 至少设计三项检查：

1. 状态码等于 200。
2. 响应体 JSON 可以解析，并包含 `status: ok`。
3. 使用不需要启动真实网络监听的 `httptest` 测试路由。

把断言写在 `internal/platform/httpserver/router_test.go`。可以问 ChatGPT `httptest.NewRecorder` 与 `httptest.NewRequest` 分别干什么，但**先自己完成测试**。

### 第 2 步：本地执行质量检查

```powershell
go fmt ./...
go test ./...
go vet ./...
go build ./...
git diff --check
```

如果有测试失败，记录确切输出并先自行定位。不可通过删除断言来“让测试变绿”。

### 第 3 步：让 Codex 进行第一次只读 Review

```text
任务 ID：W01-REVIEW-01
模式：REVIEW_ONLY，禁止修改任何文件
请先阅读 AGENTS.md、docs/requirements.md、docs/architecture.md。
审查范围：cmd/api/**、internal/platform/**、migrations/**、deploy/compose.yaml。
重点检查：配置与敏感信息、启动顺序、Context、错误处理、DB 连接池、healthz 测试、迁移安全。
按照严重程度给出问题、文件路径、复现或验证方法。
区分已经证实的问题和需要额外验证的风险。
不要修改代码、依赖、数据库、Git 历史或环境配置。
```

你应逐条判断建议。对于重要问题先复现，再决定是否修改。可以要求 Codex 补充测试设计，但本周核心逻辑不让它直接重写。

### 第 4 步：可选的 GitHub Actions

如果以上检查已稳定通过，可自己添加 `.github/workflows/ci.yml`，在 push / pull request 时执行 checkout、Go 环境初始化、`go test`、`go vet`、`go build`。第一周不要让 CI 依赖本地 `.env`，也不要在公共工作流里放数据库密码。

### Day 6 验收

- [ ] 健康检查测试真实通过。
- [ ] `go test`、`go vet`、`go build` 都有记录。
- [ ] Codex 只读审查未产生代码改动。
- [ ] 至少处理一个真实的审查发现或记录为何不采纳。
- [ ] 你能说明 `httptest` 和真正发 HTTP 请求的区别。

---

## Day 7：README、复现演示、学习复盘（建议 2 小时）

### 第 1 步：完善 README 并亲手复现

README 至少写明：

- 项目目前处于 Week 1，哪些功能**尚未实现**。
- Go、Git、Docker、Goose 的前置条件。
- `.env.example` 复制为 `.env` 的步骤。
- PostgreSQL 启动与迁移命令。
- 运行 Go API 的命令、`/healthz` 的验证命令。
- 常见报错的排查入口。

最好关闭 API 和数据库容器后，再从 README 一步步启动，确认不是依赖某个未记录的手工操作。停止容器使用 `docker compose ... down` 即可，**不要使用 `down -v`**，它会删除本地数据卷。

### 第 2 步：独立技术复述（不看 AI 对话）

你应该能回答：

1. Go Module 解决什么问题？`go.mod` 与 `go.sum` 有什么区别？
2. 为什么 `cmd/api` 不适合存放核心业务逻辑？
3. `AGENTS.md` 能做什么，不能做什么？
4. `.env.example` 与 `.env` 为什么要分开？
5. 为什么需要 DB migration 而不是手工在数据库里建表？
6. Gin 路由如何处理 `GET /healthz`？
7. `go test`、`go vet`、`go build` 的作用分别是什么？
8. Docker PostgreSQL 与本地 Go API 如何连接？

对于答不上来的问题，先用文档和最小实验补足，再请 AI 向你出几道追问。

### 第 3 步：整理 Git 与成果

```powershell
git status
git diff --check
go test ./...
go vet ./...
go build ./...
```

确认没有未预期文件、敏感配置与测试失败，然后手动提交有意义的变更。建议至少有初始化、API、数据库、测试文档等阶段性提交；不需要为了凑数量而制造空 Commit。

### 第 4 步：形成下一周交接说明

在 `docs/weekly/week01.md` 的末尾增加：

- 当前稳定运行的版本与验证证据。
- 尚未解决的问题，按影响程度排列。
- 第 2 周准备实现的 `users`、`organization_members`、认证和授权目标。
- 本周你独立解决的一个问题及其根本原因。

### Day 7 验收

- [ ] 按 README 成功从零复现服务启动。
- [ ] 可以离线解释第一周各模块和配置。
- [ ] 没有把真实环境配置或密码提交到 Git。
- [ ] 有清楚的 Git 历史、测试记录和 Week 2 交接说明。

---

## 3. 本周 AI 分工矩阵

| 工作 | 你必须亲自做 | ChatGPT 的职责 | Codex 的职责 |
| --- | --- | --- | --- |
| P0 需求 | 写用户故事、范围、验收条件 | 挑错、追问、比较方案 | 审查文档一致性 |
| 架构设计 | 绘图、写 ADR、做决定 | 解释模块化单体取舍 | 只读审查仓库结构 |
| 目录与 Git | 创建仓库、确认每个文件 | 解释 Go Module 与分层 | 可以草拟 `.gitignore` |
| `AGENTS.md` | 设定最终行为边界 | 帮助制定规则 | 遵守规则、不能代替权限设置 |
| Gin 健康检查 | **自己写** | 解释 Gin / HTTP / 错误处理 | **只读 Review** |
| 数据库连接 | **自己写** | 解释连接池、Context | 检查敏感信息和错误路径 |
| migration | **自己写、自己执行** | 解释约束与 Up/Down | 审查 SQL，不自行回滚 |
| 测试 | 决定断言、自己跑 | 解释 httptest | 审查或在授权后补充测试 |
| 部署与版本控制 | 自己验证、自己提交 | 提供检查清单 | 不自动 push / merge / deploy |

### 3.1 第一周最重要的 AI 使用规则

遇到技术不理解时，使用“讲原理 + 最小示例 + 练习题”，**不要让 AI 直接把整个模块写好**；遇到错误时先收集现象和日志，再请 AI 提出可验证假设；涉及代码修改时先审查 Diff、运行测试，再由你决定是否合并。

### 3.2 四种直接复制的提示词

**A. 技术导师模式**

```text
我是 NexusDesk 的开发者，计算机本硕，已会基础 Go/Gin/SQL。
当前学习：[填写 Go Modules / Context / pgxpool / migration]。
我的理解：[先写 3～5 句话]。
请先指出不准确的地方，再通过一个小例子解释；不要生成项目完整模块。
最后给我 3 个能独立检验理解的问题。
```

**B. 方案审查模式**

```text
这是我自己写的 NexusDesk 设计：[文档或片段]。
请检查目标与约束是否完整、哪些地方有隐藏风险、有哪些替代方案。
先指出问题，不要直接替我修改文件。请把可验证的建议与主观偏好区分开。
```

**C. Codex 只读审查模式**

```text
请先阅读 AGENTS.md 和本周 docs/weekly/week01.md。
工作模式 REVIEW_ONLY；只检查本任务相关文件，禁止编辑、提交、push、部署或迁移回滚。
优先指出明确 Bug、安全隐患和测试缺失；提供文件路径、证据和复现步骤。
所有“测试通过”的描述必须基于真实运行结果。
```

**D. Bug 排查模式**

```text
我正在排查一个问题，预期：[填写]；实际：[填写]；
报错与日志：[去掉密钥和敏感内容后填写]；已尝试：[填写]。
请先列出按概率排序的可能原因及其验证步骤，不要立即给完整替换代码。
等我报告验证结果后，再建议最小修复与回归测试。
```

---

## 4. 本周故障排查速查表

| 现象 | 先检查什么 | 不要做什么 |
| --- | --- | --- |
| `go` 命令不存在 | PATH、安装位置、重开终端 | 直接复制不明安装脚本 |
| Go import 失败 | `go.mod`、module 路径、网络与 `go mod tidy` | 直接删除 `go.sum` 碰运气 |
| Gin 返回 404 | 路径、方法、Router 注册、实际监听端口 | 直接替换整个项目 |
| 端口已占用 | 当前监听进程、`HTTP_ADDR`、Docker 端口映射 | 随意杀死不明进程 |
| Docker 连接失败 | Docker Desktop、守护进程、Compose 版本 | 直接删除数据卷 |
| PostgreSQL 起不来 | `docker compose ps`、容器日志、端口/权限 | 执行 `down -v` |
| Goose migration 失败 | migration 注解、连接信息、SQL 约束、版本状态 | 直接对重要库强制回滚 |
| DB 密码认证失败 | `.env` 与已有数据卷初始化密码是否匹配 | 在聊天中贴真实密码 |
| `go test` 失败 | 首个失败断言、可复现性、依赖服务 | 删除测试或改成恒真断言 |
| Codex 改动过多 | 任务范围、`git diff`、`AGENTS.md` | 不审查就接受全部补丁 |

**特别提醒**：PostgreSQL 的初始化密码通常在数据目录首次初始化时生效。后续仅修改 `.env` 不会自动修改已有数据卷内用户的密码。遇到认证失败应先理解该机制，不要为了修复而立即删库。

---

## 5. Week 1 最终验收表（全部需要证据）

| 验收项 | 通过条件 | 证据 |
| --- | --- | --- |
| P0 产品范围 | 3 个用户故事，每个有验收条件 | `docs/requirements.md` |
| 架构与设计 | 模块职责与 6 张核心表可解释 | 架构图、ADR、DB 文档 |
| 开发协作规则 | 根目录 `AGENTS.md`，默认只读、任务范围明确 | 文件和 Codex Review 记录 |
| Git 与敏感配置 | `.env` 不跟踪；Commit 可解释 | `git status`、`git check-ignore` |
| Go 项目启动 | `go run ./cmd/api` 成功 | 终端实际输出 |
| HTTP 健康检查 | 200 + 固定 JSON | `curl.exe -i` 结果 |
| PostgreSQL | 数据库容器正常、可连接 | `docker compose ps`、查询结果 |
| SQL migration | Up 正常，Down 在空本地库验证 | Goose status / SQL 查询 |
| Go 质量检查 | `go test`、`go vet`、`go build` 通过 | 实际命令输出 |
| 独立解释能力 | 不看 AI 解释 8 个基础问题 | 自测记录 |
| 排障与复盘 | 至少一个真实问题完成“现象-证据-根因-验证” | `docs/weekly/week01.md` |

**本周通过的定义**：不是项目文件数量多，而是能从干净环境复现、最小服务稳定、数据库结构可迁移、关键代码你自己掌握。尚未完成的部分保留为下一周待办，不需要用 AI 编造“完成”的记录。

---

## 6. 和第 2 周衔接

下周从组织和身份开始，按 `organizations` → `users` → `organization_members` 建立真实关系，学习密码安全存储、JWT、中间件和组织级授权。第 3 周才开始完整工单 CRUD 与事务，第 4 周再做前端联调。遵循前一版 12 周计划，不提前引入与本周目标无关的基础设施。

## 参考资料

- Go 官方 Module 与 Gin 教程：https://go.dev/doc/tutorial/
- Go Module 参考：https://go.dev/ref/mod
- Goose 官方 README：https://github.com/pressly/goose
- Codex 持久化仓库说明与开发流程：https://developers.openai.com/cookbook/examples/codex/iterating-development-workflows-with-codex
- Codex 分阶段规划与验证：https://developers.openai.com/blog/run-long-horizon-tasks-with-codex

**注意**：以上工具行为和语法应以当时实际安装的版本与官方说明为准。

# NexusDesk 第一周工作记录

- 周次：W01
- 记录日期：2026-11-09
- 当前阶段：Day 6 / Day 7 的可代办工程项已完成；个人复习、本人复现与 Git 提交待开发者进行。
- 编写方式：AI 根据当前仓库检查和本次协作事实整理；个人学习体会由开发者后续补充。

> “已生成文档”“已执行命令”“已通过功能验收”分别记录。最小 HTTP 服务、PostgreSQL、首条迁移、测试、只读审查和干净目录复现已有证据。以下不伪造开发者个人理解、独立实现或已提交历史。

## 1. 本周目标

1. 明确首个业务版本的需求与验收条件。
2. 设计模块职责、六张核心表和架构取舍。
3. 建立仓库、Go Module、配置模板与协作规则。
4. 亲自实现最小 API、PostgreSQL 连接和首条迁移。
5. 完成测试、复现、审查与技术复盘。

## 2. 已有成果与证据

| 成果 | 当前证据 | 状态说明 |
| --- | --- | --- |
| 需求初稿 | [requirements.md](../requirements.md) | AI 根据开发者委托生成，含 3 个故事和 18 条验收条件；业务尚未实现 |
| 架构设计 | [architecture.md](../architecture.md) | 已生成设计稿，包含当前与未来阶段边界 |
| 六张核心表设计 | [database.md](../database.md) | 已生成设计稿；Day 5 只实际创建并验证组织表 |
| 架构决策 | [ADR-0001](../decisions/0001-modular-monolith.md) | 已整理方案、理由、代价和重评条件 |
| 独立 Git 仓库 | `git rev-parse --show-toplevel` 返回项目自身目录 | 仓库根为 `/media/yjz/pzp/Go/projects/NexusDesk` |
| Go Module | [go.mod](../../go.mod) | `github.com/pengfeifei6/NexusDesk`，Go 指令为 `1.26.1` |
| 已有提交 | `git log -1 --format='%h %s'` | 本次编辑前为 `28bee06 第一次完整提交` |
| Day 4 核心代码 | `cmd/api/main.go`、`internal/platform/config/config.go`、`internal/platform/httpserver/router.go` | 已实现环境配置、健康检查及 HTTP 启动 / 退出；由 AI 根据开发者委托编写 |
| Day 4 测试 | 三个相邻的 `*_test.go` | 配置、路由和启动测试通过；Day 5 将 HTTP 生命周期测试与真实数据库验证分开 |
| Day 5 实现 | `deploy/compose.yaml`、`migrations/00001_create_organizations.sql`、`internal/platform/database/postgres.go` | 已通过独立测试库验证，API 启动先 Ping 数据库 |
| Day 5 可复现验证 | [day5-smoke.py](../../scripts/day5-smoke.py) | 自动使用临时端口、独立项目、测试凭据和可丢弃卷，不读取真实 `.env` |

本次已补齐根目录 `AGENTS.md`、`.gitignore`、`.env.example`、`README.md` 和本记录。实际配置检查结果见第 4 节。

## 3. 实际环境检查

以下是 2026-11-09 Day 3 时在当前 Linux 工作区实际执行的初始结果摘要，不包含凭据；Day 5 工具准备后的结果见第 9 节。

| 命令 | 结果 | 判断 |
| --- | --- | --- |
| `go version` | `go version go1.26.1 linux/amd64` | Go 可调用 |
| `git --version` | `git version 2.25.1` | Git 可调用 |
| `docker version --format 'Client={{.Client.Version}} Server={{if .Server}}{{.Server.Version}}{{else}}unavailable{{end}}'` | `Client=26.1.3 Server=26.1.3` | Docker 客户端可连接服务端 |
| `docker compose version` | `docker: 'compose' is not a docker command.` | Compose 插件当前不可用，待数据库阶段前补齐 |
| `git status --short --untracked-files=all` | 本次编辑前无输出 | 当时工作区干净，不代表本次新增文件已经提交 |

Day 3 未安装、升级软件或修改 Docker 配置，Goose 当时未验证。Day 5 后续安装情况单独记录，不覆盖历史检查结果。

## 4. Day 3 文件与配置检查

本节基于文件写入后的实际检查结果，不代表应用功能通过验收。

- 五个文件完整性、文档本地链接、行尾和代码围栏检查通过。
- `git check-ignore --no-index --verbose` 验证 `.env`、`.env.local`、`.env.production` 和嵌套的 `deploy/.env` 路径被忽略；构建、覆盖率和 profile 路径也按规则匹配。以上是路径规则检查，没有创建或读取这些真实配置文件。
- `.env.example` 命中允许跟踪的例外规则，并且由 `git ls-files --others --exclude-standard -- .env.example` 确认出现在待跟踪文件中；`go.mod`、`go.sum`、首条 migration 路径没有被忽略。
- `.env.example` 恰好包含约定的 8 个变量，密码为明确占位值；`bash -n .env.example` 语法检查通过，未加载该文件到运行环境。
- `git diff --check` 无报错；由于五个文件尚未跟踪，同时另外检查了新文件内容，避免仅依据空 Diff 判断完成。
- 本地真实 `.env`：Day 3 检查时不存在；Day 4 开始时仅通过文件存在性检查确认已存在，未读取内容。
- Git 提交：本次不执行提交、推送或合并。

检查过程说明：AI 校验脚本最初将 `git check-ignore` 命中 `!` 例外规则的退出状态误判为“文件被忽略”。通过查看具体匹配规则并交叉核对可跟踪列表，已修正校验判断并重新通过检查，忽略文件本身无需因此改动。

## 5. 当前待办

- [ ] 阅读并按个人理解修订需求、架构和数据库设计。
- [ ] 开发者核对本地 `.env` 已填写正确配置；AI 仅确认存在且被忽略，没有读取或验证真实值。
- [ ] 核对本次文件并自行提交。
- [x] Day 4：实现配置读取、启动入口、Gin 路由和 `/healthz`，由 AI 受托完成。
- [x] Day 4：AI 使用临时测试配置验证 HTTP 返回值与启动失败场景。
- [ ] Day 4：开发者阅读代码并亲自运行，不据 AI 完成记录推定个人已掌握。
- [x] Day 5 前补齐 Compose 插件与 Goose。
- [x] Day 5：实现 PostgreSQL Compose、首条组织表迁移和连接池，并用临时测试库验证。
- [ ] Day 5：开发者按 README 使用自己的本地配置复现，理解连接池和迁移的职责。
- [x] Day 6 工程项：归档测试、错误复现和只读审查，补齐基础 CI 文件。
- [x] Day 7 工程项：干净目录复现、数据库保留卷重启、演示摘要、工程复盘和 Week 2 交接。
- [ ] Day 6 / 7 个人项：阅读下文参考说明、亲自复现并补充自己的理解。

## 6. 本次 AI 协作记录

- 开发者依次委托 AI 完成 Day 1～5 文档和工程实现，本次要求整理测试情况并完成可代办的 Day 6 / 7 内容。
- AI 已解释可信身份与组织权限的区别，以及角色属于组织成员关系的原因。
- 需求和设计由 AI 编写，未记录为开发者独立编写，也不据此判断开发者已经掌握全部内容。
- Day 3 未实现 Go 代码；Day 4 引入 Gin v1.12.0，生成核心代码和必要测试。Day 5 引入 pgx v5.12.0，完成数据库基础，仅操作本次新建的可丢弃测试库，未接触真实配置或现有数据卷。
- Day 4 发现示例模板被填入密码值，已恢复为占位符；真实 `.env` 保持不动，测试使用独立的虚构凭据。
- 新增设计中的组合外键、事件版本和组织行锁属于后续实现建议，需要实际运行验证。

## 7. 个人学习与排障记录

以下由开发者根据真实过程填写，当前保留为待补充：

- 我能独立解释的内容：待补充。
- 仍不理解的内容：待补充。
- 我亲自实现的代码：尚未记录。
- 我遇到的问题、初始假设和验证过程：待发生后记录。
- 已证实的根因与修复结果：暂无记录。
- 个人运行服务、测试或迁移的命令及输出：待开发者补充；AI 的验证见下节。

## 8. Day 4 实际实现与验证

- 核心文件：配置读取 `config.go`、路由 `router.go`、启动入口 `main.go`。
- 测试先运行并确认因 `Load`、`NewRouter`、`run` 尚未实现而失败；补齐实现后全量测试通过。
- 配置测试覆盖 8 个变量的缺失、空白、非法地址和端口、SSL 模式、密码占位值与原文保留，以及错误不包含测试密码。
- 路由测试通过 httptest 验证 HTTP 200、JSON Content-Type 和固定 `status=ok`。
- 启动测试验证配置失败、占用端口失败、Context 取消退出。
- `go test ./...`：三个包通过。
- `go test -race ./...`：三个包通过。
- `go vet ./...`：通过，无报错。
- `go build ./...`：通过。
- 真实进程使用临时构建二进制和虚构环境变量启动，实际访问 `/healthz` 得到 HTTP 200 与 `{"status":"ok"}`；SIGTERM 退出码为 0。
- 临时占用端口后启动，程序退出码为 1，错误包含 `listen HTTP server`；非法或缺失 `HTTP_ADDR` 同样退出 1。
- 烟测日志不包含测试密码，进程与临时二进制已清理；没有读取真实 `.env`。
- 独立只读审查未发现需要修复的问题；审查方的三包 race 与 200 次取消测试通过。后续可将“健康检查成功后取消并确认监听关闭”补成持久化回归测试，本次已有真实进程 SIGTERM 验证；SIGINT 仅核对代码注册，未单独做真实进程实验。
- Day 4 当时尚未实现数据库连接、Compose 和 migration；后续 Day 5 的真实验证见下节，不把先前的 DB 配置校验当作连通性证据。

## 9. Day 5 实际实现与验证

- Compose `v5.6.0` 已按官方用户级安装方式安装，并校验发布文件 SHA-256；`docker compose version` 实际通过。
- Goose 通过固定版本 `go install github.com/pressly/goose/v3/cmd/goose@v3.29.0` 安装，`goose -version` 显示 `v3.29.0`。
- PostgreSQL 镜像为官方 Docker Hub 的 `postgres:17`，平台 `linux/amd64`，当前导入镜像 ID 为 `sha256:eb0fb2377534e65aad04f37838956b3dfb888e1a3dd3c1acf957bc6557eb6c7d`。
- Docker 守护进程从 Docker Hub 和镜像缓存拉取时出现 `unexpected EOF`。随后在终端网络环境中下载官方 manifest 与镜像层，逐层核对压缩内容 digest 和解压后 diff ID，再通过 `docker load` 导入；没有修改 Docker 守护进程配置。
- 新增 `database.Open`：安全编码 URL、5 秒启动上限、实际 Ping、连接失败关闭池、错误不输出连接字符串或密码。API 返回前关闭连接池。
- 测试先证实旧 API 在数据库不可达时仍会启动，补齐接入后这一回归测试通过。
- 独立审查建议补测黑洞 TCP 服务：已新增默认启动超时和连接过程取消的测试；临时移除两处超时后确认测试失败，恢复原实现后通过。
- 2026-11-09 实际执行 `python3 scripts/day5-smoke.py`，退出码为 0。测试项目为 `nexusdesk-day5-5298be5b1358`，使用本机临时端口和包含空格、单引号、反斜杠的随机测试密码，未读取真实 `.env`。
- 数据库容器达到 `healthy`；Goose Up / Status / Down / Up 成功，最终迁移版本为 1。Down 仅在刚创建的空测试库执行。
- Go 数据库集成测试通过，实际验证 SQL 查询、Identity 主键与默认时间；组织名称为 NULL 的写入被数据库拒绝。
- 全量 race 检查在上述独立数据库环境中通过，包含数据库集成测试。
- 最终 `go test ./...` 四个包通过，`go vet ./...`、`go build ./...`、Go 格式和文档链接检查通过；普通测试中的真实数据库集成项按设计跳过，由上述烟测单独执行通过。
- 真实 API 进程在数据库 Ping 后启动，`GET /healthz` 返回 200 和 `{"status":"ok"}`；SIGTERM 与 SIGINT 均正常退出 0。
- 错误密码、缺失数据库和数据库不可达均在 HTTP 监听前退出 1，错误日志没有测试密码。
- 本次项目的测试容器和命名卷已清理；删除测试卷前核对了 Compose 项目标签，没有删除已有开发数据卷。
- 自己的日常开发数据库没有由 AI 使用真实 `.env` 启动；开发者可按 README 用本地配置启动并执行 Up。

## 10. Day 6 测试情况与审查

本节由 AI 在 2026-11-09 实际执行并整理，供开发者后续阅读。

### 10.1 命令与结果

| 检查 | 实际结果 | 证据 |
| --- | --- | --- |
| `go test -json -count=1 ./...` | 4 个包通过；顶层测试 14 通过、0 失败、1 跳过 | [测试摘要 JSON](evidence/week01-test-summary.json)、[完整事件日志](evidence/week01-go-test.jsonl) |
| `go test -race -count=1 ./...` | 4 个包通过 | [质量日志](evidence/week01-quality.log) |
| `go vet ./...` | 退出 0，无报错 | 同上 |
| `go build ./...` | 退出 0 | 同上 |
| `git diff --check` | 退出 0；另检查新文件格式，避免漏掉未跟踪内容 | 同上及本次格式核对 |
| Go 源码格式 | `gofmt -l` 无待格式化文件 | 本次实际检查 |
| 干净目录数据库烟测 | 退出 0，包含实际 PostgreSQL 集成和全量 race | [复现日志](evidence/week01-clean-reproduction.log) |
| CI 文件 | YAML 和 Shell 静态检查通过，等效质量命令本地通过 | [.github/workflows/ci.yml](../../.github/workflows/ci.yml)；GitHub 远程未触发 |

顶层测试统计不把子测试重复计入。包括子测试在内共 33 个通过节点、0 个失败、1 个跳过节点。跳过的是 `TestOpenPostgresIntegration`：普通测试不依赖真实数据库；完整烟测通过 `NEXUSDESK_TEST_POSTGRES=1` 在自己的测试库中另行执行并通过它。Skip 不等于 Pass。

### 10.2 测试实际覆盖什么

| 范围 | 主要行为 | 尚未覆盖的未来业务 |
| --- | --- | --- |
| 配置 | 8 个变量、缺失与空白、地址/端口、SSL 模式、密码保留和错误脱敏 | JWT 配置与认证逻辑 |
| HTTP 路由 | 200、JSON Content-Type、可解析且固定的 `status=ok` | 业务请求、组织和对象授权 |
| HTTP 启动 | 配置错误、监听端口占用、Context 取消 | 登录和工单接口 |
| 数据库连接 | 不可达、预取消、握手途中取消、无回应连接的启动上限 | Repository、事务业务与查询性能 |
| 真实数据库 | 特殊字符密码、SQL 查询、Identity 与默认时间、名称非空约束 | 用户、成员、工单等后续表 |
| 进程与迁移 | 数据库先于 HTTP、错误退出、SIGINT/SIGTERM、迁移往返、保留卷重启 | 生产部署和灾难恢复 |

`httptest.NewRequest` 构造内存请求，`NewRecorder` 记录响应，调用 Router 的 `ServeHTTP` 不需要真正监听端口；真实进程烟测则覆盖 TCP 监听、配置、数据库和操作系统信号。两者分别验证不同边界，不能互相替代。

### 10.3 审查发现与处理

已完成独立只读审查，完整记录见 [W01-REVIEW-01](../reviews/week01-review.md)。审查方没有修改文件、运行测试或读取真实配置。

- 未发现 W1 运行逻辑中的阻塞缺陷，不据此宣称后续认证或业务已安全实现。
- 新发现：CI 在干净 checkout 中执行 `git diff --check` 无法检查已提交差异。已移除这一空检查步骤，保留有效的 Go 格式、race、vet、build；本地 Diff 检查单独保留。
- 既有发现：数据库超时和途中取消的测试缺口已补齐并通过；已响应 HTTP 后的退出行为由持久化进程烟测覆盖。
- 云端 CI 仍待代码提交并推送后真实触发，本次不伪造远程通过记录。

## 11. Day 7 复现演示与工程复盘

### 11.1 干净目录复现

在 `/var/tmp` 下新建项目快照，仅复制运行需要的源码、模块、Compose、迁移、脚本和示例配置，不复制真实 `.env`、`.git` 和生成缓存。执行 `go mod download` 和完整烟测后退出 0。

使用的是现有主机工具、模块缓存和本地 PostgreSQL 镜像；这证明干净项目目录可复现，不证明一台全新机器已完成工具安装。演示摘要见 [week01-demo.md](week01-demo.md)，原始结果见 [成功日志](evidence/week01-clean-reproduction.log)。

实际确认：数据库 healthy；Up / Status / Down / Up；停止容器并保留卷后重新启动，组织表与迁移版本 1 仍在，再次 Up 不重复执行；真实 API 返回 200；退出信号正常；错误密码、缺库、不可达均在监听前失败。最终测试容器、卷和快照已清理。

### 11.2 一次真实排障记录

| 项目 | 本次事实 |
| --- | --- |
| 现象 | 第一次快照数据库步骤通过，但 `go build` 报 `error obtaining VCS status` |
| 初始证据 | [首次失败日志](evidence/week01-clean-reproduction-attempt1.log)，没有删除失败记录 |
| 缩小范围 | 同一代码在原仓库可构建，在 `/tmp` 快照失败，问题集中在构建的版本信息检测 |
| 环境证据 | 共享 `/tmp/.git` 标记存在，但该目录的 Git 仓库识别失败 |
| 处理 | 保持他人文件不动，将快照移到祖先没有 Git 标记的 `/var/tmp` |
| 回归 | 相同源码在新快照通过全部数据库、构建和进程验证 |
| 归属 | AI 在本次工程验证中排查；不能写成开发者独立解决的 Bug |

本周还处理了 Docker 守护进程镜像下载 EOF、Goose 默认 `.env` 自动加载与秘密配置隔离、数据库启动门槛，以及超时验证缺口。对应事实见 Day 4 / 5 记录和审查报告，不将工具假设写成未验证的产品优化。

### 11.3 已完成与待后续的验收表

| 项目 | 状态 |
| --- | --- |
| 需求故事、架构、六表关系与 ADR | 文档已生成，个人复述待复习 |
| API、配置、连接池、组织表 migration | 工程验证通过 |
| 测试、race、vet、build、错误实验 | 本地通过，有日志 |
| README 从干净项目目录复现与重启保留数据 | AI 实际通过 |
| 只读审查与发现处置 | 已完成，有记录 |
| 基础 CI | 文件及本地等效检查完成，远程未运行 |
| 8 个知识点 | 参考说明已整理，未代替个人闭卷回答 |
| 真实环境配置 | 未读取或修改；按路径确认不被 Git 跟踪 |
| Git 历史与本周提交 | 现有提交 `28bee06`；新成果未自动提交，整理方法见下节 |
| 下周交接 | 已整理，尚未实现 Week 2 功能 |

## 12. Git 整理与当前基线

当前代码是“第一周工程工作区基线”，不是已发布版本或新 Git 标签。已有提交为 `28bee06 第一次完整提交`，后续新增和修改仍在工作区；[源码校验清单](evidence/week01-source-baseline.json) 用于标识本次交付的运行源码与配置，不冒充提交记录。

仓库约定要求明确授权后才提交；本次没有自动 commit、push、merge 或部署。开发者阅读后，可在项目根目录检查并保存一次真实的 W1 成果：

```bash
git status --short
git diff --check
git diff
git add AGENTS.md .gitignore .env.example README.md go.mod go.sum cmd/ internal/ deploy/ migrations/ scripts/ .github/ docs/
git diff --cached --check
git diff --cached --stat
git diff --cached
git commit -m "chore: complete week 1 API database and verification baseline"
```

清单包含工程文件、文档、CI 和测试证据，保留真实 `.env` 的忽略规则。新加入的第二周手册不在上述暂存清单里，本次也未修改它，由开发者自行决定何时纳入版本管理。

## 13. 八个知识点参考说明

以下为 AI 整理的阅读材料，不是个人测试成绩或已掌握记录。

2026-11-10 补充：[第一周手册问题与参考回答](week01-answers.md) 按 Day 1～Day 7 展开说明，覆盖以下八题及架构、SQL 字段、配置、排障等问题；个人作答与掌握情况仍待开发者记录。

1. **Go Module、go.mod 与 go.sum。** Module 定义项目导入路径和依赖边界；`go.mod` 记录模块名、Go 要求和依赖版本，`go.sum` 记录下载模块内容的校验值。它们应纳入版本管理，不能靠删除校验文件解决未知依赖问题。
2. **启动入口。** `cmd/api` 组合配置、数据库和 HTTP 生命周期。工单状态、权限和事务规则在未来领域 Service 中实现，避免入口成为难以测试的业务集合。
3. **AGENTS.md。** 记录本仓库的任务边界、协作方式和检查要求。它是协作说明，不是操作系统权限隔离或技术上的秘密保护工具，仍需配合实际访问控制和人工审查。
4. **示例配置与本地配置。** `.env.example` 提供可提交的变量名和占位值；`.env` 保存自己的本地值且被忽略。Go 读取的是进程环境，文件需要在本地由开发者先加载。
5. **数据库迁移。** migration 将结构变化按版本记录，便于复现和协作，Goose 通过版本表追踪执行。Up 应用结构，Down 可能删除数据，只在可丢弃测试环境验证；已共享执行的迁移通过新增文件演进。
6. **Gin 健康检查。** Router 注册 GET 路径，匹配后 Handler 返回 JSON 与状态码。内存路由测试覆盖处理结果；真实 HTTP 请求另外覆盖监听和网络，`/healthz` 本身不代表所有依赖持续正常。
7. **test、vet、build。** `go test` 验证测试描述的行为；`go vet` 提示一类可疑代码；`go build` 验证编译链接。race 检查只分析执行路径中的竞态，它们都不能单独证明全部业务正确。
8. **宿主机与容器连接。** Go 通过本机映射端口连接容器内 PostgreSQL，连接参数来自环境。pgxpool 管理多条连接，启动 Ping 验证可用性，Context 限制等待，退出时关闭池；Compose 服务名 `db` 在当前宿主机运行方式下不能直接当作宿主机地址。

## 14. Week 2 交接说明

### 已有稳定基础

沿用当前仓库、Module、配置读取、HTTP 入口、连接池、Compose 和 `00001_create_organizations.sql`。第一周原始迁移保持不变，新表通过增量 migration 添加。当前只有 `/healthz`，没有用户注册、登录、JWT 或组织业务接口。

### 按影响排列的后续事项

1. **设计衔接。** 新第二周手册使用小写角色名，现有需求与表设计使用大写 `MEMBER/SUPPORT/ADMIN`；第二周手册允许有效成员查看成员列表，现有需求权限表只允许管理员；第二周增加组织创建和添加已注册成员，第一周需求原先以演示数据起步。实现前统一这些契约并同步需求、权限矩阵和测试，不能在不一致的文档上直接写授权代码。
2. **未提交基线。** 本周成果仍在工作区；保存经过核对的 W1 提交后，再开始第二周改动，便于回归和回退。
3. **远程 CI。** 需未来明确授权推送后触发，当前只证明本地等效检查通过。
4. **环境边界。** 当前复现依赖已安装工具和镜像；换机器时仍需按 README 安装，镜像拉取网络问题按实际环境处理。
5. **个人理解。** 当前生成、实现和验证大部分由 AI 受托完成，开发者的阅读、复述和独立修改仍待后续进行。

### 下周实施目标

- 新增 `00002_create_users.sql` 和 `00003_create_organization_members.sql`，保留已有组织表。
- 增加密码安全散列、注册、登录、JWT 签发与校验、`GET /me`。
- 按统一后的契约实现组织创建、本人组织列表、成员列表和管理员添加已注册成员；创建组织与首名管理员使用同一事务。
- 成员联合主键、角色与组织边界由数据库约束和 Service 授权共同验证。
- 覆盖无凭据、过期或错误签名 Token、重复注册、组织越权、重复成员及事务回滚等异常场景，保留 W1 回归。
- JWT 变量只在示例模板增加占位值，不写真实密钥；未认证用户无法访问受保护接口。

新手册见 [第二周详细开发手册](../../项目开发文件/每周开发手册/05_NexusDesk_第二周详细开发手册.md)。此处完成交接，不代表已经开始实现第二周业务。

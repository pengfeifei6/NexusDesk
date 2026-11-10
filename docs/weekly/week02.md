# NexusDesk 第二周工作记录

- 周次：W02；起始记录日期：2026-11-10（Asia/Shanghai）。
- 当前阶段：Day 7工程收尾已完成；整周审查、回归、八步演示、参考回答与W3交接已归档。个人独立复述/演示、Git保存与远程CI另行记录。
- 编写方式：AI 根据开发者明确委托完成 Day 1～Day 7工程任务；实现与验证分别记录，个人掌握不代填。
- 依据：[第二周手册](../../项目开发文件/每周开发手册/05_NexusDesk_第二周详细开发手册.md)、[执行计划](../superpowers/plans/2026-11-10-week02-day1.md)。

## 1. 本周范围与状态

沿用第一周 Go API、连接池、PostgreSQL、Goose、组织表和测试。当前 GET /healthz、POST /api/v1/auth/register 和 POST /api/v1/auth/login 可用，用户表和成员表已建立，TokenManager 已能签发/验证短期凭据。Day 5增加受保护 /me、POST/GET /organizations；Day 6完成成员GET/POST与RBAC，本周七个业务接口均已实现。第一周历史证据见 [week01.md](week01.md)，本周记录保留阶段区别。

| 日期 / 阶段 | 本阶段交付 | 实际状态 |
| --- | --- | --- |
| 2026-11-10 / Day 1 | 认证方案、七 API 契约、权限矩阵、ADR、设计衔接 | 文档交付完成，验证与审查见第 5 节 |
| 2026-11-10 / Day 2 | users / organization_members 增量迁移与 Repository 契约 | 已完成专用数据库验证，见第 10 节 |
| 2026-11-10 / Day 3 | 注册、Argon2id、用户存储、严格输入、单元与真实集成测试 | 已完成，见第 12 节 |
| 2026-11-10 / Day 4 | 登录、JWT、配置、失败场景与真实回归 | 已完成，见第 14 节 |
| 2026-11-10 / Day 5 | 认证中间件、/me、组织创建事务与本人组织列表 | 已完成，见第 16 节 |
| 2026-11-10 / Day 6 | 成员权限、专用数据库集成与越权测试 | 已完成，见第18节 |
| 2026-11-10 / Day 7 | 回归、安全审查、Alice/Bob/过期演示、复习回答与交接 | 工程完成，见第20/21节；个人学习/Git保存不代填 |

## 2. Day 1 产物与验收

| 手册任务 | 本次产物 | 说明 |
| --- | --- | --- |
| 阅读已有需求、架构与数据库 | 已核对，差异记录于下节 | 原始手册不修改，当前设计同步 |
| 注册与登录时序、认证/授权边界 | [auth-design.md](../auth-design.md) | 含两个 Mermaid 时序图和密码/JWT/成员检查边界 |
| 七 API 的输入、输出、状态与错误 | [auth-organization.md](../api/auth-organization.md) | 明确字段、公开 DTO、校验顺序、分页和失败语义 |
| 权限与跨组织响应策略 | [authorization-matrix.md](../authorization-matrix.md) | 匿名、非成员、disabled、member、admin 分别定义 |
| Argon2id、JWT、成员表的取舍 | [ADR-0002](../decisions/0002-auth-security.md) | 有备选、代价、本地边界与后续风险 |
| Day 1 五个理解问题 | [认证设计第 8 节](../auth-design.md#8-day-1-五个问题的参考回答) | AI 参考回答，开发者独立复述待进行 |
| W02 协作约束 | [AGENTS.md](../../AGENTS.md) | 增量更新，保留真实配置与危险操作边界 |

七 API 范围固定为注册、登录、/me、创建组织、本人组织列表、成员列表、添加已注册成员。Day 1 当时只交付设计，没有创建接口 Go 文件、空模块或请求演示；后续实际代码分别记录在对应实施日。

## 3. 与第一周设计的差异处理

| 原规划 | 本次统一结果 | 更新位置 |
| --- | --- | --- |
| 大写三角色与 ACTIVE/INACTIVE | W02 小写 admin/member、active/disabled；support 留待后续 | 需求、架构、数据库、ADR、AGENTS |
| 成员列表仅管理员 | active member/admin 均可读；字段不含邮箱和认证信息 | 需求、权限矩阵、API |
| 第一批组织靠演示数据 | 注册只建账号，登录后主动创建组织与首名 admin | 需求、数据库、认证与 API |
| W02 修改角色 | 本周只添加成员；角色变更、最后管理员与工单职责规则后续实施 | 需求阶段表、架构、数据库 |
| users 无 display_name / 邮箱唯一实现不具体 | display_name 必填，lower(email) 唯一索引 | 数据库与 API |
| W1 成员表预留 updated_at | W02 按新手册只保留 created_at；后续有修改行为再增量添加 | 数据库 |

未来增加 support 或更新字段要新增迁移，不能重写已执行的成员迁移。第一周的工单 OPEN 等状态名保持原规划；第一周周报、参考答案与原始资料作为历史材料保留。

手册提到的 `AGENTS.week02.append.md` 在仓库中未找到。本次依据手册直接补齐 W02 条款，没有伪称使用过该模板，也没有因此中断授权任务。

## 4. Day 1 受托补齐的具体选择

- Argon2id 初始 19 MiB、2 次、并行度 1、随机盐 16 字节、派生值 32 字节；并发计算门限为 2，超额返回 503；Day 3 已实测成本，见第 12 节。
- 密码 15～128 个 Unicode 码点，最多 512 UTF-8 字节，原文保留；JSON 请求体上限 16 KiB。
- JWT 固定 HS256、15 分钟；必需 sub/iss/aud/iat/exp，不携带角色，验证身份后查当前成员关系。
- 列表 page/page_size 默认 1/20、每页最多 100，先授权过滤再分页，不提供 total。
- 401 表示身份凭据不被接受；403 表示 active 成员角色不足；404 隐藏不存在组织、非成员和 disabled；409 表示邮箱或成员重复。
- 添加已注册成员的目标 user_id 是合法业务参数，操作者 ID 只能来自可信上下文；已有 disabled 关系不能通过添加隐式恢复。
- 采用手册允许的回环地址方案；可靠登录频率限制是公开部署前待办。Day 1 尚未改变启动代码；Day 3 已随注册接入显式回环 IP 约束。

这些是 AI 在 Day 1 委托范围内作出的设计选择。Day 1 当时未运行密码基准、签发 Token、创建新表或执行迁移；后续 Day 2 的专用测试库结果单列在第 10 节，真实开发数据库仍未访问。

## 5. Day 1 验证与审查记录

- 开始前对照第一周源码清单，17 个文件散列全部一致；随后只修改 Day 1 所需文档与 AGENTS。
- 本地链接与锚点、Markdown 围栏与表格、JSON 示例解析、七端点范围、两个时序图结构、角色与状态术语检查通过；原始统计归档在 [Day 1 文档检查日志](evidence/week02-day1-doc-check.log)。时序图核对源文本结构，未调用外部渲染器。
- 与第一周清单比对，除本次有意更新的 AGENTS 外，其余 16 个运行源码、配置、依赖与迁移文件散列保持一致；git diff --check 通过。未跟踪的新文档另行检查，没有用空 Diff 代替检查。
- 独立只读文档审查发现一项 ADR 中 Token 输出范围与登录响应的矛盾，已修正为仅登录成功响应返回 Token、禁止进入日志/快照，见 [审查记录](../reviews/week02-day1-design-review.md)。这不是 W02 代码安全 Review，不声称运行了登录用例。
- 本次没有运行 Go 测试或数据库烟测；历史第一周通过记录不冒充本周功能验证。
- 没有读取或修改真实 `.env`，没有生成密钥，没有改 Go/依赖/Compose/迁移，没有提交或推送。

检查过程说明：首轮脚本把 git check-ignore 命中 `!` 允许规则的退出码误判为日志被忽略。核对具体规则和可跟踪文件列表后，修正脚本判断并重新检查；原有 .gitignore 无需修改。这是检查脚本的判断问题，不是产品运行 Bug。

## 6. A01～A18 执行进度

以下是完整业务验收进度。Day 6已对A01～A18建立可重复自动化证据，成员接口与当前约定RBAC均通过；Day 7已完成整周工程审查/演示，仍不代替个人复述或公开部署条件。

| 编号 | 将验证的行为 | 当前状态 |
| --- | --- | --- |
| A01 | 正常注册 201，响应无散列 | Day 3 单元、专用库集成与真实进程通过 |
| A02 | 大小写邮箱与并发重复被唯一索引拒绝 | Day 3 数据库及注册 API 409 / 并发通过 |
| A03 | 非法密码 400，不创建账号 | Day 3 边界单测、HTTP 与专用库无写入验证通过 |
| A04 | 同密码随机盐不同且均可验证 | Day 3 密码单测与真实存储验证通过 |
| A05 | 正确凭据登录获得有效 Token | Day 4 单元、专用库及真实进程登录通过；Python 独立验签与 Claim 核对通过 |
| A06 | 未知邮箱与错密码通用失败，包含 dummy 路径 | Day 4 单元与真实 HTTP 统一 401，通过；dummy 匹配也不可登录 |
| A07 | 缺失、篡改和过期 Token 拒绝 | Day 5 Token/中间件单元与真实HTTP缺失/篡改401通过 |
| A08 | 错误算法、issuer、audience 或缺必需 Claim 拒绝 | Day 6补齐受保护HTTP：HS384/none、错误key/issuer/audience、缺sub/iat/exp与过期均401；Token层全量边界继续通过 |
| A09 | 未认证 /me 为 401 | Day 5 单元、真实库与真实进程401/challenge通过 |
| A10 | 组织创建与 admin 成员共同提交 | Day 5 真实库及TCP201、数据库admin/active验证通过 |
| A11 | 第二步失败导致创建组织整体回滚 | Day 5 成员写入、延迟COMMIT、取消与缺创建者均真实回滚，无孤立组织 |
| A12 | 跨组织读取和添加统一 404 | Day 6真实库与TCP验证他人组织读取/添加统一404且无写入 |
| A13 | active member 添加为 403 | Day 6真实库/TCP/HTTP单元403；先授权再解析输入/查询目标 |
| A14 | active admin 添加已注册账号为 201 | Day 6真实库与TCP201，member/admin均可添加，五字段DTO和存储关系核对通过 |
| A15 | 已有或并发重复关系为 409 | Day 6重复/本人/disabled均409且不更改状态角色；两admin并发一201一409、一条关系 |
| A16 | disabled 不可访问组织，旧 Token 不能绕过 | Day 6旧Token在DB禁用后读取/添加该组织均404，其他有效组织仍200；列表继续过滤disabled |
| A17 | 伪造操作者/owner/status 字段无效 | Day 6成员接口拒绝org_id/actor_id/owner_id/status等字段；目标user_id不改变可信操作者，不能跨组织代写 |
| A18 | SQL/连接失败返回安全错误，不泄露秘密 | Day 6用户/组织/成员的关闭池、SQL/提交失败安全500，错误与日志无密码/散列/DSN/Token；取消无半成品 |

补充测试由相应实施日承担：请求体和字段边界、JSON 未知字段、分页过滤、散列计算上限、异常散列参数、回环监听限制。测试库使用隔离的专用数据库或 Compose Project，不指向日常数据。

## 7. 个人学习与 AI 协作记录

Day 1～Day 7工程任务的设计、实现与验证由 AI 按授权完成，开发者本人独立作答、代码实现或运行结果另行记录。个人复述可从认证五问、权限矩阵、密码封装、JWT 三段结构与 API 错误字典开始。

个人独立解决的问题与 Bug：待实际发生后记录。本次设计差异处理不冒充运行 Bug 或个人排障经历。

## 8. Day 1 结束时的 Day 2 交接（现已执行）

1. 沿用当前仓库和第一周迁移，新增 `00002_create_users.sql` 与 `00003_create_organization_members.sql`，字段按当前数据库文档。
2. users 包含 id/email/display_name/password_hash/created_at/updated_at；建立 users_email_ci_unique；成员表包含联合主键、外键、role/status CHECK 与按 user_id 查组织的索引。
3. 定义 CreateUser、FindUserByEmail、FindUserByID、CreateOrganizationWithAdmin、ListOrganizationsByUser、FindActiveMembership、ListMembers、AddMember 的输入输出、事务归属和错误分类。
4. 在专用测试库运行迁移并检查字段、索引、重复邮箱、重复成员、非法角色/状态和外键；不要改 00001，也不在真实开发库试 Down。
5. 后续实施逐项保留第一周回归；Day 2 不等于注册、登录与 JWT 已完成。

以上五项中的 Day 2 迁移、约束验证和方法设计已执行，产物和真实结果见第 10 节；Repository 方法只设计，实际实现留到后续每日任务。

## 9. 仍待后续解决的事项

当前已有第一周提交 923ae93（开发者保存的既有提交），第二周新增与修改仍在工作区，本次不自动提交。远程 CI 尚未触发。密码参数本机成本在 Day 3 已记录；可靠登录限流、邮箱所有权、常见密码检查、Token 撤销和生产 HTTPS 仍未完成。

## 10. Day 2 迁移、Repository 设计与真实验证

### 10.1 产物

| 产物 | 本次完成内容 |
| --- | --- |
| [00002_create_users.sql](../../migrations/00002_create_users.sql) | 账号字段、Identity / 时间默认值、基础非空白 CHECK、lower(email) 唯一索引 |
| [00003_create_organization_members.sql](../../migrations/00003_create_organization_members.sql) | 联合主键、组织/用户外键、角色/状态 CHECK、active 默认值与 user_id/org_id 索引 |
| [Repository 契约](../repository-contracts.md) | 八个方法的类型与签名、查询范围、错误映射、授权前提和事务归属，以及 BEGIN/COMMIT/ROLLBACK 说明 |
| [schema_test.go](../../tests/integration/schema_test.go) | 显式启用的真实数据库测试，默认跳过，检查专用测试库范围 |
| [week02-day2-smoke.py](../../scripts/week02-day2-smoke.py) | 独立 Compose Project、迁移升级/逆序回滚/重升与真实数据库全量 race |

第一周 00001 未变，Go 业务入口、配置、路由、连接池和 go.mod/go.sum 未变。原 day5-smoke 固定 up-to 1，并隔离继承的测试开关，使历史回归不误用最新迁移；CI 格式范围增加 tests 目录。没有创建 auth/organization 空模块、注册或 JWT 实现。

### 10.2 先失败、再通过与迁移实验

1. 先写 schema 测试及隔离运行器，只应用 W1 版本 1。实际返回 1，失败明确为缺少 users / organization_members；[失败证据](evidence/week02-day2-red.log) 保留，测试资源已清理。
2. 补齐 00002/00003 后再次运行，在独立库从 1 升到 3，Status 显示三条 Applied；重复 up-to 3 无重复执行，原组织记录仍在。
3. 只在该可丢弃库逆序 Down 3 → 2 → 1，再 Up 到 3，确认组织表和原组织记录保留，随后运行实际数据库测试。见 [PostgreSQL 记录](evidence/week02-day2-postgres.log)。

已验证真实字段类型、非空/长度、Identity 和时间默认值、active 默认、邮箱大小写唯一、成员联合唯一、多组织不同角色、非法 role/status、缺失组织/用户外键、禁止删除被引用对象，以及并发邮箱/成员各恰好一个成功、一个唯一冲突。测试中的散列为 schema 专用占位内容，不能把它写成已经运行 Argon2id。

### 10.3 回归、审查与限制

- 新烟测全量 `go test -race -count=1 -v ./...` 通过，包含 W1 连接集成与 W02 schema 集成；普通 Go 检查不启用这两个数据库入口，明确 Skip。
- [测试统计](evidence/week02-day2-test-summary.json)：隔离数据库环境 5 包、顶层 16 通过、0 失败、0 跳过；含子测试共 51 个通过节点。普通检查顶层 14 通过、2 跳过，含子测试 33 通过节点；不重复计数，也不将 Skip 计为通过。
- 原 W1 烟测通过健康响应、SIGINT/SIGTERM 退出、保留卷重启和错误密码/缺库/不可达失败。见 [W1 回归](evidence/week02-day2-week01-regression.log)。
- race、vet、build、gofmt 与 git diff --check 结果见 [质量日志](evidence/week02-day2-quality.log)；后续审查处置与最终文档检查随本次收尾归档。
- 独立只读审查未发现需要修复的实质问题，见 [Day 2 审查记录](../reviews/week02-day2-review.md)；最终链接、格式、CI 静态检查与源码核对见 [收尾检查](evidence/week02-day2-delivery-check.log)。
- 为验证继承开关隔离，最终 W1 回归故意在外层设置 NEXUSDESK_TEST_W02_SCHEMA=1，脚本清除该开关后仍通过；没有在版本 1 上误跑 W02 schema 测试。
- 真实 `.env` 未读取或修改，未使用已有开发库或卷；两轮专用库和回归库均只清理自己创建的资源。没有 stage、commit 或 push。
- A02 / A15 只完成数据库部分；尚未实现 Repository、注册、JWT、HTTP 409、组织业务事务或授权。A10/A11 仍待真正方法实现后注入失败验证，不能用迁移事务代替。

个人独立实现与复述仍待开发者记录；Day 2 设计、代码和验证由 AI 按本次委托完成。

本次 [源码与配置清单](evidence/week02-day2-source-baseline.json) 标识未提交工作区内容，保留第一周清单作历史对照，不伪装成 Git 提交或发布版本。

## 11. Day 2 结束时的 Day 3 交接（现已执行）

按认证设计实现用户模型、用户 Repository、注册 Service/Handler，以及 Argon2id 封装和密码测试。重点验证规范化邮箱与唯一冲突映射、随机盐、密码边界、无散列泄露、数据库失败和散列并发门限；记录实际密码参数成本。不要提前把登录/JWT 或组织接口视为已完成。

## 12. Day 3 注册、密码与用户存储

### 12.1 本次实现

| 文件 / 范围 | 已实现行为 |
| --- | --- |
| [auth/model.go](../../internal/auth/model.go) / errors.go | 用户、凭据、注册参数与静态安全错误；密码和散列字段禁止 JSON 编码 |
| [auth/password.go](../../internal/auth/password.go) | 固定 Argon2id 参数、安全随机盐、原文保留、长度/UTF-8、两计算名额、取消与异常编码/参数拒绝 |
| [auth/service.go](../../internal/auth/service.go) | 邮箱规范化、显示名与密码策略、散列后写用户；取消后不继续写库 |
| [auth/handler.go](../../internal/auth/handler.go) / json_input.go | 16 KiB、严格字段/单对象/UTF 编码、统一错误与公开 DTO、no-store |
| [auth/repository.go](../../internal/auth/repository.go) | CreateUser、FindUserByEmail、FindUserByID；参数化 SQL、3 秒上限、指定唯一索引与安全错误 |
| Router / main / config | 回调注册 /api/v1/auth/register；共享一个 PasswordHasher；显式回环监听与 HTTP 读写超时 |

只新增一个注册接口，不自动建组织、成员或签发 Token。登录/JWT 与组织五个 Repository 方法仍未实现，已有三条迁移、Compose 和 .env.example 均保持不变；真实 .env 未读取或修改。

依赖只新增 alexedwards/argon2id v1.0.0，保留 Go 1.26.1、Gin/pgx 与现有 x/crypto v0.48.0；go mod tidy 补齐校验记录，没有升级既有模块。

### 12.2 测试与真实运行证据

| 验证 | 结果与证据 |
| --- | --- |
| 测试先行 | 密码、注册/路由/回环、Repository 分别确认缺失实现失败；见 [密码红灯](evidence/week02-day3-password-red.log)、[注册红灯](evidence/week02-day3-register-red.log)、[存储红灯](evidence/week02-day3-repository-red.log) |
| 最终普通 race | 6 包、顶层 32 通过、0 失败、3 数据库入口跳过；[JSON 事件](evidence/week02-day3-go-test.jsonl) |
| 专用 PostgreSQL race | 6 包、顶层 35 通过、无失败或跳过；包含 schema、注册和原连接集成；[真实数据库日志](evidence/week02-day3-postgres.log) |
| 真实 API 进程 | TCP 注册 201、大小写重复 409、伪造 role 400、无组织副作用、应用日志无测试凭据、SIGTERM 退出 0；同上 |
| W1 进程回归 | 版本 1 的健康响应、保留卷重启、SIGINT/SIGTERM、连接失败均通过；[回归日志](evidence/week02-day3-week01-regression.log) |
| 静态与构建 | go vet、go build、go mod verify、gofmt、git diff --check 通过；[质量日志](evidence/week02-day3-quality.log) |
| JSON 模糊测试 | 5s 预算、2 workers、18,137 次执行通过；[Fuzz 日志](evidence/week02-day3-json-fuzz.log) |

统计见 [Day 3 摘要](evidence/week02-day3-test-summary.json)。顶层不重复计子测试；计入子测试与 fuzz seeds 后，普通环境 129 个通过节点、3 跳过，真实数据库环境 155 个通过节点、无跳过。Skip 不是 Pass。

真实存储覆盖：注册邮箱小写与显示名去空白、数据库仅存散列、正确密码可验证、同密码不同盐、大小写/并发重复只有一条记录、查询参数化、缺失用户、在途取消，以及数据库失败不会返回成功或创建用户。非法密码与显示名 NUL 的真实链路均返回 400 且没有用户记录。

### 12.3 独立审查与修复

独立审查发现两项 P2 输入边界问题：标准 JSON 解码器会替换未配对 Unicode 转义；显示名 NUL 直到数据库才被拒绝。已先补失败复现，再分别修复解码前检查和 Service 名称校验。合法代理对、真实 U+FFFD、字面反斜杠及密码 NUL 的保留另有通过用例。

[审查红灯](evidence/week02-day3-review-red.log) 与 [修复单测](evidence/week02-day3-review-green.log) 保留；修正后的最终全量 race 与真实库通过。完整发现、处置与审查边界见 [Day 3 审查记录](../reviews/week02-day3-review.md)，不假装审查代理重新运行了验证。

过程中另有两次验证问题已保留：首次测试启动早于依赖安装结束；一个空白保留夹具去空白后只有 14 字符，错误触及最小长度策略。已等待依赖完成、修正夹具而不放松策略；见 [依赖时序记录](evidence/week02-day3-dependency-attempt.log)、[夹具记录](evidence/week02-day3-whitespace-fixture-attempt.log)。这些由 AI 处理，不冒充开发者独立排障。

### 12.4 参数成本与学习参考

本机小样本 Hash/Verify 中位约 32.89/30.61 ms，分配约 19 MiB/op；双工作协程摊销约 20.17 ms/op，整个基准进程峰值 RSS 约 91 MiB。每项只有 3 个样本、每样本 5 次操作，不能据此推导生产容量或 P95，详见 [基准摘要](evidence/week02-day3-password-benchmark.json) 与 [ADR](../decisions/0002-auth-security.md#7-day-3-实现与本机成本记录)。

理解时区分三件事：随机盐让同一密码产生不同存储值，盐可与散列一起保存；慢散列提高每次离线猜测成本，不负责“解密密码”；并发名额控制计算资源，不是限制某个账号的猜测次数。开发者的独立复述仍待后续记录。

所有运行使用本次随机测试项目、测试密码与专用库，只清理自己创建的容器和卷。没有 Git 暂存、提交、推送或部署。最终 [源码清单](evidence/week02-day3-source-baseline.json) 标识当前未提交工作区，[交付检查](evidence/week02-day3-delivery-check.log) 记录文档、源码与资源核对，保留历史清单不冒充新提交。

## 13. Day 3 结束时的 Day 4 交接（现已执行）

新增登录、JWT 签发和验证、四个 JWT 配置占位项及失败测试；复用现有用户查询和同一个 PasswordHasher。实现未知邮箱的同成本 dummy 验证与通用登录错误，固定 HS256 / issuer / audience / exp / iat / sub 检查。此交接在 Day 3 结束时尚未实施，Day 4 的实际产物与测试如下；/me 与组织授权仍未完成。

## 14. Day 4 登录、JWT 与失败场景

### 14.1 手册任务与实际产物

| 手册任务 | 本次完成内容 |
| --- | --- |
| 规范化邮箱→查用户→验密码→签发 | [LoginService](../../internal/auth/service.go) 与 [LoginHandler](../../internal/auth/handler.go)，POST /api/v1/auth/login 已在启动入口接通；复用严格 JSON 与安全错误 |
| 未知邮箱与错密码统一失败 | 启动时生成一次同参数 dummy hash；查询不到也验证密码，固定 401 INVALID_CREDENTIALS；必须确认用户存在才能签发 |
| JWT 签发与验证接口 | [token.go](../../internal/auth/token.go)：固定 HS256、15 分钟、必需 sub/iss/aud/iat/exp；错签名/算法、篡改、空值、过期和畸形输入拒绝 |
| 四个配置变量 | [config.go](../../internal/platform/config/config.go) 读取并验证 issuer/audience、固定 TTL=15、标准 Base64 规范编码与解码密钥≥32字节；[示例](../../.env.example) 仅占位 |
| Header/Payload/Signature 学习任务 | [JWT 说明](../jwt-guide.md) 提供三段结构、虚构 Payload、字段含义、解码与签名可信的区别；这是 AI 参考材料，个人作答待记录 |
| 正确/错误/未知/空密码四组 | 单元、真实 PostgreSQL 与 API 进程验证，实际状态分别为 200 / 401 / 401 / 400；[请求示例](../../requests/week02.http) 可供本地复现 |
| 日志不打印 Token | 真实进程的响应 Token 仅在内存中验签/比较；检查应用日志不含测试密码、散列、密钥或 Token，归档不保存原文 |

新增 golang-jwt/jwt/v5 v5.3.1，保持既有 Go/Gin/pgx/Argon2id 版本。没有新增迁移，三条 SQL、Compose、用户存储和密码实现保持 Day 3 基线。注册/登录共用同一个 PasswordHasher 的两个计算名额，dummy 不在每次请求重新 Hash。输入仍限 16 KiB，取消与数据库/散列/签发故障不会伪装成成功。

### 14.2 测试情况与可复现命令

| 检查 | 实际结果 / 证据 |
| --- | --- |
| 测试先行 | 缺失 Token/配置与登录能力时分别失败：[Token 红灯](evidence/week02-day4-token-config-red.log)、[登录红灯](evidence/week02-day4-login-red.log)；实现后 [单元通过](evidence/week02-day4-unit-green.log) |
| 最终普通 race | 6 包，顶层 48 通过、0 失败、4 个真实数据库入口跳过；[JSON 事件](evidence/week02-day4-go-test.jsonl) |
| 专用 PostgreSQL race | 6 包，顶层 52 通过、0 失败、0 跳过；schema、注册、登录与 W1 连接全部实际运行；[真实库日志](evidence/week02-day4-postgres.log) |
| 实际 TCP 登录 | 200 返回 Bearer/900；Python HMAC 独立验签并核对五个 Claim、用户 ID 与时钟；错密码和不存在邮箱完全同一 401 JSON，空密码 400；同上 |
| 失败/副作用 | 损坏存储散列、关闭数据库池返回安全 500；表锁下取消查询有界退出；登录前后 users/organizations/members 数量不变；同上 |
| W1 回归 | 固定版本 1 的健康响应、保留卷重启、SIGINT/SIGTERM 与三类连接失败通过；[回归日志](evidence/week02-day4-week01-regression.log) |
| 模糊测试 | FuzzLoginJSON，5s 预算、2 workers、18,274 次执行通过；[Fuzz 日志](evidence/week02-day4-login-fuzz.log) |
| 静态/构建 | go vet、go build、go mod verify、gofmt、git diff --check 全部通过；[质量日志](evidence/week02-day4-quality.log) |

[测试摘要](evidence/week02-day4-test-summary.json) 区分顶层与子测试：普通环境含子测试/fuzz seeds 为 221 个通过节点、4 跳过；专用库为 255 个通过节点、无跳过。没有把 Skip 算作 Pass，也没有用旧日报冒充本次结果。

在项目根目录运行（不加载真实 .env）：

```bash
go test -race -count=1 ./...
python3 scripts/week02-day2-smoke.py --login
python3 scripts/day5-smoke.py
```

--login 隐含 --registration，只在随机项目与专用测试库执行，使用临时测试密钥；W1 脚本仍固定 up-to 1。最终两套脚本均退出 0，各自清理自己的容器和标记卷，不操作日常数据。

### 14.3 独立审查与边界修复

发现两项 P2 并先复现失败：库默认 NumericDate 截掉小数，可能放宽未来 iat/nbf 或 900 秒上限；合法长 issuer/audience 转义后可能超过原 4096 字节输入预算，导致自己签发的 Token 无法验证。

修复为时间字段严格要求 int64 十进制整数秒，拒绝小数/指数/字符串/null/溢出，不修改全局精度；Token 预算增加到 8 KiB 并在签发端检查长度，覆盖两个 256 字节标签的最坏转义。增加最坏标签与最大用户 ID 自签发/自验证、超长正确签名输入拒绝的回归。

[红灯](evidence/week02-day4-review-red.log)、[修复单测](evidence/week02-day4-review-green.log) 与最终全量回归均保留。[审查记录](../reviews/week02-day4-review.md) 明确：独立代理只审查修复前代码，修复与验证由主代理执行；不是完整 W02 安全审计。

### 14.4 本地配置与交付状态

真实 .env 未读取、修改或生成密钥。已有本地配置需要开发者按 [README](../../README.md#准备本地配置) 手动补齐四个 JWT 变量后启动，不能仅凭旧八变量启动本版 API。`.env.example` 保留占位，测试夹具的虚构密钥不用于日常配置。

[实施计划](../superpowers/plans/2026-11-10-week02-day4.md)、[源码清单](evidence/week02-day4-source-baseline.json) 与 [交付检查](evidence/week02-day4-delivery-check.log) 保存本次未提交工作区证据；不覆盖 Day 2/3 清单，不冒充新 Git 提交或发布。没有 stage、commit、push 或部署；个人学习记录仍由开发者补充。

## 15. Day 4 结束时的 Day 5 交接（现已执行）

1. 新增 middleware.go，严格解析唯一 Bearer 凭据，调用 TokenManager.Verify，再查询用户仍存在；缺失、畸形或无效 Token 为 401，数据库故障为 500。可信上下文只存用户 ID，健康检查、注册和登录保持公开。
2. 实现 GET /api/v1/me，返回当前用户公开 DTO，不接受前端 user_id 指定操作者，补齐 A07～A09 的受保护 HTTP 验证。
3. 实现创建组织与首条 admin/active 成员关系的原子事务、本人 active 组织列表；验证第二步失败整体回滚、未知身份字段拒绝、先过滤后分页。
4. 继续保留注册/登录与 W1 回归，不提前把成员添加、跨组织和 disabled 授权写成通过；这些在 Day 6 完成。

## 16. Day 5 认证中间件、组织事务与本人列表

### 16.1 本次完成的代码与行为

| 范围 | 本次交付 |
| --- | --- |
| [auth/middleware.go](../../internal/auth/middleware.go) | RequireUser严格单Bearer，验签后查询用户存在，仅存可信int64 ID；无效身份401带challenge、存储失败500 |
| [auth/me.go](../../internal/auth/me.go) | GET /api/v1/me，只返回id/email/display_name；拒绝查询参数和请求体，不接收客户端身份 |
| [organization/service.go](../../internal/organization/service.go) | 可信操作者、组织名去空白/1..100码点/UTF-8/无NUL、分页范围与乘法溢出保护 |
| [organization/handler.go](../../internal/organization/handler.go) | POST仅name，复用既有16KiB严格字符串JSON；GET只接page/page_size，无body；安全错误与明确DTO |
| [organization/repository.go](../../internal/organization/repository.go) | CreateOrganizationWithAdmin同pgx.Tx写组织和admin/active，COMMIT后返回；ListOrganizationsByUser只查本人active关系，先过滤再分页 |
| [启动组装](../../cmd/api/main.go) | 新三接口放入RequireUser保护子Group，健康、注册和登录保持公开 |
| [隔离烟测](../../scripts/week02-day2-smoke.py) | --organizations包含--login/--registration，新增真实库开关及真实TCP三个接口验收 |

沿用原十二变量配置、Go和全部依赖版本、Compose及三条迁移；不改历史SQL，不新增数据库触发器作为产品功能。测试中的失败触发器只安装在本次专用库并移除。真实 .env 未读取或修改，日常数据库和卷未访问；没有Git暂存、提交、推送或部署。

### 16.2 手册验收与实际响应

| 验收点 | 实际结果 |
| --- | --- |
| 注册后没有组织 | 真实注册/登录后本人列表200，items=[]；不自动认领W1旧组织 |
| 登录后能创建组织 | 真实TCP创建201，name已规范化，my_role=admin |
| 创建者成为管理员 | 真实SQL核对同组织/当前用户关系为admin/active，两次写入已共同提交 |
| 第二步失败整体回滚 | BEFORE INSERT触发器令成员写入失败：安全500、Repository无半成品、按名称查组织0条 |
| 提交失败也不能成功 | 延迟约束触发器在COMMIT抛错：安全500、没有遗留组织 |
| 等待中取消 | 独占锁阻塞成员写入，150ms父截止时间终止；事务回滚、没有孤立组织，后续连接查询成功 |
| 无法伪造他人代建 | owner_id/admin_user_id/user_id/role/status额外字段均400，无写入；真正创建者仅取认证Context |
| 本人列表隔离/分页 | 他人和disabled关系先排除，再按组织ID分页；role变更取数据库当前值，超出结果返回[] |
| 用户消失/数据库故障 | 已删除用户的旧Token401；用户查询或组织池关闭为安全500，不吞成401/200 |
| 组织禁用不等于账号删除 | disabled组织从列表消失，原Token访问本人/me仍200；成员资源404策略留Day6 |

### 16.3 测试证据与复现

先编写认证与组织Service/HTTP测试，观察缺失实现失败，见 [认证红灯](evidence/week02-day5-auth-red.log)、[组织红灯](evidence/week02-day5-organization-red.log)。随后实现并通过 [认证测试](evidence/week02-day5-auth-green.log) 和 [普通单元回归](evidence/week02-day5-unit-green.log)。数据库验收使用真实PostgreSQL，不以mock结果替代事务证明。

| 检查 | 最终结果 / 证据 |
| --- | --- |
| 普通全量race | 7包，顶层53通过、0失败、5个数据库入口跳过；[JSON事件](evidence/week02-day5-go-test.jsonl) |
| 专用库全量race | 7包，顶层58通过、0失败、0跳过；五个数据库入口全部实际运行；[PostgreSQL日志](evidence/week02-day5-postgres.log) |
| 真实API进程 | 注册/登录回归、独立HMAC验签、/me、空列表、创建/admin、本人列表、伪造字段400和受保护401；日志无Token/测试秘密；同上 |
| 组织JSON模糊测试 | 5s预算、2workers、11,119次执行通过；[Fuzz日志](evidence/week02-day5-json-fuzz.log) |
| 第一周回归 | 固定版本1的迁移、保留卷重启、健康响应、SIGINT/SIGTERM与三类连接失败通过；[W1日志](evidence/week02-day5-week01-regression.log) |
| 静态/构建 | vet、build、mod verify、gofmt和diff检查通过；[质量日志](evidence/week02-day5-quality.log) |

[统计摘要](evidence/week02-day5-test-summary.json) 区分顶层与子测试/fuzz seeds：普通环境267通过节点、5跳过；真实库310通过节点、0跳过。Skip不算Pass；历史Day2～Day4统计保留，不能套用于本次代码。

项目根目录复现：

```bash
go test -race -count=1 ./...
python3 scripts/week02-day2-smoke.py --organizations
python3 scripts/day5-smoke.py
```

运行器使用随机独立Compose Project、测试库与临时凭据，不读取真实.env。--organizations同时启用NEXUSDESK_TEST_W02_ORGANIZATIONS=1；各脚本仅清理自己创建且核对归属的容器/卷，日常库不受影响。

### 16.4 审查、学习材料与交付状态

独立只读静态审查未发现需修复的P0/P1/P2实质问题或Day5必须补齐的测试缺口；审查方未运行测试，主代理负责上述真实执行。见 [Day5审查](../reviews/week02-day5-review.md)，不将本次结论写成完整W02安全验收。

[可信身份与事务学习说明](../organization-transaction-guide.md) 解释操作者来源、两次写入为何必须共用tx、提交失败、独立清理Context、序号不连续与提交结果不确定性，以及先过滤后分页。[请求示例](../../requests/week02.http) 已补本人资料、创建/列表、伪造字段和分页失败，只有虚构输入与Token占位。

[计划](../superpowers/plans/2026-11-10-week02-day5.md)、[49文件源码清单](evidence/week02-day5-source-baseline.json) 和 [收尾检查](evidence/week02-day5-delivery-check.log) 标识当前未提交工作区，不冒充新Git版本。实现、测试与文档均由AI按授权完成；开发者个人独立实现、复述和排障不代填。已有本地.env仍需由开发者按README手动补齐Day4的四个JWT变量。

## 17. Day 5 结束时的 Day 6 交接（现已执行）

实现GET/POST /organizations/:orgID/members：先验证当前身份和目标组织active成员关系，再检查操作角色；active member/admin可读，只有active admin可添加已注册用户。非成员、disabled与不存在组织统一404，角色不足403，重复关系409。

Repository补FindActiveMembership、ListMembers、AddMember；所有查询限定目标org_id，不能信任客户端角色，不能用UPSERT恢复disabled成员。补跨组织、重复/并发添加、旧Token撤权与安全错误验证，继续保留注册/登录、组织事务和W1回归。

## 18. Day 6 成员接口、RBAC与跨组织隔离

### 18.1 本次产物与手册任务

| 手册任务 | 实际完成 |
| --- | --- |
| 将权限表变成可执行规则 | [policy.go](../../internal/organization/policy.go) 与 [MemberService](../../internal/organization/member_service.go)：每请求查目标组织active关系；读允许member/admin，写只允许admin |
| 成员查询与添加 | [MemberHandler](../../internal/organization/member_handler.go) 接通GET/POST /api/v1/organizations/:orgID/members；可信操作者来自Context，业务输入在授权后解析 |
| 数据库方法 | [member_repository.go](../../internal/organization/member_repository.go) 实现FindActiveMembership/ListMembers/AddMember，显式组织范围、参数化SQL、指定约束错误分类 |
| 严格数字/JSON | [共用解码](../../internal/auth/handler.go) 抽取RawMessage字段入口，保留原Unicode/重复字段/16KiB约束；user_id正int64整数，role精确member/admin |
| 安全成员DTO | 仅user_id/display_name/role/status/created_at，UTC RFC3339；按user_id升序，空列表[]，分页沿用Day5 |
| 专用集成测试库 | 复用随机独立Compose Project与PostgreSQL17；--members包含组织/登录/注册/schema，新增NEXUSDESK_TEST_W02_MEMBERS=1 |
| 学习与演示材料 | [成员权限说明](../member-authorization-guide.md)、[请求示例](../../requests/week02.http) 和真实Alice/Bob进程验收；个人独立作答待开发者记录 |

保留原配置、全部依赖、Compose和三条迁移。已有Day5 Service接口保持不变，成员功能使用单独MemberService/Handler，不创建通用权限框架。添加在同tx读目标显示名、INSERT/RETURNING并COMMIT，成功后才返回；失败独立且有界Context回滚，不UPSERT、不恢复disabled关系。

### 18.2 权限与失败行为实际验证

| 场景 | 实际结果 |
| --- | --- |
| 非成员A请求B组织、组织不存在、disabled | 读/写统一404 NOT_FOUND，响应完全一致；非法输入/目标存在性不能改变该结果 |
| 同一用户跨组织不同角色 | Bob在A为member、B为admin；不能管理A，但可管理B，改URL不能取得其他组织权限 |
| active member读取/添加 | 读取200，包含本组织active/disabled关系；添加403且不探测目标，非法JSON也先403 |
| admin添加现有member/admin | 201，数据库有关系且status固定active，返回安全五字段DTO |
| 重复、本人、已有disabled关系 | 409 MEMBER_ALREADY_EXISTS；disabled保持原角色和状态，不隐式恢复 |
| 两名admin并发添加同一用户 | 一个201、另一个409，仅一条关系，依靠联合主键 |
| 目标用户不存在 | 仅已授权admin看到404 USER_NOT_FOUND；非成员/member仍分别404 NOT_FOUND/403 |
| 禁用/降级后的旧Token | 专用库置disabled后原Token读写该组织均404，其他组织仍可访问；admin降为member后原Token写403、读200 |
| 伪造身份/组织/状态、非法数字/角色 | 额外org_id/actor_id/owner_id/status拒绝；user_id字符串/小数/指数/null/溢出及support/大小写别名400，无写入 |
| 提交失败和等待中取消 | 延迟触发器令COMMIT失败安全500，无关系遗留；成员表锁等待150ms截止取消，回滚且无关系 |
| 连接/SQL故障 | 关闭成员连接池和提交异常安全500；指定主键/FK才映射业务错误，未知约束不误报409/404 |
| JWT错误组合 | 补受保护HTTP的HS384/none、错误key/issuer/audience、缺sub/iat/exp、过期，均401带challenge；不触发用户查询 |

角色/状态变化仅是隔离测试夹具的UPDATE，本次没有新增修改API。Handler先授权保护错误顺序，Service入口再次授权以保护直接调用，合法HTTP多一次成员查询。每请求重新查权限不等于并发撤权与在途写入完全线性化；未来变更接口需重新设计事务/锁定语义。

### 18.3 测试先行、真实运行与统计

先写策略/Service/HTTP、真实库测试和约束分类测试，分别观察缺失能力失败：[成员红灯](evidence/week02-day6-member-red.log)、[存储红灯](evidence/week02-day6-repository-red.log)、[约束红灯](evidence/week02-day6-constraints-red.log)。实现后 [成员/auth通过](evidence/week02-day6-member-green.log)，[普通全套通过](evidence/week02-day6-unit-green.log)。

| 检查 | 最终结果 / 证据 |
| --- | --- |
| 普通全量race | 7包，顶层58通过、0失败、6个数据库入口跳过；[JSON事件](evidence/week02-day6-go-test.jsonl) |
| 专用PostgreSQL全量race | 7包，顶层64通过、0失败、0跳过；六个数据库入口均实际运行；[真实库日志](evidence/week02-day6-postgres.log) |
| 真实TCP演示 | Alice/Bob登录、建组织、跨组织404、member读200/写403、admin添加201、重复409、禁用旧Token404及另一组织仍200；同上 |
| 成员JSON模糊测试 | 5s预算、2workers、13,074次执行通过；[Fuzz日志](evidence/week02-day6-json-fuzz.log) |
| 注册解码回归模糊测试 | 共用解析器调整后再运行，5s预算、2workers、16,067次执行通过；[注册Fuzz](evidence/week02-day6-register-fuzz.log) |
| W1历史回归 | 固定版本1，迁移、保留卷重启、健康响应、SIGINT/SIGTERM及三类连接失败通过；[回归日志](evidence/week02-day6-week01-regression.log) |
| 静态与构建 | vet/build/mod verify/gofmt/diff检查通过；[质量日志](evidence/week02-day6-quality.log) |

[统计摘要](evidence/week02-day6-test-summary.json) 区分顶层和子测试/fuzz seeds：普通环境318个通过节点、6跳过；真实库371个通过节点、无跳过。历史Day2～Day5记录保持当时统计，不把Skip算作通过。

复现方式（项目根目录，不加载真实.env）：

```bash
go test -race -count=1 ./...
python3 scripts/week02-day2-smoke.py --members
python3 scripts/day5-smoke.py
```

--members隐含--organizations、--login和--registration；只使用本次独立临时数据库/凭据。真实Token仅留测试内存，进程日志检查无Token/密码/散列/密钥，归档不保存原文。最终两套运行器都退出0，仅清理各自创建且核对归属的容器/卷。

### 18.4 审查与交付状态

[独立只读审查](../reviews/week02-day6-review.md) 未发现需修复的P0～P3实质问题或阻断Day6的测试缺口。审查方只读源码，不运行测试或操作数据库/Git；真实验证由主代理执行，不把本次审查称为完整发布安全审计。

[执行计划](../superpowers/plans/2026-11-10-week02-day6.md)、[58文件源码清单](evidence/week02-day6-source-baseline.json) 与 [交付检查](evidence/week02-day6-delivery-check.log) 标识本次未提交工作区；Day5开始前49文件散列已核对，保留历史清单。真实.env未读取或修改，没有访问日常库、修改迁移/依赖、暂存/提交/推送或部署。

Day6实现、自动化验证和文档由AI按明确授权完成，不能代填开发者独立实现、掌握程度或个人排障。A01～A18当前范围已有证据，本地运行仍须开发者维护Day4起的十二项配置；可靠公开部署条件仍未完成。

## 19. Day 6 结束时的 Day 7 交接（现已执行）

进行本周整体审查与回归，复核A01～A18及Alice/Bob完整演示、鉴权与事务说明、安全边界和遗留事项；整理个人复述、Bug/AI参与记录和下一周交接。Git保存由开发者按实际情况执行，本次不自动提交。没有新增工单、角色修改、停用或公开部署任务。

## 20. Day 7 整周审查、回归、复盘与回答文档

### 20.1 手册任务与完成边界

| 手册任务 | 本次实际完成 |
| --- | --- |
| 质量检查 | 重新执行普通全量race、vet/build/mod verify、格式与diff检查，未套用Day6旧日志 |
| 只读安全审查 | 独立代理覆盖auth/organization、迁移、路由/配置及测试运行器，未发现需修复的实质问题 |
| Alice/Bob端到端演示 | 新目录真实进程按八步执行，数据库核对角色/关系，最后过期Token访问/me及成员接口均401 |
| 整周复盘 | 归档历史四项真实Bug、AI参与、测试证据、未完成事项和W3风险；个人独立掌握不代填 |
| 本周问题参考答案 | [week02-answers.md](week02-answers.md)：按Day1～7共41道问答、十题导航和18项常见故障排查 |
| Git保存 | 当前成果保留工作区与清单，未自动stage/commit/push；手册命令是参考，开发者按实际自行保存 |

Day7工程任务完成不等于手册中“本人确认/独立复述/亲自演示”条件已由开发者完成。开发者的闭卷十题、独立操作和演示视频仍待本人记录；远程CI未触发，公开部署不在本次范围。

### 20.2 新目录复现与新测试证据

按Day6源码清单在/var/tmp建立本次独立快照，只复制58个必要文件，不复制真实.env/.git/生成缓存。使用现有主机工具、模块缓存和本地postgres:17，运行go mod download与--closeout；这是干净项目目录复现，不是全新机器安装。

| 本次检查 | 结果与证据 |
| --- | --- |
| 普通全量race | 7包，顶层58通过、0失败、6个数据库入口Skip；[JSON事件](evidence/week02-day7-go-test.jsonl) |
| 新目录真实库race | 7包，顶层64通过、0失败、0跳过；[完整复现日志](evidence/week02-day7-clean-reproduction.log) |
| 八步真实TCP演示 | Alice注册/登录→建A→Bob注册/登录→建B→Bob读A404→Alice加Bob201→Bob读200/写403→过期凭据401；同上 |
| 数据库效果核对 | A/Alice与B/Bob为admin/active；添加前A/Bob零条，添加后member/active；并非仅比较HTTP显示；同上 |
| 原W1回归 | 固定版本1，迁移/保留卷重启、健康、SIGINT/SIGTERM及错误连接失败均通过；[回归日志](evidence/week02-day7-week01-regression.log) |
| 静态/构建 | vet/build/mod verify/gofmt/diff通过；[质量日志](evidence/week02-day7-quality.log) |
| 复现内容清单 | [快照清单](evidence/week02-day7-reproduction-manifest.json)；实际复制内容的散列，快照执行完已清理 |

[Day7统计](evidence/week02-day7-test-summary.json) 分开记录顶层和含子测试/fuzz seeds的节点：普通318通过/6跳过，真实库371通过/0跳过。六个数据库入口在专用库均实际运行；没有把Skip算作Pass，也没有把历史模糊测试说成本日新跑。

--closeout包含--members与全部历史模式，只增强安全演示摘要，不新增业务接口。过期凭据用临时测试密钥在内存签名，单次采样时间确保iat/exp差900秒且已过期，不改生产时钟或归档Token。演示和环境限制见 [week02-demo.md](week02-demo.md)。

独立审查时58项中57项与Day6一致，唯一变化是运行器；之后只更新AGENTS/文档与演示时间采样夹具，后者已重新执行最终快照通过。业务Go源码、依赖、配置模板、Compose和三条迁移均未改，最终 [源码清单](evidence/week02-day7-source-baseline.json) 与 [交付检查](evidence/week02-day7-delivery-check.log) 标识未提交状态。

### 20.3 整周审查与真实问题复盘

[安全审查报告](../reviews/week02-security-review.md) 未发现需修复的P0～P3实质问题或阻断Day7的测试缺口。审查是独立静态只读检查，未运行测试/数据库/漏洞扫描；实际验证由主代理执行，不宣称生产安全认证。

| 本周已发生的真实Bug | 已验证的根因与处理 | 证据 |
| --- | --- | --- |
| 未配对Unicode转义被接受 | 标准解码器替换为U+FFFD；解码前拒绝，合法Unicode及密码NUL保留 | [Day3审查与红绿链接](../reviews/week02-day3-review.md) |
| 显示名NUL触发数据库500 | UTF-8合法不等于文本可存；Service在散列/写库前返回400 | 同上 |
| 小数JWT时间放宽边界 | 库按秒截断；RawMessage要求int64整数秒，不改全局精度 | [Day4审查与红绿链接](../reviews/week02-day4-review.md) |
| 自签发合法长标签无法验证 | JSON转义扩张超过原4KiB预算；8KiB输入预算与签发长度检查 | 同上 |

这些由AI按委托发现/修复或处置审查建议，不冒充开发者独立排障。依赖安装时序、错误的空白密码夹具与文档检查脚本判断属于验证过程问题，已留在原日记录，不编造新产品Bug。Day5/6/7独立复核没有新实质修复项，也不因此删除历史发现。

### 20.4 个人复习、AI参与与Git状态

本周设计、迁移、业务实现、测试、审查协调和资料由AI按各次明确委托完成。开发者此前提出的前端输入可信性、users/member关系问题已在参考答案Q01～Q04、Q24～Q31集中解释；不将这些讨论代填为全部技术点已经掌握。

推荐先闭卷复述手册十题，再对照 [答案导航](week02-answers.md#阅读导航)，最后亲自在隔离环境演示八步。个人独立实现清单、掌握程度和本人的Bug记录仍待真实发生后填写。

已有HEAD为923ae93（第一周保存的既有提交）；本周结果仍在未提交工作区。本次未暂存/提交/推送/部署。开发者可先检查以下内容，再按数据库、认证、组织权限、测试/文档等有意义边界自行保存；共享文件的暂存组合须能构建，不为凑次数制造空提交：

```bash
git status --short
git diff --check
git diff --stat
git diff
git ls-files --others --exclude-standard
git check-ignore .env
git diff --cached --check
git diff --cached --stat
```

这些命令只作为保存前的检查说明，未在此自动执行暂存或提交。真实.env未读取或修改；新目录和测试项目只清理本次资源，最终归属/清理核对见交付检查。Git清单不是新发布或新标签。

## 21. 第三周交接：稳定能力、数据关系与风险

### 21.1 已经稳定的基础

三条迁移提供organizations、users、organization_members及版本记录；用户邮箱大小写唯一、成员联合主键/外键/CHECK已验证。七个API、Argon2id、短期JWT、可信身份中间件、组织/admin事务、成员RBAC及A01～A18都有自动化证据，第一周健康检查和生命周期没有回归。

RequireUser验证凭据并查询账号存在，仅通过CurrentUserID提供可信ID。组织Service按当前DB成员关系授权，角色/状态不从JWT或前端取。成员Handler先授权再解析，Service入口再授权；新工单接口应复用身份入口，按业务补对象级权限。

### 21.2 尚未解决的安全与运行债务

| 事项 | 当前边界与后续处理 |
| --- | --- |
| 可靠注册/登录限流 | 两个密码计算名额只控制资源，不按账号/IP限制尝试；公开部署前另行设计 |
| Token撤销/轮换/多端会话 | 当前15分钟到期重登，无Refresh/撤销表，客户端删副本不即时撤销 |
| 邮箱所有权、弱/泄露密码检查 | 当前未实现；注册409存在账号枚举取舍，不声称完整防护 |
| 邀请/找回密码/角色状态变更 | 无接口，添加仅针对现有账号；本周测试UPDATE只是夹具 |
| 并发撤权 | 每请求查当前关系，不保证已在途操作与撤权完全线性化；变更API需定义事务/锁语义 |
| HTTPS、凭据管理、审计与漏洞评估 | 仅回环学习环境；本轮静态审查不代替发布前工程 |
| 配置、CI、Git和个人学习 | 真实配置由开发者维护，远程CI未执行、未自动提交，个人掌握另行记录 |

### 21.3 工单、评论和事件怎样关联

以下为下一阶段设计，尚未创建新迁移或代码；以第三周手册与现有 [数据库设计](../database.md) 对照落实：

| 新表 | 组织/身份关系与必须守住的边界 |
| --- | --- |
| tickets | 带org_id；creator_id来自可信身份；创建者/处理人与该组织成员关系关联，评估复合FK；UNIQUE(org_id,id)供子表引用 |
| ticket_comments | 带org_id、ticket_id、author_id；(org_id,ticket_id)引用同组织工单，作者是当前组织成员，不能借另组织ticket_id串数据 |
| ticket_events | 带org_id、ticket_id、actor_id、事件类型/版本；与工单变更同事务，操作者来自后端，事件内容不能由客户端伪造 |

普通member按已有需求仅可查看本人创建工单；管理员的组织内范围不等于任意组织范围。对象级读取/修改、评论和状态动作需逐项落成权限表，不能把“active成员”直接当作所有工单操作的授权。

现有工单设计含OPEN/IN_PROGRESS/RESOLVED/CLOSED、version和处理人约束；support目前不在成员CHECK允许值中，第三周若引入须新增迁移并同步策略/测试，不能改写00003。本次没有提前实现工单接口。

### 21.4 下一周最先验证的两个问题

1. **工单写入与事件原子性**：创建/状态修改与事件同tx；故意让事件写入或提交失败，确认工单整体回滚，不返回虚假成功。未来并发领取另用条件更新/锁语义证明只有一个成功，race不能代替数据库实验。
2. **租户和对象级授权**：查询始终限定org_id，再限制普通member仅本人对象及动作；交换组织ID、ticket_id、作者/创建者字段，验证跨组织/越权拒绝且无写入，评论/事件也必须归属同一工单组织。

继续保留--closeout与W1回归。第三周实现前先核对新手册和以上债务，不能把尚未开发的工单、前端、AI或公开部署写成第二周已完成功能。

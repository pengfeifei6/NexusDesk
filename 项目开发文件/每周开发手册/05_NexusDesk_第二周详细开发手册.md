# NexusDesk 第二周详细开发手册

**版本：W02 · v1.0**  
**主题：身份认证、组织管理与权限控制**  
**适用对象：计算机科班本硕、研二，已经能够使用 Go、Gin 和 PostgreSQL 编写基础接口**  
**建议投入：16～22 小时。以功能验收与掌握程度为准，不为了追赶时间跳过安全测试。**

> 本周沿用第一周创建的 `nexusdesk` 仓库、`go.mod`、Docker PostgreSQL、Goose、`AGENTS.md` 和 `GET /healthz`。不重新生成仓库，不覆盖第一周代码。本手册提供目录、接口契约、数据约束、操作命令、测试矩阵和 Codex 协作规则，但不会代写完整认证业务代码。核心实现由你自己完成。

---

## 0. 本周边界和验收总览

### 0.1 本周必须实现

1. 为 `users` 与 `organization_members` 增加增量 migration，并保留第一周的 `organizations` 表。
2. 完成用户注册、登录、当前用户信息获取，密码只保存强散列。
3. 通过服务端 JWT 验证识别当前用户，并拦截未认证请求。
4. 实现创建组织、查看自己加入的组织、查看组织成员，以及管理员添加已注册成员。
5. 为成员角色建立清晰的授权矩阵，所有组织相关读取和写入都检查成员关系。
6. 为认证、数据一致性、越权和异常场景编写测试，并做一次 Codex 只读安全 Review。

### 0.2 本周不做

暂不实现刷新令牌、找回密码、邮件验证码、OAuth 第三方登录、前端登录页、Redis 分布式限流、邀请邮件、工单 CRUD、RAG、Agent、MCP。保留合理的扩展入口即可，不需要预先写空模块。

**安全边界说明：** 本周以本地学习和演示环境为目标。注册与登录是安全敏感功能，不应把本周版本直接公开在互联网。正式上线前需要进一步完善 HTTPS、登录限流、账号防滥用、凭据管理、审计和会话失效策略。

### 0.3 建议 API（统一前缀 `/api/v1`）

| 接口 | 身份要求 | 行为与成功响应 |
| --- | --- | --- |
| `POST /auth/register` | 匿名 | 创建用户，`201`；响应不包含密码散列 |
| `POST /auth/login` | 匿名 | 返回短期 Access Token，`200` |
| `GET /me` | 登录 | 查询当前用户，`200` |
| `POST /organizations` | 登录 | 创建组织与自己的 admin 成员记录，`201` |
| `GET /organizations` | 登录 | 只列出本人属于的组织，`200` |
| `GET /organizations/:orgID/members` | 当前组织的有效成员 | 返回成员列表，`200` |
| `POST /organizations/:orgID/members` | 当前组织的有效 admin | 添加**已经注册**的用户，`201` |

**刻意选择的业务规则：** 注册只创建用户，**不会自动创建组织**。用户登录后主动创建一个组织，事务内同时创建其 `admin` 成员记录。这样更利于理解“用户”和“租户”的区别。用户可属于多个组织。添加成员由管理员通过现有用户 ID 完成，本周不实现邀请系统。

### 0.4 周末必须可演示

- Alice 注册、登录、创建组织 A，创建后为 A 的 admin。
- Bob 注册、登录、创建组织 B，不能访问组织 A 的成员列表，也不能把自己加入组织 A。
- Alice 可将 Bob 添加为组织 A 的 member。Bob 随后可以读取组织 A 的成员列表，但不能继续添加成员。
- 过期、错误签名、格式不合法和缺失的 JWT 均不能访问 `GET /me`。
- 数据库里只有密码散列，不存储明文密码；测试和日志不暴露真实密钥与 Token。
- `GET /healthz` 和第一周的 PostgreSQL 功能仍然可用；`go test ./...`、`go vet ./...`、`go build ./...` 全部实际运行。

---

## 1. 先检查第一周成果，再进入第二周

进入已存在的项目根目录，不要再次执行 `git init` 或 `go mod init`。

```powershell
Set-Location "D:\workspace\nexusdesk"  # 将路径改为你实际的仓库位置
git status --short
go version
go test ./...
go build ./...
docker compose --env-file .env -f deploy/compose.yaml ps
```

如果工作区有未提交的第一周修改，先检查差异并提交。建议创建独立开发分支：

```powershell
git switch -c feat/week02-auth-organization
```

如果出现数据库连不上、第一周测试失败、migration 状态不一致等问题，**先修复第一周基线**，不要在未知状态下叠加认证代码。

第一周遗留的 `organizations` 表结构大致为 `id`、`name`、`created_at`。第二周 **不能通过修改已经执行的 `00001_create_organizations.sql` 来补字段**，只能创建新的 migration。这里体现真实项目中数据库演进的基本原则。

---

## 2. 第二周需要新增或修改的目录与文件

```text
nexusdesk/
├── AGENTS.md                             # 修改既有文件，增加 W02 约束
├── .env.example                          # 增加 JWT 配置占位值
├── cmd/api/main.go                       # 修改依赖组装
├── internal/
│   ├── auth/
│   │   ├── model.go                      # 用户模型与输入输出类型
│   │   ├── handler.go                    # 注册、登录、me 的 HTTP 适配
│   │   ├── service.go                    # 注册与身份验证业务逻辑
│   │   ├── repository.go                 # 用户 SQL 读写
│   │   ├── password.go                   # 密码散列与验证
│   │   ├── token.go                      # JWT 签发与验证
│   │   ├── middleware.go                 # Gin 身份认证中间件
│   │   ├── password_test.go              # 独立单元测试
│   │   └── token_test.go                 # 独立单元测试
│   ├── organization/
│   │   ├── model.go                      # 组织与成员类型
│   │   ├── handler.go                    # 组织/成员接口
│   │   ├── service.go                    # 创建组织/成员策略
│   │   ├── repository.go                 # 组织/成员 SQL、事务
│   │   └── policy.go                     # RBAC 权限判定
│   └── platform/
│       ├── config/config.go              # 修改，读取 JWT 配置
│       └── httpserver/router.go          # 修改，注册 API 和 Middleware
├── migrations/
│   ├── 00001_create_organizations.sql    # 保持不变
│   ├── 00002_create_users.sql            # 新建
│   └── 00003_create_organization_members.sql # 新建
├── docs/
│   ├── auth-design.md                    # 新建，认证与会话方案
│   ├── authorization-matrix.md           # 新建，权限矩阵
│   ├── database.md                       # 更新实际落地的表结构
│   ├── api/auth-organization.md          # 新建，接口契约
│   ├── decisions/0002-auth-security.md   # 新建，技术取舍 ADR
│   └── weekly/week02.md                  # 新建，每日记录与验收
├── tests/
│   └── integration/
│       └── auth_org_test.go              # 集成测试，使用专用测试库
└── requests/
    └── week02.http                      # 只存占位数据的演示请求
```

**提醒：** 上述是本周结束时可能出现的结构，并非 Day 1 必须创建全部 Go 文件。你应按每天任务逐步创建。`tests/integration` 可以选用 Go build tag 或通过环境变量显式启用，避免默认单元测试依赖正在运行的数据库。`requests/week02.http` 禁止写入真实 Access Token。

### 2.1 第一天可先创建文档目录

```powershell
$dirs = @('internal/auth', 'internal/organization', 'docs/api', 'tests/integration', 'requests')
foreach ($d in $dirs) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
$files = @(
  'docs/auth-design.md', 'docs/authorization-matrix.md',
  'docs/api/auth-organization.md', 'docs/decisions/0002-auth-security.md',
  'docs/weekly/week02.md'
)
foreach ($f in $files) { if (-not (Test-Path $f)) { New-Item -ItemType File -Path $f | Out-Null } }
```

### 2.2 修改而不是覆盖 `AGENTS.md`

第一周的根文件已经规定默认 `REVIEW_ONLY`、危险命令禁止自动执行、禁止读取 `.env` 等规则。**保留这些规则**，新增 W02 的身份认证专项约束即可。资料包中提供 `AGENTS.week02.append.md`，请审查后手动合并到现有 `AGENTS.md`。

需要更新它所引用的当前周计划，从 `docs/weekly/week01.md` 改为同时阅读 `week02.md`，并强调本周的正确权限模型。Codex 的仓库说明会被读取作为工作指导，但不是强制的系统安全边界；真正的命令和文件权限仍需要在工具配置中约束。

---

## 3. 本周必须先确定的技术决策

### 3.1 密码散列：优先 Argon2id

OWASP 当前优先推荐 Argon2id，早期教学资料常见的 bcrypt 也能用于旧系统，但有 72 字节输入限制。为了让项目采用更现代的安全实践，本周推荐 **Argon2id**。可先使用成熟的 Go 封装（例如 `github.com/alexedwards/argon2id`）来完成安全随机盐、参数编码与比对，不要自行发明密码算法。

- 密码不能保存明文、SHA-256 直接摘要或可逆加密值。
- 参考 OWASP 的基础参数，例如 19 MiB、2 次迭代、并行度 1；实际参数须在本机测试并记录成本。
- 通过封装 `HashPassword` 与 `VerifyPassword`，让业务逻辑不依赖具体算法。
- 为密码设置合理的长度上限以限制过大的计算成本。长度限制按 UTF-8 字节长度和产品策略明确说明。
- 登录失败向客户端返回通用错误，尽量避免通过提示或响应时长暴露邮箱是否存在。

### 3.2 Access Token：短期 JWT，暂不做 Refresh Token

建议使用 `github.com/golang-jwt/jwt/v5`，签名算法固定为 **HS256**，有效期建议 15 分钟。Token 携带 `sub`（用户 ID）、`iss`、`aud`、`iat`、`exp`，**不携带组织角色及明文密码**。

验证时需要固定允许算法，检查签名、`iss`、`aud`、`exp`，并拒绝无效 `sub`。示例配置：

```text
JWT_ISSUER=nexusdesk
JWT_AUDIENCE=nexusdesk-api
JWT_TTL_MINUTES=15
JWT_SECRET_BASE64=<本机随机生成的32字节密钥的Base64编码>
```

`.env.example` 只保留占位值，不要提交真实密钥。服务启动时验证 Base64 能正确解码，且密钥长度达到至少 32 字节。Windows PowerShell 本机生成示例（只在本机使用，**不要把结果粘贴给 AI**）：

```powershell
[Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
```

后端需要先 Base64 解码再将字节用于 HMAC 签名，不能一边按 Base64 配置、一边直接将字符串当作密钥而未说明。

**会话边界：** 本周不实现注销后立即失效与服务端撤销 Token。短期 Token 到期重新登录。用户角色变化时每次组织请求重新查数据库成员身份，防止沿用 Token 中的旧权限。

### 3.3 授权：租户隔离比角色判断更重要

本周两种角色为 `admin` 和 `member`，成员状态先使用 `active`、`disabled`。组织内角色是**组织维度的身份**，不是用户全局角色。JWT 只证明用户是谁，**不能证明用户可以访问哪个组织**。

| 动作 | 非成员 | member | admin |
| --- | --- | --- | --- |
| 查看当前用户资料 | 不涉及组织；需登录 | 允许 | 允许 |
| 新建自己的组织 | 已登录即可 | 允许 | 允许 |
| 查看组织成员 | 拒绝并隐藏组织是否存在 | 允许 | 允许 |
| 添加已经注册的成员 | 拒绝 | 拒绝 | 允许 |
| 修改其他人的角色 | 本周不实现 | 本周不实现 | 本周不实现 |

建议未加入组织的用户请求该组织资源时返回 `404`，避免暴露组织是否存在。属于组织但权限不足时返回 `403`。Token 不合法或缺失时返回 `401`。整个项目应遵循默认拒绝授权的原则。

### 3.4 登录限流不能完全留到第 9 周

第 9 周会做 Redis 限流和高并发优化，但**本周至少需要基础的登录防滥用措施**。可在本地单实例环境为登录增加可配置的简单失败次数/时间窗限制，或者先保持仅回环地址运行并将登录限流列为严格的上线阻断项。若本周准备在可公开访问的环境部署，则必须先完成可靠的登录防暴力破解控制，而不是直接开放注册与登录服务。测试、日志和演示截图也不能含真实账号凭据。

---

## Day 1：认证方案、API 合同、权限矩阵（建议 2～3 小时）

**个人任务：先自己设计，不写业务实现。**

1. 阅读第一周的 `requirements.md`、`database.md`、`architecture.md`，确认本周端点不会冲突。
2. 在 `docs/auth-design.md` 画出注册和登录时序图。区分身份认证、授权、Token 解析、成员身份检查。
3. 在 `docs/api/auth-organization.md` 写明 7 个 API 的输入、输出、HTTP 状态码和错误码。
4. 在 `docs/authorization-matrix.md` 形成上节权限表，明确跨组织访问的响应策略。
5. 在 `docs/decisions/0002-auth-security.md` 解释选 Argon2id、JWT、租户成员表的原因和替代方案。

**自己至少回答五个问题：** JWT 为什么不能作为角色权限的最终来源？用户注册与创建组织为什么分开？系统为何必须检查 `organization_members`？为什么密码需要慢散列？为什么 Access Token 失效不等于账号被删除？

**AI 协作：** 让 ChatGPT 挑设计漏洞；让 Codex 在 `PLAN_ONLY` 中阅读相关文档，只指出矛盾，不修改仓库。

**Day 1 验收：** 文档完整；能够讲清 401、403、404、409 的区别；本周接口范围冻结。

---

## Day 2：Migration 与 Repository 设计（建议 2～3 小时）

### 第 1 步：自己设计新增表

`users` 建议字段：`id BIGINT IDENTITY` 主键、`email VARCHAR(254) NOT NULL`、`display_name VARCHAR(80) NOT NULL`、`password_hash TEXT NOT NULL`、`created_at TIMESTAMPTZ`、`updated_at TIMESTAMPTZ`。邮箱约定统一规范化后存储，并在数据库建立大小写不敏感的唯一约束（例如 `UNIQUE INDEX ON lower(email)`）。不要只靠“先查是否存在”防止重复注册，因为会有并发竞态。

`organization_members` 建议：`org_id` 外键、`user_id` 外键、`role VARCHAR(16)`、`status VARCHAR(16)`、`created_at`。复合主键 `(org_id, user_id)`，角色和状态通过 `CHECK` 约束限制取值。建议添加从 `user_id` 查组织的索引。

**兼容第一周：** `organizations` 表继续使用既有 `id/name/created_at`。如需要 `updated_at` 或 `slug`，另开 migration 并记录原因。本周无需为未来工单或 AI 模块添加任何字段。

### 第 2 步：创建两个 Migration 文件

```powershell
New-Item -ItemType File -Force migrations/00002_create_users.sql | Out-Null
New-Item -ItemType File -Force migrations/00003_create_organization_members.sql | Out-Null
```

每个文件包含 `-- +goose Up` 与 `-- +goose Down`。第二个 migration 依赖第一个 `users` 和既有的 `organizations`。示例约束片段：

```sql
CREATE UNIQUE INDEX users_email_ci_unique ON users (lower(email));
-- 其他建表 SQL 由你手写，切勿覆盖第一周已经执行的迁移。
```

检查 migration 顺序和状态：

```powershell
# 先按第一周方式安全加载 .env 到当前 PowerShell 进程
# 构造本地专用连接参数 $dbConn，绝不打印含密码的参数
goose -dir migrations postgres $dbConn status
goose -dir migrations postgres $dbConn up
goose -dir migrations postgres $dbConn status
```

**注意**：不要在共享库上试运行 Down。个人测试库中的回滚练习，也要先确认其中没有需要保留的数据。

### 第 3 步：先设计 Repository 方法，不做过度抽象

至少列出 `CreateUser`、`FindUserByEmail`、`FindUserByID`、`CreateOrganizationWithAdmin`、`ListOrganizationsByUser`、`FindActiveMembership`、`ListMembers`、`AddMember` 的输入输出和错误分类。

重点是判断哪个操作必须在事务里完成。例如 `CreateOrganizationWithAdmin` 需要在同一数据库事务内创建组织和管理员关系，以避免只有组织、没有管理员的半成品状态。

**Day 2 验收：** 可以查询两张新表、索引和约束；重复邮箱被数据库拒绝；能说出 `BEGIN / COMMIT / ROLLBACK` 的意义。

---

## Day 3：注册与密码散列（建议 2～3 小时）

### 第 1 步：按职责创建认证模块

```powershell
$files = @('internal/auth/model.go', 'internal/auth/handler.go', 'internal/auth/service.go', 'internal/auth/repository.go', 'internal/auth/password.go', 'internal/auth/password_test.go')
foreach ($f in $files) { if (-not (Test-Path $f)) { New-Item -ItemType File -Path $f | Out-Null } }
```

`Handler` 只解析 JSON、做基本校验、调用 Service 并转换为 HTTP 响应；`Service` 负责邮箱规范化、密码策略、散列以及业务错误映射；`Repository` 只处理 SQL、读取用户和唯一冲突；`password.go` 封装第三方安全散列实现。

### 第 2 步：实现 `POST /api/v1/auth/register`

建议输入：`email`、`display_name`、`password`。建议返回 `201`、`user.id`、`user.email`、`user.display_name`。**绝不返回 `password_hash`**。

你必须自己完成：邮箱规范化、密码强度与长度策略、散列、写库、唯一冲突识别、统一错误响应和 Context 传播。

可以安装经过审查的 Argon2id 封装：

```powershell
go get github.com/alexedwards/argon2id
```

请在 `docs/decisions/0002-auth-security.md` 记录选用库、参数和性能权衡。Argon2id 会消耗一定 CPU/内存，本周不要让不可信请求无限并发触发散列计算。

### 第 3 步：亲自写密码测试

- 同一个密码散列两次，结果不同（随机盐）。
- 正确密码验证通过，错误密码不通过。
- 存储值不是原始密码，且服务端 JSON 不泄露散列。
- 不同大小写邮箱不能重复创建两个账号。
- 数据库操作失败时不能假装注册成功。

**AI 协作：** ChatGPT 用小例子解释盐、慢散列和参数；Codex 只读审查是否有明文泄露或竞态风险，不直接重写 `service.go`。

**Day 3 验收：** 实际注册成功；在数据库可确认保存的是散列；重复注册与非法密码被正确拒绝。

---

## Day 4：登录、JWT 与失败场景（建议 2～3 小时）

### 第 1 步：设计登录流程

`POST /api/v1/auth/login` 输入邮箱和密码。先规范化邮箱，查询用户，验证密码，再签发 Access Token。对于邮箱不存在或密码错误，向客户端返回**相同的通用错误**，并关注不存在账号时的时间差，避免账号枚举。

JWT 推荐使用：

```powershell
go get github.com/golang-jwt/jwt/v5
```

在 `internal/auth/token.go` 写好签发和验证接口，并由 `config.go` 读取 `JWT_ISSUER`、`JWT_AUDIENCE`、`JWT_TTL_MINUTES`、`JWT_SECRET_BASE64`。不要把真实密钥写在源码、文档、测试快照或 Git 中。

### 第 2 步：JWT 验证必须覆盖

- 使用明确的算法白名单（例如仅 HS256），拒绝 `none` 或意外算法。
- 验证签名、发行方 `iss`、接收方 `aud`、过期时间 `exp`。
- 检查 `sub` 可解析为有效用户 ID；不信任没有校验的 Claim。
- 过期 Token、篡改 Token、错误密钥 Token、空 Token 均拒绝。
- 令牌不包含角色、密码、密码散列、密钥或不必要的用户资料。

**学习任务：** 自己画出 JWT 的 Header、Payload、Signature，说明“Base64 可解码”和“签名可信”不是同一件事。

### 第 3 步：登录安全与测试

至少测四组：正确密码、错误密码、不存在用户、密码字段为空。记录服务端对于异常的实际响应。基础防滥用保护不足时禁止公开部署。

**Day 4 验收：** 可以登录获取 15 分钟 Access Token；JWT 验证单元测试覆盖伪造、过期和签名错误；不会把 Token 打印在应用日志。

---

## Day 5：鉴权中间件、当前用户与组织创建（建议 2～4 小时）

### 第 1 步：自己实现认证中间件

新建 `internal/auth/middleware.go`，从 `Authorization: Bearer <token>` 解析 Token，验证后将可信用户 ID 放入 Gin 请求上下文。在 Handler/Service 中通过可信上下文取得当前用户，不接受客户端 JSON 自带 `user_id` 冒充身份。

要求：

- 没有 `Authorization` 或格式错误返回 `401`。
- `/healthz`、注册和登录仍是公开接口。
- `GET /api/v1/me` 强制要求认证。
- Gin Context 中不要存储明文密码、密钥或完整 Token。

### 第 2 步：实现创建组织的事务

`POST /api/v1/organizations` 输入 `name`，不接受客户端指定 `owner_id`、`admin_user_id`。服务端从 JWT 验证后的身份识别用户，事务中：

1. 插入 `organizations`。
2. 插入 `organization_members(org_id,user_id,role='admin',status='active')`。
3. 任一步失败均回滚。

这里是本周最值得自己动手写的 SQL 事务。建议你故意模拟第二步失败，验证第一步也会回滚。

### 第 3 步：实现个人组织列表

`GET /api/v1/organizations` 只查询当前用户拥有 `active` 成员身份的组织。注意 SQL 必须使用认证后的当前用户 ID，并使用参数化查询。

**Day 5 验收：** 注册后无组织；登录后能创建组织；创建者成为 admin；事务失败没有孤立组织；其他人无法通过伪造 JSON ID 代建组织。

---

## Day 6：RBAC、跨租户隔离与集成测试（建议 3～4 小时）

### 第 1 步：先自己写权限判定表

把 `docs/authorization-matrix.md` 变成后端可执行规则：

- `GET /organizations/:orgID/members`：仅 active 成员可访问。
- `POST /organizations/:orgID/members`：仅 active admin 可执行。
- 为已有成员再次添加时返回 `409`，且不能重复插入。
- admin 不能通过客户端额外字段把自己伪装成其他组织的管理员。
- 被设置为 disabled 的成员不能继续读取该组织数据，即使 JWT 尚未过期。

### 第 2 步：自己实现成员查询和添加

`internal/organization/policy.go` 可以负责角色策略，但实际的组织和用户身份查询必须发生在可信服务端。**不是把路由参数 `orgID` 解析出来就算授权完成。**

成员列表响应可包含成员 ID、显示名、角色、状态、加入时间，不包含密码散列与敏感认证信息。添加成员请求只接收现有 `user_id` 和允许的 `role`，本周限制为 `member` 或 `admin`，并由当前组织的 admin 授权。

### 第 3 步：搭建专用集成测试库

优先使用与第一周相同的 PostgreSQL 主版本，但使用独立的测试库或独立的测试 Compose Project。不要把 `go test` 指向平时需要保留数据的开发库。测试启动后执行迁移、准备临时用户与组织，结束后清理自己的测试数据。

核心负向测试见第 8 节。可以让 Codex 在 `TEST_SCOPED` 下补充测试文件，但测试期望必须由你明确，并亲自执行确认。不要让 Codex 自动执行 `DROP DATABASE` 或 `down -v`。

**Day 6 验收：** 非成员 A 无法读取 B 的组织成员；A 的 member 无法添加成员；admin 能添加已注册用户；重复关系返回 409；禁用成员不能越权。

---

## Day 7：审查、回归、复盘与交接（建议 2～3 小时）

### 第 1 步：执行质量检查

```powershell
go fmt ./...
go test ./...
go vet ./...
go build ./...
git diff --check
git status --short
```

对于依赖测试库的集成测试，另执行明确标记的命令并保存结果，例如你自己选用 `go test -tags=integration ./tests/integration/...`。有些测试只有外部数据库就绪才能成功，应在 README 中说明前置条件，不要把“未执行”写成“通过”。

### 第 2 步：Codex 只读安全审查

```text
任务：W02-SECURITY-REVIEW
模式：REVIEW_ONLY。阅读 AGENTS.md、docs/weekly/week02.md、auth-design.md 与权限矩阵。
审查范围：internal/auth/**、internal/organization/**、新增 migration、路由和配置改动。
优先检查：JWT 签名与 Claim 验证、密码安全、注册并发唯一性、组织创建事务、跨租户权限、SQL 注入、敏感信息泄露。
对每项问题说明文件位置、触发条件、影响、验证方法和最小修复建议。
只报告，不编辑、不执行破坏性操作、不提交或推送。
```

### 第 3 步：亲自完成端到端演示

按顺序：Alice 注册与登录 → Alice 创建 A 组织 → Bob 注册与登录 → Bob 创建 B 组织 → Bob 请求 A 成员列表被拒绝 → Alice 添加 Bob 到 A → Bob 可查看 A 成员但不能添加成员 → 过期 Token 被拒绝。

使用专门的测试账号与占位密码，不将真实 Token 保存进版本控制。对比数据库实际状态，确认每个效果不是仅靠前端显示。

### 第 4 步：独立复盘并提交

完成 `docs/weekly/week02.md`：记录本周完成项、独立实现清单、AI 参与的修改、真实 Bug、验证证据、未完成事项和第 3 周风险。检查差异，按功能小步提交。可分别提交数据库、认证、组织权限、测试文档，不为凑提交次数造空 Commit。

---

## 4. 核心接口请求与返回约定

以下 JSON 只是接口契约示例，不代表已经编写或执行了 Go 实现。

**注册：** `POST /api/v1/auth/register`

```json
{"email":"alice@example.test","display_name":"Alice","password":"ExampleOnly-ChooseYourOwnLongPassword"}
```

```json
{"data":{"id":101,"email":"alice@example.test","display_name":"Alice"}}
```

**登录：** `POST /api/v1/auth/login`；返回 Access Token 和有效期：

```json
{"data":{"access_token":"<ACCESS_TOKEN>","token_type":"Bearer","expires_in":900}}
```

**创建组织：** `POST /api/v1/organizations`（Bearer Token）：

```json
{"name":"Engineering Support"}
```

```json
{"data":{"id":201,"name":"Engineering Support","my_role":"admin"}}
```

**添加成员：** `POST /api/v1/organizations/201/members`：

```json
{"user_id":102,"role":"member"}
```

建议统一错误格式：

```json
{"error":{"code":"FORBIDDEN","message":"permission denied","request_id":"<REQUEST_ID>"}}
```

错误码建议：`400` 无效输入、`401` 未登录或 Token 无效、`403` 有组织成员身份但角色不允许、`404` 不存在或跨租户不可见、`409` 邮箱/成员重复冲突、`429` 被限流、`500` 非预期服务器错误。`request_id` 可暂时留空或由现有中间件提供，**不要把与本周无关的全套观测系统强行提前**。

---

## 5. 第二周的 AI 分工矩阵

| 具体任务 | 你亲自负责 | ChatGPT 协助 | Codex 协助 |
| --- | --- | --- | --- |
| 认证设计 | 选方案、画时序、定义失败情况 | 比较 JWT/Session、讲安全原理 | `PLAN_ONLY` 找文档矛盾 |
| Migration | 自己设计、写 SQL、执行与检查 | 解释索引、约束、事务 | `REVIEW_ONLY` 查 SQL 缺陷 |
| 密码处理 | 自己封装库、确认参数、写测试 | 讲 Argon2id 原理和成本 | 审查散列调用及泄露风险 |
| 注册/登录 | **自己写完整核心 Service** | 针对错误情况提供提示 | 只读审查，受限补测试 |
| JWT 与中间件 | **自己写并能解释** | 解释算法混淆、Claims、Context | 审查安全边界 |
| 组织事务 | **自己写事务与 SQL** | 提问引导你判断一致性 | 审查事务边界 |
| RBAC | **自己确定权限并实现核心判断** | 讨论跨租户越权案例 | 生成测试数据和负向测试（授权后） |
| 文档、重复测试脚手架 | 审核内容和真实运行结果 | 优化说明文字 | 可在指定文件内生成 |
| Git、数据库修改 | 最终检查、执行、提交 | 辅助复盘 | 无授权不得 commit/push/回滚 |

**本周学习红线：** `service.go` 中的认证逻辑、`token.go` 的验证规则、组织事务和 `policy.go` 的授权判定必须首先由你自己完成。AI 可以提供最小示例、指出缺陷、补充测试，但不能直接用完整生成版本替换你的实现。

### 5.1 四个推荐提示词

**A. 密码安全原理学习**

```text
我正在亲自实现 NexusDesk 的用户注册，准备使用 Argon2id。
请先提问检查我对散列、随机盐、参数成本和密码验证的理解。
根据回答纠正误区，给出最小可运行示例即可，不要生成 auth/service.go。
最后设计 3 个测试来证明我的实现没有保存明文密码。
```

**B. JWT 方案评审**

```text
我设计的 JWT Claims、校验流程和中间件如下：[粘贴去敏后的方案]。
请重点审查算法白名单、签名、issuer、audience、exp、sub 和错误响应。
不要生成完整实现，请列出明确风险、验证测试和我需要查阅的官方 API。
```

**C. Codex 限范围测试补充**

```text
模式：TEST_SCOPED。
只允许修改 internal/auth/token_test.go 与 internal/auth/password_test.go。
目标：为已有实现补充过期 Token、篡改签名、错误密码等测试。
不得修改 token.go、password.go、service.go 或任何数据库、密钥、配置。
说明你实际运行过的命令与结果；没有运行就明确说未运行。
```

**D. 权限 Bug 诊断**

```text
我怀疑某个组织成员接口存在跨租户访问问题。
预期行为：[填写]；实际行为：[填写]；相关 SQL 与代码：[去敏内容]。
请先给我三个可检验的原因及对应的测试步骤；不要立即修改。
等我确认原因后再讨论最小修复方案和回归测试。
```

---

## 6. 本周建议的 Git 提交节奏

建议将工作拆为相对独立的提交：

```text
/docs: define W02 auth and authorization contracts
feat(db): add users and organization membership migrations
feat(auth): implement password-based registration and login
feat(auth): validate access tokens and protect me endpoint
feat(org): implement organization creation and membership policy
test(auth): add authorization and security regression tests
/docs: record week02 results and handoff
```

上面是**建议的提交信息**，不是要求必须凑够 7 个提交。提交之前总是执行 `git status`、`git diff`、`git diff --cached`，确认没有 `.env`、密钥、真实用户数据或者测试 Token。

---

## 7. 常见故障排查指南

| 问题现象 | 先由你做的排查 | 可向 AI 问什么 |
| --- | --- | --- |
| 用户重复注册仍成功 | 检查 DB 唯一索引与规范化是否生效 | 比较并发先查后插与唯一约束 |
| 登录一直报密码错误 | 核对散列存储、参数编码、比对 API | 解释 Argon2id 验证失败路径 |
| JWT 始终无效 | 检查密钥解码、算法、issuer、audience 和时钟 | 提出最小化的 token_test 测试 |
| JWT 过期却可访问 | 检查是否真的校验 exp、是否绕开中间件 | 审查路由组和 parser 选项 |
| 用户可读取其他组织 | 追查 SQL WHERE org_id/user_id 与成员校验 | 设计跨租户负向测试 |
| 组织创建只写入一张表 | 检查事务是否共用 tx、错误是否导致回滚 | 审查事务边界与错误传播 |
| `401/403/404` 混乱 | 确定认证失败、未加入组织、角色不足的定义 | 帮你检查错误映射一致性 |
| 迁移失败 | 查看依赖表、索引名称、SQL、goose 版本状态 | 只讨论原因，不自动回滚 |
| 集成测试污染开发数据 | 核查测试 DSN、数据库名称与清理策略 | 帮你设计隔离测试环境 |

**标准排障顺序：** 复现问题 → 记录预期与实际 → 缩小范围 → 提出假设 → 写最小测试 → 修复 → 回归。不要把“让 Codex 一键修复”当作默认第一步。

---

## 8. 本周必须覆盖的测试矩阵

| 编号 | 测试场景 | 预期 |
| --- | --- | --- |
| A01 | 正常注册 | 201，用户存在，响应无 hash |
| A02 | 邮箱大小写变化后重复注册 | 唯一约束拒绝，返回预定冲突码 |
| A03 | 密码不符合策略 | 400，不创建账号 |
| A04 | 两次散列同一密码 | 散列不同，均可验证 |
| A05 | 正确凭据登录 | 200，取得有效 Access Token |
| A06 | 错误密码或不存在邮箱 | 通用失败消息，不泄露账号是否存在 |
| A07 | 缺失、篡改、过期 Token | 401 |
| A08 | JWT 算法、issuer 或 audience 错误 | 401 |
| A09 | 未登录请求 `/me` | 401 |
| A10 | A 创建组织并自动成为 admin | 两条数据均存在且事务一致 |
| A11 | 组织创建第二步故意失败 | 整个事务回滚 |
| A12 | B 请求 A 组织成员列表 | 拒绝，不泄露组织信息 |
| A13 | A 组织的 member 添加成员 | 403 |
| A14 | A 组织的 admin 添加已注册用户 | 201，关系存在 |
| A15 | 重复添加同一组织成员 | 409，无重复关系 |
| A16 | disabled 成员访问组织 | 拒绝 |
| A17 | 伪造 `user_id` / `owner_id` 字段 | 无法取得他人身份和权限 |
| A18 | SQL 异常与数据库不可用 | 返回安全错误，不泄露 DSN 和密码 |

优先完成 A01～A15 的可重复自动化验证。A16～A18 也必须有明确的可执行测试或人工复现证据。`go test -race` 适合检查 Go 代码里的共享内存数据竞争，**不能取代数据库事务与跨租户权限测试**。

---

## 9. 本周任务完成判定（Definition of Done）

每个任务只有同时满足以下条件才能标记 Done：

- [ ] 在 `docs/api/auth-organization.md` 中有明确输入、输出和错误约定。
- [ ] 由本人确认核心设计，并能解释采用的技术和备选方案。
- [ ] 修改范围符合 `AGENTS.md` 和实际授权，没有未经检查的跨模块重构。
- [ ] 单元测试、需要的集成测试已经实际运行，结果有记录。
- [ ] 组织和身份相关代码已做越权检查，敏感信息没有输出到日志。
- [ ] 看过 `git diff`，没有 `.env`、密钥、临时 Token 等敏感内容。
- [ ] 已在 `docs/weekly/week02.md` 记录真实问题、AI 建议与处理决定。

**第二周最终验收门槛：** 七个 API 的目标流程可演示；至少完成一轮跨组织访问否定测试；完成一轮事务回滚实验；密码/JWT 核心测试通过；第一周的 `/healthz` 没有回归。

---

## 10. 脱离 AI 的十个自测问题

1. 身份认证和授权分别解决什么问题？
2. Argon2id 与直接用 SHA-256 计算密码摘要的区别是什么？
3. 密码中的随机盐有什么作用？
4. JWT Payload 为什么不能保存密码和其他敏感信息？
5. JWT 的签名验证、签发方校验、受众校验和过期校验有什么不同？
6. Gin 中间件应该如何向后续 Handler 传递可信用户 ID？
7. 为什么“JWT 有效”不代表“可以读取组织 A 的数据”？
8. 为什么邮箱唯一性必须由数据库约束，而不是只靠 `SELECT` 判断？
9. 创建组织和创建 admin 成员为什么要放在一个事务中？
10. 如果组织中的用户被禁用，其旧 JWT 还没过期，该怎么保证业务访问被立即拒绝？

建议 Day 7 关闭 AI 对话，自己口述答案并在纸上画出登录与组织访问流程。答不上的问题，回到对应源码与测试中验证，再用 ChatGPT 进行追问。

---

## 11. 第三周交接内容

在 `docs/weekly/week02.md` 最后补上：

- 已经稳定的表、接口、权限与测试。
- 存在但暂时没有解决的安全债务，如 Token 吊销、可靠登录限流、邀请流程。
- 当前 Auth 中间件如何提供可信 `user_id`；组织服务如何检查成员权限。
- 第三周 `tickets`、`ticket_comments`、`ticket_events` 应怎样关联 `users` 与 `organizations`。
- 第三周需要重点验证的两个问题：工单写入事务，以及租户与对象级授权。

**建议保留 30～60 分钟录制可展示的 API Demo。** 求职简历中的功能和安全特性必须与实际代码和测试证据一致，不要把计划中未完成的功能写成已上线功能。

---

## 12. 参考文档（阅读后再根据自己的实现决策记录）

- OWASP Password Storage Cheat Sheet： https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html
- OWASP Authentication Cheat Sheet： https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html
- OWASP Authorization Cheat Sheet： https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html
- golang-jwt v5 API： https://pkg.go.dev/github.com/golang-jwt/jwt/v5
- Go Argon2id 封装参考： https://github.com/alexedwards/argon2id
- Codex AGENTS.md 官方说明： https://learn.chatgpt.com/docs/agent-configuration/agents-md
- PostgreSQL 官方文档： https://www.postgresql.org/docs/current/

*说明：使用第三方密码库前要自行确认维护状态、依赖版本和适用参数，生产部署时还需进行完整安全评估。*

# NexusDesk

面向企业内部软件支持团队的工单与知识库项目，以 Go 后端工程能力为核心，后续逐步加入带来源引用的 AI 问答。

首个业务版本支持普通用户提交问题、支持人员领取处理、管理员维护组织角色，并验证组织隔离、状态流转、数据库事务和并发领取。

## 当前进度

**2026-11-10：Week 2 Day 7 工程收尾已完成**。当前可用全部七个身份/组织业务接口及 GET /healthz。阅读入口见 [第二周工作记录](docs/weekly/week02.md)。

Day 1 已形成认证、API 与权限设计；Day 2 已增加 users / organization_members 迁移；Day 3 实现了用户存储、Argon2id、注册与严格输入；Day 4 增加登录、dummy 与 JWT；Day 5 接入可信身份、本人资料、组织/admin原子创建与本人列表；Day 6完成成员列表、添加与组织RBAC；Day 7完成整周审查、新目录复现和复习材料。验证使用独立可丢弃数据库，日常配置由开发者本地维护。

**Week 1 工程基础继续保留**：API 先验证数据库可连接，再启动 HTTP，已有迁移、连接池、测试与复现证据。当前已有第一周提交 923ae93；本次第二周成果未由 AI 自动提交，个人复习与远程 CI 仍按实际记录。

工单业务、前端、角色修改与停用接口尚未实现。`/healthz` 是固定响应的存活检查，不在每次请求查询数据库；数据库启动与迁移是独立步骤，启动 API 不会自动建表。

| 阶段 | 计划成果 |
| --- | --- |
| W1 | 最小 API、`GET /healthz`、PostgreSQL 连接、组织表迁移和测试 |
| W2 | 身份认证、组织成员和角色权限 |
| W3～W4 | 工单、评论、事件、并发领取和最简前端 |
| W5 以后 | 文档上传、RAG、异步任务、SSE 与受控工具调用 |

完整功能以实际代码、测试和演示结果为准。

第二周文档：[认证设计](docs/auth-design.md) · [API 契约](docs/api/auth-organization.md) · [权限矩阵](docs/authorization-matrix.md) · [ADR-0002](docs/decisions/0002-auth-security.md)。本周角色采用 admin/member，状态 active/disabled；support 与角色修改留到后续阶段。第一周的运行与复现步骤继续使用下文说明。

## 开发环境

- 当前开发环境：Linux / Bash。
- `go.mod` 声明：`go 1.26.1`；Module 为 `github.com/pengfeifei6/NexusDesk`。
- Git 用于版本管理。
- Docker Engine 与 Compose 插件；PostgreSQL 镜像固定主版本 `postgres:17`。
- Goose CLI 固定 `v3.29.0`，当前已安装。
- Gin 固定为 [v1.12.0](https://github.com/gin-gonic/gin/releases/tag/v1.12.0)，pgx 固定 `v5.12.0`，依赖由 `go.mod` / `go.sum` 记录。
- 密码封装使用 alexedwards/argon2id `v1.0.0`，参数与本机成本见 [ADR-0002](docs/decisions/0002-auth-security.md)。
- JWT 使用 golang-jwt/jwt/v5 `v5.3.1`，只接受 HS256，详见 [JWT 说明](docs/jwt-guide.md)。
- Python 3.8 及以上用于迁移 URL 编码示例和独立数据库烟测。

可在本机检查：

```bash
go version
git --version
docker version
docker compose version
goose -version
```

2026-11-09 当前工作区：Go `1.26.1`、Git `2.25.1`、Docker 客户端及服务端 `26.1.3`、Compose `v5.6.0`、Goose `v3.29.0`。Compose 已按 [Docker 官方用户级安装方式](https://docs.docker.com/compose/install/linux/#install-the-plugin-manually) 安装并核对发布文件校验和。

在其他机器安装同版 Goose：

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.29.0
export PATH="$(go env GOPATH)/bin:$PATH"
goose -version
```

## 项目结构

```text
NexusDesk/
├── AGENTS.md                 # 仓库协作约定
├── .gitignore                # 本地配置与产物忽略规则
├── .env.example              # 环境变量示例，无真实凭据
├── README.md
├── go.mod
├── go.sum
├── cmd/api/                  # main.go 启动与信号退出，main_test.go
├── internal/platform/
│   ├── config/               # 已实现环境配置读取与测试
│   ├── httpserver/           # 已实现 Gin 健康检查与测试
│   └── database/             # pgxpool 创建、启动 Ping、失败脱敏与测试
├── internal/auth/            # 注册/登录、JWT、可信身份中间件、/me 与测试
├── internal/organization/    # 组织/成员事务、列表、RBAC、严格输入与测试
├── deploy/compose.yaml       # 本机 PostgreSQL 17
├── migrations/               # 组织、用户、成员三条增量 Goose migration
├── scripts/day5-smoke.py     # 独立可丢弃数据库的完整验证
├── scripts/week02-day2-smoke.py # W02验收；--closeout含全量测试与八步TCP演示
├── tests/integration/        # 显式启用的真实 schema / 注册 / 登录 / 组织 / 成员测试
├── requests/week02.http     # 虚构输入的本地请求示例
├── .github/workflows/ci.yml  # Go 格式、race、vet、build（远程待运行）
├── docs/
│   ├── requirements.md
│   ├── architecture.md
│   ├── database.md
│   ├── decisions/
│   └── weekly/week01.md
└── 项目开发文件/              # 原始项目设计、周期计划与开发手册
```

Git 不跟踪空目录；若从提交重新克隆，尚未放入文件的目录可能不存在，按需创建即可。

## 准备本地配置

以下操作在项目根目录执行。

1. 复制示例模板；已有 `.env` 时保留原文件：

   ```bash
   cp -n .env.example .env
   ```

2. 在本地编辑 `.env`，将 `DB_PASSWORD` 的占位值替换为自己设置的本地密码，并填齐下述四个 JWT 变量。已有 `.env` 的开发者需手动补这四项；AI 未读取或修改真实配置。示例占位值不能作为有效密码或密钥。
3. 检查真实配置被忽略，示例模板可正常纳入版本管理：

   ```bash
   git check-ignore .env
   git status --short
   ```

`git check-ignore .env` 应显示 `.env`。实际密码由开发者在本机填写，`.env.example` 始终保留占位值。配置加载拒绝尚未替换的密码占位值，数据库连接阶段再验证连通性。

Day 4 起共 12 个必需变量，原数据库与 HTTP 八项保留，新增：

| 变量 | 本地填写要求 |
| --- | --- |
| JWT_ISSUER | 示例 nexusdesk；非空有效 UTF-8，最多 256 字节 |
| JWT_AUDIENCE | 示例 nexusdesk-api；非空有效 UTF-8，最多 256 字节 |
| JWT_TTL_MINUTES | 固定 15 |
| JWT_SECRET_BASE64 | 至少 32 随机字节的标准 Base64 规范编码，不能有空白/换行 |

开发者可在自己的本机终端运行 `openssl rand -base64 32` 生成密钥并仅保存到本地配置，不粘贴到聊天或仓库。本次没有执行该真实密钥生成操作；测试使用独立临时值。Base64 解码后的字节用于签名，配置失败不会输出密钥值。

默认 HTTP 地址为 `127.0.0.1:8080`。注册处理凭据，因此 HTTP_ADDR 只接受显式回环 IP，如 127.0.0.1 或 [::1]；localhost、空主机、通配与公网绑定会被拒绝。宿主机 Go API 通过 `127.0.0.1:5432` 连接数据库；若该端口被占用，统一调整 DB_PORT 和容器映射。

### 运行 Go 前加载环境变量

Go 的环境变量读取不会自动加载磁盘上的 `.env`。在自己维护的本地 `.env` 已准备好、且使用兼容 Bash 的赋值语法时，可在当前 Bash 终端执行：

```bash
set -a
source ./.env
set +a
```

此步骤只加载配置，不启动服务；新开终端需要重新加载。`source` 会按 Bash 语法执行文件，只用于自己维护的本地配置。带空格或 Shell 特殊字符的值需要正确引用，不从外部下载的文件直接执行。

## 运行与验证

### 1. 启动本地 PostgreSQL

在项目根目录准备并加载上述环境变量，然后执行：

```bash
docker compose --env-file .env -f deploy/compose.yaml config --quiet
docker compose --env-file .env -f deploy/compose.yaml up -d --wait db
docker compose --env-file .env -f deploy/compose.yaml ps
```

配置验证使用 `--quiet`，避免将展开后的密码输出到终端。端口只绑定本机回环地址，数据保存在项目命名卷中。Go 运行在宿主机，使用 `DB_HOST=127.0.0.1` 和映射后的 `DB_PORT`。

当前环境曾出现 Docker 守护进程下载镜像时返回 `unexpected EOF`；本次已从官方 Docker Hub 下载并校验镜像层后导入本地 `postgres:17`。其他机器若遇到相同问题，应检查守护进程的网络或代理设置，不能把代码构建成功当成镜像可下载的证据。

### 2. 执行当前数据库迁移

Goose 的 [环境变量配置](https://github.com/pressly/goose) 允许连接字符串通过环境传入。以下 Python 代码从已导出的变量构造正确编码的 URL，不把密码放进 Goose 命令参数，也不打印 URL：

```bash
python3 - <<'PY'
import os
import subprocess
from urllib.parse import quote, urlencode

host = os.environ["DB_HOST"]
if ":" in host and not host.startswith("["):
    host = "[" + host + "]"
dsn = (
    "postgres://" + quote(os.environ["DB_USER"], safe="") + ":"
    + quote(os.environ["DB_PASSWORD"], safe="") + "@" + host
    + ":" + os.environ["DB_PORT"] + "/" + quote(os.environ["DB_NAME"], safe="")
    + "?" + urlencode({"sslmode": os.environ["DB_SSLMODE"]})
)
env = dict(os.environ, GOOSE_DRIVER="postgres", GOOSE_DBSTRING=dsn)
for action in ("up", "status"):
    subprocess.run(["goose", "-env=none", "-dir", "migrations", action], env=env, check=True)
PY
```

`-env=none` 禁止 Goose 自动重新读取默认 `.env`，这里统一使用当前终端已经加载的变量。Up 应用到当前最新版本 3，包含 organizations、users、organization_members 与 Goose 版本记录；首条组织迁移仍为版本 1，不改写历史文件。

查询组织表结构：

```bash
docker compose --env-file .env -f deploy/compose.yaml exec db \
  psql -U "$DB_USER" -d "$DB_NAME" -c '\d organizations'
```

Down 每次撤销最新一条迁移：当前从 3 开始会先删除成员表，再向下回滚才涉及用户和组织表。所有 Down 都可能丢失对应表数据；回滚实验只在烟测创建的可丢弃测试库执行，不在已有开发数据上自动运行。

### 3. 启动 Go API

数据库已启动、当前终端已加载环境变量后：

```bash
go mod download
go run ./cmd/api
```

另开终端验证：

```bash
curl -i http://127.0.0.1:8080/healthz
```

响应为 HTTP 200 和 `{"status":"ok"}`。该路径已用临时测试配置实际启动进程验证，没有读取本地真实 `.env`。

核心文件的阅读顺序：

1. [config.go](internal/platform/config/config.go)：读取必要变量，校验监听地址、数据库端口和 SSL 模式，不改变密码原文、不打印密码。
2. [router.go](internal/platform/httpserver/router.go)：创建 Gin 引擎，注册健康检查。
3. [postgres.go](internal/platform/database/postgres.go)：安全编码连接参数，创建连接池，用不超过 5 秒的启动 Context 执行 Ping；失败关闭连接池并返回脱敏错误。
4. [main.go](cmd/api/main.go)：先确认数据库连接，组装用户存储、共享密码预算与注册 Handler，再监听 HTTP；收到 Ctrl+C 或 SIGTERM 后停止 HTTP，最后关闭连接池。

缺少配置时返回带变量名的错误并退出；监听失败时返回错误并以非零状态退出。HTTP_ADDR 可采用显式回环 IPv4 或带方括号的回环 IPv6 地址，端口范围为 0～65535；端口 0 用于临时测试，启动日志显示实际地址。HTTP 读取头/请求的上限为 5 秒，写响应上限 10 秒。

可在已加载配置的终端验证一次错误地址，不修改配置文件：

```bash
HTTP_ADDR=invalid-address go run ./cmd/api
```

应报告 `HTTP_ADDR` 格式错误并以非零状态退出。该行的地址覆盖只影响此次命令，之后正常启动仍使用终端原有配置。

### 4. 停止本地数据库

```bash
docker compose --env-file .env -f deploy/compose.yaml down
```

停止命令保留命名卷。测试脚本只删除自己本次创建的独立测试卷，不删除日常开发卷。

### Day 3 注册演示

先按前面的步骤应用到版本 3 并启动 API。以下仅使用虚构账号与示例密码，作为本地演示输入：

```bash
curl -i http://127.0.0.1:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  --data '{"email":"alice@example.test","display_name":"Alice","password":"ExampleOnly-ChooseYourOwnLongPassword"}'
```

首次返回 201，data 只有 id/email/display_name；大小写变化后重复注册返回 409。注册只写用户，不创建组织或成员，不返回 Token。完整合同见 [API 文档](docs/api/auth-organization.md)。密码保存带随机盐的 Argon2id，明文和散列不出现在响应或应用日志中。

密码为 15～128 码点、最多 512 UTF-8 字节，原文保留；JSON 最多 16 KiB，未知/重复字段、非法编码和未配对 Unicode 转义会被拒绝。显示名不能包含 NUL。密码计算最多同时 2 个，超出返回 503 AUTH_BUSY；这不是可靠登录频率限制，当前只支持本地学习演示。

### Day 4 登录演示

先完成上面的虚构账号注册，再执行：

```bash
curl -i http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  --data '{"email":"alice@example.test","password":"ExampleOnly-ChooseYourOwnLongPassword"}'
```

成功 200，data 仅含 access_token、token_type=Bearer、expires_in=900。Token 不保存到仓库或日志；请求示例见 [week02.http](requests/week02.http)。错密码与不存在邮箱返回相同的 401 INVALID_CREDENTIALS，空密码返回 400；登录不创建组织或成员。

[JWT 学习说明](docs/jwt-guide.md) 解释三段结构、五个必需 Claim、签名与解码的区别，以及 dummy 验证。Day 5 已将 TokenManager.Verify 与用户存在性检查接入受保护 HTTP。

### Day 5 当前用户与组织演示

Day 5 的三个新接口需要 `Authorization: Bearer <ACCESS_TOKEN>`。在本地客户端使用刚登录取得的 Token；本机终端可将其保存在临时环境变量 NEXUSDESK_DEMO_ACCESS_TOKEN，再执行：

```bash
curl -i http://127.0.0.1:8080/api/v1/me \
  -H "Authorization: Bearer $NEXUSDESK_DEMO_ACCESS_TOKEN"
curl -i http://127.0.0.1:8080/api/v1/organizations \
  -H "Authorization: Bearer $NEXUSDESK_DEMO_ACCESS_TOKEN"
curl -i http://127.0.0.1:8080/api/v1/organizations \
  -H "Authorization: Bearer $NEXUSDESK_DEMO_ACCESS_TOKEN" \
  -H 'Content-Type: application/json' --data '{"name":"Engineering Support"}'
```

新账号列表为 `[]`，创建成功为 201 且 my_role=admin，再次列出可见该组织；创建只接受 name。请求示例不保存真实 Token，事务原理与失败验证见 [Day 5 学习说明](docs/organization-transaction-guide.md)。

### 独立数据库烟测

第一周历史回归（固定迁移版本 1，不随新增迁移变化）：

```bash
python3 scripts/day5-smoke.py
```

脚本需要本地已有 `postgres:17` 镜像、可用的 Compose、Goose 和 Go。它不读取真实 `.env`，使用随机项目名、临时端口和虚构凭据，自行启动数据库并验证：健康状态、Up / Status / Down / Up、停止后保留卷重启、表默认值和非空约束、Go 连接、包含特殊字符的密码、API 健康检查、SIGINT / SIGTERM 退出，以及错误密码、缺失数据库和不可达数据库的启动失败。最后只清理本次项目的容器与核对过归属的测试卷。

Goose 使用固定迁移目录，烟测返回非零状态代表尚未通过，错误与资源清理结果应保留排查。数据库集成测试由脚本设置 `NEXUSDESK_TEST_POSTGRES=1` 后运行；平时的 `go test ./...` 会明确跳过这项集成测试。

第二周 Day 2 数据库验收：

```bash
python3 scripts/week02-day2-smoke.py
```

新脚本同样不读取 `.env`，使用自己的随机项目、端口和测试密码；验证 1 → 3 增量升级、原组织保留、重复 Up、逆序 Down 及再升级，然后实际运行包含新 schema 测试的全量 race 检查。字段、默认值、邮箱与成员并发唯一性、CHECK、FK 和基础非空/长度约束均在真实 PostgreSQL 中验证。Down 与卷清理只针对本次可丢弃测试库，不操作日常数据。

新 schema 集成测试默认跳过，由脚本设置 `NEXUSDESK_TEST_W02_SCHEMA=1`，且要求回环地址与 `nexusdesk_w02d2_test_` 库名前缀；不要手动将它指向日常库。结果与日志入口见 [第二周工作记录](docs/weekly/week02.md)。`--baseline` 只应用版本 1 并运行新测试，预期非零退出，专用于展示新增表缺失的失败证据，不是通过命令。

第二周 Day 3 注册、用户存储与真实进程验收：

```bash
python3 scripts/week02-day2-smoke.py --registration
```

该模式在同一个隔离流程中启用 NEXUSDESK_TEST_W02_REGISTER=1，验证散列、大小写/并发重复、数据库失败、在途取消、显示名 NUL 拒绝和无组织副作用；还实际构建并启动 API，通过 TCP 验证 201/409/400、健康检查与 SIGTERM。日常 `.env` 不会被加载。

第二周 Day 4 完整验收（包含注册和 schema 回归）：

```bash
python3 scripts/week02-day2-smoke.py --login
```

该模式同时启用 NEXUSDESK_TEST_W02_LOGIN=1 与注册测试。真实进程登录后由 Python HMAC 独立验证签名、Claims 和 900 秒有效期，检查统一凭据失败、空密码、故障/取消和无业务写入。测试 Token 仅在内存中比对，归档日志不包含原文；测试密钥不沿用真实配置。

### Day 5 完整验收

Day 5 完整验收（包含注册、登录和 schema）：

```bash
python3 scripts/week02-day2-smoke.py --organizations
```

此模式还启用 NEXUSDESK_TEST_W02_ORGANIZATIONS=1，真实库验证创建/admin原子性、第二步与延迟提交失败回滚、取消、本人active过滤与分页、已删除用户401、存储故障500；实际进程通过TCP验证三个新接口和伪造身份拒绝。仍只操作本次随机测试库。

### Day 6 完整验收

Day 6 完整成员/RBAC验收（包含Day2～Day5与六个真实数据库入口）：

```bash
python3 scripts/week02-day2-smoke.py --members
```

运行器新增NEXUSDESK_TEST_W02_MEMBERS=1，只使用随机隔离项目；覆盖非成员/跨组织404、member写403、admin添加201、重复与并发409、disabled旧Token404、同用户其他组织仍可用、提交/取消回滚和安全500。普通Go测试默认跳过六个真实数据库入口，不能把跳过视为这些验证已运行。

### Day 7 整周复现

Day 7整周演示与复现：

```bash
python3 scripts/week02-day2-smoke.py --closeout
```

--closeout包含全部历史模式，按手册输出Alice/Bob八步安全摘要，最后用临时测试密钥在内存构造过期凭据，验证/me与成员接口401。本次还从不含真实.env/.git的新项目目录实际通过同一命令，见 [演示记录](docs/weekly/week02-demo.md)；这是现有主机工具/缓存下复现，不是全新机器安装。

### Go 质量检查

当前已经包含配置、路由、启动和数据库失败路径测试，可以执行：

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

普通检查不依赖本地数据库。Day 6 的普通测试为 4 包通过，顶层测试 14 通过、1 项真实数据库集成测试按设计跳过；该集成项在独立数据库烟测中另行实际通过。日志、失败记录和处置见第一周周报。

以上四包统计是 W1 历史结果。W02 Day 2 新增 tests/integration 后共有 5 个测试包，普通检查跳过两个真实数据库入口；专用 W02 烟测启用两个入口并实际执行。数据库约束通过不代表注册、JWT 或 Service 授权已经实现。

Day 3 历史结果：6 包，普通 race 顶层 32 通过、3 个数据库入口跳过；当时 --registration 顶层 35 通过、无跳过，见 [Day 3 摘要](docs/weekly/evidence/week02-day3-test-summary.json)。

Day 4 历史结果：6 包，普通 race 顶层 48 通过、4 个数据库入口跳过；--login 隔离 PostgreSQL 顶层 52 通过、0 失败、0 跳过。登录 JSON 短时模糊测试 18,274 次通过，W1 回归也通过；详见 [Day 4 摘要](docs/weekly/evidence/week02-day4-test-summary.json)。Token 签发/校验已验证，受保护 HTTP 和组织授权仍待后续。

Day 5 历史结果：7 包，普通 race 顶层 53 通过、5 个数据库入口跳过；--organizations 顶层 58 通过、0 失败、0 跳过。组织 JSON 模糊测试 11,119 次通过，W1 回归、vet/build/module/格式检查均通过；见 [Day 5 摘要](docs/weekly/evidence/week02-day5-test-summary.json)。这些结果不代表尚未实现的成员接口和完整组织 RBAC 已验收。

Day 6历史结果：7包，普通race顶层58通过、6个数据库入口跳过；--members顶层64通过、0失败、0跳过。成员JSON模糊测试13,074次通过，W1回归及vet/build/module/格式检查通过，见 [Day6摘要](docs/weekly/evidence/week02-day6-test-summary.json)。本周七个业务接口及约定RBAC已验证，当时整周收尾留Day7，现已完成。

Day 7最新结果：本次重新运行普通race为7包/58顶层通过/6个数据库入口跳过；新目录--closeout为7包/64顶层通过/0失败/0跳过，八步TCP演示、W1回归及vet/build/module/格式检查均通过，见 [Day7统计](docs/weekly/evidence/week02-day7-test-summary.json)。整周独立静态审查未发现需修复的实质问题；没有运行漏洞扫描，也未宣称完成公开部署防护。

本周复习入口：[41道问题与参考回答](docs/weekly/week02-answers.md)（含手册十题导航、常见故障、真实Bug） · [八步演示](docs/weekly/week02-demo.md) · [整周审查](docs/reviews/week02-security-review.md)。个人独立掌握、演示视频、Git保存和远程CI按开发者实际完成情况记录。

Day 7 已在不含真实 `.env`、`.git` 和生成缓存的新项目目录中执行完整数据库和 HTTP 验证，含保留卷重启。该验证使用现有主机工具、模块缓存与本地镜像，不等同于全新机器安装。临时资源已清理，自己的开发库仍需按上述步骤启动。

基础 GitHub Actions 文件在 push / pull request 时检查 Go 格式、race、vet 和 build，不读取 `.env`，不启动真实数据库。本地等效命令已通过，云端执行尚未触发。

## 排查入口

| 现象 | 首先检查 |
| --- | --- |
| `docker: 'compose' is not a docker command` | Compose 插件是否安装并可被当前 Docker CLI 发现 |
| `go run ./cmd/api` 无法找到 Go 文件 | 终端是否位于本项目根目录，当前检出是否包含 Day 4 文件 |
| 环境变量缺失 | 当前终端是否加载本地配置，变量名是否与配置代码一致 |
| HTTP 端口占用 | 实际监听进程与 `HTTP_ADDR` |
| 宿主机无法连接数据库 | 容器状态、端口映射、`DB_HOST` 与 `DB_PORT` |
| 修改 `.env` 后数据库认证失败 | 已有数据库卷中的用户密码是否仍为初始化时的值；修改文件不会自动修改数据库用户密码 |

## 文档与下一步

- [需求说明](docs/requirements.md)
- [架构说明](docs/architecture.md)
- [数据库设计](docs/database.md)
- [ADR-0001：模块化单体](docs/decisions/0001-modular-monolith.md)
- [第一周周报](docs/weekly/week01.md)
- [第一周只读审查与发现处置](docs/reviews/week01-review.md)
- [第一周演示摘要](docs/weekly/week01-demo.md)
- [测试结果摘要](docs/weekly/evidence/week01-test-summary.json)
- [第一周源码基线校验清单（W02 已有增量修改）](docs/weekly/evidence/week01-source-baseline.json)
- [第一周详细开发手册](项目开发文件/每周开发手册/04_NexusDesk_第一周详细开发手册.md)

下一步：按 [第二周记录](docs/weekly/week02.md) 阅读Day7交接与参考回答，复习后按第三周手册实现工单事务及对象级权限。实现与工程验证由 AI 按开发者委托完成，不代替个人理解记录。

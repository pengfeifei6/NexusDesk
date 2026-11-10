# NexusDesk

面向企业内部软件支持团队的工单与知识库项目，以 Go 后端工程能力为核心，后续逐步加入带来源引用的 AI 问答。

首个业务版本支持普通用户提交问题、支持人员领取处理、管理员维护组织角色，并验证组织隔离、状态流转、数据库事务和并发领取。

## 当前进度

**2026-11-10：Week 2 Day 1 设计文档已形成**，包括认证流程、七个 API 契约、权限矩阵与安全 ADR；当前运行代码仍为 Week 1，尚未实现注册、登录或组织业务。阅读入口见 [第二周工作记录](docs/weekly/week02.md)。

目前已完成 **Week 1 可代办的工程收尾**：最小 Go API、PostgreSQL、迁移、连接池、测试、只读审查和干净目录复现均有实际记录；基础 CI 文件已准备。API 先验证数据库可连接，再启动 HTTP。个人复习、本人复现、Git 提交与远程 CI 尚未记录为完成。

登录、工单业务和前端尚未实现。`/healthz` 是固定响应的存活检查，不在每次请求查询数据库；数据库启动与迁移是独立步骤，启动 API 不会自动建表。

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
├── deploy/compose.yaml       # 本机 PostgreSQL 17
├── migrations/               # 首条组织表 Goose migration
├── scripts/day5-smoke.py     # 独立可丢弃数据库的完整验证
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

2. 在本地编辑 `.env`，将 `DB_PASSWORD` 的占位值替换为自己设置的本地密码。示例模板中的占位值不能作为已配置完成的密码。
3. 检查真实配置被忽略，示例模板可正常纳入版本管理：

   ```bash
   git check-ignore .env
   git status --short
   ```

`git check-ignore .env` 应显示 `.env`。实际密码由开发者在本机填写，`.env.example` 始终保留占位值。配置加载拒绝尚未替换的密码占位值，数据库连接阶段再验证连通性。

默认 HTTP 地址为 `127.0.0.1:8080`。宿主机 Go API 通过 `127.0.0.1:5432` 连接数据库；若该端口被占用，可在数据库阶段统一调整 `DB_PORT` 和容器映射。

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

### 2. 执行组织表迁移

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

`-env=none` 禁止 Goose 自动重新读取默认 `.env`，这里统一使用当前终端已经加载的变量。Up 后存在 `organizations` 和 Goose 版本记录，首条版本为 1。

查询组织表结构：

```bash
docker compose --env-file .env -f deploy/compose.yaml exec db \
  psql -U "$DB_USER" -d "$DB_NAME" -c '\d organizations'
```

Down 会删除组织表，回滚实验只在下面烟测创建的可丢弃测试库执行，不在已有开发数据上自动运行。

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
4. [main.go](cmd/api/main.go)：先确认数据库连接，再监听 HTTP；收到 Ctrl+C 或 SIGTERM 后停止 HTTP，最后关闭连接池。

缺少配置时返回带变量名的错误并退出；监听失败时返回错误并以非零状态退出。`HTTP_ADDR` 可采用 IPv4 或带方括号的 IPv6 地址，端口范围为 0～65535；端口 0 用于临时测试监听，启动日志会显示实际地址。

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

### 独立数据库烟测

```bash
python3 scripts/day5-smoke.py
```

脚本需要本地已有 `postgres:17` 镜像、可用的 Compose、Goose 和 Go。它不读取真实 `.env`，使用随机项目名、临时端口和虚构凭据，自行启动数据库并验证：健康状态、Up / Status / Down / Up、停止后保留卷重启、表默认值和非空约束、Go 连接、包含特殊字符的密码、API 健康检查、SIGINT / SIGTERM 退出，以及错误密码、缺失数据库和不可达数据库的启动失败。最后只清理本次项目的容器与核对过归属的测试卷。

Goose 使用固定迁移目录，烟测返回非零状态代表尚未通过，错误与资源清理结果应保留排查。数据库集成测试由脚本设置 `NEXUSDESK_TEST_POSTGRES=1` 后运行；平时的 `go test ./...` 会明确跳过这项集成测试。

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
- [当前源码基线校验清单](docs/weekly/evidence/week01-source-baseline.json)
- [第一周详细开发手册](项目开发文件/每周开发手册/04_NexusDesk_第一周详细开发手册.md)

下一步：阅读周报中的测试情况、知识点参考和 Week 2 交接，核对并保存 W1 提交，再协调第二周手册与既有需求后开始认证和组织功能。实现与工程验证由 AI 按开发者委托完成，不代替个人理解记录。

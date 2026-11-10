# Day 5 PostgreSQL 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 提供本地 PostgreSQL Compose、首条 Goose 迁移和带启动 Ping 的 pgxpool，并真实验证。

**Architecture:** API 读取配置后创建数据库连接池，Ping 成功才启动 HTTP；退出先停止 HTTP，再关闭连接池。健康检查不在每次请求访问数据库。迁移由 Goose 独立执行。

**Tech Stack:** Go 1.26.1、pgx v5.12.0、PostgreSQL 17、Goose v3.29.0；Compose 按安装后实际版本记录。

**Spec:** [第一周手册 Day 5](../../../项目开发文件/每周开发手册/04_NexusDesk_第一周详细开发手册.md)、[架构](../../architecture.md)、[数据库](../../database.md)。

## Global Constraints

- 用户已授权工具准备和 Day 5 实现；在当前工作区继续，不自动提交。
- 不读取或修改真实 `.env`，使用独立 Compose 项目、临时环境变量和虚构凭据验证。
- 回滚只在本次创建的可丢弃数据库执行，不删除现有数据卷；验证结束只清理本次测试资源。
- Go Module 和 Go 指令保持原值，新增 pgx 依赖，其他版本由 Go 最小版本选择处理。
- 不实现登录、业务 Repository、前端、Redis 或 AI 功能。

## Review Focus

- DSN 中的空格、引号、反斜杠正确编码，错误不泄漏密码。
- 数据库不可达或 Context 已取消时返回错误，不启动 HTTP。
- 连接失败时释放池，HTTP 停止后释放数据库资源。
- 原有 HTTP 测试不依赖真实数据库，数据库集成测试明确选择执行。
- 迁移可在空的独立测试库 Up / Down / Up，组织表默认值和约束真实可用。

## Task 1：数据库连接与启动接入

**Files:** `internal/platform/database/postgres.go`、`postgres_test.go`、`cmd/api/main.go`、`main_test.go`、`go.mod`、`go.sum`。

**Interfaces:** `database.Open(context.Context, config.DatabaseConfig) (*pgxpool.Pool, error)`；入口 `run(context.Context) error` 负责配置与数据库生命周期，`runHTTP(context.Context, string) error` 管理 HTTP 生命周期。

- [x] 先测试不可达、已取消和特殊密码的安全报错，观察缺失实现或旧行为导致的失败。
- [x] 实现独立于字符串拼接的连接 URL，连接与 Ping 使用 5 秒启动超时，失败关闭连接池。
- [x] 接入 API，调整原有 HTTP 测试保持无数据库依赖。
- [x] 运行全量单元测试。

## Task 2：Compose、迁移与实际验证

**Files:** `deploy/compose.yaml`、`migrations/00001_create_organizations.sql`、`scripts/day5-smoke.py`。

- [x] Compose 仅绑定本地端口，使用命名卷和健康检查，密码通过环境变量提供。
- [x] migration 只创建组织 ID、名称和创建时间，包含 Goose Up / Down。
- [x] 编写可复现烟测：独立测试项目、环境变量虚构凭据、迁移与真实 Go API 进程，不自动读取 `.env`。
- [x] 启动独立数据库并验证健康、迁移、Go 连接、失败退出、信号退出和测试资源清理。
- [x] 集成测试验证连接、表约束与密码编码；普通测试没有数据库时明确跳过集成部分。

## Task 3：交接与检查

**Files:** `README.md`、`docs/weekly/week01.md`、必要的阶段状态说明。

- [x] 更新 Bash 启动、迁移、工具版本和实际证据，不写入凭据。
- [x] 运行 `go test ./...`、`go test -race ./...`、`go vet ./...`、`go build ./...`、格式与 Diff 检查。
- [x] 请求独立只读审查，修复影响本次目标的问题。

## 执行记录

- 起始单元测试三个包通过。
- 首次 Compose 安装命令被执行环境拒绝，原因是带有宽泛删除命令；改为只管理专属临时目录的安装流程。
- Compose v5.6.0 发布文件校验后安装，Goose v3.29.0 固定版本安装成功。
- 守护进程拉取镜像 EOF；改为在终端下载并逐层验证官方 Docker Hub 镜像后导入，不修改守护进程配置。
- 普通测试通过；独立审查未发现阻断缺陷，建议的 TCP 黑洞超时与途中取消测试已补齐。通过移除超时的临时变异确认测试能捕捉回归，再恢复后通过。
- 真实数据库烟测全部通过，包括带特殊字符的测试密码、迁移往返、全量 race、API 启动门槛和两种退出信号。仅清理本次测试容器及带匹配标签的测试卷，结果详见第一周周报。

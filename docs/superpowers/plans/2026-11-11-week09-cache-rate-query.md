# Week09 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 完成第九周缓存、AI 限流、真实 SQL 优化与验收文档。
**Architecture:** 授权后 cache-aside，PostgreSQL 事务版本隔离缓存代次，共用 Redis 客户端原子限流。
**Tech Stack:** 现有 Go/Gin/pgx/go-redis/PostgreSQL/Vue，不升级依赖。
**Spec:** `docs/superpowers/specs/2026-11-11-week09-cache-rate-query.md`

## Global Constraints

- 保持 Go/module/依赖版本，不修改旧迁移 00001..00012；新增迁移从 00013 起。
- 每次列表访问先做实时数据库成员授权；所有 key 与 SQL 显式包含 org_id；当前 scope 为 active-members-all。
- 数据库是事实源；缓存失效只在事务提交后对其他读者可见，Redis 故障不能使已提交写入失败。
- Redis 复用 W07 配置；缓存/限流独立前缀，不 FLUSHDB，不触碰原 6379 服务。
- 限流在 SSE/模型调用/StartTurn 之前；429 带 Retry-After，Redis 故障返回安全 503。
- 不读上传原文件，不输出真实 .env/Key；真实集成仅随机独立测试库、自有 Redis、合成数据；日常库只备份后 Up。
- 不自动 commit/push/merge；既有 W01..W08 证据保留，不将 Skip 或模拟模型说成真实云验收。

## Review Focus

- 缓存填充与写提交竞争：旧填充只进入旧版本；Task1 真实并发测试。
- Worker/AI 外层事务更新：触发器覆盖提交/回滚，Task1 验证。
- Redis 故障及恢复：不能恢复旧可用缓存；Task1 缓存与 Task2 限流测试。
- 非 owner/撤权：不读缓存、不消耗 AI 额度；Task1/2 测试。
- 同时间戳与稀疏筛选：分页无重复，参数化 SQL 结果相同；Task3 实验。

### Task 1: 事务版本与两列表缓存

Files: internal/platform/cache/*, internal/platform/config/performance.go + config.go, internal/ticket/service.go, internal/knowledge/service.go, cmd/api/main.go, migrations/00013_cache_revisions.sql, tests/integration/week09_cache_test.go, .env.example, scripts/dev.py。

Interface: `cache.NewClient(config.AsyncConfig, timeout)` 返回共享 redis.Client；`cache.New(client,pool,options)` 提供读版本/JSON Get/Set/Stats；领域 Service 用可选 WithCache 注入，原 NewService 与旧单测保持兼容。实际签名以 Task1 report 为 Task2 接口约定。本任务不改 repository.List SQL。

- [ ] 先写组织/过滤 key、故障回源、实时权限、提交与回滚测试并观察失败。
- [ ] 追加事务版本表与两触发器；实现有界 Redis 客户端、缓存和两个授权后列表集成。
- [ ] 接入配置、启动和示例/launcher白名单，unit/race 验证；集成通过 root 隔离 harness 运行。
- [ ] 自查和独立任务审查；保留未提交 diff。

### Task 2: AI 发消息限流与前端提示

Files: internal/platform/ratelimit/*, internal/platform/config/performance.go, internal/ai/handler.go, cmd/api/main.go, web/src/lib/{client,ai-stream}.ts 与相关 tests, tests/integration/week09_rate_test.go, .env.example, scripts/dev.py。

Consumes Task1 shared Redis client/config。Produces limiter Check(ctx, trusted org,user) allowed/retryAfter/error；Handler 可选注入，生产默认启用，旧测试可保持无 limiter。

- [ ] 写并发限额、窗口到期、用户/组织隔离、HTTP 429/503 不 StartTurn、非 owner 不消耗额度、前端信息/输入保留测试，观察失败。
- [ ] Lua 原子首请求固定窗口，过期使用 Redis TTL；输入及 owner 校验后执行，503 保守拒绝。
- [ ] 配置默认 10/60 秒、开关、Retry-After 前端展示；unit/race 和前端 checks。
- [ ] 自查和独立任务审查；保留未提交 diff。

### Task 3: SQL、隔离验收与本周交付（controller）

Files: internal/{ticket,knowledge}/repository.go, migrations/00014_*（仅实测需要时）, scripts/week09-{smoke.py,performance.py,browser.mjs}, docs/performance/week09/*, docs/weekly/week09*.md, docs/testing/week09-test-plan.md, docs/demo/week09-demo.md, requests/week09.http, README.md, AGENTS.md。

- [ ] 基线 clean HEAD 3613e9f；隔离 schema12 造合成数据，记录两个旧 SQL 实际计划与查询结果。
- [ ] 改为固定谓词片段 + 参数值；测相同参数的改后计划，必要索引仅追加；至少 7 次测中位数、分页结果一致。
- [ ] 扩展隔离 harness 全门禁+新 Redis/HTTP/浏览器测试，不依赖云计费，不覆盖 W08 证据。
- [ ] 全 Go race/vet/build + web tests/build/lint/format + Python，确认无 Skip；只自有资源清理。
- [ ] 备份日常库后 Up、重启本地自有进程，health/proxy确认，补完每日记录、问题答案与运行文档。
- [ ] 最终整分支独立审查及一轮修复；不自动提交。

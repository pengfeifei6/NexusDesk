# 第九周：缓存、限流与查询优化设计

依据第九周手册及 W08 实现。用户委托整周完成，以下实现选择由 AI 补齐，不代表开发者已经独立掌握。保留当前工作目录运行方式，不自动提交、推送或公开部署。

## 范围与选择

缓存工单列表和知识文档元数据列表。所有 active member/admin 当前共享同一组织可见范围；先查数据库成员权限，再访问缓存。详情、正文、下载、RAG 来源、审批及写入决策仍直接查库。

考虑过逐写路径提交后 DEL、Redis 组织版本、数据库组织版本三种方案。选择数据库版本：API、Worker、AI 外层事务都会写列表数据，仅在 Service 加 DEL 容易漏路径；Redis 中断时丢失失效指令还可能恢复旧缓存。新增 `cache_revisions(org_id,domain,version)`，tickets/knowledge_documents 行变更触发器在原事务中增加版本；其他请求只在提交后看到新版本。列表读版本后构造 Redis key，提交前读请求可完成旧快照，但不能把旧结果放进新版本 key。回滚不失效，Redis 故障不影响写事务。旧版本 key 自动 TTL 回收，无扫描删 key、无 Redis 写入依赖。成本：每次列表增加一个索引点查，每次领域写入增加版本行写及同组织领域短暂串行；不宣称消除全部数据库访问。

缓存使用既有 REDIS_ADDR/DB/PASSWORD，与 Asynq 分前缀 `nexusdesk:cache:v1:`。API 共用一个 go-redis 客户端供缓存与限流；Worker 不需新客户端，触发器覆盖其更新。独立 cache 开关，默认 30 秒 TTL + 0..20% 正抖动；每操作默认 100ms、禁自动重试、限制池等待及缓存值大小。miss、故障、坏 JSON 都回源，只有成功结果入缓存；权限和数据库错误不缓存。缓存值保留组织边界检查，提供内部计数用于测试与关闭时安全日志，不增公开监控接口。

AI 发消息路由在身份/组织/会话归属与输入检查后、StartTurn 和 SSE 前限流。可信 user+org，默认每 60 秒 10 次；Redis Lua 原子计数和 TTL；429 RATE_LIMITED + Retry-After。Redis 故障 fail closed，503 RATE_LIMIT_UNAVAILABLE，普通列表继续回源；确认/拒绝及历史不受此额度影响。开关与窗口/额度可配置。前端明确显示稍后重试，保留输入，不自动重试或重放创建。

优化两条真实列表 SQL：根据合法可选筛选拼接固定 SQL 片段，值仍参数化，避免 generic plan 的可选 OR。已有组织/时间和组织/状态/时间索引不重复创建；只有真实计划证据支持才新增优先级索引。稳定 `created_at DESC,id DESC`；保留现有 OFFSET 协议并说明深页成本。

## Global Constraints

- 保持 Go/module/依赖版本，不修改旧迁移 00001..00012；新增迁移从 00013 起。
- 每次列表访问先做实时数据库成员授权；所有 key 与 SQL 显式包含 org_id；当前 scope 为 active-members-all。
- 数据库是事实源；缓存失效只在事务提交后对其他读者可见，Redis 故障不能使已提交写入失败。
- Redis 复用 W07 配置；缓存/限流独立前缀，不 FLUSHDB，不触碰原 6379 服务。
- 限流在 SSE/模型调用/StartTurn 之前；429 带 Retry-After，Redis 故障返回安全 503。
- 不读上传原文件，不输出真实 .env/Key；真实集成仅随机独立测试库、自有 Redis、合成数据；日常库只备份后 Up。
- 不自动 commit/push/merge；既有 W01..W08 证据保留，不将 Skip 或模拟模型说成真实云验收。

## 验收

真实 PG/Redis 验证命中、筛选/分页/组织隔离、撤权、TTL、空结果、坏 JSON、断连回源恢复、提交/回滚/AI 确认/Worker 更新失效；并发限流精确上限、用户隔离、429/503 不写消息；两个 SQL 同数据同参数多次 EXPLAIN ANALYZE BUFFERS 中位数、结果一致/分页边界；全部旧门禁 Go race、前端测试/type/build/lint/format、启动器检查。补齐性能文档、测试矩阵、周报、参考答案、运行演示。

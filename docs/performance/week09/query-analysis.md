# 两条真实列表查询优化

位置：`internal/ticket/repository.go:ListTickets` 和 `internal/knowledge/repository.go:List`。

旧写法在 WHERE 使用 `($2 IS NULL OR status=$2)` 或 `($2='' OR status=$2)`。改为仅在存在合法筛选时拼接固定的 `AND status=$n` / `AND priority=$n`。值始终经 pgx 参数发送，用户值不参与 SQL 字符串拼接。无筛选、单筛选、双筛选形成有限几种语句形状。分页和字段协议保持一致。

PostgreSQL 的 prepared statement 可以使用 custom 或 generic plan，默认 auto 根据前几次成本选择；generic 无法针对本次参数把可选 OR 简化。这里既测默认 auto，也显式测 `force_generic_plan` 作为诊断场景，**没有改变生产 plan_cache_mode、禁用顺序扫描或强制索引**。[PG17 PREPARE](https://www.postgresql.org/docs/17/sql-prepare.html)

00014 仅追加 `tickets_org_priority_created_idx(org_id,priority,created_at DESC,id DESC) WHERE deleted_at IS NULL`。优先级筛选先缩小范围，再按现有排序返回；未给所有字段加索引。知识状态列表复用 00007 的索引，无新索引。两个筛选都给时仍由优化器选择现有可用索引，不声称所有查询组合都最优。

原始计划：`docs/testing/evidence/week09/sql-plans.json`。每个组合预热 2 次、测 9 次；EXPLAIN ANALYZE BUFFERS JSON 保存节点估计/实际行数、索引、过滤行、buffer、规划和执行时间。测量自带仪器开销；不能把数据库毫秒数当作端到端 HTTP 延迟。[PG17 EXPLAIN](https://www.postgresql.org/docs/17/using-explain.html)

结果等价验证比较两版 SQL 三页的**完整行文本**；Go 集成测试通过真实 repository 比较所有状态/优先级组合、空结果和多页 ID，并包含时间戳并列与软删。

分页固定 `created_at DESC,id DESC`；相同时间以 ID 唯一打破平局。实验同一数据快照下无重复/遗漏；在用户翻页期间插入/删除数据，OFFSET 仍可能移动边界，不能当作快照分页。深 OFFSET=80000 计划也保存了，数据库仍须跳过前面行。cursor/keyset 分页改变 API，本周未加入。

新增索引占磁盘并增加创建、优先级修改、软删等写入维护成本；00013 版本行也有写放大。本周证明特定读负载改善，未做生产规模写入压测或冷磁盘测试。上线大表创建索引的锁持有时间需另行安排；当前 Up 为本地开发迁移。

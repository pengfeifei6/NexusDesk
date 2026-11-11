# ADR-0004：数据库原子领取与同事务事件

2026-11-10；AI 依据 W04 整周授权作出实施选择。

使用 PostgreSQL READ COMMITTED 下的单条条件 UPDATE RETURNING，而非先读空闲状态再无条件 UPDATE，也不引入 Redis/进程互斥锁。条件包含 org_id、id、status='OPEN'、assignee_id IS NULL、deleted_at IS NULL、expected_version 及 int64 防溢出。

复用 Service.write：5 秒事务 → 在事务内检查 active 成员 → 条件更新 → TICKET_ASSIGNED → COMMIT；独立最多 3 秒 context 回滚。领取不先调用 SELECT FOR UPDATE；竞争 UPDATE 自身获取行锁，等待后重新检查条件。0 行后才按同组织未删除范围查询对象：不存在 404、仍可见 409，查询故障 500。条件中 assignee=NULL 已确定事件 before，before 的 version 使用 expected_version，after 使用 RETURNING，不能以客户端受理人写审计。

事件失败或 COMMIT 失败，不返回成功。继承 W03 clock_timestamp 写入时钟语义。既有六条 migration、状态机、admin 手动指派规则不变；admin 按 W03 仍可代为开始处理，手册 C16 的“只有受理人”不取消这一既有授权。

成员在 tx 内读取但不增加成员行锁：当前没有成员禁用 API，保持 W03 的一致锁顺序与边界。外部 SQL 并发撤权不保证完全线性化；将来增加撤权 API 时统一设计锁顺序并补并发撤权测试。无公平性/排队承诺，无自动重试或幂等键。

验收用多连接真实 PostgreSQL、同时起跑、20 请求×20 轮，另两人竞争。检查唯一成功、赢家身份、OPEN、version+1、恰好一条匹配事件；事件插入与延迟 COMMIT 注入失败必须完全回滚。race 只补充 Go 内存竞争检测。

依据：[PostgreSQL 17 READ COMMITTED](https://www.postgresql.org/docs/17/transaction-iso.html)、[UPDATE RETURNING](https://www.postgresql.org/docs/17/dml-returning.html)。

# 列表缓存的边界与一致性

入口：`ticket.Service.List`、`knowledge.Service.List`；先数据库 Authorize，再验证参数，再 `cache.List`。HTTP Cache-Control 仍是 no-store，浏览器不保存私有响应。缓存只减少列表 SQL 的扫描与反序列化工作，不省掉身份、成员检查与版本点查。

```text
HTTP → JWT身份 → 数据库组织成员授权 → 参数校验 → 查询已提交的PG版本r → 组织/版本/筛选Key
  ├─ Redis命中且组织/数据有效 → 返回列表
  └─ miss / 超时 / 错误 / 坏值 → 带组织条件的SQL → 成功结果
       ├─ 尝试以同一个r写Redis（有界TTL，失败只计数）
       └─ 返回数据库列表
写事务 → 业务行变更 + 版本增加 → COMMIT → 后续读者使用r+1；回滚两者都撤销
```

Key：`nexusdesk:cache:v1:<org>:<tickets|documents>:<revision>:active-members-all:created_at-desc-id-desc:<filters SHA256>`。filters 是服务端验证后的结构，含所有筛选、页码、页大小。当前所有 active member/admin 都可看本组织全部工单和文档，所以可以共享缓存；将来加入“只看自己的工单”等规则时必须同时调整查询、scope 和 key，不能仅改变页面按钮。

Redis JSON envelope 带 org_id，每个条目也校验组织与软删范围；坏 JSON、null、超大值、组织不符都回源。空列表可以缓存；授权失败、数据库失败、详情、审批、下载、RAG 资料和 JWT 不缓存。

## 为什么使用数据库版本

00013 的 `cache_revisions` 和两个触发器在原写事务里增加对应组织/领域版本。版本对其他连接在 COMMIT 后才可见，这是本项目的逻辑失效；不在提交前 DEL，也无需对 Redis 发送可能丢失的失效命令。

| 写路径 | 失效依据 |
|---|---|
| 创建/编辑/指派/领取/状态/软删工单 | tickets 行变更 |
| AI 确认并创建工单 | AI 外层事务中的 tickets INSERT；整个事务成功才可见 |
| 评论/审计追加 | 列表不展示评论/事件，不需失效 |
| 文档上传/PROCESSING/READY/FAILED/删除 | knowledge_documents 行变更 |
| Worker 处理、重试、取消恢复状态 | 同一文档行变化自动覆盖 |
| 索引/任务状态单独变化 | 不在文档列表响应中；相应状态接口继续查库 |
| 事务回滚/未知提交结果 | PG 可见版本与已提交事实一致，无 Redis 补偿判断 |

请求先取得版本 r，之后只读写 r 的 key。若期间写事务提交成 r+1，旧请求最多完成并返回其在途结果，不能污染 r+1。后续请求读取新版本。并发撤权与在途响应仍沿用原项目语义，不宣称完全线性化撤权。

Redis 中断时请求回源；写事务不访问 Redis。恢复后仍读 PG 最新版本，因此恢复的旧 key 不可命中。旧版本靠 TTL 回收，无 KEYS/SCAN/FLUSHDB。备份恢复或人工重建数据库后，应停 API、用新的缓存命名空间或清除**确认属于本项目的 cache 前缀**，避免回退版本对应历史 key；不能清队列 DB。

## 配置与成本

- `CACHE_ENABLED=true`，`CACHE_TTL_SECONDS=30`，实际 TTL 30～36 秒正抖动。
- `REDIS_OPERATION_TIMEOUT_MS=100`，连接/读/写/池等待有界，禁自动重试；GET 失败后正常回源，SET 失败不影响结果。
- `CACHE_MAX_BYTES=1048576`；GETRANGE 限制读取体积，超限列表直接用查询结果。工单 body 较大时可能不入缓存，这是有意的限制。
- API 缓存与限流共用一个 Redis client，最多 10 活跃连接；复用 W07 地址/密码/DB，前缀独立于 Asynq。Worker 无新 Redis 配置。
- 计数 Hits/Misses/Failures/Corrupt/Stores 在内存中并在正常退出日志汇总，不含 key、正文或凭据。
- 成本是每次列表一个版本点查、每次领域写入一个版本行更新。同组织/领域写入会短暂串行，批量导入写放大需另测。
- 未加入 singleflight、分布式锁或断路器；高并发冷缓存仍可能多次回源。现有 PG pool 限制连接，不代表已完成容量规划。

# W07：可靠的异步文档处理

本设计补上 W06 同步解析/索引与 W08 流式问答之间的缺口。工程选择由 AI 根据开发者整周委托补齐；原第八周手册保持不变。

## 一次任务经过哪里

1. 浏览器上传文档，获得 201 和 document_id；再 POST 文档的 `/jobs`，获得 202 和 job_id。
2. API 在 PostgreSQL 提交 knowledge_jobs 后返回，不调用模型，也不在请求内启动后台 goroutine。
3. Worker 每两秒扫描最多 50 个可投递任务；短事务预留 dispatch_seq，事务外向 Redis/Asynq 投递最小消息。
4. Worker 重新从数据库取得任务、执行者和文档，领取租约；事务外提取正文、调用 BGE；短事务发布正文或完整索引。
5. 浏览器每两秒查询任务，最多五分钟；成功时刷新正文和索引，继续使用既有 Search/Ask。

`knowledge_jobs` 同时保存业务状态和待投递意图。PostgreSQL 与 Redis 没有跨库原子事务：发送前崩溃、发送失败或 Redis 丢消息，由 15 秒重新投递窗口补偿。Redis AOF 配置 `appendfsync everysec`，仍不能代替业务幂等与 PostgreSQL 记录。[Redis 持久化说明](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/)。

## 状态与实际执行次数

| 状态 | 含义 | 下一步 |
| --- | --- | --- |
| PENDING | 持久保存，等待领取 | Worker 领取或取消 |
| RUNNING | 有有效租约 | 完成、失败、取消、租约到期 |
| RETRYING | 临时失败，等待 next_run_at | 到期后重新领取 |
| SUCCEEDED | 当前正文与索引已核验 | 不自动重跑 |
| FAILED | 永久错误或三次已用尽 | 创建者/管理员人工重试 |
| CANCELED | 用户取消或文档删除 | 文档仍有效时人工重试 |

每轮最多三次实际领取；第一次失败等待 5 秒，第二次等待 30 秒。模糊的 MODEL_UPSTREAM_ERROR 包含确定性4xx与部分500，本周保守按永久失败，允许修正原因后人工重试；已明确的429/503/超时才自动重试。重复消息和提前到达的消息不能绕过 PostgreSQL 的 attempts/next_run_at。Asynq 另设 MaxRetry=2、Retention=1h，属于传输层预算。[Asynq 重试文档](https://github.com/hibiken/asynq/wiki/Task-Retry)。

人工重试只允许 FAILED/CANCELED，增加 run_version、清零 attempts、保留 total_attempts 和 requested_by，actor_user_id 改为本次实际重试者。重复 Submit 返回原任务，不隐式重试。阶段只有 WAITING/EXTRACTING/INDEXING/DONE，不显示虚构百分比。

## 所有权与恢复

租约 30 秒、每 5 秒续约；续约要求尚未到期。上下文携带 job/org/document/run/actor/token。文档和索引的 task_token 记录本次执行所有权；每次异步写入均在父文档→任务的锁顺序下检查有效租约、版本、当前成员与原文件 hash。同步入口在存在活动异步任务时冲突，不能争抢处理。

取消/租约到期后的清理是单独事务：核对结束的 run/token，只重置它拥有的 PROCESSING/INDEXING，保留 READY。旧 Worker 的独立清理保留原上下文所有权，因此不能覆盖新一轮。没有 token 的旧 W05/W06 遗留中间态不会自动重置。

若索引已经发布但 Worker 在确认任务前崩溃，恢复先验证原文件、正文、提取器、模型、维度、切块版本、当前 index_version 与块数；全部有效则标记 SUCCEEDED，即使已经第三次，也不再调用模型。原文件 SHA 与提取正文 SHA 是两种依据，不能混用。

撤权、软删除、原文件依据变化优先于成功恢复；删除的任务对外 404。取消不能撤销已提交的正文，只阻止后续受保护发布。本设计使用事务内检查，不宣称租约时钟与事务提交瞬间完全线性一致。

## 固定边界

- 队列 `nexusdesk:knowledge:v1`，类型 `knowledge:ingest:v1`；消息只有规范正 int64 的 job_id/org_id/run_version，最大 1 KiB。
- 默认独立 Redis 在 127.0.0.1:6380，DB 6；本机原 6379 已有服务，不使用或清空它。Worker 并发默认 2、范围 1～4。
- Worker 总预算 180 秒、索引 150 秒；原 API 索引仍为 60 秒。正文提取 10 秒、Embedding 20 秒、SQL 读 3 秒/事务 5 秒/清理 3 秒。
- API 启动不 Ping Redis；Worker 启动有界检查 Redis。Worker 使用 LoadWorker，不需要 HTTP/JWT 签名配置。
- 固定 bge-m3:567m / 1024 维、rune-500-80-v1；PDF 仅保存/下载，任务会明确失败 PDF_UNSUPPORTED。
- 只新增迁移 00011 和五个 API；SSE、会话、工具调用、写操作确认留给 W08。

运行见 [演示](demo/g3-jobs-demo.md)，HTTP 见 [契约](api/knowledge-jobs.md)，验收见 [测试说明](testing/week07-jobs.md)。

# W07 任务 API

前缀 `/api/v1/organizations/:orgID`。Bearer JWT 只识别用户，后端实时检查组织成员。所有端点禁止 body 与 query；路径 ID 为规范正 int64。

| 方法 | 相对路径 | 成功结果 |
| --- | --- | --- |
| POST | `/knowledge/documents/:documentID/jobs` | 202，新增或已有 Job |
| GET | `/knowledge/documents/:documentID/jobs` | 200，`data.items`，最近 20 个 |
| GET | `/jobs/:jobID` | 200，Job |
| POST | `/jobs/:jobID/retry` | 200，更新后的 Job |
| POST | `/jobs/:jobID/cancel` | 200，更新后的 Job |

页面先读文档任务列表，再对当前任务展示状态及重试/取消；单任务 GET 供脚本或其他客户端直接读取，页面轮询使用列表接口。

统一 `{ "data": ... }` / `{ "error": {"code", "message", "request_id"} }`，禁用缓存。202 仅证明任务已在数据库接收，Redis 或 Worker 暂停时可长期为 PENDING。

## 字段

Job 的 22 个字段为：id、org_id、document_id、requested_by、actor_user_id、status、phase、run_version、attempts、max_attempts、total_attempts、failure_code、retryable、last_dispatch_error、source_sha256、embedding_model、chunker_version、created_at、updated_at、started_at、completed_at、next_run_at。

ID 与 run_version 是 JSON int64；前端必须无损读成十进制字符串。attempts 是本轮次数，total_attempts 是所有轮次数；retryable 描述本次失败分类，并不赋予额外权限。时间为 RFC3339，尚未开始/完成为 null。last_dispatch_error 只允许 QUEUE_UNAVAILABLE；不会返回 Redis 原始错误。lease_token、lease_until、dispatch_seq、原文件私有路径不公开。

## 授权和冲突

同组织 active admin/member 可提交和读取；只有 requested_by 或当前 admin 可重试、取消。重试者记录到 actor_user_id，但不改 requested_by。未登录 401，非成员/disabled/跨组织/已删除 404，同组织普通成员修改别人任务 403。提交不接收 user_id/role/path/source 等字段。

| 状态码 | code | 处理 |
| --- | --- | --- |
| 400 | INVALID_REQUEST | 去除 body/query，检查规范 ID |
| 401 | UNAUTHENTICATED | 重新登录 |
| 403 | FORBIDDEN | 仅创建者或管理员能操作 |
| 404 | NOT_FOUND | 不再展示原保护内容 |
| 409 | JOB_STATE_CONFLICT | 刷新；仅 FAILED/CANCELED 可重试 |
| 409 | DOCUMENT_STATE_CONFLICT | 文档正在同步处理或状态不允许 |
| 503 | ASYNC_DISABLED | 文档仍保留，启用后再提交 |
| 500 | INTERNAL_ERROR | 安全固定错误，不输出数据库详情 |

`ASYNC_ENABLED=false` 禁止 Submit/Retry；Get/List/Cancel 仍可用。现有终态任务重复提交保持终态。取消不能回滚已提交正文或索引；成功任务不能人工重试。案例见 [week07.http](../../requests/week07.http)。

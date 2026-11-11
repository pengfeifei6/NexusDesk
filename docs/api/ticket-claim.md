# W04 原子领取 API

`POST /api/v1/organizations/:orgID/tickets/:ticketID/claim`，沿用 W02 Bearer 认证及 W03 组织隔离。实现由 AI 按用户整周委托完成，个人理解另记。

只接 `{"expected_version":0}`，必需、非负十进制 JSON int64 整数；禁止 query、未知/重复字段、客户端 assignee_id/actor_id/org_id。沿用 64 KiB、严格 Unicode 与 application/json 限制。

active admin/member 均可领取同组织未删除、OPEN、assignee_id IS NULL 的工单。受理人为可信当前用户；成功保持 OPEN、version 恰好 +1，写一条 TICKET_ASSIGNED，同事务提交后返回 200 和 W03 的完整十一字段工单 DTO。不是自动开始处理，继续使用 `/transition`。

| HTTP | code | 条件 |
| --- | --- | --- |
| 401 | UNAUTHENTICATED | 身份无效 |
| 404 | NOT_FOUND | 非成员/disabled/错组织/不存在/软删除 |
| 409 | TICKET_CLAIM_CONFLICT | 已指派、非 OPEN、旧版本、版本达到 int64 上限；同人重复也冲突 |
| 400 | INVALID_REQUEST | 请求字段/类型/值不合法 |
| 413/415 | REQUEST_TOO_LARGE / UNSUPPORTED_MEDIA_TYPE | 请求体/媒体类型 |
| 500 | INTERNAL_ERROR | 数据库或提交失败，静态安全提示 |

409 固定 message 为 `ticket already assigned or version changed`。其他错误沿用 W03；不返回他组织受理人或对象存在性。成功/错误均 no-store。前端收到 409 后重新 GET，不自动重新领取、不用旧数据覆盖。没有请求级幂等或网络断开后的提交结果保证。

详细事务选择见 [ADR-0004](../decisions/0004-atomic-ticket-claim.md)。

# 39 个业务 API 总索引

当前完整字段、请求/响应示例与错误见[中文完整API文档](../api/NexusDesk_API完整文档.md)，机器契约见[OpenAPI3.1](../api/openapi.json)。本页保留快速索引。

统一前缀 `/api/v1`，下表 O 表示 `/organizations/:orgID`，T 表示 `O/tickets`，D 表示 `O/knowledge/documents`，A 表示 `O/ai`。仅计规范路径；自动尾斜杠重定向不另计。

除注册/登录外都需要 Bearer 身份。组织接口逐次检查 active 关系；AI 还限制会话 owner。401/403/404 的顺序、输入约束与完整返回以各领域契约和代码为准。旧文档允许空 request_id 的描述已由 W11 服务端请求 ID 覆盖。

| # | 方法 | 路径 | 页面/用途 |
|---:|---|---|---|
| 1 | POST | /auth/register | 注册 |
| 2 | POST | /auth/login | 登录 |
| 3 | GET | /me | 当前会话、账号编号 |
| 4 | POST | /organizations | 创建组织 |
| 5 | GET | /organizations | 组织选择分页 |
| 6 | GET | O/members | 成员列表、指派候选 |
| 7 | POST | O/members | 管理员添加已注册成员 |
| 8 | POST | T | 创建工单 |
| 9 | GET | T | 工单筛选分页 |
| 10 | GET | T/:ticketID | 详情 |
| 11 | PATCH | T/:ticketID | 编辑、指派、解除指派 |
| 12 | DELETE | T/:ticketID | 管理员软删 |
| 13 | POST | T/:ticketID/transition | 状态流转 |
| 14 | POST | T/:ticketID/comments | 评论 |
| 15 | GET | T/:ticketID/comments | 评论分页 |
| 16 | GET | T/:ticketID/events | 事件分页 |
| 17 | POST | T/:ticketID/claim | 原子领取 |
| 18 | POST | D | 上传 |
| 19 | GET | D | 文档列表 |
| 20 | GET | D/:documentID | 文档详情 |
| 21 | POST | D/:documentID/process | 同步提取 TXT/MD |
| 22 | GET | D/:documentID/text | 正文预览 |
| 23 | GET | D/:documentID/download | 授权下载 |
| 24 | DELETE | D/:documentID | 上传者/admin 删除 |
| 25 | POST | D/:documentID/index | 同步索引 |
| 26 | GET | D/:documentID/index | 索引状态 |
| 27 | POST | O/knowledge/search | 向量或混合检索 |
| 28 | POST | O/knowledge/ask | 引用问答 |
| 29 | POST | D/:documentID/jobs | 创建/返回异步任务 |
| 30 | GET | D/:documentID/jobs | 文档最近任务 |
| 31 | GET | O/jobs/:jobID | 查询任务 |
| 32 | POST | O/jobs/:jobID/retry | 人工重试 |
| 33 | POST | O/jobs/:jobID/cancel | 取消任务 |
| 34 | POST | A/conversations | 创建对话 |
| 35 | GET | A/conversations | 本人对话最新 50 条 |
| 36 | GET | A/conversations/:id/messages | 消息与 action 最新各 100 条 |
| 37 | POST | A/conversations/:id/messages | 消息及 SSE 输出 |
| 38 | POST | A/tool-actions/:id/confirm | 确认受限写操作 |
| 39 | POST | A/tool-actions/:id/reject | 拒绝提议 |

详细契约：[认证组织](../api/auth-organization.md)、[工单](../api/ticket.md)、[文档](../api/knowledge-documents.md)、[RAG](../api/rag.md)、[任务](../api/knowledge-jobs.md)、[AI](../api/ai-conversation.md)。

另有公开存活检查 `GET /healthz`。可选 API/Worker 诊断端口有 `/metrics` 与限定 pprof/trace 路径，不在业务端口开放，详见[观测说明](../observability.md)。

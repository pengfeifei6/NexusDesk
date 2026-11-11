# NexusDesk API 文档

当前统一入口：[中文完整API手册](NexusDesk_API完整文档.md) · [OpenAPI 3.1 JSON](openapi.json) · [39个规范业务操作索引](../release/api-index.md)。另列公开`GET /healthz`，诊断端口不计业务API。全部示例为合成数据。

完整手册按实际Handler/DTO/Service核对认证、active成员/owner权限、严格JSON与int64、分页、工单版本、上传下载、异步Job、RAG/usage与AI SSE/人工确认。后端协议仍为JSON int64，JavaScript客户端必须无损处理；文档没有改动后端。

维护时运行：

```sh
python3 scripts/check_api_reference.py
```

检查器仅用Python标准库、只读解析路由和总索引；核对39个操作、schema引用、operationId、路径参数、请求媒体和示例结构，并逐操作校验Markdown/OpenAPI示例一致、状态变化、版本、非空文件及确认字段等语义。不是运行时HTTP验收或完整第三方OpenAPI验证。

原领域文档保留周次历史：[认证组织](auth-organization.md)、[工单](ticket.md)、[领取](ticket-claim.md)、[文档](knowledge-documents.md)、[RAG](rag.md)、[异步任务](knowledge-jobs.md)、[AI对话](ai-conversation.md)。历史文档中的空request_id或阶段性未实现范围已由当前完整手册和代码更新，不应据此实现旧契约。

# W08 AI 对话 API

所有路径前缀 `/api/v1/organizations/{orgID}/ai`，Bearer 认证，当前 active 成员且只有会话创建者可读写。管理员无越过 owner 的权限。UUID 为小写规范字符串；既有 org/user/ticket/chunk ID 为 JSON bigint，前端无损字符串。

| 方法 | 相对路径 | 请求 | data |
|---|---|---|---|
| POST | `/conversations` | `{"title":"索引排查"}` | Conversation，201 |
| GET | `/conversations` | 无 query/body | Conversation[] 最新50条 |
| GET | `/conversations/{id}/messages` | 无 query/body | `{messages: Message[], actions: Action[]}` 最新各100条，按时间升序 |
| POST | `/conversations/{id}/messages` | `{"content":"问题","client_request_id":"客户端 UUID"}` | SSE，200 |
| POST | `/tool-actions/{id}/confirm` | 空 body，无 query | 持久化 Action |
| POST | `/tool-actions/{id}/reject` | 空 body，无 query | 持久化 Action |

title 去首尾空白后1～100字符，content1～4000字符且≤16KiB。JSON严格拒绝未知/重复字段、null、无效Unicode；发送请求体≤32KiB。一个请求唯一关联USER+ASSISTANT两行。相同请求ID或不同内容复用ID均409；重新读历史可查看已保存结果，不自动重放。每会话同时只允许一个生成，整个API实例最多两个生成。

## 返回字段

Conversation: `id, org_id, owner_user_id, title, status(ACTIVE), created_at, updated_at`。

Message: `id, conversation_id, client_request_id, role(USER/ASSISTANT), content, status(STREAMING/COMPLETED/FAILED/INTERRUPTED), model_name, org_id, citations: Source[], input_tokens, output_tokens, created_at, completed_at`。

Source: 与 W06 相同的 `chunk_id, document_id, chunk_index, source_name, score, excerpt, start_rune, end_rune`。是后端本轮实际检索依据，不是模型编造的引用。

Action: `id, conversation_id, assistant_message_id, tool_name(propose_create_ticket), status(PROPOSED/EXECUTED/REJECTED/EXPIRED), org_id, proposed_by_user_id, arguments:{title,body,priority}, expires_at, created_at, confirmed_by_user_id, ticket_id, confirmed_at`。priority1低/2普通/3高。最后三项未执行时null。卡片须展示完整arguments、组织、提出者与有效期。15分钟有效，确认只读取数据库已存参数。重复确认返回同一ticket_id。停止生成不会撤销已经保存的提议，可单独拒绝。

## SSE 契约

POST fetch + ReadableStream；每个事件由 `event: 名称\ndata: JSON\n\n` 组成。UTF8网络分块与事件边界无对应关系。心跳 `: ping\n\n` 每10秒。Content-Type为text/event-stream、no-cache/no-store/no-transform、X-Accel-Buffering:no。整轮60秒，适配器单请求35秒；页面75秒保护超时。

| event | data |
|---|---|
| message_start | `{message_id,user_message_id,conversation_id,client_request_id}` |
| content_delta | `{message_id,delta}` |
| citation | `{message_id,source:Source}` |
| tool_approval_required | `{message_id,action:Action}` |
| message_end | `{message_id,status:"COMPLETED",usage:{input_tokens,output_tokens}}` |
| error | `{code,message,retryable}` |

持久化完成后才发message_end。error后关闭，之后从历史读取FAILED/INTERRUPTED。缺少message_end且无error的EOF视为网络中断，不能显示成功。AbortController停止会取消上游；切换组织/会话/登录同样取消并忽略旧事件。失败不能自动发送新request_id重试以免重复轮次。历史STREAMING可在120秒后读取时收敛为INTERRUPTED，覆盖崩溃/数据库暂时不可用时的清理失败。

## 错误

流开始前 `{error:{code,message,request_id}}`；400 INVALID_REQUEST、401 UNAUTHENTICATED、403 FORBIDDEN、404 NOT_FOUND、409 AI_CONFLICT、410 AI_EXPIRED、422 AI_LIMIT、429 AI_BUSY、502 MODEL_INVALID_RESPONSE、503 MODEL_UNAVAILABLE、504 MODEL_TIMEOUT、500 INTERNAL_ERROR。流开始后只发error事件。前端按code显示固定中文，禁止原样显示上游错误。

## 记录与引用的边界

消息只保存最终/失败时已经发出的有界内容；不逐token写库。既有历史是会话owner的记录，删除源文档不会擦除已保存对话文字。为避免把历史检索当成新证据，带引用的旧助手内容不再送入后续模型；需要资料时重新检索。当前轮在发布文本前复核成员、工单版本和文档索引依据。没有断流自动续传或历史归档API。

多轮历史保留已处理的ASSISTANT轮次标记；带引用/未完成的旧助手内容替换为服务端固定占位说明，既不继续发送旧私密片段，也不把旧USER消息误当待处理任务。模型只处理最后一条用户请求。
# W09 补充：发送配额

只有 `POST /api/v1/organizations/:orgID/ai/conversations/:conversationID/messages` 增加时间窗口配额。当前身份、组织成员、会话 owner 与输入验证通过后检查，再进入 StartTurn/SSE。默认同 user+org 每60秒10次，由首次接受请求开始窗口；被配额接受后发生并发/业务冲突也可能计次。确认、拒绝和历史不消耗此额度。

- 超额：HTTP429，`error.code=RATE_LIMITED`，`Retry-After` 为向上取整的正整数秒。
- Redis故障：HTTP503，`error.code=RATE_LIMIT_UNAVAILABLE`，`Retry-After: 1`；没有创建本轮消息/调用模型。
- 两者均为普通 JSON 错误 envelope，保留 Cache-Control:no-store；未开始 SSE。客户端显示安全提示并保留当前未发送输入，不自动重试。
- 配置：`AI_RATE_LIMIT_ENABLED=true`、`AI_RATE_LIMIT_REQUESTS=10`、`AI_RATE_LIMIT_WINDOW_SECONDS=60`；复用 W07 Redis 与 API100ms超时。

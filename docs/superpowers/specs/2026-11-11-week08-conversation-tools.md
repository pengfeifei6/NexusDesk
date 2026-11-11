# W08 实现设计

依据第八周手册与用户整周委托。保持已有 DeepSeek、W01～W07 代码与未提交修改；AI 协助实现，个人掌握须自行复习。本设计记录实现决定，不是完成证明。

## 决定

- 组织路径 `/api/v1/organizations/:orgID/ai`，新增六个接口：POST/GET conversations，GET/POST conversations/:conversationID/messages，POST tool-actions/:actionID/confirm、reject。身份由 RequireUser 和当前成员关系取得。只有对话创建者可访问，组织管理员不自动获得访问权。
- 三个新表，通过 00012 追加。UUID 用于会话、消息、提议；既有 bigint 业务 ID 不变。一个请求生成 USER、ASSISTANT 两行，唯一键 `(conversation_id, client_request_id, role)`；重复请求返回 409，由页面重新获取历史，不再次调用模型。每对话只允许一个 STREAMING 助手消息。开始新轮或读历史时，超过 120 秒的遗留 STREAMING 收敛 INTERRUPTED；正常整轮预算 60 秒。
- POST fetch SSE，JSON 事件 message_start/content_delta/citation/tool_approval_required/message_end/error。心跳 10 秒，单 writer 写入，逐事件 Flush；流前错误普通 HTTP，流后安全 error；取消通过请求 Context 传递。完成状态写库成功才发送 message_end。断连清理独立 3 秒且有状态条件；清理失败由 120 秒遗留回收兜底。
- DeepSeek 固定官方 HTTPS，当前配置模型；纯本地配置提供 Ollama 原生流式适配器。两者统一 model.ChatModel，不引入 SDK/framework。真实请求有限时、大小、输出及并发，禁代理和重定向。工具参数碎片积累到结束后才提供 ToolCallReady。
- 每轮最多 3 个工具轮次、总计 4 次顺序调用、1 个建单提议，最多 4 次模型请求；历史最近 20 条、序列化输入上限 24 KiB，输出总计 16 KiB。提示和结果不可信；工具仅 search_knowledge、get_ticket、propose_create_ticket。不提供 create_ticket 或任意 URL/SQL/shell/file 能力。
- 查询复用 RAG.Search 与 Ticket.Get；参数严格拒绝未知/重复字段、损坏 Unicode、错误类型/范围。Actor 只来自服务端。只发送授权检索结果，后端构造来源事件，不相信模型自报的来源。引用标注为本轮检索依据；不声称逐句事实验证。工具执行和答案发布复查成员/来源。
- 提议字段沿用工单 `title/body/priority`（priority 为 1/2/3）；规范化后 SHA256。15 分钟有效；只允许会话本人确认/拒绝。确认请求空 body，不可重新提交参数。动作 FOR UPDATE；在同一 pgx.Tx 重新授权、通过 TicketService 创建及写 TICKET_CREATED（含 AI proposal 关联）、更新 EXECUTED/ticket_id。禁止复制建单 SQL。重复确认返回同一提议与 ticket_id，过期/拒绝/摘要不匹配不能执行；错误回滚整个事务。
- 前端第九页“AI 对话”：选择组织、会话列表/新建、历史、逐段文本、停止、来源、确认卡片及工单链接；卡片明确组织/创建者/有效期/标题/正文/优先级。纯文本渲染。组织/会话/登录切换取消并清空旧数据，401 不影响新登录会话。
- 已保存消息是创建者的组织内对话记录；删除原文档不自动擦除已保存的历史文字。后续模型历史不继续发送失效来源对应的旧助手内容；当前来源再次验证后发布。完整历史分页/归档、断流续传、公开部署限流及异步 AI 运行不是本周范围。

## 内部契约

`internal/ai` 中 Store 持久化/确认；ChatService、Handler、Runner 编排由后续任务添加。

```go
type Actor struct { UserID, OrgID int64 }
type Conversation struct { ID string; OrgID, OwnerUserID int64; Title, Status string; CreatedAt, UpdatedAt time.Time }
type Message struct { ID, ConversationID, ClientRequestID, Role, Content, Status, ModelName string; OrgID int64; Citations []rag.Source; InputTokens, OutputTokens int64; CreatedAt time.Time; CompletedAt *time.Time }
type TicketArguments struct { Title string `json:"title"`; Body string `json:"body"`; Priority int16 `json:"priority"` }
type Action struct { ID, ConversationID, AssistantMessageID, ToolName, Status string; OrgID, ProposedByUserID int64; Arguments TicketArguments; ExpiresAt, CreatedAt time.Time; ConfirmedByUserID, TicketID *int64; ConfirmedAt *time.Time }
type History struct { Messages []Message `json:"messages"`; Actions []Action `json:"actions"` }
type Turn struct { User, Assistant Message; History []Message }
NewStore(pool *pgxpool.Pool, tickets *ticket.Service) *Store
(*Store).Authorize(ctx context.Context, actor Actor) error
(*Store).CreateConversation(ctx context.Context, actor Actor, title string) (Conversation,error)
(*Store).ListConversations(ctx context.Context, actor Actor) ([]Conversation,error) // latest 50
(*Store).History(ctx context.Context, actor Actor, conversationID string) (History,error) // latest 100 ordered
(*Store).StartTurn(ctx context.Context, actor Actor, conversationID, requestID, content, modelName string) (Turn,error)
(*Store).FinishTurn(ctx context.Context, actor Actor, messageID, status, content string, citations []rag.Source, inputTokens, outputTokens int64) error
(*Store).Propose(ctx context.Context, actor Actor, conversationID, assistantID string, args TicketArguments) (Action,error)
(*Store).Confirm(ctx context.Context, actor Actor, actionID string) (Action,error)
(*Store).Reject(ctx context.Context, actor Actor, actionID string) (Action,error)
NormalizeArguments(TicketArguments) (TicketArguments,error)
ValidUUID(string) bool
Code(error) string // safe codes only; NewError(code) error
```

所有公共结构 JSON 使用 snake_case。错误码 INVALID_REQUEST / UNAUTHENTICATED / NOT_FOUND / FORBIDDEN / AI_CONFLICT / AI_EXPIRED / AI_BUSY / AI_LIMIT / MODEL_UNAVAILABLE / MODEL_INVALID_RESPONSE / MODEL_TIMEOUT / INTERNAL_ERROR。context 取消单独处理。完成、失败与中断均持久化有界已输出文本。

## 验收

实际 PG 上验证组织/owner 隔离、去重、确认锁、摘要和回滚；Go mock/HTTP流验证网络错误和取消；Vue/Chromium 验证跨chunk UTF8、安全渲染、停止、卡片、重复确认。隔离库覆盖 W01～W07 回归。真实 DeepSeek/BGE 只使用合成夹具；证据放 docs/testing/evidence/week08，运行说明与 T01～T35、复习答案、ADR、录屏分别交付。日常数据库仅备份后 Up 到 12，绝不 Down 或删卷。

## 上游依据

2026-11-11 核对 [DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/) 与 [Tool Calls](https://api-docs.deepseek.com/guides/tool_calls/)；本项目只用原生函数工具和非思考文本流。

# NexusDesk API 完整文档

更新时间：2026-11-11。本文由 AI 按用户委托，根据当前 Handler、DTO、Service、Repository 和路由只读核对编写；未修改后端。覆盖 **39 个规范业务操作**，另列健康检查。旧周文档保留历史语境，尤其其中空 request_id、未实现Worker/SSE等表述不代表当前实现。

机器契约：[OpenAPI 3.1 JSON](openapi.json)。索引：[39业务API](../release/api-index.md)。所有账号、编号、时间、内容和 Token 占位均为合成示例，不是运行结果或可用凭据。

## 接入与共同契约

- 本地示例根地址 `http://127.0.0.1:8080`；业务前缀 `/api/v1`。按实际回环监听端口调整，不将示例视为公网部署配置。
- 仅注册、登录及 `/healthz` 匿名。其他接口发送**单个** `Authorization: Bearer <ACCESS_TOKEN>`；scheme不区分大小写，中间恰好一个ASCII空格，Token非空且≤8KiB。拒绝重复头、逗号或额外空白。Token为HS256、仅确认用户，必需sub/iss/aud/iat/exp，当前版本TTL固定900秒（JWT_TTL_MINUTES必须为15），无刷新/退出/撤销API。sub为规范正int64字符串；iat/exp/可选nbf仅接受int64十进制整数秒（不接受字符串/小数/指数/null），零宽限，必需issuer/audience各最多256字节。有效Token对应用户已删除也401。
- 组织资源实时检查数据库active关系；仅 `admin/member`，不支持support。非成员、disabled、组织不存在统一404；跨组织、父对象软删/不存在统一404。已在可见组织但动作权限不足403。AI进一步限定会话owner，admin不能越过owner；目标用户不存在的`USER_NOT_FOUND`只向已授权添加成员的admin披露。注册重复邮箱409是已知枚举取舍。
- 认证→orgID格式→active组织关系→对象/动作权限→业务输入，是主要顺序；目标ID格式和对象加载顺序按具体Handler执行，不保证所有输入错误都优先400。不要把统一404解释成对象确实不存在。成员添加先授权再解析目标。权限来自数据库，不能传入actor、owner、role来替代授权。
- 普通成功 `{"data":...}`；列表是否带items由下文确定。错误 `{"error":{"code":"INVALID_REQUEST","message":"invalid request","request_id":"0123456789abcdef0123456789abcdef"}}`。message为安全固定文本；AI Handler常用code本身，共享认证/JSON解析仍用英文文案，客户端按code处理，不直接展示上游原文。401受保护响应带`WWW-Authenticate: Bearer`。每次请求服务端生成32位小写hex `X-Request-ID`，错误JSON和日志使用同值；不沿用客户端ID。普通响应`Cache-Control: no-store`。204删除无JSON响应体；下载和SSE有独立媒体。
- JSON写请求为`application/json`，允许合法charset参数。单个对象，严格字段名/大小写；拒绝未知/重复字段、尾随第二个值、缺必填、错误类型、无效UTF-8及未配对UTF-16转义。除明确nullable字段外null不合法。16KiB用于认证/组织/成员/RAG，64KiB用于工单，32KiB用于AI，按实际字节限制；超限413，媒体不符415。multipart有独立错误码/边界。
- `GET`无非空body。无输入的POST（process/index/jobs/retry/cancel/confirm/reject）也必须空body；不要发送`{}`。除列出的分页筛选外query禁止。**实现差异**：register/login/createOrganization/addMember未主动拒绝额外query（忽略且不使用）；客户端仍应不发送。不是所有POST都具有相同query行为。
- ID/version/token计数等Go int64在线上为**JSON整数**：正ID为1..9223372036854775807，version/expected_version等非负值为0..9223372036854775807。不得改发字符串；整数输入不接受小数、指数或null。JS需无损JSON解析并在内存保留规范十进制字符串，再以无损JSON整数序列化发送；先`JSON.parse`成Number再转字符串已经可能丢精度。OpenAPI `format:int64`并不保证客户端生成器无损。
- 路径ID为十进制。Jobs及AI的orgID要求规范形式（拒绝前导零/正号）；其他领域当前数字路径解析允许前导零，客户端统一发送规范正值。AI conversation/action/client_request_id为36字符小写UUID格式，非int64；实现不强制版本位。时间UTC RFC3339，可带小数秒。
- 字符上限按Unicode码点（Go rune），不是UTF-16单元；有字节限制时另列。多数名称/正文TrimSpace后校验有效UTF-8、禁止NUL；password保持原文且允许NUL。

## 分页、排序与重试

真正分页的组织、成员、工单、评论、事件、文档接口默认 `page=1&page_size=20`，page_size为1..100。仅ASCII数字、参数唯一且非空，拒绝未知参数/溢出；`(page-1)*page_size`也不得溢出int64。返回`data.items/page/page_size`，空items为`[]`，**没有total**。组织id ASC，成员user_id ASC；工单/文档created_at DESC,id DESC；评论/事件created_at ASC,id ASC。OFFSET深页和并发写入可能造成跨页变化，不提供跨请求快照。

Jobs最近20条、AI对话最近50条、AI消息/actions各最近100条是**固定上限的最近记录**，不接收page/page_size，无完整历史归档/游标API；对话data直接为数组，Jobs为data.items，历史为data.messages/actions。

除Job业务去重、Action重复同向决策外没有通用幂等键。普通创建、评论、组织请求重复可产生多条；版本冲突409刷新再由用户决定，不自动重放。连接断开/提交结果不明时，HTTP失败不能证明没有持久化。AI client_request_id去重返回409而非重放SSE。

## 工单状态与授权

| 操作 | 权限/状态 |
| --- | --- |
| 创建/读取/评论 | active member/admin；评论CLOSED时409 |
| 编辑title/body/priority | OPEN且创建者或admin；必须实际变化 |
| 指派/解除 | 仅admin且OPEN；assignee_id为当前active成员正ID或null |
| 领取 | 任意active成员将OPEN且未分配工单领取给本人；仍OPEN |
| OPEN→IN_PROGRESS | 当前active受理人或admin，必须有active受理人 |
| IN_PROGRESS→RESOLVED | 当前active受理人或admin，必须有active受理人 |
| RESOLVED→IN_PROGRESS | 仅admin，必须有active受理人 |
| RESOLVED→CLOSED | 仅admin |
| 其他流转（含同状态、CLOSED重开） | 409 CONFLICT |
| 软删除 | 仅admin、任意状态；无expected_version |

工单创建version=0；编辑/指派/领取/状态变化/软删使version+1，评论不增加version。PATCH/transition/claim都要求expected_version；领取冲突使用`TICKET_CLAIM_CONFLICT`，其余状态/版本冲突使用`CONFLICT`。title1..200、body1..10000、评论content1..5000码点，均TrimSpace后非空无NUL。priority1低/2普通/3高，创建默认2。当前version达到int64上限时需要递增的操作409，避免溢出；入参落在schema范围不保证状态/权限允许写入。assignee_id缺省不修改，null解除；仅admin可出现该字段。事件快照按类型给出title/priority/assignee_id/status/version、body_changed、deleted、comment_id或AI创建时的ai_proposal_id，不包含正文或评论原文。

## 文档上传、下载与处理

multipart/form-data必须恰好一个部分，`Content-Disposition: form-data; name="file"; filename="example.md"`仅这两个参数；不接受其他字段/第二部分。任何`Content-Transfer-Encoding`（即使空值）都400。客户端MIME不作为类型或路径依据。默认文件5242880字节（5MiB），完整请求8388608字节（8MiB），流式计数，超限`DOCUMENT_TOO_LARGE`。

文件名有效UTF-8、1..200字节、简单名称，无斜杠/反斜杠/控制字符/首尾空白，不能为`.`/`..`或仅扩展名。仅.txt/.md/.pdf（后缀大小写不敏感）。TXT/MD必须有效UTF-8、除TAB/CR/LF外无控制字符，不得伪装PDF/ZIP/ELF/PNG。空文件400；纯空白可上传但提取422 EMPTY_TEXT。PDF必须有受支持的`%PDF-1.x`或`%PDF-2.x`头；仅支持上传/下载，处理422 PDF_UNSUPPORTED，头检测不等于完整解析或病毒扫描。

UPLOADED/FAILED可同步处理；PROCESSING/READY重复409。TXT/MD去首UTF-8 BOM、CRLF/CR归一LF，其余正文保留；默认10秒、正文最多1048576UTF-8字节，extracted_chars计码点。正文与READY同事务；失败尽力写FAILED，进程崩溃或清理失败可留下中间状态，不能任意抢占无租约同步处理。READY才可取text。文档READY与索引READY独立。

下载200为原始字节，`application/octet-stream`、安全attachment文件名、Content-Length、nosniff、no-store；必须授权fetch，不提供静态URL或Range契约。传输中断可能无法再返回JSON，客户端需校验完整下载。删除仅上传者/admin，软删任意状态且隐藏子资源；原文件/正文物理保留，不提供恢复或物理删除API。

```sh
curl -H 'Authorization: Bearer <ACCESS_TOKEN>' -F 'file=@example.md' \
  http://127.0.0.1:8080/api/v1/organizations/201/knowledge/documents
curl -H 'Authorization: Bearer <ACCESS_TOKEN>' --output example.md \
  http://127.0.0.1:8080/api/v1/organizations/201/knowledge/documents/401/download
```

## 索引、RAG与用量

索引模型`bge-m3:567m`、1024维，chunker=`rune-500-80-v1`，500rune窗口/80重叠，最多200块。当前正文hash、模型、chunker相同的READY复用；新执行升index_version，INDEXING冲突。source_content_sha256是提取正文hash，Document.sha256_hex/Job.source_sha256是原文件hash，不可混用。索引ABSENT版本/块数0、hash空、failure_code及时间null。

query TrimSpace后1..1000码点且≤4000字节、无NUL；top_k为整数1..10，默认5。retrieval_mode省略vector，只允许精确vector/hybrid，显式空/null非法。vector按余弦距离升序/chunk_id升序；hybrid向量/词项各最多20候选，RRF Σ1/(60+rank)，rank从1开始，融合分降序/chunk_id升序取top_k。词项为小写ASCII标识符和相邻汉字bigram，按出现顺序去重最多32项，literal substring匹配/不同命中词数排序，不是BM25。无词项命中保留向量路线。两路都应用最低余弦阈值（默认0.40）与组织、软删、正文hash、当前索引版本/模型/chunker/READY过滤。Source.score仍余弦相似度，hybrid额外fusion_score，非准确率。excerpt为切块原文，start_rune/end_rune是规范正文的闭开区间。

Ask只引用本轮真实检索块；无证据固定`知识库未检索到足够依据。`、sources=[]、insufficient_evidence=true，不调用生成器。有效答案至少一个合法引用；生成前后复核所有召回输入（包括未引用项），来源变化409 INDEX_SOURCE_CHANGED，不返回过期答案。纯文本渲染模型/摘录，知识内容不是可执行指令。

RAG Usage字段固定：known、model_called、prompt_tokens、completion_tokens、total_tokens、cache_hit_tokens、cache_miss_tokens。计数非负int64或null，不按字符估算。known仅供应商可信prompt/completion/total且相加一致时true；缓存分项可缺失为null；畸形/缺用量不使合法答案失败，known=false且全部计数null。model_called仅指生成器，不包含embedding。无召回known=true/model_called=false且全部计数0；模型拒答仍记实际调用。timings是embedding_ms/search_ms/generation_ms/total_ms墙钟毫秒；total包含授权/复核，不必等于前三项和，未生成generation_ms=0。

索引60秒/Search25秒/Ask60秒，embedding20秒/批最多8，chat35秒；模型并发2，无自动重试。RAG关闭新模型操作503，旧业务和索引状态可读。默认本地Ollama；已支持生成模型白名单qwen3.5:35b/deepseek-flash/deepseek-v4-pro，模型名以实际响应为准。模型上下文与字节预算不等于精确token计费。

## 异步Job生命周期

202仅证明数据库接收，不证明入Redis或Worker成功。唯一键由文档/原文件hash/提取器utf8-v1/embedding/chunker确定，重复Submit返回已有Job，**终态不自动重开**。status=PENDING/RUNNING/RETRYING/SUCCEEDED/FAILED/CANCELED，phase=WAITING/EXTRACTING/INDEXING/DONE；成功phase=DONE。max_attempts固定3，attempts为当前run，total_attempts跨run累加。自动重试只对已识别的可重试失败，5/30秒退避；泛化上游错误保守永久失败。

所有active成员可提交/读；仅requested_by或admin可重试/取消。人工重试只允许FAILED/CANCELED，run_version+1、attempts=0、保留requested_by/total_attempts，actor_user_id记录当前重试者；retryable是分类信息，不是额外授权。取消仅PENDING/RUNNING/RETRYING，不回滚已有正文/索引。ASYNC关闭Submit/Retry503，Get/List/Cancel仍可用。last_dispatch_error只公开null或QUEUE_UNAVAILABLE，无原始Redis错误；lease_token/租约/私有路径不公开。默认Worker180秒、租约30秒/心跳5秒，恢复由持久状态约束，不以重复消息次数冒充实际执行次数。

## AI对话、SSE与人工确认

AI对话和历史仅owner可见，即使其他成员是admin也404。title TrimSpace后1..100码点；content1..4000且原文≤16KiB，无NUL，请求JSON≤32KiB。每轮一对USER+ASSISTANT，同会话client_request_id重复（含相同内容）409，不提供断流续传或幂等重放。每会话同时一轮、实例最多两轮，超并发429 AI_BUSY。

发送顺序：认证/组织/owner/输入preflight→Redis原子固定窗口→StartTurn事务→SSE/模型。默认user+org从首次接受起10次/60秒，接受后再遇并发或业务冲突可能已计次，不退款/自动重试；历史/确认/拒绝不计额度。超限429 RATE_LIMITED + Retry-After正整数秒；Redis故障503 RATE_LIMIT_UNAVAILABLE + Retry-After:1，保守拒绝，未创建本轮消息或调用模型。

通过POST fetch/ReadableStream读取`text/event-stream; charset=utf-8`。UTF-8字节块不等于字符/事件边界；帧为`event: 名称\ndata: JSON\n\n`，每10秒`: ping\n\n`注释心跳。头为no-cache,no-store,no-transform和X-Accel-Buffering:no。无SSE id、Last-Event-ID或重连恢复契约。整轮60秒，适配器单次35秒；每次写5秒截止。runner最多4轮模型调用、总计4次工具调用、本次用户轮次最多1个工单提议；单次序列化输入≤24KiB，累计输出≤16KiB，单工具结果≤16KiB，超预算AI_LIMIT。

| event | JSON data schema | 意义 |
| --- | --- | --- |
| message_start | SSEMessageStart | 已创建两条消息，关联客户端请求 |
| content_delta | SSEContentDelta | 增量纯文本 |
| citation | SSECitation | 服务端实际检索并复核的来源 |
| tool_approval_required | SSEToolApproval | 已保存完整提议，尚未创建工单 |
| message_end | SSEMessageEnd | 持久化完成后才发送COMPLETED及AIUsage |
| error | SSEError | 流内安全错误；随后关闭 |

流开始前是常规JSON错误与HTTP状态；开始后HTTP已经200，只能发error事件，不再有request_id错误信封。retryable仅AI_BUSY/MODEL_UNAVAILABLE/MODEL_TIMEOUT为true，不表示可自动重放消息。没有message_end的EOF不能视为成功；error/断流后读历史查看FAILED/INTERRUPTED。Abort取消上游并独立有界清理；历史读取可将超过120秒的STREAMING收敛INTERRUPTED。AIUsage仅input_tokens/output_tokens非负整数，**不同于RAG Usage**，没有known；默认0不能推断真实用量已报告。

服务端只提供search_knowledge/get_ticket受控读取和propose_create_ticket提议，模型没有创建/确认权限。Action.arguments完整展示title/body/priority与组织/提出人/有效期；15分钟有效。confirm空body，仅读取数据库不可变参数并验摘要，同一事务创建OPEN/version0工单、事件及EXECUTED。重复confirm返回同ticket_id；重复reject返回同REJECTED；反向决策409，过期410。停止生成不撤销已保存提议，FAILED/INTERRUPTED消息的有效提议仍可人工决策。confirm/reject都重新验证当前成员和owner。

历史是owner保存的记录，删除源文档不擦除已保存文字；带引用或未完成的历史助手内容不作为新证据送回模型，服务端用固定占位替换并只处理最后用户消息。没有会话归档/删除/共享或工具任意执行API。

## 合成SSE帧示例

下列展示各数据形状，实际轮次可以不含citation或tool_approval_required；纯文本增量可以有多个。数字只是协议示意，不是实测用量。

```text
event: message_start
data: {"message_id": "33333333-3333-4333-8333-333333333333", "user_message_id": "22222222-2222-4222-8222-222222222222", "conversation_id": "11111111-1111-4111-8111-111111111111", "client_request_id": "44444444-4444-4444-8444-444444444444"}

event: content_delta
data: {"message_id": "33333333-3333-4333-8333-333333333333", "delta": "已保存提议，请审核后确认。"}

event: citation
data: {"message_id": "33333333-3333-4333-8333-333333333333", "source": {"chunk_id": 501, "document_id": 401, "chunk_index": 0, "source_name": "example.md", "score": 0.82, "excerpt": "先检查文档正文与索引状态。", "start_rune": 0, "end_rune": 13}}

event: tool_approval_required
data: {"message_id": "33333333-3333-4333-8333-333333333333", "action": {"id": "44444444-4444-4444-8444-444444444444", "conversation_id": "11111111-1111-4111-8111-111111111111", "assistant_message_id": "33333333-3333-4333-8333-333333333333", "tool_name": "propose_create_ticket", "status": "PROPOSED", "org_id": 201, "proposed_by_user_id": 101, "arguments": {"title": "合成工单提议", "body": "审核后才创建工单。", "priority": 2}, "expires_at": "2026-11-11T08:15:00Z", "created_at": "2026-11-11T08:00:00Z", "confirmed_by_user_id": null, "ticket_id": null, "confirmed_at": null}}

event: message_end
data: {"message_id": "33333333-3333-4333-8333-333333333333", "status": "COMPLETED", "usage": {"input_tokens": 32, "output_tokens": 12}}

```

失败时可用下面的error帧结束，不能把error作为成功message_end：

```text
event: error
data: {"code":"MODEL_TIMEOUT","message":"MODEL_TIMEOUT","retryable":true}

```

各操作以下示例是**相互独立的合成场景**；“示例前提”仅供解释操作者、路径取值及操作前状态，**不是API请求字段**。无分页query时示例默认1/20；需要状态变化的接口明确给出before与成功后状态。reject实际只更新status，confirmed_by_user_id/ticket_id/confirmed_at仍为null，没有单独的拒绝人字段。


## 39个操作逐项参考

| # | 方法 | 路径 | 操作 |
| --- | --- | --- | --- |
| 1 | POST | `/api/v1/auth/register` | [注册](#operation-register) |
| 2 | POST | `/api/v1/auth/login` | [登录](#operation-login) |
| 3 | GET | `/api/v1/me` | [读取当前用户](#operation-getMe) |
| 4 | POST | `/api/v1/organizations` | [创建组织](#operation-createOrganization) |
| 5 | GET | `/api/v1/organizations` | [本人组织分页](#operation-listOrganizations) |
| 6 | GET | `/api/v1/organizations/{orgID}/members` | [组织成员分页](#operation-listMembers) |
| 7 | POST | `/api/v1/organizations/{orgID}/members` | [添加已注册成员](#operation-addMember) |
| 8 | POST | `/api/v1/organizations/{orgID}/tickets` | [创建工单](#operation-createTicket) |
| 9 | GET | `/api/v1/organizations/{orgID}/tickets` | [筛选工单分页](#operation-listTickets) |
| 10 | GET | `/api/v1/organizations/{orgID}/tickets/{ticketID}` | [工单详情](#operation-getTicket) |
| 11 | PATCH | `/api/v1/organizations/{orgID}/tickets/{ticketID}` | [编辑或指派工单](#operation-patchTicket) |
| 12 | DELETE | `/api/v1/organizations/{orgID}/tickets/{ticketID}` | [软删除工单](#operation-deleteTicket) |
| 13 | POST | `/api/v1/organizations/{orgID}/tickets/{ticketID}/transition` | [流转工单状态](#operation-transitionTicket) |
| 14 | POST | `/api/v1/organizations/{orgID}/tickets/{ticketID}/comments` | [新增评论](#operation-createComment) |
| 15 | GET | `/api/v1/organizations/{orgID}/tickets/{ticketID}/comments` | [评论分页](#operation-listComments) |
| 16 | GET | `/api/v1/organizations/{orgID}/tickets/{ticketID}/events` | [审计事件分页](#operation-listTicketEvents) |
| 17 | POST | `/api/v1/organizations/{orgID}/tickets/{ticketID}/claim` | [原子领取工单](#operation-claimTicket) |
| 18 | POST | `/api/v1/organizations/{orgID}/knowledge/documents` | [上传文档](#operation-uploadDocument) |
| 19 | GET | `/api/v1/organizations/{orgID}/knowledge/documents` | [文档分页](#operation-listDocuments) |
| 20 | GET | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}` | [文档元数据](#operation-getDocument) |
| 21 | POST | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/process` | [同步提取正文](#operation-processDocument) |
| 22 | GET | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/text` | [正文预览](#operation-getDocumentText) |
| 23 | GET | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/download` | [授权下载原文件](#operation-downloadDocument) |
| 24 | DELETE | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}` | [软删除文档](#operation-deleteDocument) |
| 25 | POST | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/index` | [同步索引文档](#operation-indexDocument) |
| 26 | GET | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/index` | [读取索引状态](#operation-getDocumentIndex) |
| 27 | POST | `/api/v1/organizations/{orgID}/knowledge/search` | [向量或混合检索](#operation-searchKnowledge) |
| 28 | POST | `/api/v1/organizations/{orgID}/knowledge/ask` | [带引用知识问答](#operation-askKnowledge) |
| 29 | POST | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/jobs` | [接收或复用异步任务](#operation-submitDocumentJob) |
| 30 | GET | `/api/v1/organizations/{orgID}/knowledge/documents/{documentID}/jobs` | [最近文档任务](#operation-listDocumentJobs) |
| 31 | GET | `/api/v1/organizations/{orgID}/jobs/{jobID}` | [读取任务](#operation-getJob) |
| 32 | POST | `/api/v1/organizations/{orgID}/jobs/{jobID}/retry` | [人工重试任务](#operation-retryJob) |
| 33 | POST | `/api/v1/organizations/{orgID}/jobs/{jobID}/cancel` | [取消任务](#operation-cancelJob) |
| 34 | POST | `/api/v1/organizations/{orgID}/ai/conversations` | [创建本人对话](#operation-createConversation) |
| 35 | GET | `/api/v1/organizations/{orgID}/ai/conversations` | [本人最近对话](#operation-listConversations) |
| 36 | GET | `/api/v1/organizations/{orgID}/ai/conversations/{conversationID}/messages` | [读取消息与提议](#operation-getConversationHistory) |
| 37 | POST | `/api/v1/organizations/{orgID}/ai/conversations/{conversationID}/messages` | [发送并读取SSE](#operation-sendConversationMessage) |
| 38 | POST | `/api/v1/organizations/{orgID}/ai/tool-actions/{actionID}/confirm` | [人工确认创建工单](#operation-confirmToolAction) |
| 39 | POST | `/api/v1/organizations/{orgID}/ai/tool-actions/{actionID}/reject` | [拒绝工单提议](#operation-rejectToolAction) |

<a id="operation-register"></a>

### 01. 注册

`POST /api/v1/auth/register` · operationId=`register` · 匿名

匿名；只创建用户，不建组织、不签发Token。注册重复邮箱409会披露已注册事实。此Handler未拒绝额外query（不使用），客户端不要发送。

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {}
}
```

请求：`RegisterInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "email": "Alice@example.test",
  "display_name": "示例成员",
  "password": "Synthetic-Password-Only-2026"
}
```

成功：HTTP 201，`UserResponse`（data类型`User`）。

```json
{
  "data": {
    "id": 101,
    "email": "alice@example.test",
    "display_name": "示例成员"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST`, `INVALID_EMAIL`, `INVALID_PASSWORD` |
| 409 | `EMAIL_ALREADY_EXISTS` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |
| 503 | `AUTH_BUSY` |
| 500 | `INTERNAL_ERROR` |

<a id="operation-login"></a>

### 02. 登录

`POST /api/v1/auth/login` · operationId=`login` · 匿名

匿名；不存在邮箱与错误密码统一401 INVALID_CREDENTIALS。同成本dummy验证不承诺绝对恒定时间。query未主动拒绝，不使用。

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {}
}
```

请求：`LoginInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "email": "alice@example.test",
  "password": "Synthetic-Password-Only-2026"
}
```

成功：HTTP 200，`AccessTokenResponse`（data类型`AccessToken`）。

```json
{
  "data": {
    "access_token": "<ACCESS_TOKEN>",
    "token_type": "Bearer",
    "expires_in": 900
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST`, `INVALID_EMAIL`, `INVALID_PASSWORD` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |
| 503 | `AUTH_BUSY` |
| 500 | `INTERNAL_ERROR` |
| 401 | `INVALID_CREDENTIALS` |

<a id="operation-getMe"></a>

### 03. 读取当前用户

`GET /api/v1/me` · operationId=`getMe` · Bearer认证

只读本人公开资料；禁止所有query及非空body。

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {}
}
```

请求body：空。

成功：HTTP 200，`UserResponse`（data类型`User`）。

```json
{
  "data": {
    "id": 101,
    "email": "alice@example.test",
    "display_name": "示例成员"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |

<a id="operation-createOrganization"></a>

### 04. 创建组织

`POST /api/v1/organizations` · operationId=`createOrganization` · Bearer认证

同一事务创建组织及当前用户admin/active成员关系。名称可重复，重复请求可创建多个组织。query未主动拒绝，不使用。

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {}
}
```

请求：`OrganizationInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "name": "工程支持"
}
```

成功：HTTP 201，`OrganizationResponse`（data类型`Organization`）。

```json
{
  "data": {
    "id": 201,
    "name": "工程支持",
    "my_role": "admin"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-listOrganizations"></a>

### 05. 本人组织分页

`GET /api/v1/organizations` · operationId=`listOrganizations` · Bearer认证

仅当前用户active组织，按组织id ASC；无body、无total。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| page | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"default":1}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| page_size | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":100,"default":20}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {},
  "query": {
    "page": 1,
    "page_size": 20
  }
}
```

请求body：空。

成功：HTTP 200，`OrganizationPageResponse`（data类型`OrganizationPage`）。

```json
{
  "data": {
    "items": [
      {
        "id": 201,
        "name": "工程支持",
        "my_role": "admin"
      }
    ],
    "page": 1,
    "page_size": 20
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |

<a id="operation-listMembers"></a>

### 06. 组织成员分页

`GET /api/v1/organizations/{orgID}/members` · operationId=`listMembers` · Bearer认证

active member/admin可读；含active/disabled关系，按user_id ASC；不公开邮箱。先授权后解析分页。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| page | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"default":1}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| page_size | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":100,"default":20}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active",
  "query": {
    "page": 1,
    "page_size": 20
  }
}
```

请求body：空。

成功：HTTP 200，`MemberPageResponse`（data类型`MemberPage`）。

```json
{
  "data": {
    "items": [
      {
        "user_id": 102,
        "display_name": "示例同事",
        "role": "member",
        "status": "active",
        "created_at": "2026-11-11T08:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-addMember"></a>

### 07. 添加已注册成员

`POST /api/v1/organizations/{orgID}/members` · operationId=`addMember` · Bearer认证

仅active admin；新关系固定active，既有disabled关系也409且不恢复。先授权后解析目标；query未主动拒绝，不使用。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求：`MemberInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "user_id": 102,
  "role": "member"
}
```

成功：HTTP 201，`MemberResponse`（data类型`Member`）。

```json
{
  "data": {
    "user_id": 102,
    "display_name": "示例同事",
    "role": "member",
    "status": "active",
    "created_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND`, `USER_NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `MEMBER_ALREADY_EXISTS` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-createTicket"></a>

### 08. 创建工单

`POST /api/v1/organizations/{orgID}/tickets` · operationId=`createTicket` · Bearer认证

所有active成员；OPEN/version=0，默认priority=2，无指派字段；禁止query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求：`TicketInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "title": "合成工单",
  "body": "仅用于说明协议",
  "priority": 2
}
```

成功：HTTP 201，`TicketResponse`（data类型`Ticket`）。

```json
{
  "data": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-listTickets"></a>

### 09. 筛选工单分页

`GET /api/v1/organizations/{orgID}/tickets` · operationId=`listTickets` · Bearer认证

active成员可读组织内全部可见工单；created_at DESC,id DESC，无body/total。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| page | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"default":1}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| page_size | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":100,"default":20}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| status | query | 否 | TicketStatus ；唯一且非空。 |
| priority | query | 否 | Priority ；唯一且非空。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active",
  "query": {
    "page": 1,
    "page_size": 20
  }
}
```

请求body：空。

成功：HTTP 200，`TicketPageResponse`（data类型`TicketPage`）。

```json
{
  "data": {
    "items": [
      {
        "id": 301,
        "org_id": 201,
        "creator_id": 101,
        "assignee_id": null,
        "title": "合成工单",
        "body": "仅用于说明协议",
        "priority": 2,
        "status": "OPEN",
        "version": 0,
        "created_at": "2026-11-11T08:00:00Z",
        "updated_at": "2026-11-11T08:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-getTicket"></a>

### 10. 工单详情

`GET /api/v1/organizations/{orgID}/tickets/{ticketID}` · operationId=`getTicket` · Bearer认证

禁止query/body；缺失、跨组织、软删统一404。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`TicketResponse`（data类型`Ticket`）。

```json
{
  "data": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-patchTicket"></a>

### 11. 编辑或指派工单

`PATCH /api/v1/organizations/{orgID}/tickets/{ticketID}` · operationId=`patchTicket` · Bearer认证

仅OPEN；创建者或admin编辑title/body/priority，仅admin指派/解除。expected_version必须匹配，至少一项实际变化；无变化400，成功version+1。禁止query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求：`TicketPatch`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "expected_version": 0,
  "title": "合成工单（已更新）",
  "priority": 3
}
```

成功：HTTP 200，`TicketResponse`（data类型`Ticket`）。

```json
{
  "data": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单（已更新）",
    "body": "仅用于说明协议",
    "priority": 3,
    "status": "OPEN",
    "version": 1,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST`, `INVALID_ASSIGNEE` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `CONFLICT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-deleteTicket"></a>

### 12. 软删除工单

`DELETE /api/v1/organizations/{orgID}/tickets/{ticketID}` · operationId=`deleteTicket` · Bearer认证

仅admin；任意状态可软删，version+1并记事件。不接收expected_version/body/query；重复404。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 204，无响应体。

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `CONFLICT` |

<a id="operation-transitionTicket"></a>

### 13. 流转工单状态

`POST /api/v1/organizations/{orgID}/tickets/{ticketID}/transition` · operationId=`transitionTicket` · Bearer认证

状态机及当前受理人权限见完整手册；要求expected_version，成功version+1。禁止query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": 102,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  },
  "active_assignee_ids": [
    102
  ]
}
```

请求：`TransitionInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "expected_version": 0,
  "to_status": "IN_PROGRESS"
}
```

成功：HTTP 200，`TicketResponse`（data类型`Ticket`）。

```json
{
  "data": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": 102,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "IN_PROGRESS",
    "version": 1,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `CONFLICT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-createComment"></a>

### 14. 新增评论

`POST /api/v1/organizations/{orgID}/tickets/{ticketID}/comments` · operationId=`createComment` · Bearer认证

active成员可写；CLOSED返回409。成功记事件但不增加工单version；禁止query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求：`CommentInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "content": "已开始排查。"
}
```

成功：HTTP 201，`CommentResponse`（data类型`Comment`）。

```json
{
  "data": {
    "id": 701,
    "ticket_id": 301,
    "author_id": 101,
    "content": "已开始排查。",
    "created_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 409 | `CONFLICT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-listComments"></a>

### 15. 评论分页

`GET /api/v1/organizations/{orgID}/tickets/{ticketID}/comments` · operationId=`listComments` · Bearer认证

按created_at ASC,id ASC；仅page/page_size，无body。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| page | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"default":1}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| page_size | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":100,"default":20}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "query": {
    "page": 1,
    "page_size": 20
  }
}
```

请求body：空。

成功：HTTP 200，`CommentPageResponse`（data类型`CommentPage`）。

```json
{
  "data": {
    "items": [
      {
        "id": 701,
        "ticket_id": 301,
        "author_id": 101,
        "content": "已开始排查。",
        "created_at": "2026-11-11T08:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-listTicketEvents"></a>

### 16. 审计事件分页

`GET /api/v1/organizations/{orgID}/tickets/{ticketID}/events` · operationId=`listTicketEvents` · Bearer认证

按created_at ASC,id ASC；安全结构化快照，不含正文和评论文本；仅page/page_size，无body。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| page | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"default":1}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| page_size | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":100,"default":20}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "query": {
    "page": 1,
    "page_size": 20
  }
}
```

请求body：空。

成功：HTTP 200，`TicketEventPageResponse`（data类型`TicketEventPage`）。

```json
{
  "data": {
    "items": [
      {
        "id": 801,
        "ticket_id": 301,
        "actor_id": 101,
        "event_type": "TICKET_CREATED",
        "ticket_version": 0,
        "before_data": {},
        "after_data": {
          "title": "合成工单",
          "priority": 2,
          "assignee_id": null,
          "status": "OPEN",
          "version": 0
        },
        "created_at": "2026-11-11T08:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-claimTicket"></a>

### 17. 原子领取工单

`POST /api/v1/organizations/{orgID}/tickets/{ticketID}/claim` · operationId=`claimTicket` · Bearer认证

active member/admin可将OPEN且未指派工单指派给本人；状态仍OPEN，version+1，TICKET_ASSIGNED。重复/已指派/旧版本/非OPEN均专用409；禁止query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| ticketID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "ticketID": 301
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": null,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求：`ClaimInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "expected_version": 0
}
```

成功：HTTP 200，`TicketResponse`（data类型`Ticket`）。

```json
{
  "data": {
    "id": 301,
    "org_id": 201,
    "creator_id": 101,
    "assignee_id": 101,
    "title": "合成工单",
    "body": "仅用于说明协议",
    "priority": 2,
    "status": "OPEN",
    "version": 1,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 409 | `TICKET_CLAIM_CONFLICT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-uploadDocument"></a>

### 18. 上传文档

`POST /api/v1/organizations/{orgID}/knowledge/documents` · operationId=`uploadDocument` · Bearer认证

active成员；唯一file部分，禁止query。文件名/内容与大小约束见完整手册。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active",
  "upload_filename": "example.md",
  "upload_content": "先检查文档正文与索引状态。"
}
```

请求：`UploadInput`，媒体`multipart/form-data`。字段约束见后面的同名schema表。

multipart示例的file部分为UTF-8非空内容，filename取示例前提upload_filename；不把文件名或解释字段作为第二个multipart部分。

```json
{
  "file": "先检查文档正文与索引状态。"
}
```

成功：HTTP 201，`DocumentResponse`（data类型`Document`）。

```json
{
  "data": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "UPLOADED",
    "failure_code": null,
    "extracted_chars": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 413 | `DOCUMENT_TOO_LARGE` |
| 415 | `UNSUPPORTED_DOCUMENT_TYPE` |

<a id="operation-listDocuments"></a>

### 19. 文档分页

`GET /api/v1/organizations/{orgID}/knowledge/documents` · operationId=`listDocuments` · Bearer认证

created_at DESC,id DESC；不含正文；无body/total。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| page | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"default":1}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| page_size | query | 否 | integer {"type":"integer","format":"int64","minimum":1,"maximum":100,"default":20}；仅ASCII十进制数字、唯一非空值；偏移乘法须不溢出int64。 |
| status | query | 否 | string {"type":"string","enum":["UPLOADED","PROCESSING","READY","FAILED"]}；唯一且非空。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active",
  "query": {
    "page": 1,
    "page_size": 20
  }
}
```

请求body：空。

成功：HTTP 200，`DocumentPageResponse`（data类型`DocumentPage`）。

```json
{
  "data": {
    "items": [
      {
        "id": 401,
        "org_id": 201,
        "uploaded_by": 101,
        "original_filename": "example.md",
        "media_type": "text/markdown",
        "size_bytes": 39,
        "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
        "status": "UPLOADED",
        "failure_code": null,
        "extracted_chars": 0,
        "created_at": "2026-11-11T08:00:00Z",
        "updated_at": "2026-11-11T08:00:00Z"
      }
    ],
    "page": 1,
    "page_size": 20
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-getDocument"></a>

### 20. 文档元数据

`GET /api/v1/organizations/{orgID}/knowledge/documents/{documentID}` · operationId=`getDocument` · Bearer认证

禁止body/query，不公开storage_key。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "UPLOADED",
    "failure_code": null,
    "extracted_chars": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`DocumentResponse`（data类型`Document`）。

```json
{
  "data": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "UPLOADED",
    "failure_code": null,
    "extracted_chars": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-processDocument"></a>

### 21. 同步提取正文

`POST /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/process` · operationId=`processDocument` · Bearer认证

UPLOADED/FAILED可处理，PROCESSING/READY冲突；TXT/MD规范化后READY，PDF不支持提取。默认10秒、正文≤1MiB。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "UPLOADED",
    "failure_code": null,
    "extracted_chars": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`DocumentResponse`（data类型`Document`）。

```json
{
  "data": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "READY",
    "failure_code": null,
    "extracted_chars": 13,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 409 | `DOCUMENT_STATE_CONFLICT` |
| 422 | `INVALID_TEXT`, `EMPTY_TEXT`, `TEXT_TOO_LARGE`, `PDF_UNSUPPORTED` |
| 504 | `PROCESS_TIMEOUT` |

<a id="operation-getDocumentText"></a>

### 22. 正文预览

`GET /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/text` · operationId=`getDocumentText` · Bearer认证

仅READY可读，否则409；纯文本数据，无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求body：空。

成功：HTTP 200，`DocumentTextResponse`（data类型`DocumentText`）。

```json
{
  "data": {
    "document_id": 401,
    "content_text": "先检查文档正文与索引状态。",
    "content_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "extractor_version": "utf8-v1",
    "extracted_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 409 | `DOCUMENT_STATE_CONFLICT` |

<a id="operation-downloadDocument"></a>

### 23. 授权下载原文件

`GET /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/download` · operationId=`downloadDocument` · Bearer认证

原始字节，需Bearer，不提供静态URL/Range契约；无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "UPLOADED",
    "failure_code": null,
    "extracted_chars": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  },
  "download_content": "先检查文档正文与索引状态。"
}
```

请求body：空。

成功：HTTP 200 `application/octet-stream`原始字节，无data包裹。

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-deleteDocument"></a>

### 24. 软删除文档

`DELETE /api/v1/organizations/{orgID}/knowledge/documents/{documentID}` · operationId=`deleteDocument` · Bearer认证

仅上传者或当前admin；任意状态。保留私有原对象/正文，普通API隐藏。无body/query，重复404。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "UPLOADED",
    "failure_code": null,
    "extracted_chars": 0,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 204，无响应体。

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |

<a id="operation-indexDocument"></a>

### 25. 同步索引文档

`POST /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/index` · operationId=`indexDocument` · Bearer认证

仅正文READY；相同hash/模型/chunker的READY直接复用，不升版。否则新index_version；INDEXING冲突。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "READY",
    "failure_code": null,
    "extracted_chars": 13,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z"
  },
  "content_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b"
}
```

请求body：空。

成功：HTTP 200，`IndexInfoResponse`（data类型`IndexInfo`）。

```json
{
  "data": {
    "document_id": 401,
    "status": "READY",
    "index_version": 1,
    "embedding_model": "bge-m3:567m",
    "embedding_dimension": 1024,
    "chunker_version": "rune-500-80-v1",
    "source_content_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "chunk_count": 1,
    "failure_code": null,
    "started_at": "2026-11-11T08:02:00Z",
    "indexed_at": "2026-11-11T08:03:00Z",
    "updated_at": "2026-11-11T08:03:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `INDEX_STATE_CONFLICT`, `INDEX_SOURCE_CHANGED` |
| 422 | `INDEX_CONTENT_INVALID`, `INDEX_TOO_MANY_CHUNKS` |
| 502 | `EMBEDDING_COUNT_MISMATCH`, `EMBEDDING_DIMENSION_MISMATCH`, `EMBEDDING_INVALID_VECTOR`, `MODEL_INVALID_RESPONSE`, `MODEL_UPSTREAM_ERROR` |
| 503 | `MODEL_UNAVAILABLE`, `MODEL_RATE_LIMITED`, `RAG_BUSY` |
| 504 | `MODEL_TIMEOUT`, `INDEX_TIMEOUT` |

<a id="operation-getDocumentIndex"></a>

### 26. 读取索引状态

`GET /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/index` · operationId=`getDocumentIndex` · Bearer认证

无记录为ABSENT，版本/块数0、hash空、时间null。RAG关闭也可读。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active",
  "visible_resource": {
    "id": 401,
    "org_id": 201,
    "uploaded_by": 101,
    "original_filename": "example.md",
    "media_type": "text/markdown",
    "size_bytes": 39,
    "sha256_hex": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "status": "READY",
    "failure_code": null,
    "extracted_chars": 13,
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`IndexInfoResponse`（data类型`IndexInfo`）。

```json
{
  "data": {
    "document_id": 401,
    "status": "ABSENT",
    "index_version": 0,
    "embedding_model": "bge-m3:567m",
    "embedding_dimension": 1024,
    "chunker_version": "rune-500-80-v1",
    "source_content_sha256": "",
    "chunk_count": 0,
    "failure_code": null,
    "started_at": null,
    "indexed_at": null,
    "updated_at": null
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 504 | `MODEL_TIMEOUT` |

<a id="operation-searchKnowledge"></a>

### 27. 向量或混合检索

`POST /api/v1/organizations/{orgID}/knowledge/search` · operationId=`searchKnowledge` · Bearer认证

默认vector；hybrid按RRF60融合各20候选，最多32词项。score仍余弦，fusion_score仅hybrid。全部结果受组织/当前hash/index/模型/软删过滤。无querystring。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求：`QueryInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "query": "如何排查索引失败？"
}
```

成功：HTTP 200，`SearchResultResponse`（data类型`SearchResult`）。

```json
{
  "data": {
    "items": [
      {
        "chunk_id": 501,
        "document_id": 401,
        "chunk_index": 0,
        "source_name": "example.md",
        "score": 0.82,
        "excerpt": "先检查文档正文与索引状态。",
        "start_rune": 0,
        "end_rune": 13
      }
    ],
    "retrieval_mode": "vector",
    "embedding_model": "bge-m3:567m",
    "embedding_ms": 12,
    "search_ms": 2
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `INDEX_SOURCE_CHANGED` |
| 502 | `EMBEDDING_COUNT_MISMATCH`, `EMBEDDING_DIMENSION_MISMATCH`, `EMBEDDING_INVALID_VECTOR`, `MODEL_INVALID_RESPONSE`, `MODEL_UPSTREAM_ERROR` |
| 503 | `MODEL_UNAVAILABLE`, `MODEL_RATE_LIMITED`, `RAG_BUSY` |
| 504 | `MODEL_TIMEOUT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-askKnowledge"></a>

### 28. 带引用知识问答

`POST /api/v1/organizations/{orgID}/knowledge/ask` · operationId=`askKnowledge` · Bearer认证

仅引用本轮真实检索来源，生成前后复核全部输入来源。无证据返回固定拒答和空sources并跳过生成；usage/timings见完整手册。无querystring。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；正十进制int64；现有此入口允许前导零，客户端统一发送规范值。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求：`QueryInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "query": "如何排查索引失败？"
}
```

成功：HTTP 200，`AskResultResponse`（data类型`AskResult`）。

```json
{
  "data": {
    "answer": "知识库未检索到足够依据。",
    "sources": [],
    "request_id": "0123456789abcdef0123456789abcdef",
    "insufficient_evidence": true,
    "retrieval_mode": "vector",
    "embedding_model": "bge-m3:567m",
    "generation_model": "deepseek-flash",
    "usage": {
      "known": true,
      "model_called": false,
      "prompt_tokens": 0,
      "completion_tokens": 0,
      "total_tokens": 0,
      "cache_hit_tokens": 0,
      "cache_miss_tokens": 0
    },
    "timings": {
      "embedding_ms": 12,
      "search_ms": 2,
      "generation_ms": 0,
      "total_ms": 18
    }
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `INDEX_SOURCE_CHANGED` |
| 502 | `EMBEDDING_COUNT_MISMATCH`, `EMBEDDING_DIMENSION_MISMATCH`, `EMBEDDING_INVALID_VECTOR`, `MODEL_INVALID_RESPONSE`, `MODEL_UPSTREAM_ERROR` |
| 503 | `MODEL_UNAVAILABLE`, `MODEL_RATE_LIMITED`, `RAG_BUSY` |
| 504 | `MODEL_TIMEOUT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-submitDocumentJob"></a>

### 29. 接收或复用异步任务

`POST /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/jobs` · operationId=`submitDocumentJob` · Bearer认证

同文档/原文件hash/提取器/模型/chunker唯一任务，重复返回已有任务（包括终态）。202只表明PG持久化接收；Redis/Worker停机可继续PENDING。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求body：空。

成功：HTTP 202，`JobResponse`（data类型`Job`）。

```json
{
  "data": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "PENDING",
    "phase": "WAITING",
    "run_version": 1,
    "attempts": 0,
    "max_attempts": 3,
    "total_attempts": 0,
    "failure_code": null,
    "retryable": false,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z",
    "started_at": null,
    "completed_at": null,
    "next_run_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 409 | `DOCUMENT_STATE_CONFLICT` |
| 503 | `ASYNC_DISABLED` |

<a id="operation-listDocumentJobs"></a>

### 30. 最近文档任务

`GET /api/v1/organizations/{orgID}/knowledge/documents/{documentID}/jobs` · operationId=`listDocumentJobs` · Bearer认证

固定最近20条，id DESC，无分页参数/body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| documentID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "documentID": 401
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求body：空。

成功：HTTP 200，`JobListResponse`（data类型`JobList`）。

```json
{
  "data": {
    "items": [
      {
        "id": 601,
        "org_id": 201,
        "document_id": 401,
        "requested_by": 101,
        "actor_user_id": 101,
        "status": "PENDING",
        "phase": "WAITING",
        "run_version": 1,
        "attempts": 0,
        "max_attempts": 3,
        "total_attempts": 0,
        "failure_code": null,
        "retryable": false,
        "last_dispatch_error": null,
        "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
        "embedding_model": "bge-m3:567m",
        "chunker_version": "rune-500-80-v1",
        "created_at": "2026-11-11T08:00:00Z",
        "updated_at": "2026-11-11T08:00:00Z",
        "started_at": null,
        "completed_at": null,
        "next_run_at": "2026-11-11T08:00:00Z"
      }
    ]
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-getJob"></a>

### 31. 读取任务

`GET /api/v1/organizations/{orgID}/jobs/{jobID}` · operationId=`getJob` · Bearer认证

active成员可读；父文档软删后隐藏。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| jobID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "jobID": 601
  },
  "actor_role": "admin",
  "membership_status": "active",
  "visible_resource": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "PENDING",
    "phase": "WAITING",
    "run_version": 1,
    "attempts": 0,
    "max_attempts": 3,
    "total_attempts": 0,
    "failure_code": null,
    "retryable": false,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z",
    "started_at": null,
    "completed_at": null,
    "next_run_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`JobResponse`（data类型`Job`）。

```json
{
  "data": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "PENDING",
    "phase": "WAITING",
    "run_version": 1,
    "attempts": 0,
    "max_attempts": 3,
    "total_attempts": 0,
    "failure_code": null,
    "retryable": false,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z",
    "started_at": null,
    "completed_at": null,
    "next_run_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-retryJob"></a>

### 32. 人工重试任务

`POST /api/v1/organizations/{orgID}/jobs/{jobID}/retry` · operationId=`retryJob` · Bearer认证

仅requested_by或admin；仅FAILED/CANCELED可重试；run_version+1，attempts归0，保留total_attempts/requested_by，actor_user_id改为重试者。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| jobID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "jobID": 601
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "FAILED",
    "phase": "INDEXING",
    "run_version": 1,
    "attempts": 3,
    "max_attempts": 3,
    "total_attempts": 3,
    "failure_code": "MODEL_TIMEOUT",
    "retryable": true,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z",
    "started_at": "2026-11-11T08:00:00Z",
    "completed_at": "2026-11-11T08:01:00Z",
    "next_run_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`JobResponse`（data类型`Job`）。

```json
{
  "data": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "PENDING",
    "phase": "WAITING",
    "run_version": 2,
    "attempts": 0,
    "max_attempts": 3,
    "total_attempts": 3,
    "failure_code": null,
    "retryable": false,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z",
    "started_at": null,
    "completed_at": null,
    "next_run_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `JOB_STATE_CONFLICT`, `DOCUMENT_STATE_CONFLICT` |
| 503 | `ASYNC_DISABLED` |

<a id="operation-cancelJob"></a>

### 33. 取消任务

`POST /api/v1/organizations/{orgID}/jobs/{jobID}/cancel` · operationId=`cancelJob` · Bearer认证

仅requested_by或admin；仅PENDING/RUNNING/RETRYING可取消。取消不回滚已提交正文/索引。ASYNC关闭仍可取消。无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| jobID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "jobID": 601
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "PENDING",
    "phase": "WAITING",
    "run_version": 1,
    "attempts": 0,
    "max_attempts": 3,
    "total_attempts": 0,
    "failure_code": null,
    "retryable": false,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z",
    "started_at": null,
    "completed_at": null,
    "next_run_at": "2026-11-11T08:00:00Z"
  }
}
```

请求body：空。

成功：HTTP 200，`JobResponse`（data类型`Job`）。

```json
{
  "data": {
    "id": 601,
    "org_id": 201,
    "document_id": 401,
    "requested_by": 101,
    "actor_user_id": 101,
    "status": "CANCELED",
    "phase": "WAITING",
    "run_version": 1,
    "attempts": 0,
    "max_attempts": 3,
    "total_attempts": 0,
    "failure_code": "PROCESS_CANCELED",
    "retryable": false,
    "last_dispatch_error": null,
    "source_sha256": "60879f9c4994e0d7c565a6fcf150ff9744e12e2af74295adbc6e168052f1e83b",
    "embedding_model": "bge-m3:567m",
    "chunker_version": "rune-500-80-v1",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:01:00Z",
    "started_at": null,
    "completed_at": "2026-11-11T08:01:00Z",
    "next_run_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `JOB_STATE_CONFLICT` |

<a id="operation-createConversation"></a>

### 34. 创建本人对话

`POST /api/v1/organizations/{orgID}/ai/conversations` · operationId=`createConversation` · Bearer认证

当前active成员，创建者为owner；只接受title，无querystring。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求：`ConversationInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "title": "索引排查"
}
```

成功：HTTP 201，`ConversationResponse`（data类型`Conversation`）。

```json
{
  "data": {
    "id": "11111111-1111-4111-8111-111111111111",
    "org_id": 201,
    "owner_user_id": 101,
    "title": "索引排查",
    "status": "ACTIVE",
    "created_at": "2026-11-11T08:00:00Z",
    "updated_at": "2026-11-11T08:00:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-listConversations"></a>

### 35. 本人最近对话

`GET /api/v1/organizations/{orgID}/ai/conversations` · operationId=`listConversations` · Bearer认证

固定最新50条ACTIVE，updated_at DESC,id DESC；data直接是数组，无items包裹，无body/query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求body：空。

成功：HTTP 200，`ConversationListResponse`（data类型`ConversationList`）。

```json
{
  "data": [
    {
      "id": "11111111-1111-4111-8111-111111111111",
      "org_id": 201,
      "owner_user_id": 101,
      "title": "索引排查",
      "status": "ACTIVE",
      "created_at": "2026-11-11T08:00:00Z",
      "updated_at": "2026-11-11T08:00:00Z"
    }
  ]
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-getConversationHistory"></a>

### 36. 读取消息与提议

`GET /api/v1/organizations/{orgID}/ai/conversations/{conversationID}/messages` · operationId=`getConversationHistory` · Bearer认证

仅owner（admin也不越权）；messages/actions分别取最新100条，再按created_at,id ASC返回。无body/query；可将超过120秒STREAMING收敛为INTERRUPTED。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| conversationID | path | 是 | UUID ；小写UUID。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "conversationID": "11111111-1111-4111-8111-111111111111"
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求body：空。

成功：HTTP 200，`HistoryResponse`（data类型`History`）。

```json
{
  "data": {
    "messages": [
      {
        "id": "22222222-2222-4222-8222-222222222222",
        "conversation_id": "11111111-1111-4111-8111-111111111111",
        "client_request_id": "44444444-4444-4444-8444-444444444444",
        "role": "USER",
        "content": "请提议创建一张索引排查工单。",
        "status": "COMPLETED",
        "model_name": "",
        "org_id": 201,
        "citations": [],
        "input_tokens": 0,
        "output_tokens": 0,
        "created_at": "2026-11-11T08:00:00Z",
        "completed_at": "2026-11-11T08:00:00Z"
      },
      {
        "id": "33333333-3333-4333-8333-333333333333",
        "conversation_id": "11111111-1111-4111-8111-111111111111",
        "client_request_id": "44444444-4444-4444-8444-444444444444",
        "role": "ASSISTANT",
        "content": "已保存提议，请审核后确认。",
        "status": "COMPLETED",
        "model_name": "qwen3.5:35b",
        "org_id": 201,
        "citations": [],
        "input_tokens": 32,
        "output_tokens": 12,
        "created_at": "2026-11-11T08:00:00Z",
        "completed_at": "2026-11-11T08:01:00Z"
      }
    ],
    "actions": [
      {
        "id": "44444444-4444-4444-8444-444444444444",
        "conversation_id": "11111111-1111-4111-8111-111111111111",
        "assistant_message_id": "33333333-3333-4333-8333-333333333333",
        "tool_name": "propose_create_ticket",
        "status": "PROPOSED",
        "org_id": 201,
        "proposed_by_user_id": 101,
        "arguments": {
          "title": "合成工单提议",
          "body": "审核后才创建工单。",
          "priority": 2
        },
        "expires_at": "2026-11-11T08:15:00Z",
        "created_at": "2026-11-11T08:00:00Z",
        "confirmed_by_user_id": null,
        "ticket_id": null,
        "confirmed_at": null
      }
    ]
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |

<a id="operation-sendConversationMessage"></a>

### 37. 发送并读取SSE

`POST /api/v1/organizations/{orgID}/ai/conversations/{conversationID}/messages` · operationId=`sendConversationMessage` · Bearer认证

仅owner；一个client_request_id关联USER/ASSISTANT两行，重复ID一律409，无自动重放。默认user+org每60秒10次；每会话1生成/实例2并发。流开始前JSON错误，之后error事件。无querystring。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| conversationID | path | 是 | UUID ；小写UUID。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "conversationID": "11111111-1111-4111-8111-111111111111"
  },
  "actor_role": "admin",
  "membership_status": "active"
}
```

请求：`SendMessageInput`，媒体`application/json`。字段约束见后面的同名schema表。

```json
{
  "content": "请提议创建一张索引排查工单。",
  "client_request_id": "44444444-4444-4444-8444-444444444444"
}
```

成功：HTTP 200 SSE，六种事件及完整数据字段见SSE章节/schema；流内错误不能用HTTP状态判断。

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 409 | `AI_CONFLICT` |
| 429 | `AI_BUSY`, `RATE_LIMITED` |
| 422 | `AI_LIMIT` |
| 502 | `MODEL_INVALID_RESPONSE` |
| 503 | `MODEL_UNAVAILABLE`, `RATE_LIMIT_UNAVAILABLE` |
| 504 | `MODEL_TIMEOUT` |
| 413 | `REQUEST_TOO_LARGE` |
| 415 | `UNSUPPORTED_MEDIA_TYPE` |

<a id="operation-confirmToolAction"></a>

### 38. 人工确认创建工单

`POST /api/v1/organizations/{orgID}/ai/tool-actions/{actionID}/confirm` · operationId=`confirmToolAction` · Bearer认证

仅提议owner，当前active；从数据库读取不可变完整arguments，验证摘要，同事务建工单/记事件/更新EXECUTED。PROPOSED有效15分钟。重复confirm返回同ticket_id；REJECTED冲突，过期410。空body，无query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| actionID | path | 是 | UUID ；小写UUID。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "actionID": "44444444-4444-4444-8444-444444444444"
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": "44444444-4444-4444-8444-444444444444",
    "conversation_id": "11111111-1111-4111-8111-111111111111",
    "assistant_message_id": "33333333-3333-4333-8333-333333333333",
    "tool_name": "propose_create_ticket",
    "status": "PROPOSED",
    "org_id": 201,
    "proposed_by_user_id": 101,
    "arguments": {
      "title": "合成工单提议",
      "body": "审核后才创建工单。",
      "priority": 2
    },
    "expires_at": "2026-11-11T08:15:00Z",
    "created_at": "2026-11-11T08:00:00Z",
    "confirmed_by_user_id": null,
    "ticket_id": null,
    "confirmed_at": null
  }
}
```

请求body：空。

成功：HTTP 200，`ActionResponse`（data类型`Action`）。

```json
{
  "data": {
    "id": "44444444-4444-4444-8444-444444444444",
    "conversation_id": "11111111-1111-4111-8111-111111111111",
    "assistant_message_id": "33333333-3333-4333-8333-333333333333",
    "tool_name": "propose_create_ticket",
    "status": "EXECUTED",
    "org_id": 201,
    "proposed_by_user_id": 101,
    "arguments": {
      "title": "合成工单提议",
      "body": "审核后才创建工单。",
      "priority": 2
    },
    "expires_at": "2026-11-11T08:15:00Z",
    "created_at": "2026-11-11T08:00:00Z",
    "confirmed_by_user_id": 101,
    "ticket_id": 301,
    "confirmed_at": "2026-11-11T08:01:00Z"
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `AI_CONFLICT` |
| 410 | `AI_EXPIRED` |

<a id="operation-rejectToolAction"></a>

### 39. 拒绝工单提议

`POST /api/v1/organizations/{orgID}/ai/tool-actions/{actionID}/reject` · operationId=`rejectToolAction` · Bearer认证

仅提议owner；重复reject返回REJECTED，已EXECUTED冲突，过期410。停止生成不自动拒绝已有提议。空body，无query。

| 参数 | 位置 | 必填 | 类型/约束 |
| --- | --- | --- | --- |
| orgID | path | 是 | integer {"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807}；规范正十进制，不允许前导零。 |
| actionID | path | 是 | UUID ；小写UUID。 |

示例前提（合成解释信息，不属于API请求）：

```json
{
  "actor_user_id": 101,
  "path_parameters": {
    "orgID": 201,
    "actionID": "44444444-4444-4444-8444-444444444444"
  },
  "actor_role": "admin",
  "membership_status": "active",
  "before": {
    "id": "44444444-4444-4444-8444-444444444444",
    "conversation_id": "11111111-1111-4111-8111-111111111111",
    "assistant_message_id": "33333333-3333-4333-8333-333333333333",
    "tool_name": "propose_create_ticket",
    "status": "PROPOSED",
    "org_id": 201,
    "proposed_by_user_id": 101,
    "arguments": {
      "title": "合成工单提议",
      "body": "审核后才创建工单。",
      "priority": 2
    },
    "expires_at": "2026-11-11T08:15:00Z",
    "created_at": "2026-11-11T08:00:00Z",
    "confirmed_by_user_id": null,
    "ticket_id": null,
    "confirmed_at": null
  }
}
```

请求body：空。

成功：HTTP 200，`ActionResponse`（data类型`Action`）。

```json
{
  "data": {
    "id": "44444444-4444-4444-8444-444444444444",
    "conversation_id": "11111111-1111-4111-8111-111111111111",
    "assistant_message_id": "33333333-3333-4333-8333-333333333333",
    "tool_name": "propose_create_ticket",
    "status": "REJECTED",
    "org_id": 201,
    "proposed_by_user_id": 101,
    "arguments": {
      "title": "合成工单提议",
      "body": "审核后才创建工单。",
      "priority": 2
    },
    "expires_at": "2026-11-11T08:15:00Z",
    "created_at": "2026-11-11T08:00:00Z",
    "confirmed_by_user_id": null,
    "ticket_id": null,
    "confirmed_at": null
  }
}
```

| HTTP错误 | code |
| --- | --- |
| 400 | `INVALID_REQUEST` |
| 401 | `UNAUTHENTICATED` |
| 500 | `INTERNAL_ERROR` |
| 404 | `NOT_FOUND` |
| 403 | `FORBIDDEN` |
| 409 | `AI_CONFLICT` |
| 410 | `AI_EXPIRED` |

## Schema字段字典

固定输出DTO字段均必需（可空字段保留为null）；Source.fusion_score可省略，EventData按事件类型出现的字段均可省略。输入仅下表标必需项必填，additionalProperties=false；对象不是可任意扩展的字典。JSON schema不能表达所有TrimSpace/UTF8字节/事务权限/原始整数词法约束，必须同时遵守上文。

### Schema `ID`

正int64。线上是JSON整数；JavaScript必须无损解析，不能先经Number舍入。

类型/约束：`{"type":"integer","format":"int64","minimum":1,"maximum":9223372036854775807,"description":"正int64。线上是JSON整数；JavaScript必须无损解析，不能先经Number舍入。"}`

### Schema `Version`

0..9223372036854775807；expected_version须以JSON整数发送，不能是字符串、小数或指数。

类型/约束：`{"type":"integer","format":"int64","minimum":0,"maximum":9223372036854775807,"description":"0..9223372036854775807；expected_version须以JSON整数发送，不能是字符串、小数或指数。"}`

### Schema `UUID`

小写36字符UUID；实现校验格式，不强制UUID版本位。

类型/约束：`{"type":"string","description":"小写36字符UUID；实现校验格式，不强制UUID版本位。","pattern":"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$","examples":["11111111-1111-4111-8111-111111111111"]}`

### Schema `Timestamp`

UTC RFC3339，可含小数秒。

类型/约束：`{"type":"string","description":"UTC RFC3339，可含小数秒。","format":"date-time","examples":["2026-11-11T08:00:00Z"]}`

### Schema `RequestID`

服务端生成，与X-Request-ID响应头关联。客户端值不可信且不会沿用。

类型/约束：`{"type":"string","description":"服务端生成，与X-Request-ID响应头关联。客户端值不可信且不会沿用。","pattern":"^[0-9a-f]{32}$","examples":["0123456789abcdef0123456789abcdef"]}`

### Schema `Role`

类型/约束：`{"type":"string","enum":["member","admin"]}`

### Schema `TicketStatus`

类型/约束：`{"type":"string","enum":["OPEN","IN_PROGRESS","RESOLVED","CLOSED"]}`

### Schema `Priority`

1低、2普通、3高。

类型/约束：`{"type":"integer","format":"int64","minimum":1,"maximum":3,"description":"1低、2普通、3高。"}`

### Schema `Error`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| error | object | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `User`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| email | string | 是 | 见同名引用schema；字段名即协议名称。 |
| display_name | string | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `RegisterInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| email | string | 是 | TrimSpace后3..254 ASCII字节，net/mail普通地址，内部无空白/显示名；转小写，不改写+tag或点号。 {"minLength":3,"maxLength":254} |
| display_name | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":80} |
| password | string | 是 | 保留原文；有效UTF-8、15..128码点、最多512字节，非全空白。可含NUL。不裁剪。 {"minLength":15,"maxLength":128} |

### Schema `LoginInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| email | string | 是 | TrimSpace后3..254 ASCII字节，net/mail普通地址，内部无空白/显示名；转小写，不改写+tag或点号。 {"minLength":3,"maxLength":254} |
| password | string | 是 | 保留原文；有效UTF-8、15..128码点、最多512字节，非全空白。可含NUL。不裁剪。 {"minLength":15,"maxLength":128} |

### Schema `AccessToken`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| access_token | string | 是 | 仅登录返回；此处为不可用占位示例。 |
| token_type | string | 是 |  {"const":"Bearer"} |
| expires_in | integer | 是 | 当前版本固定15分钟（900秒），JWT_TTL_MINUTES必须为15。 {"const":900} |

### Schema `OrganizationInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| name | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":100} |

### Schema `Organization`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| name | string | 是 | 见同名引用schema；字段名即协议名称。 |
| my_role | Role | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `MemberInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| user_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| role | Role | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Member`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| user_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| display_name | string | 是 | 见同名引用schema；字段名即协议名称。 |
| role | Role | 是 | 见同名引用schema；字段名即协议名称。 |
| status | string | 是 |  {"enum":["active","disabled"]} |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `TicketInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| title | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":200} |
| body | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":10000} |
| priority | Priority | 否 |  {"default":2} |

### Schema `TicketPatch`

至少一个其他字段且须产生实际变化；只有admin可出现assignee_id（null解除指派），仅OPEN可编辑。

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| expected_version | Version | 是 | 见同名引用schema；字段名即协议名称。 |
| title | string | 否 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":200} |
| body | string | 否 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":10000} |
| priority | Priority | 否 | 见同名引用schema；字段名即协议名称。 |
| assignee_id | ID / null | 否 | 见同名引用schema；字段名即协议名称。 |

### Schema `TransitionInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| expected_version | Version | 是 | 见同名引用schema；字段名即协议名称。 |
| to_status | TicketStatus | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `ClaimInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| expected_version | Version | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `CommentInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| content | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":5000} |

### Schema `Ticket`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| org_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| creator_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| assignee_id | ID / null | 是 | 见同名引用schema；字段名即协议名称。 |
| title | string | 是 | 见同名引用schema；字段名即协议名称。 |
| body | string | 是 | 见同名引用schema；字段名即协议名称。 |
| priority | Priority | 是 | 见同名引用schema；字段名即协议名称。 |
| status | TicketStatus | 是 | 见同名引用schema；字段名即协议名称。 |
| version | Version | 是 | 见同名引用schema；字段名即协议名称。 |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| updated_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Comment`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| ticket_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| author_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| content | string | 是 | 见同名引用schema；字段名即协议名称。 |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `EventData`

按事件类型出现的安全快照；不含工单body或评论content。

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| title | string | 否 | 见同名引用schema；字段名即协议名称。 |
| priority | Priority | 否 | 见同名引用schema；字段名即协议名称。 |
| assignee_id | ID / null | 否 | 见同名引用schema；字段名即协议名称。 |
| status | TicketStatus | 否 | 见同名引用schema；字段名即协议名称。 |
| version | Version | 否 | 见同名引用schema；字段名即协议名称。 |
| body_changed | boolean | 否 | 见同名引用schema；字段名即协议名称。 |
| deleted | boolean | 否 | 见同名引用schema；字段名即协议名称。 |
| comment_id | ID | 否 | 见同名引用schema；字段名即协议名称。 |
| ai_proposal_id | UUID | 否 | 见同名引用schema；字段名即协议名称。 |

### Schema `TicketEvent`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| ticket_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| actor_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| event_type | string | 是 |  {"enum":["TICKET_CREATED","TICKET_UPDATED","TICKET_ASSIGNED","TICKET_STATUS_CHANGED","TICKET_DELETED","TICKET_COMMENTED"]} |
| ticket_version | Version | 是 | 见同名引用schema；字段名即协议名称。 |
| before_data | EventData | 是 | 见同名引用schema；字段名即协议名称。 |
| after_data | EventData | 是 | 见同名引用schema；字段名即协议名称。 |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Document`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| org_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| uploaded_by | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| original_filename | string | 是 | 见同名引用schema；字段名即协议名称。 |
| media_type | string | 是 |  {"enum":["text/plain","text/markdown","application/pdf"]} |
| size_bytes | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| sha256_hex | string | 是 |  {"pattern":"^[0-9a-f]{64}$"} |
| status | string | 是 |  {"enum":["UPLOADED","PROCESSING","READY","FAILED"]} |
| failure_code | string / null | 是 | 见同名引用schema；字段名即协议名称。 |
| extracted_chars | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| updated_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `UploadInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| file | string | 是 | 唯一multipart部分，原始文件字节；.txt/.md/.pdf。默认文件≤5MiB、请求≤8MiB；不得有Content-Transfer-Encoding。 {"format":"binary"} |

### Schema `DocumentText`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| document_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| content_text | string | 是 | 见同名引用schema；字段名即协议名称。 |
| content_sha256 | string | 是 |  {"pattern":"^[0-9a-f]{64}$"} |
| extractor_version | string | 是 | 见同名引用schema；字段名即协议名称。 |
| extracted_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `IndexInfo`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| document_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| status | string | 是 |  {"enum":["ABSENT","INDEXING","READY","FAILED"]} |
| index_version | Version | 是 | 见同名引用schema；字段名即协议名称。 |
| embedding_model | string | 是 |  {"const":"bge-m3:567m"} |
| embedding_dimension | integer | 是 |  {"const":1024} |
| chunker_version | string | 是 |  {"const":"rune-500-80-v1"} |
| source_content_sha256 | string | 是 | ABSENT时为空，其余为正文hash；不同于原文件hash。 |
| chunk_count | integer | 是 |  {"format":"int64","minimum":0,"maximum":200} |
| failure_code | string / null | 是 | 见同名引用schema；字段名即协议名称。 |
| started_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |
| indexed_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |
| updated_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `QueryInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| query | string | 是 | TrimSpace后1..1000码点、≤4000UTF-8字节，有效UTF-8，无NUL。 {"minLength":1,"maxLength":1000} |
| top_k | integer | 否 |  {"minimum":1,"maximum":10,"default":5} |
| retrieval_mode | string | 否 |  {"enum":["vector","hybrid"],"default":"vector"} |

### Schema `Source`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| chunk_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| document_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| chunk_index | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| source_name | string | 是 | 见同名引用schema；字段名即协议名称。 |
| score | number | 是 | 1-余弦距离；不是准确率。 |
| excerpt | string | 是 | 见同名引用schema；字段名即协议名称。 |
| start_rune | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| end_rune | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| fusion_score | number | 否 | 仅hybrid出现的RRF融合分，不替代score。 |

### Schema `SearchResult`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Source[] | 是 | 见同名引用schema；字段名即协议名称。 |
| retrieval_mode | string | 是 |  {"enum":["vector","hybrid"]} |
| embedding_model | string | 是 |  {"const":"bge-m3:567m"} |
| embedding_ms | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| search_ms | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |

### Schema `Usage`

供应商计数，未知为null；不按字符估算。无召回known=true/model_called=false且全部计数0。

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| known | boolean | 是 | 见同名引用schema；字段名即协议名称。 |
| model_called | boolean | 是 | 见同名引用schema；字段名即协议名称。 |
| prompt_tokens | integer / null | 是 | 见同名引用schema；字段名即协议名称。 |
| completion_tokens | integer / null | 是 | 见同名引用schema；字段名即协议名称。 |
| total_tokens | integer / null | 是 | 见同名引用schema；字段名即协议名称。 |
| cache_hit_tokens | integer / null | 是 | 见同名引用schema；字段名即协议名称。 |
| cache_miss_tokens | integer / null | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Timings`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| embedding_ms | integer | 是 | 服务端墙钟毫秒，可为0。 {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| search_ms | integer | 是 | 服务端墙钟毫秒，可为0。 {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| generation_ms | integer | 是 | 服务端墙钟毫秒，可为0。 {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| total_ms | integer | 是 | 服务端墙钟毫秒，可为0。 {"format":"int64","minimum":0,"maximum":9223372036854775807} |

### Schema `AskResult`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| answer | string | 是 | 见同名引用schema；字段名即协议名称。 |
| sources | Source[] | 是 | 见同名引用schema；字段名即协议名称。 |
| request_id | RequestID | 是 | 见同名引用schema；字段名即协议名称。 |
| insufficient_evidence | boolean | 是 | 见同名引用schema；字段名即协议名称。 |
| retrieval_mode | string | 是 |  {"enum":["vector","hybrid"]} |
| embedding_model | string | 是 |  {"const":"bge-m3:567m"} |
| generation_model | string | 是 | 见同名引用schema；字段名即协议名称。 |
| usage | Usage | 是 | 见同名引用schema；字段名即协议名称。 |
| timings | Timings | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Job`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| org_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| document_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| requested_by | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| actor_user_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| status | string | 是 |  {"enum":["PENDING","RUNNING","RETRYING","SUCCEEDED","FAILED","CANCELED"]} |
| phase | string | 是 |  {"enum":["WAITING","EXTRACTING","INDEXING","DONE"]} |
| run_version | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| attempts | integer | 是 |  {"format":"int64","minimum":0,"maximum":3} |
| max_attempts | integer | 是 |  {"const":3} |
| total_attempts | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| failure_code | string / null | 是 | 见同名引用schema；字段名即协议名称。 |
| retryable | boolean | 是 | 见同名引用schema；字段名即协议名称。 |
| last_dispatch_error | string / null | 是 | 见同名引用schema；字段名即协议名称。 |
| source_sha256 | string | 是 |  {"pattern":"^[0-9a-f]{64}$"} |
| embedding_model | string | 是 |  {"const":"bge-m3:567m"} |
| chunker_version | string | 是 |  {"const":"rune-500-80-v1"} |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| updated_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| started_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |
| completed_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |
| next_run_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `ConversationInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| title | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":100} |

### Schema `SendMessageInput`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| content | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 原始content另限制≤16384UTF-8字节。 {"minLength":1,"maxLength":4000} |
| client_request_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Conversation`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| org_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| owner_user_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| title | string | 是 | 见同名引用schema；字段名即协议名称。 |
| status | string | 是 |  {"const":"ACTIVE"} |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| updated_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Message`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| conversation_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| client_request_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| role | string | 是 |  {"enum":["USER","ASSISTANT"]} |
| content | string | 是 | 见同名引用schema；字段名即协议名称。 |
| status | string | 是 |  {"enum":["STREAMING","COMPLETED","FAILED","INTERRUPTED"]} |
| model_name | string | 是 | 见同名引用schema；字段名即协议名称。 |
| org_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| citations | Source[] | 是 | 见同名引用schema；字段名即协议名称。 |
| input_tokens | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| output_tokens | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| completed_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `TicketArguments`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| title | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":200} |
| body | string | 是 | TrimSpace后计Unicode码点；有效UTF-8，无U+0000。 {"minLength":1,"maxLength":10000} |
| priority | Priority | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `Action`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| conversation_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| assistant_message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| tool_name | string | 是 |  {"const":"propose_create_ticket"} |
| status | string | 是 |  {"enum":["PROPOSED","EXECUTED","REJECTED","EXPIRED"]} |
| org_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| proposed_by_user_id | ID | 是 | 见同名引用schema；字段名即协议名称。 |
| arguments | TicketArguments | 是 | 见同名引用schema；字段名即协议名称。 |
| expires_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| created_at | Timestamp | 是 | 见同名引用schema；字段名即协议名称。 |
| confirmed_by_user_id | ID / null | 是 | 见同名引用schema；字段名即协议名称。 |
| ticket_id | ID / null | 是 | 见同名引用schema；字段名即协议名称。 |
| confirmed_at | Timestamp / null | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `History`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| messages | Message[] | 是 |  {"maxItems":100} |
| actions | Action[] | 是 |  {"maxItems":100} |

### Schema `AIUsage`

AI流的供应商计数协议；不同于RAG Usage，没有known或nullable字段；0不证明供应商已报告真实用量。

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| input_tokens | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |
| output_tokens | integer | 是 |  {"format":"int64","minimum":0,"maximum":9223372036854775807} |

### Schema `SSEMessageStart`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| user_message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| conversation_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| client_request_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `SSEContentDelta`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| delta | string | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `SSECitation`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| source | Source | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `SSEToolApproval`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| action | Action | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `SSEMessageEnd`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| message_id | UUID | 是 | 见同名引用schema；字段名即协议名称。 |
| status | string | 是 |  {"const":"COMPLETED"} |
| usage | AIUsage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `SSEError`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| code | string | 是 | 见同名引用schema；字段名即协议名称。 |
| message | string | 是 | 等于code。 |
| retryable | boolean | 是 | 仅AI_BUSY/MODEL_UNAVAILABLE/MODEL_TIMEOUT为true；不授权自动重发。 |

### Schema `OrganizationPage`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Organization[] | 是 | 见同名引用schema；字段名即协议名称。 |
| page | integer | 是 |  {"format":"int64","minimum":1,"maximum":9223372036854775807} |
| page_size | integer | 是 |  {"format":"int64","minimum":1,"maximum":100} |

### Schema `MemberPage`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Member[] | 是 | 见同名引用schema；字段名即协议名称。 |
| page | integer | 是 |  {"format":"int64","minimum":1,"maximum":9223372036854775807} |
| page_size | integer | 是 |  {"format":"int64","minimum":1,"maximum":100} |

### Schema `TicketPage`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Ticket[] | 是 | 见同名引用schema；字段名即协议名称。 |
| page | integer | 是 |  {"format":"int64","minimum":1,"maximum":9223372036854775807} |
| page_size | integer | 是 |  {"format":"int64","minimum":1,"maximum":100} |

### Schema `CommentPage`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Comment[] | 是 | 见同名引用schema；字段名即协议名称。 |
| page | integer | 是 |  {"format":"int64","minimum":1,"maximum":9223372036854775807} |
| page_size | integer | 是 |  {"format":"int64","minimum":1,"maximum":100} |

### Schema `TicketEventPage`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | TicketEvent[] | 是 | 见同名引用schema；字段名即协议名称。 |
| page | integer | 是 |  {"format":"int64","minimum":1,"maximum":9223372036854775807} |
| page_size | integer | 是 |  {"format":"int64","minimum":1,"maximum":100} |

### Schema `DocumentPage`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Document[] | 是 | 见同名引用schema；字段名即协议名称。 |
| page | integer | 是 |  {"format":"int64","minimum":1,"maximum":9223372036854775807} |
| page_size | integer | 是 |  {"format":"int64","minimum":1,"maximum":100} |

### Schema `JobList`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| items | Job[] | 是 |  {"maxItems":20} |

### Schema `ConversationList`

类型/约束：`{"type":"array","items":{"$ref":"#/components/schemas/Conversation"},"maxItems":50}`

### Schema `UserResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | User | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `AccessTokenResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | AccessToken | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `OrganizationResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Organization | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `MemberResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Member | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `TicketResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Ticket | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `CommentResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Comment | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `DocumentResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Document | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `DocumentTextResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | DocumentText | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `IndexInfoResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | IndexInfo | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `SearchResultResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | SearchResult | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `AskResultResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | AskResult | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `JobResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Job | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `ConversationResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Conversation | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `HistoryResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | History | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `ActionResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | Action | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `OrganizationPageResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | OrganizationPage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `MemberPageResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | MemberPage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `TicketPageResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | TicketPage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `CommentPageResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | CommentPage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `TicketEventPageResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | TicketEventPage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `DocumentPageResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | DocumentPage | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `JobListResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | JobList | 是 | 见同名引用schema；字段名即协议名称。 |

### Schema `ConversationListResponse`

| 字段 | 类型 | 必需 | 约束/说明 |
| --- | --- | --- | --- |
| data | ConversationList | 是 | 见同名引用schema；字段名即协议名称。 |

## 错误码查阅与处理

上面的每个操作列出HTTP错误与code；同一HTTP可有不同领域含义。`INVALID_REQUEST`400、`UNAUTHENTICATED`401、`FORBIDDEN`403、`NOT_FOUND`404和`INTERNAL_ERROR`500为共同分类，500不公开SQL/路径/凭据。严格JSON体还可能413 REQUEST_TOO_LARGE/415 UNSUPPORTED_MEDIA_TYPE；文档上传分别用DOCUMENT_TOO_LARGE/UNSUPPORTED_DOCUMENT_TYPE。登录401用INVALID_CREDENTIALS，受保护资源401用UNAUTHENTICATED，不混淆。

RAG模型错误502包括向量数量/维度/有限非零校验、模型结构/引用错误和上游错误；503为模型不可用/上游限流/并发占满；504为MODEL_TIMEOUT或INDEX_TIMEOUT。文档提取422为INVALID_TEXT/EMPTY_TEXT/TEXT_TOO_LARGE/PDF_UNSUPPORTED，504 PROCESS_TIMEOUT。Job失败字段可保存固定安全码（AUTHORIZATION_REVOKED、DOCUMENT_DELETED、SOURCE_CHANGED、提取/索引/模型类码、TASK_TIMEOUT、PROCESS_CANCELED、STORAGE_ERROR、LEASE_EXPIRED、INVALID_REQUEST）；这些持久化failure_code不意味着任务GET会用对应HTTP错误返回。

文档其他持久化failure_code可能为SOURCE_UNAVAILABLE/SOURCE_CHANGED/PROCESS_CANCELED/PROCESS_FAILED；索引可保存INDEX_FAILED或PROCESS_CANCELED等，HTTP仍按当前动作映射。request_id用于定位当前请求，不能用作幂等键。409后刷新当前状态/版本，不自动重放写操作；403/404清除当前不可访问内容；401重新登录。本文不承诺并发撤权完全线性化、断流自动恢复或跨介质文件/数据库原子事务。

## 健康检查与诊断边界

`GET /healthz`匿名200 `{"status":"ok"}`，不使用data包裹，不访问数据库；只说明API进程存活，不证明依赖或业务可用。业务规范路径外的显式尾斜杠别名GET301、其他方法307，保留query，不另计API；调用方应直接使用规范路径。

可选独立回环诊断端口提供GET /metrics及受限pprof profile/heap/allocs/goroutine/trace；不在主业务端口，不计39个业务操作。CPU/trace seconds为1..30默认5、只允许一个capture；snapshot/scrape最多2；不提供cmdline/symbol/index或gc=1等任意参数。诊断不使用业务JWT作为公开暴露的理由，部署边界见[观测说明](../observability.md)。

## 核对来源与维护

当前权威顺序：`internal/*/handler.go`注册/解析/响应 → DTO模型 → Service授权/状态/预算 → Repository查询/事务。入口见`internal/platform/httpserver/router.go`及`cmd/api/main.go`；领域为auth/organization/ticket/knowledge/rag/jobs/ai。本文不修改协议来适配OpenAPI。

执行 `python3 scripts/check_api_reference.py`：只读解析当前RegisterRoutes与总索引，对比39业务method/path，检查operationId/路径参数/schema引用/严格对象字段/示例/响应媒体及关键安全契约。它是标准库静态契约检查，不替代Go单元/集成、运行时HTTP或完整第三方OpenAPI验证器。本次为文档变更，不访问日常数据库、私有上传、备份、真实.env或云模型。

## 固定错误message字典

下面按领域列出Handler映射。所有受保护接口的401/500也可能由共享认证中间件返回authentication required/internal server error；AI严格JSON解析的400/413/415由共享解析器返回invalid request/request body too large/application/json required，AI自身failure才使用code文本。SSE error.message始终等于code。客户端应稳定依赖code，而非英文message。

| 领域 | HTTP | code | message |
| --- | --- | --- | --- |
| Auth | 400 | INVALID_REQUEST | invalid request |
| Auth | 400 | INVALID_EMAIL | invalid email |
| Auth | 400 | INVALID_PASSWORD | invalid password |
| Auth | 409 | EMAIL_ALREADY_EXISTS | email already registered |
| Auth | 413 | REQUEST_TOO_LARGE | request body too large |
| Auth | 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| Auth | 503 | AUTH_BUSY | authentication service busy |
| Auth | 500 | INTERNAL_ERROR | internal server error |
| Auth | 401 | INVALID_CREDENTIALS | invalid email or password |
| Auth | 401 | UNAUTHENTICATED | authentication required |
| Organizations | 400 | INVALID_REQUEST | invalid request |
| Organizations | 401 | UNAUTHENTICATED | authentication required |
| Organizations | 500 | INTERNAL_ERROR | internal server error |
| Organizations | 413 | REQUEST_TOO_LARGE | request body too large |
| Organizations | 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| Members | 400 | INVALID_REQUEST | invalid request |
| Members | 401 | UNAUTHENTICATED | authentication required |
| Members | 500 | INTERNAL_ERROR | internal server error |
| Members | 404 | NOT_FOUND | resource not found |
| Members | 404 | USER_NOT_FOUND | user not found |
| Members | 403 | FORBIDDEN | permission denied |
| Members | 409 | MEMBER_ALREADY_EXISTS | membership already exists |
| Members | 413 | REQUEST_TOO_LARGE | request body too large |
| Members | 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| Tickets | 400 | INVALID_REQUEST | invalid request |
| Tickets | 401 | UNAUTHENTICATED | authentication required |
| Tickets | 500 | INTERNAL_ERROR | internal server error |
| Tickets | 404 | NOT_FOUND | resource not found |
| Tickets | 413 | REQUEST_TOO_LARGE | request body too large |
| Tickets | 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| Tickets | 400 | INVALID_ASSIGNEE | assignee must be active in organization |
| Tickets | 403 | FORBIDDEN | permission denied |
| Tickets | 409 | CONFLICT | state or version conflict |
| Tickets | 409 | TICKET_CLAIM_CONFLICT | ticket already assigned or version changed |
| Knowledge | 400 | INVALID_REQUEST | invalid request |
| Knowledge | 401 | UNAUTHENTICATED | authentication required |
| Knowledge | 500 | INTERNAL_ERROR | internal server error |
| Knowledge | 404 | NOT_FOUND | resource not found |
| Knowledge | 413 | DOCUMENT_TOO_LARGE | document exceeds size limit |
| Knowledge | 415 | UNSUPPORTED_DOCUMENT_TYPE | unsupported document type |
| Knowledge | 409 | DOCUMENT_STATE_CONFLICT | document state conflict |
| Knowledge | 422 | INVALID_TEXT | document text extraction failed |
| Knowledge | 422 | EMPTY_TEXT | document text extraction failed |
| Knowledge | 422 | TEXT_TOO_LARGE | document text extraction failed |
| Knowledge | 422 | PDF_UNSUPPORTED | document text extraction failed |
| Knowledge | 504 | PROCESS_TIMEOUT | document processing timed out |
| Knowledge | 403 | FORBIDDEN | permission denied |
| RAG | 400 | INVALID_REQUEST | invalid request |
| RAG | 401 | UNAUTHENTICATED | authentication required |
| RAG | 500 | INTERNAL_ERROR | internal operation failed |
| RAG | 404 | NOT_FOUND | resource not found |
| RAG | 403 | FORBIDDEN | permission denied |
| RAG | 409 | INDEX_STATE_CONFLICT | document index state conflict |
| RAG | 409 | INDEX_SOURCE_CHANGED | document source changed |
| RAG | 422 | INDEX_CONTENT_INVALID | document content cannot be indexed |
| RAG | 422 | INDEX_TOO_MANY_CHUNKS | document has too many chunks |
| RAG | 502 | EMBEDDING_COUNT_MISMATCH | embedding count mismatch |
| RAG | 502 | EMBEDDING_DIMENSION_MISMATCH | embedding dimension mismatch |
| RAG | 502 | EMBEDDING_INVALID_VECTOR | invalid embedding vector |
| RAG | 502 | MODEL_INVALID_RESPONSE | invalid model response |
| RAG | 502 | MODEL_UPSTREAM_ERROR | model operation failed |
| RAG | 503 | MODEL_UNAVAILABLE | model unavailable |
| RAG | 503 | MODEL_RATE_LIMITED | model rate limited |
| RAG | 503 | RAG_BUSY | model capacity busy |
| RAG | 504 | MODEL_TIMEOUT | model request timed out |
| RAG | 504 | INDEX_TIMEOUT | index operation timed out |
| RAG | 413 | REQUEST_TOO_LARGE | request body too large |
| RAG | 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| Jobs | 400 | INVALID_REQUEST | invalid request |
| Jobs | 401 | UNAUTHENTICATED | authentication required |
| Jobs | 500 | INTERNAL_ERROR | internal operation failed |
| Jobs | 404 | NOT_FOUND | resource not found |
| Jobs | 409 | DOCUMENT_STATE_CONFLICT | document state conflict |
| Jobs | 503 | ASYNC_DISABLED | asynchronous processing disabled |
| Jobs | 403 | FORBIDDEN | permission denied |
| Jobs | 409 | JOB_STATE_CONFLICT | job state conflict |
| AI | 400 | INVALID_REQUEST | INVALID_REQUEST |
| AI | 401 | UNAUTHENTICATED | UNAUTHENTICATED |
| AI | 500 | INTERNAL_ERROR | INTERNAL_ERROR |
| AI | 404 | NOT_FOUND | NOT_FOUND |
| AI | 413 | REQUEST_TOO_LARGE | request body too large |
| AI | 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| AI | 409 | AI_CONFLICT | AI_CONFLICT |
| AI | 429 | AI_BUSY | AI_BUSY |
| AI | 429 | RATE_LIMITED | RATE_LIMITED |
| AI | 422 | AI_LIMIT | AI_LIMIT |
| AI | 502 | MODEL_INVALID_RESPONSE | MODEL_INVALID_RESPONSE |
| AI | 503 | MODEL_UNAVAILABLE | MODEL_UNAVAILABLE |
| AI | 503 | RATE_LIMIT_UNAVAILABLE | RATE_LIMIT_UNAVAILABLE |
| AI | 504 | MODEL_TIMEOUT | MODEL_TIMEOUT |
| AI | 403 | FORBIDDEN | FORBIDDEN |
| AI | 410 | AI_EXPIRED | AI_EXPIRED |


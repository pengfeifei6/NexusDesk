# W06 索引、检索与问答 API

日期2026-11-11，AI按整周委托定义。沿用W05 `data` / `error{code,message,request_id}`信封和active admin/member授权。非成员、disabled、跨组织、软删除统一404，无身份401。全部有效成员均可索引/检索/问答；不由JWT或前端角色决定权限。

## 路由与输入

组织前缀 `/api/v1/organizations/:orgID/knowledge`。

| 方法 | 后缀 | 输入 | 成功 |
| --- | --- | --- | --- |
| POST | /documents/:documentID/index | 无body/query | 200 IndexInfo，成功或复用有效索引 |
| GET | /documents/:documentID/index | 无body/query | 200 IndexInfo，无记录为ABSENT |
| POST | /search | JSON query、可选top_k | 200 SearchResponse |
| POST | /ask | JSON query、可选top_k | 200 AskResponse |

Search/Ask只接受唯一已知字段，不接受querystring、未知/重复JSON字段、无效Unicode、尾随JSON；整体请求最多16KiB。query去首尾空白后1..1000Unicode码点且<=4000UTF8字节，不含NUL；top_k是JSON整数1..10，默认5，不允许浮点/字符串/null。ID为正int64十进制，与旧接口一致。

文档READY只表示正文提取完成；索引另有ABSENT/INDEXING/READY/FAILED。未解析的UPLOADED/FAILED文档索引409；相同正文hash、模型、切块版本的READY索引直接复用，不重复调用模型。INDEXING重复409。失败可重试；进程崩溃遗留INDEXING由W07解决。

## 返回结构

IndexInfo固定字段：`document_id,status,index_version,embedding_model,embedding_dimension,chunker_version,source_content_sha256,chunk_count,failure_code,started_at,indexed_at,updated_at`。ABSENT的index_version/chunk_count为0、source_content_sha256为空字符串、failure_code和时间为null；模型为bge-m3:567m、维度1024、chunker_version为rune-500-80-v1。有效索引version从1开始；只在实际新执行时递增，复用不变。

Source固定字段：`document_id,chunk_id,chunk_index,source_name,score,excerpt,start_rune,end_rune`。excerpt为原始切块正文，偏移针对W05已规范化正文，闭开区间；score=1-余弦距离，不是准确率。所有ID用JSON整数，Vue无损字符串保存（包括chunk_id/index_version）。不返回向量、storage_key、全篇正文、内部prompt或配置。

SearchResponse：`items: Source[], embedding_model, embedding_ms, search_ms`。按距离升序、chunk_id升序，当前默认score>=0.40才返回，空结果为[]。指标分开记录查询Embedding与SQL检索耗时。

AskResponse：`answer, sources: Source[], request_id, insufficient_evidence, embedding_model, generation_model`。request_id由服务端生成；generation_model=qwen3.5:35b。没有证据时固定回答“知识库未检索到足够依据。”、sources=[]、insufficient_evidence=true，不调用生成器。生成器表示依据不足时同样返回固定拒答；不能从模型声称的ID凭空构建来源。

来源仅限本次真实召回结果，经模型引用ID白名单校验后由服务端构建；有效回答必须有非空正文和至少一个有效引用。回复前再次检查成员、全部召回来源（包括未引用的模型输入）的软删除/索引版本/正文hash；慢调用期间来源失效时409 INDEX_SOURCE_CHANGED，不返回旧答案。前端按纯文本渲染答案/摘录，来源链接指向当前组织文档详情，禁止渲染模型HTML或持久保存正文。

## 错误与运行预算

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | INVALID_REQUEST | 参数或严格JSON契约错误 |
| 401 / 403 / 404 | UNAUTHENTICATED / FORBIDDEN / NOT_FOUND | 沿用身份/组织范围规则 |
| 409 | INDEX_STATE_CONFLICT / INDEX_SOURCE_CHANGED | 状态冲突或正文/来源版本变化 |
| 422 | INDEX_CONTENT_INVALID / INDEX_TOO_MANY_CHUNKS | 正文不适合索引或超200块 |
| 502 | EMBEDDING_COUNT_MISMATCH / EMBEDDING_DIMENSION_MISMATCH / EMBEDDING_INVALID_VECTOR | 向量数量/维度/有限非零规范不符 |
| 502 | MODEL_INVALID_RESPONSE / MODEL_UPSTREAM_ERROR | 模型响应/引用格式或上游错误 |
| 503 | MODEL_UNAVAILABLE / MODEL_RATE_LIMITED / RAG_BUSY | 模型关闭、不可用、限流或本进程并发预算已满 |
| 504 | MODEL_TIMEOUT / INDEX_TIMEOUT | 模型或整次索引超时 |
| 500 | INTERNAL_ERROR | 脱敏存储/内部错误 |

索引总60秒，Embedding每批20秒、批量最多8；查询总25秒，问答总60秒、Chat35秒。模型2并发预算，无自动重试。HTTP写75秒，前端仅索引/问答请求显式使用70秒等待，其余仍按默认15秒（Search固定使用30秒）。任何失败都刷新当前索引元数据，不把浏览器超时等同于后台必然回滚。

默认使用本机Ollama回环地址；本周禁止云模型与外部地址。RAG_ENABLED=false时新模型操作返回503，旧工单/文件业务保持可用，索引状态查询仍可读。不自动拉模型、不使用生产Mock兜底。

模型上下文num_ctx16384，输出最多512tokens；完整序列化messages不超过12000UTF8字节，按低排名移除整块以满足预算，不截断问题或正文。引用白名单只包含实际发送给生成器的块。字节限制是保守预算，不宣传为精确Token计数。

# W05 文档入库设计

日期：2026-11-11。按开发者整周委托由 AI 实现；实际证据见[周报](../weekly/week05.md)。接口字段以 [API 契约](../api/knowledge-documents.md)为准。

## 交付范围

组织内有效成员可以上传、查询、下载并处理 TXT/Markdown；上传者和管理员可以软删除。上传保存原文件，点击“解析文档”后才生成可预览正文。PDF 仅作签名检查后的私有附件保存和下载，解析明确返回 `422 PDF_UNSUPPORTED`。本周没有 RAG、Embedding、PDF 解析器、Redis、Worker 或公开文件地址。

## 分层与数据

`Handler → Service → Repository / ObjectStorage / TextExtractor`。Handler 处理严格 HTTP 输入与固定错误映射；Service 检查真实成员关系、对象状态和删除策略；Repository 执行带组织范围的 SQL，并封装创建、解析完成的短事务。Storage 只接收内部生成的 key；Extractor 不访问数据库。

`knowledge_documents` 保存元数据、组织、上传者、原文件名、随机 key、大小、原文件 SHA-256、状态、固定失败码和时间。`knowledge_document_texts` 一文档一行，保存规范化正文、正文 hash、`utf8-v1` 和提取时间。列表不查询正文。复合外键保证上传者属于目标组织、文本属于同组织文档；active 与角色必须由 Service 实时查库。

## 上传顺序与失败补偿

1. 验证 JWT 身份和目标组织有效成员，再解析 multipart；只接受一个 `file` 部分，无附加字段。任何 `Content-Transfer-Encoding` 均为 400，使用 `NextRawPart` 保留原始字节。
2. 整体请求 `MaxBytesReader` 默认 8 MiB，包括多余部分和结尾数据；文件复制读取上限为 5 MiB+1，用多出的一个字节判超限。不能用 `ParseMultipartForm` 的内存参数替代请求限制。
3. 验证简单文件名、后缀和实际内容。TXT/MD 完整流检查 UTF-8、NUL 和控制字符，拒绝常见二进制伪装；不信任客户端 MIME。PDF 只检查文件头，不声称完整有效性或病毒扫描。
4. 随机 32 字节转 64 位十六进制 key。写入私有根目录的独占临时文件，同时计算 SHA-256 和实际长度；文件同步后通过同目录硬链接原子发布，拒绝覆盖已有 key，再删除临时链接并同步目录。
5. 原对象成功后创建 UPLOADED 元数据并提交。数据库明确拒绝时用独立短 Context 删除对象；删除失败写待核对记录。COMMIT 结果不明时保留对象并记录 `metadata_outcome_unknown`，避免误删已提交行引用的文件。

文件系统与 PostgreSQL 没有跨介质事务。即使正常错误路径有补偿，断电/进程被杀也可能留下无记录孤儿；不能将此方案描述成绝对原子。清理记录自身先写临时文件、Sync/Close，再原子 Rename，重试可替换截断记录，不沿符号链接写出根目录。具体维护规则见 [ADR-0005](../decisions/0005-private-object-storage.md)。

## 解析状态与并发

| 当前状态 | process | text | delete（有权限） |
| --- | --- | --- | --- |
| UPLOADED | 条件更新为 PROCESSING | 409 | 204 |
| PROCESSING | 409 | 409 | 204 |
| READY | 409 | 200 | 204 |
| FAILED | 可重试，重新取得处理权 | 409 | 204 |
| 已软删除 | 404 | 404 | 404 |

条件 UPDATE 保证同一文档至多一个请求取得处理权。解析期间不持有数据库事务；读取私有原文件，移除首部 UTF-8 BOM、将 CRLF/CR 统一为 LF，保留其余文本。输出最多 1 MiB UTF-8 字节，空白正文失败；`extracted_chars` 按 Unicode 码点统计。重新计算源 hash 检查源文件是否被改动，原文件不会被规范化覆盖。

完成阶段短事务锁定同组织且未删除的 PROCESSING 父行，写正文并更新 READY，一起提交。先删除后完成会拒绝发布；先完成后删除则由软删除统一隐藏。失败独立用最多 3 秒 Context 写 FAILED，且只更新仍为 PROCESSING、未删除的行，不覆盖 READY。

处理 Context 默认 10 秒，涵盖 claim、提取和完成阶段；数据库等锁超时同样返回 504/PROCESS_TIMEOUT，取消用 PROCESS_CANCELED。HTTP 读头 5 秒、读请求 15 秒、写响应 30 秒，预留失败收尾时间。前端请求超时 15 秒，客户端停止等待不代表服务端状态已经回滚，应重新查看元数据。

## 授权下载与前端

所有入口独立验证组织关系和对象可见性。下载先查元数据，再打开私有对象，不接受客户端磁盘路径，输出 attachment/octet-stream/nosniff/no-store。软删除后即使文件仍在也返回 404；没有静态文件服务器和匿名下载 URL。已授权并开始发送的下载不承诺被后续撤权/删除即时中断。

Vue 预览仅用纯文本插值，不渲染 Markdown HTML。Blob 下载带 Authorization，完成后撤销 ObjectURL；token 不入 URL、cookie 或本地存储。切组织、路由或会话立即使旧请求失效，晚到的上传/解析/下载结果不能覆盖新页面。按钮可见性只改善操作体验，后端保留全部权限检查。

注册和创建组织沿用 W02 API：注册后回登录；没有组织的用户在工作台创建组织，服务端原子建立 admin/active 关系后进入工单区，再切“知识库”。成员管理仍使用 API。

## W06 / W07 交接

十份原创虚构样例及组织分配见 [samples](samples/README.md)，真实解析结果在[样例清单](../weekly/evidence/week05-sample-manifest.json)。该清单对应已清理的隔离库，不能将其中 ID 当作日常库 ID。

W06 从 READY 正文研究分块、向量与检索评估，不执行文档中的指令。W07 再加入持久任务、租约、心跳、重试预算与幂等完成。当前进程崩溃、claim 提交结果不明或失败状态写库失败可能遗留 PROCESSING；没有自动重试后台任务，也不在请求入口直接抢回 PROCESSING。

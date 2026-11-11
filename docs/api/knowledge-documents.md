# W05 知识库文档 API

日期：2026-11-10。AI按整周委托补齐契约。实现及实际验收结果由[第五周记录](../weekly/week05.md)单独记录。

## 范围与身份

前缀 `/api/v1/organizations/:orgID/knowledge/documents`。复用 RequireUser 和 active admin/member 校验。所有读写限定组织、非软删除对象；非成员、disabled、跨组织/缺失/软删除统一404。上传、列表、详情、下载、处理、预览允许有效成员；删除仅上传者或admin，其他有效成员403。角色不从请求体或JWT读取。

## 七个接口

| 方法 | 后缀 | 输入 | 成功 |
| --- | --- | --- | --- |
| POST | 空 | multipart/form-data，唯一文件字段file，不接受其他字段或第二部分 | 201，Document |
| GET | 空 | page默认1、page_size默认20/最大100、可选status | 200，items/page/page_size |
| GET | /:documentID | 无body/query | 200，Document |
| POST | /:documentID/process | 无body/query | 200，最终READY Document |
| GET | /:documentID/text | 无body/query | 200，DocumentText |
| GET | /:documentID/download | 无body/query | 200，原始字节 |
| DELETE | /:documentID | 无body/query | 204，无响应体；重复404 |

参数必须唯一且在范围内，未知query或多余请求体400。ID是正int64十进制整数；前端无损字符串保存。

multipart 部分禁止出现 `Content-Transfer-Encoding`（包括空值、quoted-printable、base64），返回400 INVALID_REQUEST；不做传输解码，源hash对应原始文件部分字节。请求示例见 [week05.http](../../requests/week05.http)。

## DTO

统一成功为 `{"data":...}`。Document返回且仅返回 `id,org_id,uploaded_by,original_filename,media_type,size_bytes,sha256_hex,status,failure_code,extracted_chars,created_at,updated_at`。failure_code 无失败时为null。整数以JSON数字返回；uploaded_by也是int64 ID，前端需无损处理。media_type为text/plain、text/markdown或application/pdf；时间RFC3339。禁止返回storage_key、磁盘路径、正文或密码。

列表data为 `{"items":[],"page":1,"page_size":20}`；按created_at DESC,id DESC，正文不会进入列表。没有total。

DocumentText字段为 `document_id,content_text,content_sha256,extractor_version,extracted_at`。只有READY可读取，否则409。正文由Vue文本插值或pre显示，禁止v-html。

## 上传与下载

默认文件上限5242880字节（5MiB），整个请求8388608字节（8MiB）；响应超限413。允许.txt/.md有效UTF-8、不含NUL/二进制控制字符；拒绝伪装PDF/二进制。大小从实际流计算，不信任Content-Length或客户端MIME。空文件400；仅空白有效文本可上传，处理时422。

文件名1～200字节，必须是简单名称，无斜杠、反斜杠、控制字符、首尾空白或危险路径组件；扩展名不区分大小写。`.pdf`支持有PDF头的受控上传/下载，文本解析明确422 PDF_UNSUPPORTED。文件头检查不代表病毒扫描或完整PDF有效性验证。

下载使用 `Content-Type: application/octet-stream`、安全编码的 `Content-Disposition: attachment`、`X-Content-Type-Options: nosniff`、`Cache-Control: no-store`。经授权的API流式发送，不提供静态文件URL。无Range/缓存命中语义；前端用授权fetch下载Blob并释放对象URL。

## 处理与失败

UPLOADED或FAILED可条件更新为PROCESSING；PROCESSING/READY重复处理409。提取独立于数据库长事务，默认10秒；TXT/MD去首部UTF-8 BOM、统一CRLF/CR到LF，保留其余正文，最多1048576 UTF-8字节。extracted_chars记录Unicode码点数。

正文与READY在同一个短事务内提交。并发删除后不能提交新的正文或返回可访问的结果。处理失败尽力用独立3秒Context写FAILED，失败信息只保存固定码；数据库不可用或进程崩溃仍可能遗留PROCESSING，W07再设计租约/恢复。

软删除任何状态均可，原对象和已存在正文保留；普通接口隐藏父对象与正文，未来物理清理需核对引用。

## 错误

统一 `{"error":{"code":"INVALID_REQUEST","message":"invalid request","request_id":""}}`，不把原始内部错误或用户正文放入message。前端按白名单code显示中文，不直接渲染任意服务器文本。

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | INVALID_REQUEST | 参数/文件名/multipart格式、缺文件或空文件不合法 |
| 401 | UNAUTHENTICATED | 无有效身份 |
| 403 | FORBIDDEN | 有效成员无删除权 |
| 404 | NOT_FOUND | 无可见组织/文档 |
| 409 | DOCUMENT_STATE_CONFLICT | 状态不允许当前操作 |
| 413 | DOCUMENT_TOO_LARGE | 文件或请求超限 |
| 415 | UNSUPPORTED_DOCUMENT_TYPE | 请求媒体、后缀、内容类型不支持 |
| 422 | INVALID_TEXT / EMPTY_TEXT / TEXT_TOO_LARGE / PDF_UNSUPPORTED | 提取失败，并尝试保存相同failure_code |
| 504 | PROCESS_TIMEOUT | 处理超时，尝试FAILED |
| 500 | INTERNAL_ERROR | 存储/数据库/内部错误，内容脱敏 |

其他保存的失败码：SOURCE_UNAVAILABLE、SOURCE_CHANGED、PROCESS_CANCELED、PROCESS_FAILED。API故障统一500；取消请求通常已无法接收响应。上传文件已保存但数据库明确失败时补偿删除；删除失败或元数据提交结果不明时，在私有根目录记录待核对项，不能声称文件和数据库跨介质事务原子。

## 前端与首次使用

复用已有Client，增加FormData与Blob路径，保留401代次检查和AbortSignal。路由建议 `/organizations/:orgID/knowledge`、`/organizations/:orgID/knowledge/:documentID`；顶部可切换工单与知识库。上传后进入详情；处理成功再取text；删除成功回列表。

另补 `/register` 和 `/desk` 的创建组织表单：注册调用已有三字段接口，成功返回登录页并提示；创建组织调用既有 `POST /organizations`（只有name），取得服务器返回的组织与admin角色后进入工单列表，不在客户端伪造身份。

# NexusDesk 第五周详细开发手册

**版本：W05 · v1.0**  
**主题：知识库文件上传、存储抽象、文档解析、元数据管理与安全测试**  
**适用对象：计算机科班本硕、研二，掌握 Go/Gin/PostgreSQL 基础，已完成 W01～W04 的业务 MVP**  
**建议投入：18～24 小时。文档解析和安全测试可能超时，不应通过取消测试来赶进度。**

> 本周继续使用同一个 `nexusdesk` 仓库。W01～W04 完成了服务基础、组织和认证、工单核心业务、并发领取及 Vue 页面。本周才开始构建知识库的**数据入口**。核心原则是：先保证上传、存储、解析、权限与错误恢复可靠，**下周再引入 Embedding、向量检索和 RAG；第七周再改造为真正的异步任务**。本文是开发学习手册，不是声称已经执行成功的代码仓库。

---

## 0. 本周成果、边界与验收口径

### 0.1 本周必须实现的 P0 功能

1. 在已有组织体系下新增“知识库文档”模块，并新增 `knowledge_documents` 与 `knowledge_document_texts` 两张表，采用新的 Goose 迁移文件 `00007` 和 `00008`，不可修改已经执行的迁移。
2. 增加授权上传接口，支持 **UTF-8 TXT 和 Markdown（.txt、.md）**；限制单文件大小，保存随机存储键、SHA-256、真实字节数、提交者、组织、状态、时间等元数据。
3. 设计并实现 `ObjectStorage` 接口，首版使用本地私有目录。源文件不写进 `web/public`，不对外提供磁盘路径，也不使用原始文件名作为存储路径。
4. 实现文档列表、详情、下载与软删除。所有接口都复用 W02 的组织 active 成员身份验证，并在 SQL 中约束 `org_id`。
5. 实现 TXT/Markdown 的文本提取和持久化。由显式 `POST /process` 触发 **有超时限制的同步处理**；上传接口只做上传，不直接承担耗时解析。处理状态为 `UPLOADED → PROCESSING → READY/FAILED`。
6. 浏览器新增知识库页面，能够上传、查看解析状态、查看文本预览、下载、删除，并显示可理解的失败信息。前端仍复用 W04 登录和组织选择。
7. 设计并运行安全和故障测试，覆盖越权、超大文件、伪装类型、路径穿越、解析失败、重复处理和数据库失败后的文件清理。
8. 使用 Codex 完成一次只读安全审查，并在 `docs/weekly/week05.md` 记录本人实现部分、测试证据、AI 建议和技术收获。

### 0.2 PDF 的边界，避免技术失控

- **P0 必须**：TXT/Markdown 上传、解析、管理全流程。
- **P1 建议**：支持 `.pdf` 的安全上传、列表、下载和元数据管理；不保证所有 PDF 都能提取文本。
- **P1 挑战**：挑选维护状态与许可适合项目的 PDF 文本提取方案，只处理可选择文字的 PDF，并增加页数、解析耗时和输出大小限制；扫描件、加密 PDF、损坏 PDF 返回明确的失败状态。此项不阻塞本周 P0 验收。
- 不做 OCR、DOCX、ZIP 解压、批量导入、远程 URL 下载、多模态解析、病毒扫描服务和内容索引。

这样安排不是回避难度，而是把“安全文件入口”和“复杂格式解析”拆成可以分别验证的任务。下周有可靠文本输入后，才能客观验证 RAG 效果。

### 0.3 本周明确不做

- 不安装向量数据库扩展、不创建 Embedding 任务、不设计 `document_chunks`、不调用模型接口；这些属于 W06。
- 不接入 Asynq、Redis Worker 或分布式锁；异步队列属于 W07。W05 只需把解析逻辑隔离为可复用的接口。
- 不部署 MinIO、S3、Nginx 文件公开目录；先完成 `ObjectStorage` 抽象和本地存储。远端对象存储属于后续增强。
- 不把文件上传附在工单上；知识库文件属于组织，和工单系统保持清晰边界。
- 不使用 AI 自动生成完整的 Storage、Repository、Service、Handler 四层代码。关键接口、权限规则、状态转换、文件持久化由你自己写。

### 0.4 最终演示情景

Alice 是组织 A 的 active 成员。她在知识库页面上传 `db-troubleshooting.md`，页面出现文件记录、大小和 `UPLOADED` 状态。点击“解析文本”后状态变为 `READY`，预览能看到原文内容。Alice 下载得到与原文件 SHA-256 一致的文件。Bob 作为同组织成员可查看；组织 B 的 David 无法通过猜测 document ID 读取、下载、处理或删除该文件。Alice 上传被伪装为 `.md` 的二进制文件及超过限制的文件，接口会拒绝且不会产生可访问的脏记录。软删除后文档不再被普通接口访问。

**验收原则：** 只有真正运行过的测试和前端联调才标记完成。`go test -race` 不等于数据库、文件系统一致性已经验证。

---

## 1. 开始前的 W04 基线检查（30～45 分钟）

在项目根目录运行，路径替换为本机实际路径。

```powershell
Set-Location 'D:\workspace\nexusdesk'
git status --short
go version
go test ./...
go vet ./...
go build ./...
docker compose --env-file .env -f deploy/compose.yaml ps
```

确认 W04 可演示：登录、组织选择、工单列表、工单详情、创建、领取与状态推进都正常；PostgreSQL 服务可用；`organizations`、`users`、`organization_members` 以及工单表已存在。**第五周沿用同一套 JWT 和组织成员校验，不创建第二个身份系统。**

确认 W04 变更已提交、工作区干净后，新建分支：

```powershell
git switch -c feat/week05-knowledge-ingest
```

如果上周尚未通过并发领取测试，本周不修改抢单实现；先为剩余问题建 Issue，并在进入 W05 前修复影响 G1 正确性的阻塞问题。

### 1.1 版本和依赖原则

- 沿用当前项目中已验证的 Go、Gin、PostgreSQL、Goose、Vue 3 版本，不为了本周引入大版本升级。
- 文件存储优先使用 Go 标准库 `os`、`io`、`crypto/sha256`、`path/filepath`、`crypto/rand`、`net/http`，减少不必要的依赖。
- 允许使用已安装的测试库和 SQL 工具。引入第三方 PDF 解析库之前，必须记录维护状态、许可、资源限制和测试依据。
- `.env` 和私有文件目录不提交 Git。开发用内容可以人工制作，不用用户真实文档测试。

---

## 2. 项目目录、需要建的文件与命令

### 2.1 第五周预计的增量目录

```text
nexusdesk/
├── AGENTS.md                             # 追加 W05 规则，保留历史规则
├── .env.example                          # 新增 STORAGE_ROOT / 上传限制配置项
├── .gitignore                            # 忽略 var/storage/、临时文件
├── cmd/api/main.go                        # 注入文档模块依赖
├── internal/
│   ├── knowledge/
│   │   ├── model.go                      # 文档元数据、处理状态
│   │   ├── handler.go                    # 上传、列表、下载、处理等 HTTP 入口
│   │   ├── service.go                    # 上传、解析、删除的业务规则
│   │   ├── repository.go                 # 始终限制 org_id 的数据库访问
│   │   ├── extractor.go                  # TextExtractor 接口与文本处理规则
│   │   ├── extractor_test.go             # UTF-8、空文件与边界测试
│   │   └── service_test.go               # 状态机与错误恢复测试
│   └── platform/
│       ├── storage/
│       │   ├── storage.go                # ObjectStorage 接口
│       │   ├── local.go                  # 私有目录原子写入
│       │   └── local_test.go             # 路径和失败测试
│       ├── config/config.go              # 配置项追加
│       └── httpserver/router.go          # 路由注册
├── migrations/
│   ├── 00007_create_knowledge_documents.sql
│   └── 00008_create_knowledge_document_texts.sql
├── tests/integration/
│   └── knowledge_documents_test.go       # 真实数据库与文件系统测试
├── web/src/
│   ├── api/knowledge.ts
│   ├── views/KnowledgeListView.vue
│   ├── views/KnowledgeDetailView.vue
│   └── components/DocumentUpload.vue
├── var/storage/                           # 本地私有数据，禁止提交
├── docs/
│   ├── api/knowledge-documents.md
│   ├── knowledge/ingestion-design.md
│   ├── decisions/0005-private-object-storage.md
│   ├── testing/knowledge-upload-security.md
│   └── weekly/week05.md
└── requests/week05.http
```

文件名是建议，允许按照当前实际工程结构调整，**不要因为目录树与 W01 的现有结构略有差异，就重复创建第二套事务或权限框架**。

### 2.2 Day 1 创建设计文档目录

```powershell
$dirs = @('docs/api','docs/knowledge','docs/decisions','docs/testing','docs/weekly','requests')
foreach ($d in $dirs) { New-Item -ItemType Directory -Force $d | Out-Null }
$files = @(
  'docs/api/knowledge-documents.md',
  'docs/knowledge/ingestion-design.md',
  'docs/decisions/0005-private-object-storage.md',
  'docs/testing/knowledge-upload-security.md',
  'docs/weekly/week05.md',
  'requests/week05.http'
)
foreach ($f in $files) {
  if (-not (Test-Path $f)) { New-Item -ItemType File -Force $f | Out-Null }
}
```

可以从 ZIP 的“第五周起步文件”复制上述模板，再根据自己的架构决定具体内容。模板中的 `TODO` 不能当作已实现的事实。

### 2.3 更新根目录 AGENTS.md

将压缩包内 `AGENTS.week05.append.md` 的新增规则**追加**到 W01～W04 使用的根目录 `AGENTS.md`，不直接覆盖。文件归属和 AI 参与等级要与第三周、第四周的约定保持一致。禁止 Codex 未经许可更改数据库已执行迁移、认证逻辑、工单状态机或读取私有上传目录。

---

## 3. Day 1：设计文档业务、状态机和 API（2～3 小时）

**你亲自做：** 从产品角度定义“组织知识库文档”的生命周期、权限、错误规则与最小接口。先写文档，再请 Codex 进行只读审查。

### 3.1 状态定义和业务边界

| 状态 | 说明 | 允许的后续操作 |
|---|---|---|
| `UPLOADED` | 原文件已成功保存、元数据已提交 | 允许处理、下载、删除 |
| `PROCESSING` | 当前正在提取文本 | 等待成功或失败；不得并发再处理 |
| `READY` | 文本已持久化并可供下周索引 | 允许预览、下载、删除 |
| `FAILED` | 文本解析失败，原文件仍受控保存 | 允许查询失败码、再次处理、下载、删除 |

- `UPLOADED → PROCESSING → READY` 是成功路径；`PROCESSING → FAILED` 是失败路径。
- 本周对 `READY` 文件的再次解析，先返回 `409`，未来新增“重新解析”时再设计独立接口。
- `FAILED` 可以手动重试，但必须做条件状态更新防止两个进程同时处理。
- 上传即使成功，也**不代表文档可以用于 RAG**，只有 `READY` 说明文本提取完成；W06 仍需额外索引状态。
- 软删除可在任意非物理删除状态发生。删除后 API 默认不可见；本周只标记 `deleted_at`，不立即删除磁盘文件，后续再设计安全清理任务。

### 3.2 权限方案

复用 W02 的 active 组织成员判断：组织成员可以上传、列出、读取、下载和触发自己的组织文档处理；删除权限建议限定为**上传者本人或组织 admin**。服务端对每个请求做成员与对象级权限校验，SQL 都包含组织限制。没有加入组织的用户即使获得文档 ID，也不应该访问到原文件或原文。跨组织请求按既有 W03/W04 约定返回 `404`，减少对象存在性泄露。

### 3.3 API 契约（固定第一版）

| 方法与路径 | 功能 | 成功 | 常见错误 |
|---|---|---|---|
| `POST /api/v1/organizations/:orgID/knowledge/documents` | multipart 上传 `file` | `201` | `400/401/403/413/415/500` |
| `GET /api/v1/organizations/:orgID/knowledge/documents` | 分页与状态筛选 | `200` | `401/403` |
| `GET /api/v1/organizations/:orgID/knowledge/documents/:documentID` | 查询元数据 | `200` | `404` |
| `POST /api/v1/organizations/:orgID/knowledge/documents/:documentID/process` | 显式解析文本 | `200` | `404/409/422/500` |
| `GET /api/v1/organizations/:orgID/knowledge/documents/:documentID/text` | 获取已解析文本预览 | `200` | `404/409` |
| `GET /api/v1/organizations/:orgID/knowledge/documents/:documentID/download` | 授权下载原文件 | `200` | `404` |
| `DELETE /api/v1/organizations/:orgID/knowledge/documents/:documentID` | 软删除 | `204` | `403/404` |

建议使用统一错误 JSON，例如 `{"code":"DOCUMENT_TOO_LARGE","message":"File exceeds limit"}`。请求中包含用户提供的 `filename`，但服务端只把它作为展示元数据，不参与磁盘路径拼接。

**当天交付：** `docs/api/knowledge-documents.md` 和 `docs/knowledge/ingestion-design.md` 的初稿；PR 描述记录 P0 与 P1 的边界。Codex 只读审查后，由你决定最终修改。

---

## 4. Day 2：数据库设计和迁移（3 小时）

**你自己设计：** `knowledge_documents` 保存文档元数据与解析状态；`knowledge_document_texts` 保存正文。两表分开可以避免列表查询读取大文本，也为下周 `document_chunks` 留出空间。

### 4.1 数据表字段建议

`knowledge_documents` 包含：`id`、`org_id`、`uploaded_by`、`original_filename`、`storage_key`、`media_type`、`size_bytes`、`sha256_hex`、`status`、`failure_code`、`extracted_chars`、`created_at`、`updated_at`、`deleted_at`。建议对 `(org_id, created_at DESC, id DESC)` 创建索引。`storage_key` 使用唯一约束。身份与组织外键引用前几周已有表，字段类型必须和实际旧表完全一致。

`knowledge_document_texts` 包含 `document_id`（主键、外键）、`content_text`、`content_sha256`、`extractor_version`、`extracted_at`。单行上限由业务代码限制，开发版建议最多保留 1 MiB 的提取文本，避免小项目无意识扩大数据库体积。

### 4.2 Goose 迁移骨架（示意，不是可直接运行的完整建表 SQL）

```sql
-- +goose Up
CREATE TABLE knowledge_documents (
  id BIGSERIAL PRIMARY KEY,
  org_id BIGINT NOT NULL REFERENCES organizations(id),
  uploaded_by BIGINT NOT NULL REFERENCES users(id),
  original_filename TEXT NOT NULL,
  storage_key TEXT NOT NULL UNIQUE,
  media_type TEXT NOT NULL,
  size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
  sha256_hex CHAR(64) NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('UPLOADED','PROCESSING','READY','FAILED')),
  failure_code TEXT,
  extracted_chars BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
-- 你自己追加组织分页索引和其他约束

-- +goose Down
DROP TABLE knowledge_documents;
```

**注意：** 上述只是 W05 设计骨架，如果你原来的 `organizations.id` 或 `users.id` 使用 UUID，则应改为一致的类型。`00008` 的 Down 应先于 `00007` 回滚。正式建表前先写清关系图、索引查询场景和删除策略。

### 4.3 本周值得自己验证的数据库问题

- 上传文件系统写入成功，但数据库 Insert 失败会怎样？如何清理已保存的文件？
- 数据库事务能否自动回滚本地文件写入？答案是否定的，需要业务补偿。
- 为什么列表查询不能 `SELECT *` 拉取所有文档正文？
- 同一个组织两个成员上传相同的文件，能否按哈希去重？本周记录方案，不实现跨组织共享或全局 dedup。
- `deleted_at` 的软删除如何影响查询、下载和空间回收？

**当天交付：** 自己完成迁移 SQL，在开发数据库执行 `up/status`，在专用测试数据库尝试 `down/up`。记录运行命令，不把数据库连接密码放进文档。

```powershell
goose -dir migrations postgres $dbConn up
goose -dir migrations postgres $dbConn status
```

---

## 5. Day 3：Storage 抽象与安全上传（3～4 小时）

这是本周最有 Go 后端价值的核心任务。**你亲自编写**本地文件保存、数据校验、哈希和数据库元数据写入的业务协调逻辑。

### 5.1 ObjectStorage 的职责

在 `internal/platform/storage/` 设计简洁接口。接口签名由你定，建议只提供：`Put(ctx, key, reader)`、`Open(ctx, key)`、`Delete(ctx, key)`，不要暴露用户传入的任意系统路径。**key 必须由服务端生成并验证，不得等同于 filename。** 本地实现用 `STORAGE_ROOT` 指向仓库外或 `var/storage/` 私有目录，确保该目录不受 Web 服务器静态映射。

### 5.2 推荐的安全上传步骤

1. 先验证 JWT、当前组织 active 成员资格和用户上传权限。
2. 对 HTTP 请求总大小设置上限（例如 8 MiB），对真正的文件内容设置更严格的 5 MiB 上限。Gin 的 multipart 内存阈值**不是**请求总大小限制。
3. 只接受 `.txt` 和 `.md`（P0）；客户端 `Content-Type` 与扩展名都不能作为唯一的可信依据。对文本内容执行 UTF-8 有效性、NUL 字节和大小检查；PDF P1 再补文件头识别。
4. 服务端生成随机 `storage_key`，将内容以临时文件形式写入私有目录；采用流式读取和 `sha256.New()` 计算摘要，避免一次性将整个上传读入内存。临时文件成功写入后，同一文件系统内改名为最终文件。
5. 文件保存成功后写数据库元数据，初始状态为 `UPLOADED`。如果数据库失败，删除新保存的文件；如删除也失败，记录待清理文件，不得静默忽略。
6. 返回 `201` 和文件 ID、状态、名称、大小、摘要等字段。前端永远不获取真实服务器绝对路径。

**关键边界：** 文件系统与 PostgreSQL 不属于同一个原子事务。此处通过写入顺序、失败补偿与后续清理避免长期不一致，不要在简历里声称“文件和数据库强一致事务”。

### 5.3 Go 标准库使用建议

- `http.MaxBytesReader` 限制 HTTP request body，总上限要给 multipart 开销留下空间。
- `io.LimitReader` 或带计数的流拷贝限制实际文件字节数；超限必须返回 `413`。
- `crypto/rand` 生成足够随机的存储键，`filepath` 只拼接服务端验证的相对 key。
- 使用 `crypto/sha256` 计算源文件摘要。它用于文件完整性识别，不是密码哈希。
- 临时文件使用私有目录和受限权限；创建目录时注意 Windows/Linux 的权限差异。
- 处理 multipart 临时文件清理，避免反复上传造成系统临时目录长期占用。

参考安全原则：OWASP File Upload Cheat Sheet，https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html 。

### 5.4 目录与环境配置

建议 `.env.example` 增加：

```dotenv
STORAGE_ROOT=./var/storage
UPLOAD_MAX_FILE_BYTES=5242880
UPLOAD_MAX_REQUEST_BYTES=8388608
TEXT_EXTRACT_MAX_BYTES=1048576
TEXT_EXTRACT_TIMEOUT_SECONDS=10
```

`.gitignore` 确保包含 `var/storage/`、`.env` 和上传过程产生的临时文件，但继续保留已有忽略规则。不要用 `git clean -fdx` 直接清空存储目录。

**当天交付：** 存储接口、上传 API、本地文件安全写入、hash 记录和简单的上传成功/超限/无权限测试。

---

## 6. Day 4：文档元数据、组织隔离与下载（2～3 小时）

**你自己实现：** 列表、详情、下载和软删除的 Handler、Service 与 Repository。在数据查询中始终使用 `org_id`。下载由服务端打开内部存储对象，不能让浏览器直接访问 `file:///` 或任意磁盘路径。

### 6.1 元数据 API 约定

- 列表默认按 `(created_at DESC, id DESC)` 排序，支持 `limit` 和 `cursor` 或沿用 W03 已有的分页方式；不要使用无界全量查询。
- 列表只返回元数据，不返回 `content_text`。
- 详情应包括 `status`、`failure_code`（如果失败）、`extracted_chars`、`uploaded_by`、`sha256_hex` 等字段。
- 下载设置 `Content-Disposition: attachment`，使用安全的文件名编码，并设置 `X-Content-Type-Options: nosniff`；不要以不受信任的 HTML 文本方式内联展示内容。
- 软删除先校验本人上传者或组织 admin 权限，再将 `deleted_at` 标记为当前时间。重复删除是否返回 `204`/`404`，需要在 API 文档中选定并保持一致。

### 6.2 需要验证的权限测试

- Alice 属于组织 A，David 属于 B。David 对组织 A 文件的详情、下载、处理和删除都不能成功。
- Bob 属于 A，但不是该文件上传者且不是 admin。按本周规则，Bob 可以查看和解析，但不能删除。
- 用户被从 A 移除或设为 inactive 之后，原来的 Token 即使尚未过期，也不能再读取 A 的文件。
- 软删除的记录不出现在列表，也不能下载与预览。

**当天交付：** 五个可调试的业务接口（含上传）；前端可暂时只通过 HTTP 请求工具验证。

---

## 7. Day 5：文本解析与状态转换（3～4 小时）

本周故意把文本解析放在上传之后，采用单独的处理接口。这样第七周改造为队列任务时，上传接口和持久化模型不需要重做。

### 7.1 TextExtractor 接口

建议按文件类型实现提取器。至少具备 `Extract(ctx, reader, mediaType)` 的思路；返回标准化文本、可供审计的解析版本和受控错误。接口不应直接依赖 Gin 或 PostgreSQL。

P0 文本解析规则：

- `.txt` 与 `.md` 只接受有效 UTF-8 文本，不自动猜测 GBK/UTF-16。
- 保留有意义的段落和标题；可以统一 CRLF/LF，不做激进清洗。
- 非法 UTF-8、NUL、空正文、大于提取文本上限，返回稳定的 `failure_code`，而不是整个错误堆栈。
- 读取源文件必须受限，使用 Context 和输出字符数限制；禁止无限制将内容写入数据库或日志。
- `READY` 只能在 `knowledge_document_texts` 写入成功之后提交，禁止出现状态 READY 但正文缺失的情况。

### 7.2 状态转换与数据库事务

建议使用数据库条件更新完成 `UPLOADED/FAILED → PROCESSING` 的抢占；如果已有另一个请求在处理，返回 `409`。处理完成后，在同一 PostgreSQL 事务中写或更新 `knowledge_document_texts`，并把状态设为 `READY`。解析失败要更新为 `FAILED` 并保存简短错误码。单次处理限制超时，例如开发版 10 秒；请求取消或进程崩溃时可能留下 `PROCESSING`，**本周需要记录这种局限，并预留后续恢复策略**，不要声称进程故障情况下具备自动恢复能力。

本周默认不实现多个进程同时解析同一份文档，只通过数据库状态条件更新避免重复执行。**不要在整个文件解析期间持有数据库事务或行锁**，否则会产生不必要的锁等待。

### 7.3 显式解析请求

```http
POST /api/v1/organizations/1/knowledge/documents/42/process
Authorization: Bearer <ACCESS_TOKEN>
```

P0 可以同步返回最终状态，注意此处**同步是实现约束，不是推荐的长期生产形态**。第七周将改为 `202 Accepted` 与任务状态查询，并保留上述请求语义的兼容性设计。

### 7.4 PDF P1 附加任务

如果 P0 已通过测试，可以尝试 PDF。优先只处理可选择文字的 PDF，定义 `PDF_UNSUPPORTED`、`PDF_ENCRYPTED`、`PDF_TOO_COMPLEX` 等错误类别。明确文件头检查和扩展名匹配只是初级安全措施；PDF 解析器处理不可信输入仍有资源耗尽与漏洞风险。对解析器做超时、页数、文件大小和输出长度限制。无法满足安全边界时，保留上传/下载支持但关闭 PDF 自动解析，不能勉强宣称支持。

**当天交付：** `/process`、`/text`、`Extractor`、状态转换单测和真实数据库事务测试。

---

## 8. Day 6：Vue 知识库页面与全流程联调（2～3 小时）

前端重点是能真实使用，不要求高质量 UI。你自己要掌握前端与后端的数据流，Codex 可以在明确的范围内协助实现页面基础样式。

### 8.1 只新增三种 UI 部分

1. `KnowledgeListView.vue` 展示组织下的文档列表、上传者、大小、状态、更新时间；提供状态筛选和分页。
2. `DocumentUpload.vue` 允许选择文件、提示 `.txt/.md` 和 5 MiB 限制，上传后更新列表。错误时展示服务端返回的业务信息。
3. `KnowledgeDetailView.vue` 展示文档元数据、解析按钮、文本预览、下载和删除按钮；对于 READY/FAILED/PROCESSING 展示不同交互状态。

沿用 W04 的 `api/http.ts`、JWT、组织选择及 Router，不要重新引入第二个全局状态库。上传使用 `FormData`，不要自行强制设置 multipart 的 `Content-Type` 边界，让浏览器处理。

### 8.2 联调流程

- 登录组织 A，上传 `handbook.md`，UI 展示 `UPLOADED`。
- 点击解析，看到 `PROCESSING` 后变成 `READY`（同步请求可能只看到最终状态，也必须正确处理等待态）。
- 打开文本预览，核对标题和正文确实来源于上传文件。
- 下载文档，计算本地文件 SHA-256，与数据库记录一致。
- 换组织 B，确认无法通过粘贴组织 A 文档 URL 访问。
- 在浏览器开发者工具 Network 中查看请求和响应状态，不在日志中输出 access token。

**当天交付：** `web/` 增量页面与可复现联调记录，附上截图位置或演示视频链接占位。

---

## 9. Day 7：安全测试、Codex Review 与一周复盘（3～4 小时）

本周的测试质量比 UI 精致程度更重要。建议至少覆盖以下 **24 个测试场景**。

### 9.1 业务与权限（8 项）

| 编号 | 测试情景 | 预期 |
|---|---|---|
| B01 | active 成员上传有效 TXT | `201`，记录 `UPLOADED` |
| B02 | active 成员上传有效 MD | `201`，存储 hash 正确 |
| B03 | 列表分页和状态过滤 | 无越界记录、排序稳定 |
| B04 | 查询本人组织文档详情 | `200`，不泄露路径 |
| B05 | 组织 B 成员猜测 A 的文档 ID | `404`，不读取文件 |
| B06 | 非 active 成员使用旧 Token | 拒绝访问 |
| B07 | 非上传者且非 admin 删除 | 拒绝删除 |
| B08 | 删除后再次下载/预览 | `404` |

### 9.2 文件安全（8 项）

| 编号 | 测试情景 | 预期 |
|---|---|---|
| S01 | 5 MiB 限制内合法文件 | 上传成功 |
| S02 | 比 5 MiB 多 1 字节 | `413`，不留可访问数据 |
| S03 | `.exe` 或 `.html` | `415` |
| S04 | `.md` 伪装二进制内容 | `415` 或 `422`，错误稳定 |
| S05 | 伪造客户端 Content-Type | 不能绕过校验 |
| S06 | 文件名 `../../x.md` | 不逃逸存储根目录 |
| S07 | 空文件、无文件字段 | `400/422`，按契约一致 |
| S08 | 上传成功但数据库 Insert 失败 | 清理新文件并记录异常 |

### 9.3 解析与一致性（8 项）

| 编号 | 测试情景 | 预期 |
|---|---|---|
| P01 | 有效 Markdown 解析 | `READY`，原文可查询 |
| P02 | 上传非法 UTF-8；或用测试故障注入触发解析失败 | 上传按契约拒绝；解析故障则 `FAILED` |
| P03 | 空正文或仅空白字符 | 按契约失败 |
| P04 | 提取文本超过输出上限 | 失败且未写入超大正文 |
| P05 | 两个请求同时处理同一文档 | 至多一个取得处理权 |
| P06 | `READY` 重复解析 | `409`，无重复写入 |
| P07 | 文本 Insert 失败 | 文档不能错误地成为 READY |
| P08 | 处理超时/取消 | 状态与局限被记录，后续可恢复设计明确 |

**特别说明：** HTTP 层的 `413/415/422` 应在 API 文档中统一；测试报告写实际响应。集成测试必须使用隔离的测试数据库和临时文件目录，不要用开发环境真实存储目录做破坏性测试。

### 9.4 实际执行命令

```powershell
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
git status --short
```

`go test -race` 可帮助检查 Go 共享内存层面的数据竞争，但不能证明文件系统和数据库状态一致。并发 `/process` 的数据库行为需要单独做真实 PostgreSQL 集成测试。若你把慢速测试放在单独 build tag，请运行相应测试命令并记录。

### 9.5 Codex 只读审查与个人复盘

在 Codex 中运行只读审查，重点关注：

- 用户提供的文件名是否直接参与磁盘路径、文件扩展名验证是否可绕过。
- 是否设置请求体与文本解析输出的大小限制；是否可能造成高内存占用。
- 是否泄露文件存储路径、正文、JWT 或数据库密码。
- 所有文档查询是否包含 `org_id` 且尊重 `deleted_at`。
- 文件保存成功后数据库失败时，是否有清理和错误记录。
- `READY` 与正文写入是否处于同一数据库事务。
- `PROCESSING` 的崩溃恢复是否被如实记录，不虚构自动恢复能力。

对每个建议，先通过代码与测试确认，再人工决定修复。将本周真实学习结果写入 `docs/weekly/week05.md`，至少记录一个你独立解决的问题和一个 AI 帮你发现但由你验证的问题。

---

## 10. 本周 AI 协作边界与五段提示词

### 10.1 人与 AI 的分工

| 内容 | 必须你自己主导 | 允许 AI 做什么 |
|---|---|---|
| API 与状态语义 | 设计、写契约、决策 | 提出漏项和反例 |
| 迁移 SQL 与租户隔离 | 写 schema、SQL、约束 | 审查索引和安全性 |
| 本地 Storage 与上传 | 亲自实现核心读写、hash、补偿 | 提示标准库用法和代码问题 |
| 文本解析与事务 | 亲自实现状态更新和文本提交 | 设计测试边界，解释问题 |
| Vue 界面 | 自己掌握请求与状态映射 | Codex 可生成布局与局部组件 |
| 测试与调试 | 设计关键情景、运行并解释 | 补充测试和帮助定位 |
| README 与复盘 | 记录真实结果 | 规范格式、辅助语言润色 |

### 10.2 Day 1 设计审查提示词

```text
你是 NexusDesk 的 Go 后端架构审查员。请阅读 docs/knowledge/ingestion-design.md 和 docs/api/knowledge-documents.md。
仅审查，不修改文件。指出组织隔离、上传状态、错误码、文件与数据库不一致、软删除和 W06/W07 扩展上的遗漏。
每项按“触发条件、影响、建议验证方法”输出。不要生成完整模块代码。
```

### 10.3 Day 3 上传代码审查提示词

```text
只读检查 internal/platform/storage/、internal/knowledge/ 上传代码和相关迁移。
重点检查路径穿越、权限验证、HTTP body 上限、multipart 临时文件清理、伪造 MIME、随机 key、哈希一致性、数据库失败后的文件补偿。
请给出文件位置、风险级别、可复现步骤与建议测试。不要修改其他模块。
```

### 10.4 Day 5 状态机问题诊断提示词

```text
我正在排查文档 process 接口。下面是我观察到的行为、日志和代码。
请先列出 3 个按可能性排序的假设，再告诉我每个假设如何用 SQL、日志或测试验证。
在确认根因之前不要直接输出完整修复代码，也不要自行编辑文件。
```

### 10.5 Day 6 前端授权提示词

```text
你可以仅修改 web/src/views/KnowledgeListView.vue、web/src/views/KnowledgeDetailView.vue
和 web/src/components/DocumentUpload.vue 的展示层内容。
严格使用 docs/api/knowledge-documents.md 的接口与错误语义。
不得修改后端、JWT 处理、权限逻辑、已有工单页面和依赖版本。
完成后列出每个修改文件、手动联调步骤和仍需我确认的交互问题。
```

### 10.6 Day 7 安全审查提示词

```text
请对本周相对 feat/week04-claim-web-mvp 基线的改动进行只读 Review。
优先报告真正有安全或一致性影响的 Bug，不要只关注代码风格。
每个发现列出文件位置、触发条件、后果、最小验证步骤。区分已确认问题和待验证假设。
不要自动修复，不要声称测试已经运行，除非你实际执行并显示结果。
```

---

## 11. 逐日自测问题与求职复盘

### Day 1

- JWT 已有效，为什么仍然不能允许它直接下载任意 document ID？
- 文件内容进入 RAG 之前，为什么要区分“已上传”和“已解析”？

### Day 2

- 事务为什么不能回滚本地磁盘写入？你打算如何补偿？
- 哪些字段必须做数据库约束，哪些必须在 Service 验证？

### Day 3

- `ParseMultipartForm` 的内存参数能不能限制请求总大小？
- 为什么原始文件名不能用作磁盘路径？如何防止存储 key 冲突？
- 如何在不读完整文件入内存的情况下计算 SHA-256？

### Day 4

- 为什么列表与下载权限要分别检查？
- `deleted_at` 已标记但文件还在磁盘，如何防止继续下载？

### Day 5

- 为什么不能在解析整个文件的十秒内一直持有数据库事务？
- `PROCESSING` 状态进程崩溃后会怎样？W07 需要补什么机制？

### Day 6

- 前端何时调用上传接口，何时调用解析接口？
- 前端拦截器与后端组织权限校验各承担什么作用？

### Day 7

- 你能否不用 AI 重写一个带大小限制和哈希计算的上传核心函数？
- 能否给面试官演示跨组织下载失败以及数据库插入失败后的文件清理？

**每晚在 `docs/weekly/week05.md` 记录：** 今日目标、个人写的代码、AI 给出的建议、实际测试、遇到的问题、独立解释程度（1～4 级）、下一天计划。

---

## 12. G2 前置检查与下一周交接

**W05 完成标准**：组织内文件的上传、元数据列表和详情、授权下载、软删除、TXT/MD 文本提取、状态管理和前端基本操作都可以真实运行；重点安全测试通过，且有完整记录。PDF 文本提取属于扩展项，不得因为它延迟基础模块验收。

进入 W06 前，应输出一份用于 RAG 的可靠样例集：**至少 10 份自己创建或允许使用的 TXT/Markdown 文档**，每份有标题、正文、组织归属和解析状态；不要上传真实公司机密、受保护的个人数据或他人私有文件。下一周开始研究文本切块、Embedding、pgvector 和检索效果评估。

G2 的正式完成点仍然位于后续 AI 版本，不因为本周已有知识库页面就宣称 RAG 已上线。

### 12.1 周末 Git 验收与提交

```powershell
git status --short
git diff --check
go test ./...
go vet ./...
go build ./...
# 确认未将 .env、私有文件、临时文件加入 Git
git add -A
git diff --cached --stat
git diff --cached
git commit -m 'feat: add secure knowledge document ingestion'
```

Git 提交信息要准确反映本周真实完成内容。未做的 PDF 解析、对象存储集群和异步处理不要写入提交说明或简历。

### 12.2 推荐参考文档

- Go `net/http` 官方文档：https://pkg.go.dev/net/http
- Go `io` 官方文档：https://pkg.go.dev/io
- Go `crypto/sha256` 官方文档：https://pkg.go.dev/crypto/sha256
- OWASP File Upload Cheat Sheet：https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html
- PostgreSQL 事务文档：https://www.postgresql.org/docs/current/tutorial-transactions.html
- Vue 3 官方文档：https://vuejs.org/guide/introduction.html

---

## 附录 A. 本周完成勾选表

- [ ] W04 G1 基线稳定，W05 开发分支建立。
- [ ] `docs/api/knowledge-documents.md` 完成并经过审查。
- [ ] `docs/knowledge/ingestion-design.md` 完成并决定 P0/P1 范围。
- [ ] `00007`、`00008` 迁移在测试数据库验证可回滚。
- [ ] 本地私有 Storage 和安全上传已完成。
- [ ] 文件大小、类型、哈希、路径安全和失败补偿已测试。
- [ ] 文档元数据列表、详情、授权下载、软删除已完成。
- [ ] TXT/MD 解析和 `UPLOADED/PROCESSING/READY/FAILED` 状态转换已完成。
- [ ] 解析文本与 READY 状态事务一致性已测试。
- [ ] Vue 知识库页面已完成真实联调。
- [ ] 24 项测试情景已逐项标记结果；失败项已记录。
- [ ] Codex 只读 Review 已完成，建议经过个人验证。
- [ ] `docs/weekly/week05.md` 和 README 更新，明确 W06/W07 待完成事项。

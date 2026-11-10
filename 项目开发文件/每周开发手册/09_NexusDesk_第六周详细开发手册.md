# NexusDesk 第六周详细开发手册

**版本** W06 · v1.0  
**主题** 文本切块、Embedding、pgvector、精确向量检索、带来源的 RAG 问答与基础评测  
**面向对象** 计算机本硕、研二，已会 Go/Gin/PostgreSQL，可实现简单接口，已完成 W01～W05  
**建议投入** 20～26 小时，若模型接口或数据库环境受阻，优先保证离线测试与检索链路正确  
**核心学习原则** 你亲自实现文本切块、向量模型接口、SQL 检索、授权校验、RAG 编排和评测逻辑。Codex 主要负责只读审查、辅助测试、前端界面和文档整理。

> 本手册是逐日开发与学习指引，不代表项目代码已经完成或通过测试。继续使用现有 `nexusdesk` 仓库，不能复制生成一套全新业务系统。W05 的 `knowledge_documents.status=READY` 只表示文档正文已经提取成功，W06 的“索引 READY”是另一种独立状态。W07 才把同步索引改造成 Asynq/Worker 异步任务。

---

## 0. 本周目标、边界和交付物

### 0.1 必须完成的 P0 能力

1. 复用 W05 的 `knowledge_document_texts.content_text`，为正文建立确定性的字符偏移切块流程。保留 Chunk 顺序、正文偏移和版本信息。
2. 为 Embedding 定义可替换的 Go 接口，支持真实模型适配器以及**仅用于离线测试**的确定性 Mock。固定一个模型和向量维度，拒绝维度错误及 NaN/Inf。
3. 在原有 PostgreSQL 上启用 pgvector，新增 `knowledge_chunks`、`knowledge_document_indexes`（或同等命名）两张表，采用新的迁移文件。组织权限沿用 W02；文档正文和组织归属沿用 W05。
4. 提供手动触发同步索引、查看索引状态、组织级相似度检索三个接口。对重复索引、模型错误、文档已删除和索引失败有清晰处理。
5. 基于检索结果实现**非流式**知识库问答，返回回答文本和真实来源结构，不允许模型凭空制造引用。
6. 完成至少 20 条人工标注的检索题目，记录 Recall@5、MRR、P95 检索时延、无答案问题的行为与跨组织隔离测试。
7. 完成最小前端检索与问答入口，复用 W04 登录和 W05 知识库页面。
8. Codex 至少完成一次只读架构/安全审查、一次测试审查；你的工作记录能够说明自己实现的核心代码和实际运行证据。

### 0.2 本周不做

- 不引入 Asynq、Redis 队列和后台 Worker。这是 W07 的主要任务。
- 不实现 SSE、Function Calling、MCP、自动创建工单。这些属于 W08 及后续。
- 不同时接入多个 Embedding 模型，不做大模型训练、Fine-tuning、复杂向量数据库迁移。
- 不一开始就创建 HNSW / IVFFlat 索引。先测试精确检索正确性与时延，记录以后引入近似索引的理由。
- 不做没有验证依据的“智能准确率 95%”宣传；20 道题只算项目基线，不代表充分的统计评估。
- 不把组织私有内容发送给未获授权的第三方模型；外部模型调用的真实文档须经过数据使用许可确认。

### 0.3 G1 到 G2 的衔接

**W05 数据入口**：上传 → 私有存储 → TXT/Markdown 文本提取 → `knowledge_documents.status=READY`。  
**W06 检索与问答**：获取已解析正文 → Chunk → Embedding → pgvector → Search → Prompt → Answer + Sources。  
**W07 异步可靠性**：替换同步索引入口为消息队列与 Worker，增加任务重试、幂等和故障恢复。  
**W08 Agent**：加入 SSE、工具调用、人工确认流程。

**本周演示情景**：Alice 属于组织 A，上传数据库故障排查文档并在 W05 完成解析。她在 W06 手动索引，输入“数据库连接超时需要先检查什么”，搜索能返回相关 Chunk、所属文档和相关性排序，问答返回有依据的建议与引用。组织 B 的 David 无法通过同样的关键词搜出 A 的材料；查询无关问题时，系统不会伪造知识库证据。

---

## 1. 开始前的基线检查与开发分支（30～45 分钟）

在 PowerShell 中运行，目录按本地环境调整。

```powershell
Set-Location 'D:\workspace\nexusdesk'
git status --short
go version
go test ./...
go vet ./...
go build ./...
docker compose --env-file .env -f deploy/compose.yaml ps
```

用浏览器或 `requests/week05.http` 确认 W05 能上传、触发文本解析、查看预览，且来自其他组织的用户无法读取。若 W05 的 `READY` 正文可能缺失，先修复再索引。

工作区干净且基线提交后：

```powershell
git switch -c feat/week06-rag-baseline
```

**环境提醒**：pgvector 扩展必须已安装在运行中的 PostgreSQL 服务端，仅执行 `CREATE EXTENSION vector` 无法安装缺失的扩展文件。若 W05 的 Compose 使用普通 PostgreSQL 镜像，可在备份开发数据、确认 PostgreSQL **主版本一致**后，改为含 pgvector 的兼容镜像。禁止未经检查就删除既有 Docker Volume 或执行 `docker compose down -v`。在测试数据库先验证扩展，失败时保留现有开发数据并记录诊断结果。

### 1.1 需要了解的技术知识

- **Chunk**：把长文档切为可检索的小段，既要保留语义，也要保证能回溯原文。
- **Embedding**：把文本映射到固定维度向量，主要用于衡量语义接近程度，不等于答案生成。
- **pgvector**：PostgreSQL 向量扩展，`<=>` 是余弦距离；本周按距离升序做精确近邻搜索。
- **RAG**：先检索证据，再把证据送给语言模型生成回答；引用必须指向系统真实检索到的材料。
- **评测**：Recall@K 衡量正确证据是否被检索到，MRR 衡量正确证据排序靠前程度。

---

## 2. 本周预计新增的目录和文件

以下为**增量结构**，不覆盖已有 `cmd/`、`internal/knowledge/`、`migrations/` 和 `web/`。

```text
nexusdesk/
├── AGENTS.md                              # 只追加 W06 规则
├── .env.example                           # EMBEDDING_PROVIDER、MODEL、DIM 等示例
├── cmd/api/main.go                         # 依赖注入，不重写入口
├── internal/
│   ├── knowledge/
│   │   ├── chunker.go                      # 手工实现字符切块
│   │   ├── chunker_test.go
│   │   ├── index_service.go                # 文档索引编排
│   │   └── index_repository.go             # 索引状态和 Chunk 写入
│   ├── rag/
│   │   ├── embedder.go                     # Embedding 接口
│   │   ├── retriever.go                    # 权限范围内召回
│   │   ├── generator.go                    # 文本生成接口
│   │   ├── service.go                      # 检索 → 提示词 → 回答
│   │   ├── handler.go                      # Search/Ask API
│   │   └── service_test.go
│   └── platform/
│       └── llm/
│           ├── embedding_client.go         # 真实模型 API 适配器
│           └── chat_client.go              # 真实文本生成 API 适配器
├── migrations/
│   ├── 00009_enable_pgvector_and_create_chunks.sql
│   └── 00010_create_knowledge_document_indexes.sql
├── tests/integration/
│   ├── knowledge_index_test.go
│   └── rag_search_test.go
├── web/src/
│   ├── api/rag.ts
│   ├── views/KnowledgeSearchView.vue
│   └── components/RAGSourceList.vue
├── docs/
│   ├── knowledge/
│   │   ├── chunking-policy.md
│   │   ├── embedding-contract.md
│   │   └── rag-design.md
│   ├── api/rag.md
│   ├── decisions/0006-pgvector-exact-search.md
│   ├── testing/rag-evaluation.md
│   └── weekly/week06.md
└── requests/
    ├── week06.http
    └── testdata/rag-corpus/
        ├── org-a-db-playbook.md
        ├── org-a-incident-process.md
        ├── org-b-private-guide.md
        └── rag-eval.jsonl
```

目录文件命名可根据你当前代码风格调整。已执行迁移不可被修改。本周 `00009/00010` 只是**建议的下一个迁移号**，实际必须以本地 migrations 列表为准。

### 2.1 Day 1 创建文档文件的命令

```powershell
$dirs = @('internal/rag','docs/knowledge','docs/api','docs/decisions',
          'docs/testing','docs/weekly','requests/testdata/rag-corpus')
foreach ($d in $dirs) { New-Item -ItemType Directory -Force $d | Out-Null }

$files = @(
  'docs/knowledge/chunking-policy.md',
  'docs/knowledge/embedding-contract.md',
  'docs/knowledge/rag-design.md',
  'docs/api/rag.md',
  'docs/decisions/0006-pgvector-exact-search.md',
  'docs/testing/rag-evaluation.md',
  'docs/weekly/week06.md',
  'requests/week06.http'
)
foreach ($f in $files) {
  if (-not (Test-Path $f)) { New-Item -ItemType File -Force $f | Out-Null }
}
```

把压缩包中的对应模板按需复制到文件中。**模板只是待审核的起点，不代表已完成设计。** 本周 `AGENTS.week06.append.md` 应追加到根目录已有 `AGENTS.md`，不要覆盖 W01～W05。

---

## 3. Day 1：RAG 需求、调用流程与数据契约（2～3 小时）

### 3.1 今天由你亲自完成

1. 阅读 W05 的 `knowledge_documents` / `knowledge_document_texts` 表和文本预览 API，确认组织主键的真实数据类型及授权流程。
2. 画出索引和问答的两条流程。索引流程要区分 **文档解析成功** 与 **向量索引成功**；问答流程要区分 **检索结果** 与 **生成答案**。
3. 在 `rag-design.md` 中定义 P0 约束：仅索引未删除、属于当前组织、文本状态 `READY` 的文档；一次索引只允许一个执行者；限制单文档 Chunk 数和生成请求超时。
4. 在 `embedding-contract.md` 中确定一个模型和固定维度。建议演示方案使用 `text-embedding-3-small` 的默认 **1536 维**；如果选其他提供商，先核实其真实向量维度，并据此调整迁移与配置。模型和维度不能静默混用。
5. 设计 `SearchResult` 和 `Citation` 数据结构，至少包含 `document_id`、`chunk_id`、`chunk_index`、`source_name`、`score`、`excerpt`。
6. 定义问答失败语义。无匹配证据时返回明确的“知识库未检索到足够依据”，不要把无来源的通用回答伪装成 RAG 成果。

### 3.2 推荐的后端接口契约

| 方法 | 路径 | 功能 | 说明 |
|---|---|---|---|
| POST | `/api/v1/organizations/:orgID/knowledge/documents/:documentID/index` | 手动同步建立向量索引 | 仅 READY 文档；成功返回索引版本和 Chunk 数 |
| GET | `/api/v1/organizations/:orgID/knowledge/documents/:documentID/index` | 查看索引状态 | 区分 ABSENT / INDEXING / READY / FAILED |
| POST | `/api/v1/organizations/:orgID/knowledge/search` | 组织级语义检索 | `query`、`top_k`，返回可验证来源 |
| POST | `/api/v1/organizations/:orgID/knowledge/ask` | 非流式 RAG 问答 | 返回 `answer`、`sources` 和 `request_id` |

错误语义建议：`400` 非法参数，`401` 未登录，`403` 无组织访问权限，`404` 文档不存在或不属于当前组织，`409` 文档未解析完成/索引进行中，`422` 内容不可索引，`502/503` 上游模型故障，`504` 超时。内部错误码比 HTTP 文案更稳定，例如 `EMBEDDING_DIMENSION_MISMATCH`。

### 3.3 今天怎样用 AI

ChatGPT 可以解释 RAG 的输入输出、Chunk 策略和向量距离。Codex 只能读取文档并指出 API 与 W05 的冲突；**禁止它自动生成整个 RAG 模块**。

可复制提示词：

```text
你是 NexusDesk 的 Go 后端架构审查员。请只读检查本周的
rag-design.md、embedding-contract.md、W05 数据表与组织鉴权实现。
请找出接口边界、权限、模型维度、文档状态和错误处理方面的冲突。
按“确定问题 / 需要验证的问题”分别列出，给出文件位置和原因。
不要修改文件，不要生成完整服务实现。
```

**当天交付**：3 份设计草稿、4 个接口契约、1 张简要流程图。你能口头说清楚“文件 READY 为什么还不能被搜索”。

---

## 4. Day 2：文本切块算法与单元测试（3～4 小时）

### 4.1 自己设计首版 Chunk 策略

建议先做字符窗口切块，之后再比较按段落边界切块。开发参数建议：**每块目标约 500 个 Unicode 字符，重叠约 80 字符，最多 200 个 Chunk/文档**。这是为个人项目设置的可调整初始值，不代表真实最优。还需要检查真实模型的输入 Token 上限，中文字符数不能简单等同于 Token 数。

确定如下规则：

- 按 Unicode rune 而不是 UTF-8 字节切分，避免把一个中文字符截成乱码。
- `chunk_index` 从 0 递增；`start_rune` 是闭区间起点，`end_rune` 是开区间终点。
- 同一输入、同一 `chunker_version` 必须得到完全相同的切块结果，便于重试、缓存和测试。
- 保留全文原始偏移，不能因为 `TrimSpace` 等清洗操作导致引用无法匹配原文。
- 处理空文档、短文档、连续换行、混合中英文、Emoji、无空格长文本和最后一块；重叠不能造成无限循环。
- 如果分块超出 `MAX_INDEX_CHUNKS`，明确报错并终止，不允许只索引前 200 块却仍标记为成功。

你可以自己决定在换行符附近调整切点，但必须保持可验证偏移，并在 `chunking-policy.md` 写下选择的理由。

### 4.2 自己先定义 Go 接口，再实现

```go
// 概念示意：请你自己在仓库中定义实际接口与边界。
type Chunk struct {
    Index     int
    StartRune int
    EndRune   int
    Content   string
}

type Chunker interface {
    Split(text string) ([]Chunk, error)
}
```

不需要为了四行结构体引入一整套抽象层。考虑用普通函数 `SplitText` 也可以，只要测试覆盖充分。

### 4.3 必测的 8 个切块场景

| 编号 | 场景 | 关键断言 |
|---|---|---|
| C01 | 空文本/全空白 | 返回明确不可索引错误 |
| C02 | 少于窗口大小 | 返回 1 块且内容可回溯 |
| C03 | 恰好一个窗口 | 无多余空块 |
| C04 | 超过一个窗口 | Chunk 有序且重叠不丢文字 |
| C05 | 中文与 Emoji 混排 | 字符偏移正确，UTF-8 合法 |
| C06 | 多段落与换行 | 不丢段落与真实原始偏移 |
| C07 | 长文档超限 | 返回明确超限错误，不截断伪装成功 |
| C08 | 重复输入 | 同参数结果完全一致 |

示例执行：

```powershell
go test ./internal/knowledge/... -run 'TestChunk' -v
```

Codex 可以审查边界用例，但实现算法时先由你自己写。**当天交付**：`chunker.go`、`chunker_test.go`、`chunking-policy.md`，记录测试输出。

---

## 5. Day 3：pgvector 环境、数据库表和精确检索 SQL（3～4 小时）

### 5.1 先在测试数据库检查扩展

```sql
SELECT current_setting('server_version');
SELECT name, default_version, installed_version
FROM pg_available_extensions
WHERE name = 'vector';

CREATE EXTENSION IF NOT EXISTS vector;
SELECT extversion FROM pg_extension WHERE extname = 'vector';
```

如果 `pg_available_extensions` 中没有 `vector`，需要安装含该扩展的服务端环境。不要把这一问题归因为 Go 依赖缺失。开发数据库执行迁移之前做备份；Goose 的版本号必须与实际仓库已有迁移连续规划，**不要修改 W05 已应用的 00007/00008**。

### 5.2 本周两张新表

**`knowledge_chunks`** 建议字段：`id`、`org_id`、`document_id`、`chunk_index`、`content_text`、`start_rune`、`end_rune`、`content_sha256`、`embedding vector(1536)`、`embedding_model`、`chunker_version`、`created_at`。`(document_id, chunk_index)` 需要唯一约束。`org_id` 和 `document_id` 的类型必须匹配 W05 既有表。

**`knowledge_document_indexes`** 建议字段：`document_id`（唯一/主键）、`org_id`、`status`（INDEXING、READY、FAILED）、`embedding_model`、`embedding_dimension`、`chunker_version`、`source_content_sha256`、`chunk_count`、`failure_code`、`started_at`、`indexed_at`、`updated_at`。没有记录可以视为 `ABSENT`，无需在表中强制存储该状态。

记住两个不同状态的含义：

| 数据对象 | READY 的意义 |
|---|---|
| `knowledge_documents.status` | 原文已经被提取并保存 |
| `knowledge_document_indexes.status` | 对应文本版本已经成功完成 Chunk 和向量索引 |

查询时需要确认 `knowledge_document_indexes.source_content_sha256` 与当前 `knowledge_document_texts.content_sha256` 相同。即使索引状态看起来是 READY，正文版本不一致也不应返回过期的结果。

### 5.3 精确余弦距离 SQL 草图

```sql
SELECT c.id AS chunk_id,
       c.document_id,
       c.chunk_index,
       c.content_text,
       c.embedding <=> $2::vector AS cosine_distance
FROM knowledge_chunks AS c
JOIN knowledge_documents AS d ON d.id = c.document_id
JOIN knowledge_document_texts AS t ON t.document_id = d.id
JOIN knowledge_document_indexes AS i ON i.document_id = d.id
WHERE c.org_id = $1
  AND d.org_id = $1
  AND i.org_id = $1
  AND d.deleted_at IS NULL
  AND d.status = 'READY'
  AND i.status = 'READY'
  AND i.source_content_sha256 = t.content_sha256
  AND c.embedding_model = $3
  AND i.embedding_model = $3
ORDER BY cosine_distance ASC, c.id ASC
LIMIT $4;
```

这是**查询思路示例**。实际代码中要使用参数绑定，不得把用户输入直接拼到 SQL。向量类型/驱动序列化按当前 `pgx` 和 pgvector Go 适配库验证。搜索时必须先做当前组织 active 成员验证；SQL 内的 `org_id` 是第二道隔离防线，不能两者只保留一个。

- `<=>` 返回余弦**距离**，距离越小越接近，不要误当成相似度降序。
- 若 API 要返回相似度，可以在应用层定义 `1 - distance` 作为余弦相似度展示，但应写清定义，不能把它当作概率或准确率。
- 由于这是本周的少量本地测试数据，先用精确扫描建立正确性基线；需要时用 `EXPLAIN (ANALYZE, BUFFERS)` 记录执行计划。HNSW 是后续优化实验，不是 P0。
- 新增两个测试组织，分别上传同样关键词但不同内容的文档，验证跨组织隔离。

### 5.4 一致性和失败处理

首次索引时通过条件写入建立 `INDEXING` 状态；生成所有 Embedding 成功后，再在**同一个数据库事务**中写入 Chunk、更新索引状态为 READY。外部 Embedding API 调用不应该放在长时间持有数据库写锁的事务里。失败则明确标记 FAILED，不得把半成品 Chunk 暴露给搜索。

本周同步索引在进程崩溃时可能遗留 INDEXING，必须记录为 W07 的待解决问题。对重试索引，优先通过条件状态更新防止重复执行，并设计版本匹配检查。不要声称同步接口已经具备完整的故障恢复能力。

**当天交付**：两条新的迁移、迁移 Up/Down 测试、组织范围内精确距离查询及查询计划截图或输出。

---

## 6. Day 4：Embedding 接口、向量生成和索引服务（3～4 小时）

### 6.1 先由你完成模型适配接口

```go
// 概念示意，具体方法由你自己定义。
type Embedder interface {
    Model() string
    Dimension() int
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}
```

Embedding 接口的主要价值是把业务服务与具体模型提供商隔离。你需要自己确定批量输入、超时、取消、错误类型、维度验证以及返回数量不匹配时的处理规则。

### 6.2 两种实现，职责不同

- **真实模型适配器**：使用明确配置的 API Endpoint、模型名和密钥。可选 OpenAI 等提供商；例如 `text-embedding-3-small` 默认 1536 维。必须按服务商公开文档验证接口、维度和调用限制，不把 API Key 写进仓库。
- **确定性 Mock Embedder**：仅用作单元测试、错误注入和完整调用流程测试。可以由固定哈希产生固定长度向量，但这种向量没有可靠语义意义，**不能用于宣称真实语义检索质量，也不能用它计算有意义的 RAG 召回率**。

本周不做多模型切换。索引和查询必须使用同一 Embedding 模型及兼容的维度。模型更换应触发重新索引，不允许静默混用旧向量。

### 6.3 关键边界

1. 一批 10 个 Chunk 必须收到 10 个合法向量；数量不匹配时整批失败。
2. 每个向量长度必须是配置维度，例如 1536；拒绝 NaN、Inf、零范数异常等非法结果。
3. 限制请求输入大小、单次 batch 数量和超时；`context.Context` 必须传递到真实 HTTP 请求。
4. 对 HTTP 429、超时和服务端错误返回可识别的错误码，暂不做无限重试。
5. 记录模型名、向量维度、调用耗时和可用的 Token 使用量，但日志不应记录 API Key 或完整私有文档。
6. 如果数据已索引且正文哈希、模型、Chunk 版本未变化，允许返回“已有有效索引”，避免重复扣费。
7. 索引失败不会改变 W05 文档文本 READY 状态，用户仍然可以预览和下载原文。

### 6.4 本周的索引编排步骤

```text
校验用户与组织权限
  ↓
校验文档 READY + 文本版本
  ↓
原子抢占 INDEXING 状态
  ↓
读取原文，执行确定性 Chunk
  ↓
调用 Embedder 获取向量
  ↓
检查向量数量/维度/合法性
  ↓
事务写入全部 Chunk，标记索引 READY
  ↓
返回 indexed_count、embedding_model、document_id
```

如果用户关闭页面，接口取消可能导致索引失败，前端必须展示状态而非一直显示“处理中”。W07 再改成持久化异步队列。

**当天交付**：`embedder.go`、真实与 Mock 适配器、索引服务、至少 6 项故障测试、手工成功索引 1 篇技术文档。

---

## 7. Day 5：组织级 Search 与基础 RAG 问答（3～4 小时）

### 7.1 先实现 Search，不要马上接 Chat Model

Search API 接收用户问题、`top_k`；检查字符串长度、top_k 范围（例如 1～10）和当前组织 active 成员权限，再将**问题文本**转换为同一模型的向量。对已经索引 READY 的当前组织文档执行 SQL 检索，返回排序后的 Chunk 和文档来源。

建议先在终端查看纯检索结果，确认问题确实召回对应文档，再接语言模型。如果检索质量差，先检查文本解析、切块、模型维度和 SQL 排序，不要马上通过修改 Prompt 来掩盖检索问题。

### 7.2 再实现非流式 RAG

```text
用户提问
  ↓
鉴权 + 组织成员校验
  ↓
Query Embedding
  ↓
Top-K Chunk 检索（限制组织与正文版本）
  ↓
整理证据上下文（每块含 chunk_id 和 source_name）
  ↓
构造明确的回答约束
  ↓
Chat Model 生成回答
  ↓
校验/整理来源集合
  ↓
返回 answer + sources + request_id
```

**引用设计非常重要**：接口 `sources` 由后端根据真实检索结果构造；模型可以在回答中引用指定的 `chunk_id`，但是服务端只能接受本次检索结果中出现的 ID，不允许引用没有检索过的文档。第一版也可以只返回系统检索来源列表，让前端显示“参考资料”，避免模型伪造脚注。

### 7.3 无答案、Prompt Injection 和权限

- 没有任何可用 Chunk 时，直接返回“未找到足够的知识库依据”，而不是无条件转向通用聊天。
- 如果返回 Chunk 但证据不够充分，生成器应允许表达“不确定”，不要强制输出确定结论。
- 从文档检索到的文本始终视为**不可信数据**，不能被解释为新的系统指令。文档中出现“忽略之前指令”“泄露密码”等内容时，不得执行。
- 本周 RAG 只有检索和问答，不授予工具调用、文件修改、SQL 执行、工单创建权限。
- 对传给第三方模型的数据范围做限制；不要在无授权情况下传输真实组织私有文档。
- 错误响应中不能暴露模型密钥、内部 Prompt、数据库 URL 或本地文件路径。

### 7.4 RAG 响应示例（示意）

```json
{
  "answer": "根据知识库，建议先检查数据库连接配置和网络连通性。",
  "sources": [
    {
      "document_id": 101,
      "chunk_id": 405,
      "source_name": "db-troubleshooting.md",
      "chunk_index": 2,
      "excerpt": "连接超时排查步骤……"
    }
  ],
  "request_id": "req_example_only"
}
```

实际 ID、响应字段和错误码由你结合已有代码统一。问答阶段可以使用模拟生成器测试输入到输出契约，但最终的真实问答演示必须明确记录使用的模型、环境和测试条件。

**当天交付**：Search 和 Ask 的 Handler/Service/Repository；至少 1 次真实模型问答及引用核对（如果有合法可用的模型额度）；无来源情况下的正确降级。

---

## 8. Day 6：构造评测集、跑测试和做 Vue 页面（3～4 小时）

### 8.1 先自己建立至少 20 条检索题

从 3～5 篇你自己编写或获授权的技术文档中，人工设计至少 20 道题。**标准答案证据必须由你亲自标注**，不能直接采用同一个模型生成的“正确答案”当作可信金标准。

建议分布：

| 类别 | 数量建议 | 验证目的 |
|---|---:|---|
| 直接事实定位 | 8 | 关键词与语义是否能定位正文 |
| 同义改写 | 5 | 是否具备基础语义泛化能力 |
| 跨段落相关问题 | 3 | Chunk 边界是否影响检索 |
| 无文档答案 | 2 | 无证据时是否能正确拒答 |
| 跨组织敏感诱导 | 2 | 组织隔离与检索过滤是否有效 |
| **合计** | **20** | 初始功能评测，不代表大规模科学评测 |

必须固定数据版本、模型、Chunk 参数和评测查询。不要因某一题错误就随意修改人工标签，让测试结果看起来更高。

### 8.2 至少记录以下指标

- `Recall@5`：对有标注相关证据的问题，前 5 个结果是否命中至少一个相关 Chunk。若一题对应多段证据，另可记录标准的多证据 Recall@5。
- `MRR`：首个相关 Chunk 排名倒数的平均值，数值越高表明正确证据越靠前。
- `P50/P95` 检索耗时：在固定语料、固定查询数、固定机器下统计；区分 Embedding API 耗时与数据库检索耗时。
- 来源正确性：`sources` 的 `chunk_id` 必须真实属于当前用户授权组织、未删除文档和当前索引版本。
- 无答案行为：无证据时是否没有捏造文档引用；对有误检索的情况另外记录。

**注意**：只用确定性哈希 Mock 向量跑出来的 Recall@5/MRR 只能验证评测程序是否工作，不能作为语义效果数据。语义效果报告必须使用真实语义 Embedding 模型。

### 8.3 前端最小实现

在 W05 知识库页面旁新增检索页，包含问题输入、搜索按钮、检索结果卡片、RAG 答案框和来源列表。显示索引状态、模型失败错误、无匹配结果，不把完整私有文档缓存到公共浏览器页面或 localStorage。

Codex **可在明确授权后**承担 Vue 布局、TypeScript 类型、API 客户端样板和测试辅助，但不得改变组织鉴权、Search SQL、索引事务或 RAG 引用规则。

**当天交付**：`rag-eval.jsonl`（至少 20 条人工补齐的数据）、评测脚本或测试函数、前端演示页、真实指标初稿。

---

## 9. Day 7：整体验收、Codex 审查与个人复盘（2～3 小时）

### 9.1 必须实际运行的命令

```powershell
go test ./...
go vet ./...
go build ./...
git diff --check
git status --short

# 有相关测试时再运行，结果需记录
# go test -race ./internal/knowledge/... ./internal/rag/...
```

在 W05 兼容的开发数据库运行迁移 Up 和 Status；在单独测试数据库验证 Down/Up，**不要在有真实开发数据的数据库上直接做破坏性回滚**。

### 9.2 手工验收清单（至少 24 项）

| 编号 | 验收场景 | 结果记录 |
|---|---|---|
| T01 | W05 READY 文档可触发索引 | 成功/失败 + 截图 |
| T02 | 未解析的 UPLOADED 文档不可索引 | 错误码 |
| T03 | FAILED 解析文档不可索引 | 错误码 |
| T04 | 已删除文档不可索引 | 404 |
| T05 | 组织 B 无法索引组织 A 文档 | 404/403 |
| T06 | 同一文档重复点击索引不并发执行 | 状态与响应 |
| T07 | 相同正文、模型与切块版本避免无意义重建 | 索引版本 |
| T08 | 中文和 Emoji 切块无乱码 | 测试断言 |
| T09 | Chunk 偏移可回溯到原文 | 片段一致 |
| T10 | 超过 Chunk 上限拒绝且不伪装成功 | 错误码 |
| T11 | Embedding 数量不一致被拒绝 | Mock 测试 |
| T12 | Embedding 维度错误被拒绝 | Mock 测试 |
| T13 | 非法浮点向量被拒绝 | Mock 测试 |
| T14 | Embedding API 超时能取消 | Context 测试 |
| T15 | 数据库写入失败后不暴露半成品 | 事务测试 |
| T16 | 同组织相关检索按距离升序 | SQL 断言 |
| T17 | 不同组织相同关键词无越权结果 | Alice/David |
| T18 | 旧文本哈希索引不可被检索 | 版本测试 |
| T19 | 已软删除文档不出现在结果中 | Search 测试 |
| T20 | 非法 top_k/空问题被拒绝 | 400 |
| T21 | 无匹配材料时不伪造引用 | Ask 测试 |
| T22 | 返回来源 ID 都属于真实召回结果 | 引用校验 |
| T23 | 恶意文档中的指令不改变系统权限 | 注入测试 |
| T24 | 模型不可用时业务功能保持正常 | 故障测试 |
| T25 | Vue 检索和问答可展示来源 | 浏览器截图 |
| T26 | 20 条真实语义评测有完整报告 | 结果与环境 |
| T27 | HTTP 密钥、连接串、原文不写入日志 | 日志检查 |
| T28 | `go test ./...` 与核心回归均通过 | 命令输出 |

如果本周没有真实模型额度，可以完成离线构件和 Mock 测试，但要如实标记 **真实 Embedding 检索和问答未验收**，不能宣称完整 RAG P0 已通过。

### 9.3 Codex 两轮只读审查

第一轮重点审查权限与 SQL：检查每个接口是否验证 active 成员、每个查询是否限制 `org_id`、是否过滤软删除、是否校验正文版本，以及索引状态与 Chunk 写入是否原子提交。

第二轮重点审查 RAG：检查 Prompt Injection、引用 ID 构造、无答案处理、模型超时、Token/成本上限和明文日志风险。Codex 应输出分级发现和复现路径，不直接把建议全部自动应用。

### 9.4 独立掌握能力检查

| 自测问题 | 本周应达到的能力 |
|---|---|
| 为什么要切块，为什么需要 overlap？ | 能画出原文偏移与 Chunk 边界 |
| `[]byte` 与 `[]rune` 切分有什么差异？ | 能自己写测试解释乱码问题 |
| 为什么 Embedding 模型和维度要固定？ | 能解释向量不可混用以及重新索引 |
| `<=>` 为什么升序，HNSW 为什么不一开始就用？ | 能解释精确基线与近似搜索取舍 |
| 为什么文档 READY 还不代表索引 READY？ | 能解释两套状态和正文版本校验 |
| 为什么不能只在 Prompt 里写“仅检索本组织”？ | 能指出后端权限与 SQL 必须强制执行 |
| Mock 测试与真实评测的差别是什么？ | 能明确不夸大模拟指标 |
| 如何保证来源是真实的？ | 能展示服务端只使用真实 Chunk ID 构造来源 |

至少完成一次“关闭 AI 对话，自己手写关键 SQL 和 Chunk 主流程”的自测，并把结果写进 `week06.md`。

---

## 10. 本周 Codex 使用规范和任务示例

### 10.1 你必须亲自完成的核心

- 拟定 Chunk 切分策略、字符偏移和异常情况，独立实现首版算法。
- 设计真实数据表、迁移、索引版本模型及数据库查询 SQL。
- 定义 Embedder 接口、设计索引状态更新与事务提交边界。
- 实现组织权限检查、Query Embedding、Top-K 检索和问答编排核心。
- 为 20 道评测题人工标注参考证据；判断技术指标是否真实可信。
- 定位并修复至少一个真实问题，说明日志、验证方法和根本原因。

### 10.2 Codex 可以协助完成的任务

- 只读检查 pgvector SQL 的约束、NULL、Join、排序和多租户安全。
- 只读审查 Chunk 边界与超时、并发风险，建议附加测试用例。
- 获明确授权后生成 Vue 界面、TS 类型、HTTP 示例和测试数据脚手架。
- 整理你自己记录的 API 调试与性能分析结果。
- 有明确范围后，帮助执行 `go test`、`go vet`、查询计划采集，但必须如实报告实际执行结果。

### 10.3 三种建议的 Codex 提示词

**只读设计审查**

```text
请作为 Go 后端架构审查员，检查 NexusDesk 本周的 RAG 设计文档。
只读，不修改文件。优先检查 W05 文档状态与 W06 索引状态是否混用、
Embedding 维度、org_id 过滤、索引版本和 API 错误语义。
给出文件位置、触发条件、风险、建议验证实验；不要生成整个模块。
```

**代码测试审查**

```text
仅阅读 internal/knowledge/chunker.go、index_service.go、
internal/rag/ 和相关测试。请列出尚未覆盖的边界情况。
尤其关注 Unicode 偏移、重叠无限循环、模型超时、部分失败、
重复索引和数据库事务。暂时不要修改源码。
```

**限定范围的前端任务**

```text
你只可以修改 web/src/api/rag.ts、KnowledgeSearchView.vue、
RAGSourceList.vue。先列出改动计划，获得我的确认后再编辑。
页面展示已有后端 Search/Ask 结果、来源和错误态。
不要修改后端权限、迁移、Embedding 模型和 AGENTS.md。
执行可用的前端构建/测试，并如实说明结果。
```

### 10.4 AI 产生的材料如何验收

1. 查看 `git diff` 和 `git diff --check`，确认 Codex 未修改无关文件。
2. 自己说明每一个关键业务判断的原因，不理解的代码不直接合并。
3. 使用固定的真实测试数据复现结果，保存命令、日志摘要和截图。
4. 将有价值的错误记录到 `docs/weekly/week06.md`，而不是只依赖聊天历史。
5. 真实线上 API Key 只来自环境变量或秘密管理，不允许传给模型上下文。

---

## 11. 常见故障与建议排查顺序

| 问题 | 优先自行检查 | 再让 AI 协助 |
|---|---|---|
| `type "vector" does not exist` | `pg_available_extensions`、当前数据库、服务端镜像 | 检查镜像/扩展安装方案 |
| 插入时报向量维度不匹配 | 模型实际返回长度、配置、表维度 | 审查 Embedder 验证 |
| 余弦检索排序反常 | 距离算子、升序、Embedding 模型是否统一 | 审查 SQL 与单元测试 |
| 中文 Chunk 断裂 | rune/byte 偏移、TrimSpace、滑动窗口步长 | 找 Unicode 边界用例 |
| 搜索结果跨组织 | 授权中间件、org_id 参数、JOIN 条件 | 只读安全审查 |
| 同一文档索引多份 Chunk | 并发抢占、唯一约束、清理+写入事务 | 审查重复请求场景 |
| 明明解析成功却搜不到 | W05 READY、W06 索引状态、正文哈希 | 画状态转换诊断图 |
| RAG 引用不存在 | 后端来源构造、模型引用白名单 | 校验来源映射 |
| 模型 API 报 429/超时 | 请求频次、上下文超时、输入上限 | 制定有上限的重试方案 |
| 接口生成幻觉 | 先看召回证据，再看 Prompt 和模型 | 分离检索错误/生成错误 |

每次排障都遵循：**记录实际现象 → 提出假设 → 用日志或测试验证 → 修改最小范围 → 回归测试 → 记录根因**。不通过盲目更换框架来解决普通实现问题。

---

## 12. 一次提交、一天复盘与第七周交接

### 12.1 推荐 Git 提交粒度

```text
feat(knowledge): add deterministic text chunker
feat(storage): add pgvector document schema
feat(knowledge): implement synchronous document indexing
feat(rag): add organization-scoped semantic search
feat(rag): add grounded non-streaming answers
 test(rag): add evaluation fixtures and security cases
 docs: record week06 decisions and verification
```

提交前使用 `git status` 与暂存区检查，确认没有 `.env`、真实文档、API Key、临时数据库备份。保持历史迁移不可变。

### 12.2 每日复盘四项

1. 今天我亲手写了哪一段核心代码？有没有自己做技术决策？
2. 失败过什么，怎样发现原因？有没有亲自复现？
3. AI 提出的建议哪部分采纳了，哪部分拒绝了，依据是什么？
4. 现在脱离 AI，我能解释并修改哪段代码？

### 12.3 本周通过标准

- W01～W05 回归正常，组织鉴权与文件管理无退化。
- 至少 1 篇 W05 已解析文档建立真实语义索引；每个 Chunk 有可验证来源。
- 同组织 Search 返回真实相似结果，跨组织 Search 不会泄露数据。
- 非流式 Ask 可以回答有证据的问题，并返回后端核对过的来源；无证据能说明不足。
- 至少 20 个评测查询有人工标注的标准证据，真实模型测试结果被记录而非想象。
- 关键错误、事务失败、重复索引、维度和权限情形有测试证明。
- 有一次完整 Git 提交、Codex Review 和个人技术复盘。

**进入 W07 时需要保留的真实问题**：同步索引耗时长、HTTP 请求中断会影响处理、服务崩溃可能留下 `INDEXING`、重试和成本控制不完善。这些将成为使用 Redis + Asynq + Worker 的合理动机，而不是为了堆技术栈而引入消息队列。

---

## 附录 A：本周学习资源（按实际使用查看）

1. pgvector 官方 README，支持的向量类型、余弦距离操作符和索引方案：<https://github.com/pgvector/pgvector>
2. OpenAI Embeddings 官方说明，模型参数、维度和调用限制：<https://developers.openai.com/api/docs/guides/embeddings>
3. OpenAI Go Embeddings API 接口说明：<https://developers.openai.com/api/reference/go/resources/embeddings/methods/create>
4. OWASP RAG Security Cheat Sheet，检索文本为不可信数据及上下文边界：<https://cheatsheetseries.owasp.org/cheatsheets/RAG_Security_Cheat_Sheet.html>
5. PostgreSQL 官方文档，查询执行计划和事务：<https://www.postgresql.org/docs/current/using-explain.html>

**版本说明**：文档中的模型名、模型维度和 SQL 算子在编写时核对了官方资料。实际开发仍应以你选择的模型提供商、Go 依赖版本和本地数据库版本为准。本文的 SQL 与接口片段是学习和设计示例，必须结合现有项目的主键类型、数据表和 API 契约调整。

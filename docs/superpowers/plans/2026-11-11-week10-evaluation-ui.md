# Week 10 Evaluation and UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 完成第十周可复现 AI 评测和全部现有页面的企业工作台改造。
**Architecture:** 保留模块化单体、精确向量默认链路，增加有界词项融合检索及实际供应商用量。用隔离实验连接产品真实 API；前端重用现有状态与权限边界。
**Tech Stack:** 当前锁定 Go/pgx/pgvector、Vue/TypeScript/Pinia、Python/Playwright。
**Spec:** `docs/superpowers/specs/2026-11-11-week10-evaluation-ui.md`

## Global Constraints

- 所有工作在 `/media/yjz/pzp/Go/projects/NexusDesk` 主目录；不创建 worktree，不自动 commit/push/merge，不覆盖已有 W08/W09 未提交代码。
- Go/Vue/依赖版本不升级；使用现有 Node 24.12.0；JWT/数据库/DeepSeek Key 不进入日志、浏览器或提交文件。
- 集成验收只操作自有随机 PG/Redis、合成账号和语料；不读用户上传文件/备份，不回滚或删除日常数据，不触碰 Redis 6379。
- 保留后端当前成员检查、org_id 范围、索引 hash/version/软删、超时和并发上限；保留前端内存身份、旧异步响应隔离和人工确认写操作。
- Mock 只证明接口契约；检索效果来自真实 BGE，云模型仅接收本次合成数据；如实保留失败和 unknown，不把预标注称为人工评测。

## Review Focus

- 中文/标识符混合及 `%_` 输入只能匹配普通数据；Task 1 测试词项和参数化检索。
- 混合候选中的组织外/软删/旧索引必须被过滤；Task 1 真实 DB 测试。
- 缺失/非法 token 计数不得变成准确费用；Task 1 适配器测试，Task 3 unknown 成本测试。
- 组织切换与慢响应不得显示旧资料；Task 2 保留并扩充竞态测试。
- 小屏、长标题、空组织/空记录仍可操作；Task 2/3 浏览器验收。

### Task 1: Hybrid retrieval and measured generation

**Files:** `internal/rag/{service,handler,retriever,index_repository,models}.go`、新 `hybrid.go`/测试；`internal/platform/llm/{chat_client,deepseek}.go`/用量测试；`tests/integration/week10_rag_test.go`；`docs/api/rag.md`。
**Interfaces:** QueryInput 增 `RetrievalMode string json:retrieval_mode`；SearchResponse/AskResponse 增同名字段。AskResponse 增 `usage` 与 `timings`，usage 格式由实现者落定后写报告供 UI/评测消费；包含 known/model_called/prompt_tokens/completion_tokens/total_tokens/cache_hit_tokens/cache_miss_tokens，未知计数使用 null。timings 使用四个 *_ms int64。

- [x] 先写模式校验、中文/英文词项、RRF确定性、缺失/畸形供应商usage、混合跨组织和旧来源测试，记录失败。
- [x] 实现模式缺省vector、只允许vector/hybrid（显式空字符串非法）；candidate limit20、RRF60、32去重词项、ASCII标识符及汉字bigram；两路最低余弦分沿用s.limits.MinimumScore，SQL复用currentSourceJoins/Where，3秒查询。来源Score保持余弦，不能冒充融合分；融合可加fusion_score可选字段。
- [x] 问答同时暴露用量和阶段耗时；DeepSeek usage与Ollama prompt_eval_count/eval_count只接可信非负有限整数和一致总数，未知不阻断合法答案、也不能标known。兼容现有mock零值；无召回明确零调用。模型提示补最小充分证据和不同事实必要时引用多个来源，保留三字段严格输出契约和source检查。
- [x] 测试Go受影响包，增加真实库门禁 `NEXUSDESK_TEST_W10_RAG=1`（沿用隔离前缀校验），更新API契约，报告准确JSON示例。不改前端/脚本/语料/迁移。

### Task 2: Enterprise workspace UI

**Files:** `web/src/App.vue`、`assets/main.css`、所有9个views、components、必要lib/types/client/composables/knowledge及测试，`web/index.html`。
**Interfaces:** 使用Task1的retrieval_mode、usage、timings，默认vector；不变更原有URL/API身份边界。

- [x] 读现有状态、单测；先给策略选择/取消旧响应/usage unknown 的新行为写失败测试。
- [x] 重构应用壳为固定深色侧栏、顶部面包屑和账户区、浅灰工作区；组织选择紧凑放侧栏；当前页高亮、移动导航按钮、skip link、键盘焦点。登录注册做有品牌但克制的双栏视觉；各业务页标题/操作栏、紧凑表格、状态标签、空态、表单、AI消息与提议卡统一。用户文案不塞技术实现说明；无假数据/假链接/无功能按钮。
- [x] 检索页加明确策略选择（默认精确向量），切换取消清旧答案；展示耗时、可用token用量，未知用量明确；来源卡编号、原文/文件跳转/字符范围、相似度精确标名；现有AI人工确认/拒绝/流式停止完整保留。
- [x] 运行Node24下所有前端单测、build/typecheck、lint、format；保持权限竞态、登录注册、工单、上传任务和AI原行为断言，不为UI改造删失败断言。控制器随后负责真实浏览器截图验证。

### Task 3: Evaluation, browser acceptance and handoff (controller)

**Files:** 新 `requests/testdata/rag-week10/` 60题、`scripts/week10-{evaluate,smoke}.py`、`scripts/week10-browser.mjs`及辅助模块、`docs/testing/evidence/week10/`；W10手册/周报/问答/测试/演示、README/AGENTS、`docs/rag-evaluation.md`。

- [x] 在模型运行前固定语料与证据，保留hash；脚本自动校验60题、区间、分类，错误分类和Recall@1/3/5、MRR@5/引用指标的分母明确。独立测试指标与成本unknown逻辑。
- [x] 隔离PG/Redis应用启动，真实BGE两策略各60题，固定正负子集DeepSeek真实生成，记录每题rank、耗时、usage、模型、异常及脱敏结果；官方价格快照按时段/缓存估算并区分账单，无法确定时给区间或null。
- [x] 全Go race门禁包含W10且无skip；前端验收全套；浏览器桌面/390px覆盖登录注册、组织、工单创建/详情、知识库上传/详情、检索模式与引用、AI流式及人工提议确认，截图并实际查看。
- [x] 写第十周7日手册、测试证据、问题回答、评测报告与局限、运行维护说明、阶段介绍；链接更新。重启仅自己持有的主目录API/worker/web，验证健康。完成全变更审查，修复发现，不自动提交。

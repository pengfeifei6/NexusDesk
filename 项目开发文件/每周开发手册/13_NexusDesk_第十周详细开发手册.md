# NexusDesk 第十周详细开发手册

本周主题：AI 工程增强、可复现评测与企业工作台界面。

依据《02_NexusDesk_12周开发周期与求职计划》第十周，承接第九周缓存、限流和 SQL 优化；用户追加要求统一美化现有前端。本文由 AI 根据实际仓库补充并执行，完成证据以 `docs/weekly/week10.md` 和 `docs/testing/evidence/week10/` 为准，个人学习和人工标签复核不由 AI 代称完成。

## 本周目标与边界

1. 将原 20 题扩充为固定 60 题，明确语料、来源区间、模型、参数、指标分母。
2. 比较精确向量与词项＋向量混合检索，记录 Recall@1/3/5、MRR@5、耗时、实际 Token 和费用估算。
3. 检验无答案拒答、多组织隔离、引用有效性及引用相关性，保留模型失败案例。
4. 全面统一现有 9 个页面的导航、版式、色彩、表格、表单、状态反馈与移动端体验。
5. 形成可以自己复跑、讲解和维护的材料。

继续使用原主目录 `/media/yjz/pzp/Go/projects/NexusDesk`。不新建平行项目或 Git worktree，不升级依赖，不清理日常库，不自动提交/推送。保持第九周 39 个业务 API、14 个迁移；第十周主要扩展已有检索/问答契约。MCP/Eino 是选学内容，本周不为了框架名称引入新工具权限。

## Day 1：冻结评测材料

阅读 `docs/rag-design.md`、`docs/api/rag.md`、W06 与 W09 周报，确认当前默认向量检索、组织过滤、索引版本与模型预算。

交付：`requests/testdata/rag-week10/manifest.json`、8 个原创合成文档、`rag-eval.jsonl` 60 题。类别覆盖事实、改写、多证据、精确标识符、无答案、跨组织。每条正例记录文件、Unicode 字符区间和原文；负例无相关证据。语料/标签在模型运行前固定 SHA-256。标签来源标为 AI 预标注，人工复核待办。

验收：`python3 -m unittest discover -s scripts -p test_week10_metrics.py` 检查文件 hash、区间、题量、指标计算和未知成本。不能为了提高分数在看到结果后修改原标签。

## Day 2：增加可对比检索策略

已有 `/knowledge/search` 和 `/knowledge/ask` 接受 `retrieval_mode`，省略为 `vector`；显式选择 `hybrid` 使用词项和向量排名融合。

两路均保留 org_id、成员授权、软删、正文 hash、索引版本、模型和切块版本过滤。每路最多 20 候选，RRF 常数 60，词项最多 32 个；当前最低余弦分仍为 0.40。中文双字片段与英文标识符用于参数化 substring 匹配，此方案不是 BM25，也没有 ANN 索引。

验收：模式严格校验、稳定同分排序、标点与 `%_` 不成为 SQL 通配符、跨组织/旧索引/软删过滤、模型前后来源复查。不要为了召回率绕过权限或降低来源检查。

## Day 3：观测用量并改善引用

问答响应展示检索模式、阶段耗时和模型实际 Token 计数。区别“无召回所以未调用模型”“供应商没返回用量”“已返回可信用量”。不能按字符数伪装精确 Token，不能把 unknown 费用算为零。

生成提示要求围绕问题使用最小充分证据，跨文档事实保留必要来源；适配器只能引用实际发送的片段，服务端在生成前后验证整个召回集。前端来源卡能查看文件、原文和字符范围，便于人工核对。

验收：两种供应商适配器的用量字段、缺失/错误计数兼容行为、无来源拒答、非法引用与来源变化拒绝。有效引用仅证明来源合法，不自动证明回答正确。

## Day 4：企业工作台界面

统一深色侧栏、蓝色主操作、浅灰工作区、白色内容卡。桌面与移动导航有当前页反馈；登录/注册、组织入口、工单列表及详情、知识库列表及详情、检索、AI 会话全部纳入。

保留真实已有功能：注册登录、组织创建/选择、工单创建/领取/状态/评论、上传/任务/预览/下载、检索问答、AI 会话/停止/提议确认与拒绝。不要增加假统计、装饰性无效按钮或把尚未实现的管理功能画成可用功能。

验收：键盘焦点、标签、空态、错误和忙碌反馈、长文件名/文本、390px 小屏；组织/登录变化取消旧响应，Token 仍只在内存。

## Day 5：运行真实实验

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/week10-evaluate.py --live
```

该命令显式允许付费云调用：从现有 `.env` 取 DeepSeek Key，仅把本次合成问题/文档送给 DeepSeek；Embedding 使用本地 `bge-m3:567m`。脚本创建并最终清理自己拥有的随机 PG/Redis，日常库不参与；不要把环境改指日常库。

各策略运行 60 次检索，按固定清单运行 16 题问答（8 正例、8 负例），不自动重试失败。输出 `docs/testing/evidence/week10/evaluation.json`。真实模型不可用时保留错误，不以 Mock 替代效果数字。

分析：Recall 是标注证据覆盖比例，Hit 是是否至少命中一条，MRR 看首个正确排名；这些不能混用。正例与负例单列，错误请求保留在分母。费用用公开价格日期快照和实际用量给区间，不冒充扣款账单。

## Day 6：回归与视觉验证

```bash
python3 scripts/week10-smoke.py
python3 -m unittest discover -s scripts -p 'test_*.py'
export PATH="$PWD/.local/week04-tools/node_modules/node/bin:$PATH"
cd web
npm run test:unit -- --run
npm run build
npm run lint
npm run format:check
```

烟测包含真实 PG/Redis、全部 Go race 门禁和浏览器；浏览器合成模型只验证产品流程，不用于语义指标。支持 `--backend-only` / `--web-only` 分范围排障。所有截图只能使用合成账号和资料，不能暴露密钥、Token 或真实私有附件。

## Day 7：交付与复习

- 阅读 `docs/rag-evaluation.md` 的结果、失败分类与默认策略结论。
- 对照 `docs/weekly/week10.md` 看实际完成清单，`docs/weekly/week10-answers.md` 复习常见问题。
- 按 `docs/demo/week10-demo.md` 启动并走一遍工作台。
- 简历只写真实可复现的规模和结果，注明是小型合成评测；不要把单机实验写成生产吞吐或人工语义准确率。
- 下一周继续观测、压测、故障演练与工程整理；不要提前声称第十一周已经完成。

建议自行讲清：一次检索经过哪些权限/版本过滤；hybrid 为什么可能降低排序；Recall 与 MRR 的差别；引用合法与回答正确的区别；Token unknown 的处理；原生工具调用、Agent 编排、MCP 协议分别解决什么问题。

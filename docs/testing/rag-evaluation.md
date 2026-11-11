# W06 RAG 评测与安全验收

使用真实本地 `bge-m3:567m` 1024 维和 `qwen3.5:35b`，PostgreSQL17.11/pgvector0.8.7。语料为5篇原创虚构文档，4篇组织A、1篇组织B；窗口500/重叠80，top_k=5，阈值0.40。所有数据在专用临时数据库运行，未读取或导入用户私有文件。

[固定语料和hash](../../requests/testdata/rag-corpus/manifest.json) · [20题证据偏移](../../requests/testdata/rag-corpus/rag-eval.jsonl) · [评测脚本](../../scripts/week06-evaluate.py) · [完整实测JSON](../weekly/evidence/week06-rag-evaluation.json)。标签先于首次模型调用固定，8事实/5同义/3多证据/2无答案/2跨组织诱导。标签由AI按原文预标注，**待开发者人工核验，不是已完成人工标注的金标准**。

| 指标 | 实测值 | 分母与解释 |
| --- | --- | --- |
| 手册 Recall@5，即 Hit@5 | 100% | 16/16有答案问题至少命中一段完整参考证据 |
| 全部有答案题平均证据覆盖率 | 96.875% | 16题逐题覆盖率的均值 |
| 多证据题 Recall@5 | 83.333% | M01/M02/M03，6段证据命中5段；单独聚合由保存结果计算 |
| MRR | 0.864583 | 16题首个相关片段排名倒数均值 |
| 无答案题Search返回空集 | 25% | 4题中仅1题无匹配；其余有误检索，不能掩盖 |
| 无答案/跨组织诱导Ask拒答 | 100% | 4/4固定拒答且空引用 |
| 跨组织来源泄漏 | 0 | 所有返回文档ID属于授权组织，全文偏移与固定正文一致 |
| Embedding P50 / P95 | 197 / 213 ms | 20次查询；不含索引和模型预热 |
| 数据库检索 P50 / P95 | 1 / 2 ms | Service所测Search SQL调用，毫秒整数精度 |

M02只召回两段参考证据中的一段，是本版真实漏召回，不修改标签掩盖。相似度阈值也不能完全过滤无答案问题中的主题相近文本；生成阶段的依据判断仍必要。本轮两个正常问题分别回答DB_HOST/DB_PORT、P1的10分钟，B组织带注入样例的文档回答7分钟，引用来自真实行且未产生工单副作用。来源校验不等于所有自然语言结论已被自动证明，答案蕴含关系仍需人工复核。

固定机器：Linux，本机2×RTX3090（各24GiB），Ollama0.17.5。一次顺序20题的小样本；P95按最近秩法，不是生产负载/SLA。Qwen冷启动实测118.407秒，其中加载118.168秒，证据见[冷启动记录](../weekly/evidence/week06-model-cold-start.json)。首轮评测如实因35秒请求预算失败；增加独立180秒预热后重新执行固定全套，保留[首次失败](../weekly/evidence/week06-evaluation-run.log)与[预热后成功](../weekly/evidence/week06-evaluation-warm-run.log)。没有按答案好坏单题重试、改标签或调阈值。预热使用公开短句，不计入检索延迟。

真实SQL顺序、原子写入、撤权与故障用确定性模型夹具测试；这些夹具结果不计语义效果。[集成日志](../weekly/evidence/week06-integration-green.log)中的 EXPLAIN ANALYZE/BUFFERS 仅证明本查询执行和小样本计划；没有ANN索引，也不外推大规模性能。

## 手册 T01～T28 对照

下表的自动测试替代了重复手工操作，不把自动执行写成开发者亲自验收。真实浏览器截图位于[W06证据目录](../weekly/evidence/week06-browser/results.json)。

| 编号 | 实际证据与结果 |
| --- | --- |
| T01 | 真实浏览器 index-ui：READY正文→ABSENT→真实BGE索引READY；截图01 |
| T02 | 真实数据库/HTTP：UPLOADED索引409 INDEX_STATE_CONFLICT，无模型调用 |
| T03 | 真实数据库/HTTP：提取FAILED索引409，不创建索引行 |
| T04 | 真实数据库已删除索引404，生成过程中删除拒绝发布 |
| T05 | 浏览器四路由隔离：组织B对A文档POST/GETindex404 |
| T06 | 实际并发领取屏障：一方INDEXING，重复409；模型调用期间未持父行锁 |
| T07 | 相同hash/model/chunker重复索引无新Embedding，version/timestamp/count不变 |
| T08 | chunker C01～C08：中文/补充平面Emoji保持有效UTF-8 |
| T09 | rune偏移复原、真实数据库片段与固定语料结果逐条相等 |
| T10 | chunker上限200整体拒绝；服务422、FAILED零块且不调用模型 |
| T11 | 模型数量不符502；真实数据库无片段，FAILED失败码正确 |
| T12 | 模型维度错误502；数据库vector(1024)约束另有SQLSTATE断言 |
| T13 | 零/NaN/Inf拒绝，极端有限向量归一化；不把非法向量传给SQL |
| T14 | 适配器Context取消/超时、索引独立失败回写；真实冷启动504记录保留 |
| T15 | 第2块插入触发器故障：旧块替换整体回滚，READY不半发布 |
| T16 | 真实余弦0/1/其他距离排序、并列ID、top_k、阈值及EXPLAIN |
| T17 | 同词不同组织、disabled/无身份及Embedding途中撤权；无越权结果 |
| T18 | 旧正文hash、块hash、index version、非READY状态均不可检索；异模型被CHECK拒绝 |
| T19 | 软删保留chunks但Search为空，详情/索引404 |
| T20 | HTTP拒绝空问题、非法top_k、重复/未知字段、Unicode错误与尾随JSON |
| T21 | 无召回跳过Generator；真实4题无答案/跨组织诱导均拒答、无引用 |
| T22 | 未知/缺失引用拒绝、重复去重；仅SQL真实行构造sources，生成后核验未引用片段 |
| T23 | B组织恶意片段不能调用工具/跨组织读取/创建工单；真实注入样例7分钟回答且工单不变 |
| T24 | 独立API模型端口不可用与RAG_DISABLED两种运行：启动、me/tickets/knowledge200，Search503 |
| T25 | 真实浏览器Search/Ask/source跳转/切组织/空库/删除闭环8项，截图01～04 |
| T26 | 20题真实语义结果已存；人工审核标签仍待开发者完成 |
| T27 | HTTP/适配器安全错误测试；故障进程日志无查询哨兵/密钥/Token/连接密码；原文不记录 |
| T28 | 真实PostgreSQL全量race、W01～W05回归、普通Go/vet/build、Vue测试与质量检查，见本周记录 |

复现：`python3 scripts/week06-smoke.py`。后端快速验证用 `--backend-only`；只重跑语义评测用 `--evaluation-only`；浏览器迭代用 `python3 scripts/week02-day2-smoke.py --rag --web-only`。全部创建随机专用数据库和存储，仅清理本次拥有的资源。普通 `go test ./...` 会跳过数据库入口，不能据此声称SQL已运行。

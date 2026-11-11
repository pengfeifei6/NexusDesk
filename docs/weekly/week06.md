# Week 06：组织内语义检索与带来源问答

日期：2026-11-11，Asia/Shanghai。按开发者“参照前面标准，整周全部完成”的明确委托，由AI完成实现、测试与整理；手册中的教学提示词不是额外授权限制。

工程交付和本机真实验收已完成：确定性切块、真实Embedding、pgvector索引、组织内Search、非流式Ask、匹配Vue页面及回归。手册要求的**开发者亲自审核20题标签、脱离AI手写核心代码的自测、亲自复盘和Git提交尚未代替完成**；参考答案与预标注材料已经备好。当前28业务API、10表/迁移、8主页面，没有自动commit/push/部署。

快速入口：[怎么运行](../demo/g2-rag-demo.md) · [阶段介绍](../project-stage-overview-week06.md) · [20道参考回答](week06-answers.md) · [API](../api/rag.md) · [设计](../rag-design.md) · [28项验收](../testing/rag-evaluation.md)。

## 七天交付

| 天 | 已实现的结果 | 阅读位置 |
| --- | --- | --- |
| Day1 | rune500/80、最多200块、精确偏移和C01～C08 | [策略](../chunking-policy.md)、[实现](../../internal/knowledge/chunker.go) |
| Day2 | 固定BGE-M3/1024与Qwen3.5:35b，真实本地模型适配、限制与安全错误 | [契约](../embedding-contract.md)、[llm](../../internal/platform/llm/client.go) |
| Day3 | pgvector0.8.7、迁移9/10、索引状态/version/hash与原子发布 | [数据库](../database.md)、[索引服务](../../internal/rag/index_service.go) |
| Day4 | Query Embedding、组织内精确Top-K、旧hash/version/软删过滤 | [检索](../../internal/rag/retriever.go)、[SQL](../../internal/rag/index_repository.go) |
| Day5 | 无证据拒答、真实引用映射、生成前后复查、非流式Ask | [Service](../../internal/rag/service.go)、[ADR](../decisions/0006-local-rag-baseline.md) |
| Day6 | 20题固定语义评测、匹配索引/检索/问答Vue页面 | [评测](../testing/rag-evaluation.md)、[原始结果](evidence/week06-rag-evaluation.json) |
| Day7 | 全量回归、两类独立审查与修复、日常库增量升级、运行/答疑/交接文档 | 本文与[审查](../reviews/week06-rag-tests-review.md) |

## 实际执行结果

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 真实PostgreSQL全量race | 153顶层+713子测试=866通过节点；0失败/0Skip；W06有5组54子项 | [最终集成日志](evidence/week06-integration-green.log) |
| 普通 `go test -count=1 -v ./...` | 137顶层通过，16数据库入口明确Skip；不是SQL验收替代 | [普通Go日志](evidence/week06-go-test.log) |
| `go vet ./...` / `go build ./...` / `go mod verify` | 退出0、模块校验通过 | [vet](evidence/week06-vet.log)、[build](evidence/week06-build.log)、[mod](evidence/week06-mod-verify.log) |
| Vue单元测试 | 83通过 | [最终单测](evidence/week06-web-review-green.log) |
| Vue build/lint/format | 全部退出0 | [build](evidence/week06-web-build.log)、[lint](evidence/week06-web-lint.log)、[format](evidence/week06-web-format.log) |
| 真实Chromium143浏览器 | W04十一项、W05十项、W06八项，共29项；无接口Mock | [运行日志](evidence/week06-browser-run.log)、[W04结果](evidence/week06-week04-browser/results.json)、[W05结果](evidence/week06-week05-browser/results.json)、[W06结果](evidence/week06-browser/results.json) |
| W01历史烟测 | migration1、健康/关停、故障启动等通过；不混作最新schema | [W01回归](evidence/week06-week01-regression.log) |
| W02～W05历史数据库/TCP | 与W06同一隔离race runner全部通过，旧哨兵保留 | [集成报告](../reviews/week06-integration-report.md) |
| 真实语义模型 | 5篇文档索引与复用、20查询、7个Ask样例通过 | [评测日志](evidence/week06-evaluation-warm-run.log)、[指标](evidence/week06-rag-evaluation.json) |
| 模型不可用/禁用 | 独立API正常启动，me/tickets/knowledge200、Search503；安全日志 | [故障日志](evidence/week06-model-outage.log) |
| 迁移回归 | 专用库8→10、重复Up、Down到8、再Up10；仅清理本次容器/卷 | [集成日志](evidence/week06-integration-green.log) |
| 日常库 | 私有不透明0600备份后，仅Up8→10、vector0.8.7；未Down/重建/删卷 | [日常升级](evidence/week06-development-migration.log) |
| 交付核对 | 旧1～8迁移/依赖/历史证据保持，链接/格式/差异检查 | [基线比较](evidence/week06-baseline-comparison.json)、[最终检查](evidence/week06-delivery-check.json) |

本机已有依赖和Chromium缓存被复用，没有声称全新机器安装或远程CI通过。新进程使用真实模型；契约/事务故障测试中使用的确定性假模型不参与语义指标。所有私有数据测试用独立虚构夹具，日常账号没有自动导入评测语料。

## 真实效果与局限

16个有参考证据问题Hit@5=100%、MRR=0.864583；全部有答案题平均证据覆盖率96.875%，单独3个多证据问题覆盖5/6段=83.333%。M02漏掉一段证据，保留原标签。4个无答案/跨组织诱导问题中，Search仅1题为空，Ask4题均拒答。全部返回来源无跨组织泄漏，片段与原文偏移一致。

20次查询Embedding P50/P95=197/213ms，数据库Search=1/2ms；单机顺序小样本、毫秒精度、预热后测量，不能外推生产SLA。固定语料/标签hash和各题完整来源均在JSON中。标签是AI预标注，开发者人工核验待完成；答案引用真实也不自动证明每句话正确。

## 遇到的问题与修复

1. **守护进程下载EOF**：Docker/Ollama直接拉取失败。环境准备改为宿主机下载官方公开制品，核对layer/GGUF哈希后导入，没有改全局代理/守护进程配置或已有模型。证据与精确制品ID见[环境报告](../reviews/week06-environment.md)。
2. **大模型冷启动504**：第一轮真实Ask在35秒结束；独立探针证实总118.407秒、模型加载118.168秒。新增[预热脚本](../../scripts/week06-model-warmup.py)，每模型最多180秒，只发公开短句，独立于API预算；完整固定评测重新通过，失败日志保留。长时间闲置后仍可能需再次预热。
3. **索引数据库等待未限时**：只读审查发现状态查询与Rollback可能无界，Claim/Finish预算过长。先用真实表锁/行锁复现5秒/8秒等待，再改为读3秒、事务5秒、独立Rollback3秒；局部超时映射INDEX_TIMEOUT，不把已成功提交误写成失败。RED和GREEN均保留。
4. **权限失效后保留预览**：模型POST失败后的状态刷新若404曾被忽略。新增测试复现两个失败，修复为原POST403/404立即清理、刷新403/404也清理，旧组织迟到响应不影响新页面。最终83项通过。[前端审查](../reviews/week06-frontend-review.md)。
5. **输入预算与引用边界**：设计审查要求完整消息12000字节预算、仅实际发送来源允许引用、成功答案必须非空且至少1引用、生成后检查全部召回（含未引用）。均落实并有模型/Service/真实DB测试。[设计审查](../reviews/week06-design-review.md)、[模型审查](../reviews/week06-model-review.md)、[后端审查](../reviews/week06-backend-review.md)。

## 当前机器与维护

当前原日常容器为PostgreSQL17.11，已复制完全同版官方vector扩展文件并Up到10，未替换容器/卷。**以后重建数据库容器必须带 `deploy/compose.pgvector.yaml`**。私有数据库备份在ignored `.local/week06-backups/`；未读取备份内容，也不纳入证据/Git。原文件存储仍在原私有目录；数据库备份不能代替原文件备份。

重启已有Go进程，再按[运行说明](../demo/g2-rag-demo.md)预热模型、启动前端。W06新增四个API全部有UI；前几周成员管理、工单PATCH/指派/删除的完整UI仍未补齐，不能把本周前后端匹配写成整个产品每个API都有按钮。

仍无Worker/租约恢复/SSE/工具调用。进程崩溃可能遗留INDEXING；请求取消或60秒预算可使索引失败。W07应解决可靠异步执行与有界重试，不直接抢占仍运行任务。本周没有公网部署验证、生产负载基准或完整提示注入免疫保证。

开发者后续只需先按演示操作，再人工核验20题证据，并完成一次独立SQL/切块自测。个人复盘应按真实情况填写：亲手写了什么、是否亲自复现、脱离AI能解释哪些判断。代码参考回答已备好，AI未代填个人掌握结论。

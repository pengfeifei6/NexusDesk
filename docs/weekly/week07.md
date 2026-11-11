# Week 07：可靠异步文档任务

日期：2026-11-11，Asia/Shanghai。开发者委托结合第六周和第八周补齐缺失的第七周手册，并完成整周工程。手册由AI设计补齐；原第八周文档保持不变。个人学习、标签人工复核和Git提交没有代为宣称完成。

工程实现、真实PG/Redis、Worker故障演练与真实模型浏览器验收已完成；日常库已在私有备份后只Up到11，专用Redis6380已启动。整体审查已通过，工程交付完成。

入口：[第七周手册](../../项目开发文件/每周开发手册/10_NexusDesk_第七周详细开发手册.md) · [怎么运行](../demo/g3-jobs-demo.md) · [阶段介绍](../project-stage-overview-week07.md) · [20道参考回答](week07-answers.md) · [任务API](../api/knowledge-jobs.md) · [36项矩阵](../testing/week07-jobs.md)。

## 七天交付

| 天 | 交付 | 主要位置 |
| --- | --- | --- |
| Day1 | 状态、权限、幂等与恢复边界 | jobs-design.md、ADR0007、API契约 |
| Day2 | migration11、任务表、五个接口、严格输入/去重 | internal/jobs、migrations/00011 |
| Day3 | 独立Redis/Asynq与Worker、最小消息、独立配置 | cmd/worker、platform/taskqueue、compose.redis.yaml |
| Day4 | 提取→索引流水线、任务阶段、来源/权限/所有权校验 | jobs/service、taskguard、knowledge/rag |
| Day5 | 三次退避、人工重试、取消、租约、崩溃恢复 | jobs/repository/service、真实PG/Redis测试 |
| Day6 | 上传并异步处理、任务面板、轮询生命周期、真实浏览器 | AsyncJobPanel、knowledge composable、week07-browser |
| Day7 | 故障演练、独立审查、回归、运行/维护/答疑 | 本文及testing/demo/reviews |

当前33业务API、1健康检查、11业务表、11迁移、8主页面。新增任务相关功能前后端已接通；旧成员管理和工单PATCH编辑/指派/删除的完整UI仍不在本周范围。SSE、对话、工具和写操作确认交给W08。

## 实际验证结果

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 真实PG11/Redis全量Go race | 182顶层+779子测试=961通过节点，0失败/0Skip；W07为12组55节点 | [Go日志](evidence/week07-go-race-green.log)、[原始attempt3](evidence/week07-integration-attempt3.log) |
| 旧schema8/10真实回归 | 均通过；新任务门禁未开时明确Skip，不混作最新schema | [W05](evidence/week07-week05-schema-regression.log)、[W06](evidence/week07-week06-schema-regression.log) |
| W01启动/迁移/故障/关停 | 通过 | [W01](evidence/week07-week01-regression.log) |
| Go vet/build/mod verify | 均退出0，所有模块校验通过 | [vet](evidence/week07-vet.log)、[build](evidence/week07-build.log)、[mod](evidence/week07-mod-verify.log) |
| Vue单测 | 101/101通过，含8个审查新增时序回归 | [RED](evidence/week07-ui-fix-red.log)、[GREEN](evidence/week07-ui-fix-green.log) |
| Vue build/lint/format | 均通过 | [build](evidence/week07-ui-fix-build.log)、[lint](evidence/week07-ui-fix-lint.log)、[format](evidence/week07-ui-fix-format.log) |
| 真实Worker进程 | 7项通过；实际SIGKILL/SIGTERM、Redis中断恢复、第三次已发布结果恢复 | [进程JSON](evidence/week07-process.json)、[运行日志](evidence/week07-browser-attempt1.log) |
| 真实Chromium143浏览器 | W04=11、W05=10、W06=8、W07=7，共36项，无接口Mock | [W04](evidence/week07-week04-browser/results.json)、[W05](evidence/week07-week05-browser/results.json)、[W06](evidence/week07-week06-browser/results.json)、[W07](evidence/week07-week07-browser/results.json) |
| 真实模型闭环 | 同一份异步处理文档的BGE检索与Qwen带来源回答通过；旧RAG流程通过 | [W07结果](evidence/week07-week07-browser/results.json)、[预热](evidence/week07-model-warmup.log) |
| 模型故障/禁用 | 新进程启动、身份/工单/文档可用，Search503；日志无正文/凭据 | [运行日志](evidence/week07-browser-attempt1.log) |
| 迁移回归 | 专用库10→11、重复Up、Down10、Up11；旧组织哨兵保留 | [原始日志](evidence/week07-integration-attempt3.log) |
| 日常数据库Up11 / Redis6380 | 不透明0600备份后仅Up10→11；独立Redis healthy/PONG、6380回环绑定 | [迁移](evidence/week07-development-migration.log)、[Redis](evidence/week07-development-redis.log) |
| 基线保护 | 522个基线文件无丢失，旧迁移与历史证据未变；原依赖无升级 | [比较](evidence/week07-baseline-comparison.json)、[交付核对](evidence/week07-delivery-check.json) |

实际命令分阶段执行：`python3 scripts/week07-smoke.py --backend-only` 的第三轮中Go子进程全部通过，后续Python模拟服务启动失败；修复仅测试脚本后，用 `python3 scripts/week07-smoke.py --skip-go` 完成全部进程和浏览器，整轮退出0。未把失败的整轮改写为成功。完整新机器仍可用默认W07命令一次运行；本机复用了既有依赖、模型和浏览器缓存，没有声称远程CI通过。

真实退避两次模型调用间隔6.028/31.322秒，满足5/30秒下限（测试容差0.2秒）；SIGTERM受控阻塞场景退出0.015秒，随后重启恢复总耗时37.678秒，可能等待旧租约/退避。第三次已发布索引后强杀，恢复保持attempts=3、模型调用1次、Chunk1份。此场景使用已声明的attempt/lease时间和确认延迟触发器注入，不是无干预随机故障实验。

桌面和390px页面已查看，任务/正文显示正常，无横向溢出。[完成截图](evidence/week07-week07-browser/02-real-worker-complete.png) · [手机截图](evidence/week07-week07-browser/04-mobile-job-detail.png)。

## 已发现的问题与处理

1. 配置默认值测试继承了隔离runner随机REDIS_ADDR，导致整体首次检查失败；默认值测试现在明确清理可选环境，并保留原环境恢复。新SQL/Redis测试本身在首次运行均通过。
2. 模型适配器把400/401/403/422和部分500合并为MODEL_UPSTREAM_ERROR；原策略会误重试确定性错误。现在保守按永久失败，已知429/503/超时仍退避，增加真实HTTP适配器到任务分类的回归。
3. 进程脚本使用str.removeprefix与本机Python3.8不兼容；改为正则验证后的切片。还修复了http客户端变量遮蔽http模块的启动错误。进程退避现在实际计时，清理逐项执行，失败报告只记录实际发生的信号/注入。前两次失败保留在attempt2/3日志和[失败JSON](evidence/week07-process-attempt3.json)，最终真实演练已通过。
4. 前端初次观察到SUCCEEDED可能漏刷新旧正文/索引；403/404直接清ref不能阻止先前text响应回来。已将完成去重和权限失效的请求取消放到知识库owner，并通过8个延迟响应/隐藏页/首个成功响应回归。

审查记录：[任务/领域](../reviews/week07-backend-review.md) · [运行时/前端](../reviews/week07-runtime-ui-review.md) · [测试与故障演练](../reviews/week07-integration-review.md) · [整体审查](../reviews/week07-final-review.md)。

## 运行和维护边界

当前原日常PostgreSQL17.11/pgvector0.8.7已追加迁移11；备份位于ignored `.local/week07-backups/`，0600，未读取内容。没有Down、删卷或重建容器。专用Redis项目nexusdesk-queue已启动，持久卷nexusdesk-queue_queue-data。现有API/前端进程未擅自重启，需要你从已加载配置的终端启动本周新版，并另开Worker。

API不依赖Redis启动，Worker有独立入口与配置，不需要JWT密钥。专用Redis默认6380/DB6，本机已有6379服务保持不动。Worker与API必须共享数据库和私有目录，并使用新二进制。日常.env、密码与上传原件没有读取/改写，测试使用隔离虚构数据。

租约30秒、心跳5秒、每轮最多三次/5秒30秒退避；原文件hash和提取正文hash分别校验。已发布结果在第三次崩溃后可以核验为成功，不执行第四次。旧W05/W06无token中间态不自动抢占；取消不回滚已提交阶段。

故障模型和SQL时间注入只证明可靠性契约，真实语义效果使用本地BGE/Qwen浏览器流程。未对每一个可能的PostgreSQL提交响应丢失点注入网络故障；已包含领取锁超时、实际发布后确认前强杀和重放核验。没有公网部署、生产负载基准或远程CI执行声明。

W08从migration00012开始，复用Search/Ask和任务服务；写操作确认需要独立授权/事务设计，不能把队列幂等当作用户批准。先按演示亲手操作，再用参考回答检查理解，最后由本人决定何时提交Git。

## 本次工程选择与代价

- 在原工作区切换到feat/week07-async-ingestion并保存522文件安全快照，以保留W04～W06未提交代码；仅按HEAD新建工作树会漏掉已有开发。代价是后续提交需要一起审查未提交历史。
- 任务表承担持久投递意图，Redis只投递，未引入通用事件框架；预留15秒后才具备补发资格，实际还受扫描周期和积压影响，恢复仍依赖PostgreSQL和Worker扫描。
- 测试实施者同时负责新的进程脚本，避免领域夹具与恢复演练由不同人各自假设；代价是需要额外独立审查脚本，已执行。
- 原6379已有服务，所以专用Redis采用6380；代价是运行配置需保持API/Worker/Compose一致，示例和默认值已同步。
- MODEL_UPSTREAM_ERROR按永久错误处理，防止确定性4xx浪费三次执行；代价是模糊500若后来恢复，也需人工重试。
- 未搭建逐条PostgreSQL提交丢响应的网络故障代理，已覆盖领取锁超时和实际发布后确认前强杀；代价是不能把当前证据外推为所有数据库不确定提交窗口已演练。
- 保留当前分支和未提交工作，不自动合并/推送；审查结论另存docs/reviews，工作记录保留在ignored .superpowers目录供恢复。

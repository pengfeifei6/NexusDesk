# 第十一周开发记录：观测、性能与故障恢复

根据12周总计划的第11周，补写[七天详细手册](../../项目开发文件/每周开发手册/14_NexusDesk_第十一周详细开发手册.md)，并在原NexusDesk主目录实施。AI负责本次代码、实验与文档，开发者后续自行复习、审阅和提交；不将生成文件等同于个人独立掌握。

## 七天交付

| 日程 | 实际交付 |
|---|---|
| Day1 | 保存已有工作区基线；独立随机PG/Redis夹具、合成数据与资源归属保护 |
| Day2 | 服务端请求ID、JSON日志、HTTP/SQL/缓存/连接池/Go运行时/Worker指标；可选本机pprof与trace |
| Day3 | 两组织各1万工单和1万知识库元数据；四场景、三个并发、两轮的实际API压测 |
| Day4 | CPU与分配profile定位缓存解码热点，最小优化、坏缓存边界测试、前后测量与冻结源码复跑 |
| Day5 | Redis与PG实际停止/启动；Worker优雅停止和运行中SIGKILL，等待真实租约恢复 |
| Day6 | 真实PG/Redis全门禁Go race、前端/Python/构建检查、六组浏览器流程与13张截图 |
| Day7 | 性能/故障报告、观测说明、维护指南、常见问题、阶段介绍和审查记录 |

沿用39个业务API、14迁移、9页面；无新增依赖或数据库迁移。显式尾斜杠重定向保持301/307，并纳入日志/指标，未增加业务操作。前端保留W10工作台，本周重点在后端的可诊断性和恢复验证。

## 如何使用新增能力

`python3 scripts/dev.py api --diagnostics-address 127.0.0.1:6060` 和 `worker --diagnostics-address 127.0.0.1:6061` 可分别启用本机诊断。`/metrics`提供Prometheus兼容文本；CPU/heap/allocs/goroutine/runtime trace采用明确白名单，业务8080端口不暴露pprof，默认不开启诊断。

一次失败可用响应头/错误JSON中的同一request_id定位JSON完成日志。只记录受控路由模板、状态和耗时，不打印请求体、Token、SQL或模型内容。Worker独立生成执行ID和安全job关联字段；没有接入跨HTTP与队列的分布式追踪。Worker函数nil返回计数不代表业务任务成功，任务终态仍需查询。

最终性能实验48组、277484请求、0失败；解码微基准每次分配字节约降低42.5%、耗时约降低11.4%，业务列表平均吞吐变化约+0.95%～+5.63%，不作统计显著性或生产容量承诺。原先未经重复字段修正的更高增益保留为历史记录，不能代表最终代码。完整数据见[性能报告](../performance-report.md)。所有测量固定数据量、认证准备、预热、并发与时长，读写场景分开；创建场景之间恢复合成数据及事件数量。普通API与LLM耗时分开，本周没有真实DeepSeek调用。旧版源码/构建manifest保留，可通过Go overlay重建基线而不修改当前源码。

四类故障均执行实际停机或进程信号，见[故障报告](../failure-drills.md)。Worker崩溃后没有修改数据库租约或状态，最终总尝试次数1→2、任务SUCCEEDED、当前索引只有一个唯一块。测试只操作自己的随机容器/卷/进程，日常服务不参与停机演练。

## 验收与修正记录

最终缓存修正后的全量真实PG/Redis Go race：**1181个PASS节点、286个顶层测试、0Skip**。前端149项测试/7文件、Python24项、Go vet/build、类型构建、lint和format均通过。最终数值与状态见[汇总](../testing/evidence/week11/summary.json)及各独立证据；修正后再次执行Python、Go vet/build，见[受影响检查](../testing/evidence/week11/final-affected-checks.json)。浏览器和四类停机演练在重复字段修正前已通过，UI/正常缓存/任务恢复路径未变；该边界由新增回归及最终真实缓存门禁覆盖。

浏览器六组实际流程通过：注册登录/创建组织/工单领取流转评论，上传/异步索引/预览下载，混合问答/引用/用量，SSE提议/确认前不写入/人工确认，390px导航及组织切换，退出清理/无持久Token。页面错误0；桌面1440×1000、手机390×844，共13张截图。[桌面问答](../testing/evidence/week11/08-rag.png)、[手机工单](../testing/evidence/week11/12-mobile-tickets.png)已人工查看。模型为确定性本地HTTP夹具，不能代表真实语义效果。

发现并处理的问题保留记录：

- Gin默认自动尾斜杠重定向绕过中间件，改为显式重定向别名，补301/307、参数/查询、已有成对路径与SSE兼容检查。
- 两个旧数据库测试用完整响应字节相等验证不可枚举性，新随机request_id导致失败。现先校验ID格式、头/体一致与请求间唯一，再严格比较其余完整响应；保留状态、消息、权限顺序及无写入断言。[初次失败](../testing/evidence/week11/backend-initial-failure.json)与[日志](../testing/evidence/week11/integration-initial-failure.txt)未删除。
- 独立性能审查发现重复JSON data数组会复用前一数组元素。该边界须保持坏缓存回源，修正过程及最终测量在性能报告单列，早期结果保留而不冒充最终版本。

整体验收独立审查已通过，包含主执行者编写的故障/测试脚本；无未解决的阻塞项，见[审查记录](../testing/evidence/week11/final-review.md)。主目录API/Worker已更新并启用6060/6061诊断；5173前端身份保持不变。已核对新进程身份、旧API/Worker进程组退出、健康检查、401响应ID关联和诊断白名单，见[运行证据](../testing/evidence/week11/runtime.json)。

实际命令和输出分别见 [backend](../testing/evidence/week11/backend.json)、[checks](../testing/evidence/week11/checks.json)、[web](../testing/evidence/week11/web.json)、[faults](../testing/evidence/week11/faults.json)。普通 `go test ./...` 默认跳过真实数据库门禁，不能代替本周smoke。

## 阅读、维护与边界

[运行指南](../demo/week11-demo.md) · [观测说明](../observability.md) · [问题回答](week11-answers.md) · [测试矩阵](../testing/week11-test-plan.md) · [阶段介绍](../项目阶段介绍_第十一周.md)。先看报告结论，再用profile与日志跟进代码，修改后按指南执行相关检查。

已有W01～W10证据、迁移和依赖锁保留；本周未创建worktree、未自动commit/push/merge，未修改真实.env或读取私有上传/备份。保留性与源码记录见 [preservation](../testing/evidence/week11/preservation.json) 和 [source-manifest](../testing/evidence/week11/source-manifest.json)。

本次没有部署指标持久化、Grafana、告警、分布式追踪或跨机高可用；短时同机压测不代表生产容量。现有PDF提取、部分管理UI、AI历史分页归档及公网部署限制继续保留。第十二周按总计划进入项目演示、简历与面试材料整理，不凭本次代码代写个人能力结论。


## 本次实施选择与影响

- 按用户授权直接在原main工作区完成，保留之前未提交内容，不创建worktree或提交。代价是审阅时不能只用HEAD差异归因本周；因此另外保存了W11开始时的文件哈希与分任务差异。
- 使用标准库诊断、Prometheus文本和进程内runtime trace。它满足本周定位需求；跨机追踪、长期指标存储和告警需后续另行接入，当前不能承诺这些能力。
- HTTP尾斜杠采用显式重定向别名，使301/307也有请求ID与指标，并保持原Gin writer。代价是路由注册数量增加；业务API仍按规范路径计数，新增路由时需继续覆盖别名冲突测试。
- 故障脚本由主执行者实现，交付前交给独立整体审查验收；测试资源采用随机归属和真实租约，等待时间较长，但避免改状态制造恢复结论。
- 未提交情况下保留本次审查工作区记录，用户提交前仍可追溯决定、失败与修正；这些忽略文件不会代替正式docs中的结果证据。

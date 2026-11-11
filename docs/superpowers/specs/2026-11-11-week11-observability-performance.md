# 第十一周设计：可观测性、压测与故障恢复

依据12周总计划第11周及用户整周实施授权。以下实现细节由AI结合现有代码选定，不代称用户已掌握。工作仅在原NexusDesk/main；不提交、不新建worktree、不触碰日常数据或私有上传/备份。

## 目标与选择

交付可复跑的业务API压测、至少一项有真实profile依据的性能优化与前后对比、Worker/Redis/PostgreSQL短时不可用演练，以及请求关联日志、指标、pprof和Go runtime trace。继续保留W10九页企业UI和全部业务语义。

考虑过完整OpenTelemetry/Prometheus/Grafana部署、仅写压测文档、以及当前选择的标准库诊断加Prometheus文本指标。当前是本机模块化单体，选择第三种：不新增依赖或业务表，先让定位和恢复流程可用。没有部署监控平台或跨进程分布式追踪的完成声明；runtime trace和请求ID是本周实际追踪能力。

## 可观测性契约

- 新internal/platform/observe包：独立Registry、并发安全计数/耗时直方图、HTTP在途量、runtime内存/GC/goroutines、pgx池统计、SQL查询耗时/错误、cache统计回调和worker执行/dispatch返回状态。指标用nexusdesk_前缀，秒/字节单位，HTTP标签仅受控method、Gin路由模板、status；SQL仅固定操作分类，无SQL或参数；无用户/组织/工单/request ID标签，未知路由归unmatched，注册模板最多128个、HTTP组合最多512个（含溢出汇总）。
- API/Worker使用JSON slog。服务端生成32位hex request ID，不信任客户端传入，写X-Request-ID与context。七领域HTTP错误JSON的request_id取同一context，直接无中间件单测仍允许空。RAG Ask已有request_id与HTTP保持一致，独立Service调用保持生成能力。
- HTTP完成/安全panic日志只含受控方法、路由模板、状态、耗时、request_id；不记原URL、query、header、body、Token、模型提示词或错误原文。替换会打印请求头的默认Recovery；已开始SSE不能再追加JSON错误。中间件不包裹ResponseWriter，保持Flush/ResponseController/取消语义。Gin原有301/307尾斜杠跳转使用显式别名进入同一观测链，指标归规范路由，不增加业务功能。
- Go runtime trace在HTTP与DB操作标记task/region，只有固定名称与路由模板；worker执行有独立关联ID和job_id/run_version日志（不宣称跨队列继承HTTP ID）。worker Execute返回nil不等于业务SUCCEEDED，指标按delivery_return/dispatch_return标注。
- API可选OBSERVABILITY_ADDR，Worker可选WORKER_OBSERVABILITY_ADDR：未设置/空时不监听；设置后只接受literal loopback IP及0..65535端口。独立http.Server和专用ServeMux，随主进程退出释放，绑定失败明确失败。业务8080不挂metrics/pprof。
- 诊断仅GET /metrics与明确白名单pprof profile/heap/allocs/goroutine/trace；不暴露cmdline/symbol/default mux。profile/trace限制1..30秒（缺省5），拒未知参数/非法duration，单个采集槽避免重叠。基本超时/并发scrape保护，诊断不执行DB查询。
- dev.py增加参数--diagnostics-address（仅api/worker），明确传到对应子环境，web不得收到。原.env不需修改；.env.example给禁用默认与本机端口示例。

## 性能实验

在脚本自有随机Compose PG/Redis、合成用户/两组织/工单/知识库元数据下，用真实API认证后压测工单列表、知识库列表、工单详情和受控创建。登录仅准备阶段，避免Argon2和模型混入普通API吞吐量。

用标准库HTTP keepalive客户端、固定闭环并发1/8/16、每场景至少3秒、2轮，固定总数据量（初始建议每表20000条、两组织）、预热和页大小。记录机器CPU/内存/Go/PG/Redis、时间、数据库量、请求数、非2xx、p50/p95/p99、req/s、生成器模式/限制；不把结果写成生产容量或硬CI性能门槛。无付费云调用；W10真实模型延迟仅链接独立实验，不混合统计。

优化必须先采集业务CPU/heap或SQL计划证明热点，再针对一个实际热点做最小修改，保留同场景前后原始数据/profile和正确性测试；无显著改善则如实报告，不能挑选最好一轮。优先评估缓存JSON envelope重复编码/解码分配，测量不支持时改查已证实热点。不可移除实时授权、缓存org/version校验或阈值以换性能。

## 故障演练与隔离

脚本不接受任意目标环境，复用W09-owned资源保护并更名week11，临时文件只在主目录.local/week11-runs。只有匹配随机项目label的容器和脚本自己启动的进程可停止/恢复，finally先恢复再清理自有资源。不得对6379/6380日常服务或日常PG发送停止、FLUSHDB、迁移或seed。

必须实测：
1. Redis停机时已有工单缓存回源DB成功；AI发送503 RATE_LIMIT_UNAVAILABLE且不创建新消息/提议；Redis恢复后缓存/限流恢复。
2. PG停机时healthz仍200而受保护业务失败、返回关联ID、不得回旧缓存越过授权；恢复后原账号可继续读取，没有数据丢失。
3. Worker优雅SIGTERM与重新启动；另在真实RUNNING/INDEXING且本地模型故意阻塞时杀死脚本自有Worker，等待真实30秒租约过期，经Dispatch恢复，重启后任务最终SUCCEEDED且只有当前版本一套chunks，没有双重业务结果。故障模型只证明机制不证明语义。
4. 每项记录注入/检测/恢复时间、HTTP/DB断言、相关安全日志和恢复限制。长等待分段，控制器保持沟通。

## 验收与交付

新增第十一周手册（14_NexusDesk_第十一周详细开发手册.md），Day1基线/指标、Day2日志诊断、Day3业务压测、Day4profile优化、Day5故障恢复、Day6全量测试、Day7报告维护。最终有docs/performance-report.md、docs/failure-drills.md、docs/observability.md、weekly/week11.md/answers、demo/week11-demo.md、testing/week11-test-plan.md和原始证据。

全旧门禁真实PG/Redis go test -race -count=1 -v ./...必须0Skip；前端既有149测试及build/lint/format、Python脚本测试、Go vet/build；现有W10浏览器工作流复用但输出W11，不能覆盖旧证据。可观测性新增有TDD、profile可由go tool pprof解析，trace可由go tool trace解析，配置/隐私/未知路由/并发/关闭行为有测试。保留所有旧迁移/依赖锁/W10证据与预先未提交工作。

参考：Go官方[诊断](https://go.dev/doc/diagnostics)、[HTTP pprof](https://pkg.go.dev/net/http/pprof)、Prometheus[文本协议](https://prometheus.io/docs/instrumenting/exposition_formats/)与[指标命名](https://prometheus.io/docs/practices/naming/)。使用本地Go1.26.1现有API，不升级到文档站点的最新版本。

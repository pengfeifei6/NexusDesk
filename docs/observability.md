# NexusDesk 本机可观测性

W11增加请求关联日志、Prometheus文本指标和Go诊断能力。它们服务于当前模块化单体，独立于业务权限和数据库事务；没有引入监控平台或跨进程分布式追踪。

## 请求与日志

`internal/platform/httpserver/observe.go`在请求入口生成32位hex关联ID，写入context与X-Request-ID。领域错误响应通过同一context填写request_id，RAG Ask沿用该ID。客户端传入的ID不作为可信关联来源。

API与Worker入口启用JSON slog。HTTP结束日志记录method、route、status、duration_seconds和request_id。route使用Gin模板，例如 `/api/v1/organizations/:orgID/tickets`；未知路径统一unmatched，未知方法归other。尾斜杠重定向通过显式别名保留原301/307行为，并归入对应规范路由的指标，避免Gin默认重定向绕过中间件。默认panic恢复被安全恢复替代，不输出请求头、正文、panic原文或用户路径。已开始的SSE不再追加普通JSON错误。

Worker每次投递处理生成自己的关联ID，记录job_id、run_version、耗时和返回分类。HTTP关联ID没有持久化跨队列传播，因此不能把它称为完整分布式链路。队列重投递与业务终态仍以PG任务记录为准。

## 指标入口和含义

通过 `dev.py api --diagnostics-address 127.0.0.1:6060` 和Worker对应6061启动；也可分别设置OBSERVABILITY_ADDR、WORKER_OBSERVABILITY_ADDR。默认空值关闭监听。只允许字面回环IP，使用独立监听，不挂到业务8080。

| 指标 | 含义 |
|---|---|
| nexusdesk_http_requests_total | HTTP结束数，method/route/status固定维度 |
| nexusdesk_http_request_duration_seconds | HTTP耗时直方图，包括SSE整轮时长 |
| nexusdesk_http_in_flight | 当前正在处理的HTTP请求 |
| nexusdesk_sql_queries_total | SQL调用返回数，operation与result=nil/error |
| nexusdesk_sql_query_duration_seconds | SQL操作耗时，包含读取/解码周期，具体由pgx tracer回调定义 |
| nexusdesk_pg_pool_* | pgx池连接数、获取次数/耗时、等待和取消 |
| nexusdesk_cache_{hits,misses,failures,corrupt,stores}_total | 本进程列表缓存结果；故障次数可以一次请求多次增加 |
| nexusdesk_worker_returns_total | delivery_return/dispatch_return的返回分类，不等于业务SUCCEEDED |
| nexusdesk_worker_return_duration_seconds | 上述Worker函数执行时长 |
| nexusdesk_runtime_goroutines | 当前goroutine数量 |
| nexusdesk_runtime_heap_alloc_bytes / heap_inuse_bytes / sys_bytes | Go运行时内存快照 |
| nexusdesk_runtime_alloc_bytes_total | 累计分配字节 |
| nexusdesk_runtime_gc_cycles_total / gc_pause_seconds_total | 累计GC轮数与暂停时间 |

计数随进程重启归零；API和Worker是两个独立实例，不能把各自的绝对值当成全系统累计。首次出现的路由/SQL类别才产生对应序列。耗时桶单位秒，边界为0.005、0.01、0.025、0.05、0.1、0.25、0.5、1、2.5、5、10、30和+Inf。

指标采集只读进程内存与连接池快照，不查询PG、Redis，也不读取文档。没有user/org/ticket/request ID标签、SQL文本或参数；注册路由最多128个，HTTP指标组合最多512个（含溢出汇总），未知输入统一聚合。`result=nil`只说明函数没有返回error。

## pprof与runtime trace

诊断仅接受GET，白名单是 `/metrics` 和 `/debug/pprof/profile`、`heap`、`allocs`、`goroutine`、`trace`。没有cmdline、symbol或默认全局mux入口。CPU/trace采集的seconds只允许1..30整数，缺省5秒；其他查询参数拒绝。采集有并发上限，避免叠加多个CPU/trace采样；普通指标请求另有并发限制。

```bash
curl --fail 'http://127.0.0.1:6060/debug/pprof/profile?seconds=5' -o /tmp/nexusdesk.cpu.pprof
go tool pprof -top /tmp/nexusdesk.cpu.pprof
```

本周交付的profile来自隔离合成负载，见[性能报告](performance-report.md)。采样期间要有目标业务负载，空闲进程profile不能证明业务热点。heap的inuse与alloc_space指标、CPU累计占比和端到端p95分别有不同含义。

HTTP通过runtime trace task标记请求，SQL通过固定名称region标记操作；Worker标记投递处理。trace用于查看调度、阻塞与调用区域，不包含SQL或用户正文，也没有跨进程collector。

## 排查路径

1. 从失败响应提取X-Request-ID，在API日志中定位同次请求。
2. 看route/status、耗时与相关SQL/连接池/Redis缓存指标；区分请求错误和依赖错误。
3. 请求变慢但依赖正常时，在可控负载下采CPU/分配profile，提出可验证假设。
4. Worker异常先查PG任务状态与租约，配合worker日志和返回指标；不要直接把nil指标解释为处理成功。
5. 复跑隔离故障脚本确认恢复路径；日常环境不执行测试停机命令。

HTTP指标反映最终HTTP状态，已开始SSE后的应用错误需要结合流内error事件和持久化消息状态，不能仅凭HTTP200判断生成成功。本周没有业务内容日志、完整审计日志系统、持久化监控存储或告警通知。

实现参考Go官方[诊断文档](https://go.dev/doc/diagnostics)、[net/http/pprof](https://pkg.go.dev/net/http/pprof)和Prometheus[文本协议](https://prometheus.io/docs/instrumenting/exposition_formats/)、[标签命名建议](https://prometheus.io/docs/practices/naming/)。

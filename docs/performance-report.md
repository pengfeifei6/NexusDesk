# W11 普通业务 API 性能实验（I1 修复后的最终结果）

2026-11-11 完成审查修复后的 reviewed 实验：优化前后各 24 场、每场 3 秒、两轮，共 277,484 次请求，HTTP/传输/大小限制错误均为 0。最终优化移除缓存 RawMessage 拷贝；解码基准中位数分配字节下降 42.5%、耗时下降 11.4%。列表两轮平均吞吐变化 +0.95%～+5.63%，幅度较小，不能据两轮短测建立显著性或生产容量结论。

最终证据：[reviewed baseline](testing/evidence/week11/performance-reviewed-baseline.json)、[reviewed after](testing/evidence/week11/performance-reviewed-after.json)、[最终汇总及公平性断言](testing/evidence/week11/performance-reviewed-summary.json)、[最终 5+5 基准样本](testing/evidence/week11/performance-reviewed-benchmark.txt)。每场保留全部请求延迟、nearest-rank p50/p95/p99、状态、错误、连接数、指标快照及行数，未丢弃轮次。

## I1 正确性修复和证据版本

初次 typed `[]T` 解码遇到重复 `data` 字段时复用已有 slice 元素，末尾 `[{}]`、`[null]` 或部分字段对象可能继承先前的 ID/org/正文。这会让不完整缓存项越过现有有效性检查，是实际缺陷。先添加代表性结构体回归，五个子场景均失败，见 [red 记录](testing/evidence/week11/performance-reviewed-regression-red.txt)。

最终 `listData.UnmarshalJSON` 每次字段出现时先创建新的 `[]T`，解析成功才替换；最后一个数组的字段不会继承前一个数组，完整有效替换保持与旧 RawMessage 方法相同的结果。测试覆盖末尾空对象、null 项、部分对象、有效替换和转义 data key，并验证不能借用旧 ID/org 通过校验。missing/null、foreign org、损坏/截断/trailing/type、大小边界、有效空数组和完整列表相等测试继续通过。

修复恢复了 JSON 的额外扫描边界，仅移除 RawMessage 分配和拷贝；不能继续声称最终版本是单次 typed 解码。GETRANGE 大小限制、Redis 超时、当前成员数据库授权、revision/org 校验、逐项 valid 及腐败回源全部保留，写入格式未变。

早先 [baseline](testing/evidence/week11/performance-baseline.json)、[pre-fix after](testing/evidence/week11/performance-after.json)、[旧汇总](testing/evidence/week11/performance-summary.json) 和 [旧基准全部样本](testing/evidence/week11/performance-benchmark.txt) 是已被取代实现的历史记录，其 +13.7%～+21.6% 吞吐变化和 316 allocs/op 不用于最终代码验收。所有旧 performance 证据保持原始字节，见 [完整旧文件 hash 清单](testing/evidence/week11/performance-reviewed-originals-manifest.json)；没有选择性删除或覆盖旧轮次。

## 环境和固定方法

- Linux 5.15.0-139-generic amd64，AMD EPYC 7542，可见 64 CPU，约 503.5 GiB 内存；Go 1.26.1、PostgreSQL 17.11、Redis 7.4.9。每阶段 JSON 的 machine 字段记录版本、CPU、内存与起始 loadavg；[host](testing/evidence/week11/performance-host.json) 保留 CPU affinity 及可取得的控制信息。API/PG/Redis/客户端共用宿主机，没有锁定 CPU 或频率。
- 标准库 Go HTTP/1.1 keepalive 闭环客户端，每 worker 独立 transport/连接。并发固定 1/8/16，每场至少 3 秒、两完整轮；请求 3 秒超时、响应 1 MiB 上限，最多 32 worker、30 秒、100 万请求，无代理/重定向。用实际经过时间计算 req/s，百分位包含全部请求；本次失败为零。
- 只接受 test 环境、W11 随机项目名前缀、literal loopback HTTP 目标，fixture 再检查专用数据库名、回环 PG/Redis 与无云凭据。Token 仅通过子进程 stdin 传递，不进入参数、日志或证据；未读取真实 .env、私有上传或备份。
- 两阶段分别新建相同迁移至 14 的自有数据库，通过真实 API 注册/登录两用户、创建两组织。两表各 20,000 行、每组织各 10,000 行工单/知识元数据；固定时间序列和 seed hash。登录不进入业务统计，Worker 不启动，无模型调用。
- 相同 CACHE_ENABLED=true、默认 TTL/1 MiB 阈值/诊断配置，每轮新 API 进程；每场清自有 Redis DB6，顺序预热 20 次后读热缓存，测量顺序完全相同。未与 root 重型检查重叠。
- POST 预热后删除本实验 id>20000 的工单及其事件、恢复 sequence、cache revision=10000，VACUUM ANALYZE 并清自有缓存，再开始计时。每场两表起点均 20,000；POST 终点工单增量/事件数等于成功数，其他场景不改行数，两阶段最终均恢复两表各 20,000。start/end_counts 包含组织分布；逻辑数据恢复不等于 PostgreSQL 物理页布局完全相同。

固定参数（主测组织 1）：

| 场景 | 方法与路径 | 请求体 |
|---|---|---|
| ticket_list | GET /api/v1/organizations/1/tickets?page=1&page_size=100 | 无 |
| knowledge_list | GET /api/v1/organizations/1/knowledge/documents?page=1&page_size=100 | 无 |
| ticket_detail | GET /api/v1/organizations/1/tickets/1 | 无 |
| ticket_create | POST /api/v1/organizations/1/tickets | `{"title":"W11 controlled create","body":"Synthetic bounded create body.","priority":2}` |

## Profile 支持的分配优化

初次改动前已保存源文件、binary/hash 和业务 profile。原 baseline Cache.get 累计 CPU 38.18%，RawMessage.UnmarshalJSON 为 791.15 MiB / 8,291.26 MiB 采样分配（9.54%），支持评估缓存拷贝热点。I1 修复后重新完整测量，不沿用有缺陷实现的 after 性能。

Reviewed baseline 的 [CPU 调用行](testing/evidence/week11/performance-reviewed-baseline-cache-get-list.txt) 为 Cache.get 13.54s / 36.59s（37.00%），[分配热点](testing/evidence/week11/performance-reviewed-baseline-alloc_space-top.txt) RawMessage.UnmarshalJSON 783.85 MiB（9.28%）。最终 [getList](testing/evidence/week11/performance-reviewed-after-cache-get-list.txt) 为 13.38s / 36.48s（36.68%），CPU 占比基本接近；[after 分配](testing/evidence/week11/performance-reviewed-after-alloc_space-top.txt) 中 RawMessage 热点消失。本次证明的主要收益是减少分配，不声称大幅降低整个业务 CPU。

每阶段单独 12 秒、并发 8 的热工单列表业务负载，顺序采集 5 秒 CPU、1 秒 trace，前后采 heap/allocs，无 gc=1。其请求不混入下面延迟矩阵。CPU/heap/allocs 已经 go tool pprof 解析，trace 已经 go tool trace -pprof=sched 解析；原始二进制与 top/源码行均保留。CPU 是多核累计样本，不是墙钟耗时。

## 完整未启用 CPU/trace 的矩阵

req/s 和 p50/p95/p99 为两轮各自统计值的算术平均；不是合并不同轮后的百分位。全部原始轮次、请求耗时与两轮吞吐范围见 JSON。B/请求为两轮总分配字节差额除以总请求数，包含 HTTP/SQL/缓存及诊断固定开销。

| 场景 | 并发 | req/s 前 → 后（变化） | p50/p95/p99 ms 前 → 后 | B/请求 前 → 后 |
|---|---:|---|---|---|
| ticket_list | 1 | 197.2 → 200.6 (+1.76%) | 5.14/6.41/6.91 → 5.03/6.35/6.90 | 581,799 → 509,033 |
| ticket_list | 8 | 1083.7 → 1093.9 (+0.95%) | 7.15/9.44/10.41 → 7.12/9.38/10.42 | 634,726 → 574,589 |
| ticket_list | 16 | 1727.5 → 1787.1 (+3.45%) | 9.22/11.29/12.64 → 8.74/11.09/12.27 | 617,687 → 558,622 |
| knowledge_list | 1 | 302.8 → 307.9 (+1.70%) | 3.41/4.40/5.01 → 3.17/4.27/4.91 | 242,492 → 194,225 |
| knowledge_list | 8 | 1615.9 → 1706.9 (+5.63%) | 4.85/6.65/7.54 → 4.67/6.20/7.15 | 327,898 → 273,962 |
| knowledge_list | 16 | 2543.0 → 2603.9 (+2.40%) | 6.26/7.97/8.82 → 6.08/7.87/8.72 | 331,536 → 289,374 |
| ticket_detail | 1 | 901.6 → 911.5 (+1.10%) | 1.08/1.38/1.78 → 1.06/1.36/1.88 | 19,932 → 19,937 |
| ticket_detail | 8 | 4897.0 → 4897.4 (+0.01%) | 1.54/2.41/2.88 → 1.54/2.38/2.85 | 20,527 → 20,548 |
| ticket_detail | 16 | 7834.2 → 7829.1 (-0.07%) | 1.84/3.19/3.58 → 1.84/3.19/3.60 | 22,176 → 22,153 |
| ticket_create | 1 | 386.1 → 389.3 (+0.82%) | 2.50/3.05/3.73 → 2.49/3.03/3.71 | 25,054 → 25,037 |
| ticket_create | 8 | 794.2 → 802.7 (+1.07%) | 7.60/25.47/41.52 → 7.28/26.02/42.83 | 25,099 → 25,095 |
| ticket_create | 16 | 685.4 → 688.5 (+0.44%) | 17.55/62.46/93.97 → 17.29/61.63/94.88 | 25,095 → 25,143 |

矩阵总请求 137,991 → 139,493；总分配字节 21,969,025,072 → 20,132,055,360。详情/创建平均吞吐变化 -0.07%～+1.10%，作为噪声保留。POST 并发 16 吞吐低于 8，也完整记录，未扩大优化范围来修饰结果。

独立 profile 负载完成 14,105 → 14,175 请求；allocs 差分采样总量 8,448.58 → 7,682.20 MiB、分配对象 57,355,841 → 56,797,316。约 613 → 555 KiB/请求、4066 → 4007 对象/请求。这些包含 profiler/trace 开销和 GC 采样滞后，不能给细小对象差异作显著性保证。完整 [before objects](testing/evidence/week11/performance-reviewed-baseline-alloc_objects-top.txt)、[after objects](testing/evidence/week11/performance-reviewed-after-alloc_objects-top.txt) 与 heap/allocs 快照保留；heap 存活量不能替代总分配数。

## 最终基准和验证

相同 100 条 ticket-shaped JSON，test-only comparator 保留原双 Unmarshal 方法，最终五组中位数：1,274,229 → 1,128,981 ns/op（-11.4%），135,007 → 77,659 B/op（-42.5%），322 → 321 allocs/op。所有十个原始样本保留，不设耗时硬门槛。

最终修复先红后绿，Go affected race 两包通过、W11 Python 7 测试通过，见 [reviewed tests](testing/evidence/week11/performance-reviewed-tests.txt)。测量结束后独立重算全部 nearest-rank 百分位、请求/状态/延迟数量、48 场时长和行数公平性；source/method/seed 相等，旧证据 hash 不变。测量停止后，控制器对最终代码执行 [真实 PG/Redis 后端 race](testing/evidence/week11/backend.json)：1181 测试节点、286 个顶层、0 Skip；[最终受影响检查](testing/evidence/week11/final-affected-checks.json) 的 Python 24 测试及 Go vet/build 通过。

## 复跑和基线可追溯性

```bash
python3 scripts/week11-performance.py --stage baseline --replay-label replay-2
python3 scripts/week11-performance.py --stage after --replay-label replay-2
```

阶段 JSON 不可覆盖，新标签一开始写 INCOMPLETE，完整配对通过才写 PASS。保留原始 [baseline snapshot](testing/evidence/week11/performance-baseline-cache.go.txt)、[build manifest](testing/evidence/week11/performance-baseline-build-manifest.json) 与最终 [reviewed after snapshot](testing/evidence/week11/performance-reviewed-after-cache.go.txt)。脚本按 manifest 校验 go.mod/go.sum、非测试 cmd/api/internal Go 文件集合/hash；baseline 通过 Go -overlay 编译旧 cache，after 核对最终 reviewed snapshot。当前源不匹配、缺失冻结证据或没有配对 baseline 都明确拒绝，不用当前优化源码冒充 before。没有源码替换、平行项目或 runtime 分支。

manifest 的旧 cache 在首次 baseline 前保存；其他产品源码 hash 在之后的未修改边界内记录，不是 Git 提交快照。原始 overlay 重建已验证字节一致；本次 reviewed baseline binary 同样与原始 baseline 完全相同（SHA256 fdc18ea1cc7ddf05d1fb3b6d37b207c2ad9c40b609dda624220eb325ec485125）。即使 .local 二进制丢失也可从冻结证据重建；pprof source_path/trim_path 指向相应冻结源码，避免读错行号。旧 guard-unpaired 的 INCOMPLETE 是保留的负向保护证据，最终性能状态看 performance-reviewed-summary.json。

只有两轮、顺序 before/after、短时同宿主机实验，没有随机交错或置信区间；共享 CPU、GC、SQL 锁/缓存和客户端竞争影响尾延迟与细小差异。结果不代表生产容量、SLA 或 LLM 延迟。模型质量/时延另见 [W10 独立评测](testing/evidence/week10/evaluation.json) 和 [RAG 说明](rag-evaluation.md)；本次未调用模型，未新增依赖/迁移，未读取或修改真实环境，没有提交/推送。

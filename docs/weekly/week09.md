# Week 09：Redis 缓存、接口限流与查询优化

依据第九周详细手册完成开发、故障演练和学习文档。本周由 AI 按用户整周委托实现并运行测试，不代表开发者已独立完成学习检查。第八周历史代码/迁移/证据保留。

## 每日交付

| 阶段 | 完成内容 | 入口 |
|---|---|---|
| Day 0 | W08 基线、现有索引和读接口盘点 | performance/week09/baseline.md |
| Day 1 | 比较 6 类读接口，选工单/知识元数据列表；设计权限/key/版本/故障边界 | cache-design.md |
| Day 2 | 复用 Redis 配置、共享 API 客户端、有界读取/超时、TTL 抖动、错误回源、统计 | internal/platform/cache、config |
| Day 3 | 两个 Service.List 授权后 cache-aside，保留空数组/筛选/分页协议 | ticket/knowledge service |
| Day 4 | PG 事务版本覆盖 API/Worker/AI 写入；回滚/延迟填充/中断恢复验证 | migration00013、week09_cache_test |
| Day 5 | AI 用户+组织固定窗口限流、Lua 原子性、429/Retry-After、503、页面草稿保留 | ratelimit、ai handler、前端 |
| Day 6 | 两条 SQL 固定参数化谓词、优先级索引、真实 12 万行/表执行计划与分页对照 | migration00014、query-analysis.md |
| Day 7 | 全门禁 Go race、真实浏览器 Redis 停启、前端检查、本机升级/启动、问题答案 | evidence/week09、week09-answers.md |

新增 1 张基础设施表、2 条迁移；现有 39 个业务 API / 9 个页面不变。当前 14 业务表 + cache_revisions 共 15 应用表，另有 Goose 元数据表。

## 缓存与故障处理

工单列表和知识文档列表缓存都在数据库成员授权之后。Key 包含组织、领域、版本、全部筛选/分页、固定排序和当前可见范围。没有缓存审批、权限、正文下载或 RAG 来源有效性。

采用 PG 事务内版本更新、提交后对其他连接可见的逻辑失效，覆盖外层 AI Confirm 事务及 Worker 直接写文档。Redis 写失败不影响业务提交；中断期间写入会增加 PG 版本，恢复后的旧缓存不可命中。默认 TTL 30～36 秒、单操作 100ms、最大 1MiB、无自动重试。多版本旧 key 由 TTL 回收；同组织领域版本行会串行化短写操作，这是明确成本。

AI 发消息在 owner/成员/输入校验后消耗 user+org 配额，默认 10 次/60 秒；429 返回等待秒数，Redis 故障保守返回 503。计数的是被配额接受的请求尝试，之后遇到全实例并发繁忙、重复 request ID 或其他业务冲突也可能计次，不自动退还。历史/确认/拒绝不消耗该额度；本周没有给登录或全部 RAG API 增加时间窗口限流。

## 实际验收

最终运行记录：`docs/testing/evidence/week09/result.json`、`checks.json`、`integration.log`、`checks.log`。

- 随机独立 PostgreSQL17/pgvector0.8.7、Redis，合成数据；迁移 **12→14→12→14** 通过。只清理自己创建的资源。
- `python3 scripts/week09-smoke.py`：**1079 个 Go PASS 节点、243 个顶层测试、0 FAIL、0 SKIP**，`-race -count=1`，包含 W01～W08 全部门禁与 W09 新门禁。
- 真实浏览器五步通过：列表、真实 API SSE + 明确的本地模型夹具、429/Retry-After/草稿、实际 Redis 停止时数据库回源与 AI503、Redis 恢复读取新版本。只 1 次本地模型调用、0 次云调用；429/503 没有新增消息或调用模型。
- 两个 API 实例、两个 Redis client、16 个并发网络请求：限额 1 时只有 1 个成功 SSE、15 个429、1次模型调用和2条消息。独立 Redis 测试还覆盖7/40精确接受、用户/组织隔离与窗口过期。
- 前端 **141 tests / 7 files** 通过；Node24.12.0，type-check + production build、lint、format:check 通过。
- `go vet ./...`、`go build ./...`、Python 启动器 **6 tests**、Python 编译与 diff 空白检查通过。

截图：`rate-limit.png`、`redis-unavailable.png`、`mobile-rate-limit.png`。截图中的 qwen3.5:35b 是夹具协议模型名，**不是这次实际运行 Qwen 或 DeepSeek 的证据**。旧 W08 DeepSeek 实测仍保留。

测试过程中修正了 SQL 合成夹具的参数类型及浏览器脚本的 viewport 调用；第一次浏览器功能检查完成但脚本在截图步骤失败，修正后重新跑完整烟测通过。代理曾用系统 Node20 遇到 Blob/FileReader 不兼容，切换项目要求的 Node24 后141项全过，没有为旧运行时修改业务代码。

## SQL 证据

`python3 scripts/week09-performance.py`：各表120000行、3组织、固定软删/状态/优先级分布，同参数2次预热+9次测量。auto 与 force_generic_plan 分开报告，生产配置仍 auto。原始每次 EXPLAIN ANALYZE BUFFERS JSON 保存，三页完整行结果相同。

本次工单高优先级 auto 中位数约 **0.453→0.021ms**，无关过滤行1891→0；知识列表 generic约 **4.185→0.029ms**。知识 auto 原已使用合适索引，其小幅时间变化不作为稳定倍数收益。新增索引有存储和写维护成本，版本行有写放大；未做生产冷盘或写容量压测。深 OFFSET 计划已记录，cursor 分页未实现。

## 本机运行和代码位置

按用户最新要求，第八周依赖和第九周完整成果已经迁回 **`/media/yjz/pzp/Go/projects/NexusDesk`** 主目录。当前 `main` 工作区保留未提交改动；代理没有自动提交或推送。后续开发、测试和启动统一在主目录进行。迁移前逐文件记录 SHA256，迁入后核对773个文件完全一致，再更新路径说明。

本机已不透明0600备份后仅 Up12→14；备份保存在主目录 ignored `.local/week09-backups`，不读取内容、不提交。`.env`沿用已有文件并补非秘密默认值，私有存储位置保留，文件0600。API/Worker/Web 使用主目录启动，8080 health 与5173前端返回200；Worker启动标记及进程环境隔离已核对。证据 `runtime.json`。

详细命令见 [运行指南](../demo/week09-demo.md)。代码与测试基线记录在 `source-baseline.json`；完整性能说明见 [benchmark-notes](../performance/week09/benchmark-notes.md)。

## 已知边界与复习

没有 singleflight/分布式锁/公开监控接口；冷缓存并发可多次回源。没有宣称并发撤权完全线性化、生产容量已验证、所有接口已限流、深页问题已解决。数据库备份恢复造成版本倒退时需要处理缓存命名空间；不可清空包含 Asynq 的 Redis DB。

学习问题的18道参考回答见 [week09-answers](week09-answers.md)。个人尚不理解的条目应由开发者读代码和复现实验后自评，本文不替你填写“已经掌握”。下一阶段按第十周手册推进检索效果评测与 RAG 增强。

## 独立审查

缓存任务、限流任务及最终整分支审查均通过，没有 Critical/Important/Minor 待修复项。最终审查核对代码、实际日志、查询原始计划与文档边界，没有重复冒充执行测试。代码保留在上述主目录供你查看和自行提交。

## 迁回主目录后的复验

按用户要求，全部开发成果和必要的本地记录统一回到 `NexusDesk`。迁移前记录773个源文件的SHA256，迁入后逐一验证；业务代码、配置和迁移文件保持与原验收版本一致。旧测试记录及被更新文档的原版本保存在主目录 ignored `.local/week09-migration`，原数据库备份通过文件移动保留，没有读取备份内容。

本次从主目录重新执行 `python3 scripts/week09-smoke.py`：1079个PASS节点、243个顶层测试、0跳过；真实浏览器五步通过。前端141项测试及build/type-check/lint/format、Python6项、Go vet/build再次通过。API、Worker、Web进程的工作目录均核对为主目录或其web子目录，8080与5173返回200。日常库保持迁移14，本次未执行日常数据库迁移。

后续只在主目录开发、测试和运行，不额外建立平行项目目录或worktree。迁移状态见 `../testing/evidence/week09/relocation.json`。

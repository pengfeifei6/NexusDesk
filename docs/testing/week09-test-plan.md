# 第九周验收矩阵

完整执行 `python3 scripts/week09-smoke.py`；仅真实数据库/Redis 与 Go 回归用 `--backend-only`。脚本随机创建自己拥有的 Compose 项目，执行 12→14→12→14，全部数据合成，结束删除自己的测试资源。浏览器阶段启动本地确定性模型 HTTP 夹具，不调用 DeepSeek、不证明模型语义效果；W08 真实云验收仍保留在原周证据中。

| 编号 | 场景 | 预期/证据入口 |
|---|---|---|
| C01 | 同组织同参数重复列表 | 命中计数增加，结果相同；week09_cache_test |
| C02 | 不同组织/状态/优先级/页码 | 隔离 key，无串数据 |
| C03 | 空列表 | 可缓存且始终 JSON 数组 |
| C04 | 成员撤权后命中已有缓存 | 先实时查权限并拒绝，无缓存访问 |
| C05 | 写提交/回滚/待提交 | 只提交后版本可见；回滚不变 |
| C06 | AI 外层事务 Confirm | 一张工单/事件，列表读取新版本 |
| C07 | Worker 修改文档状态 | 直接文档 SQL 也触发版本变更 |
| C08 | 旧 SQL 结果延迟填入 | 只填旧版本，不能污染新版本 |
| C09 | TTL/抖动/坏 JSON/null/超大/错误组织 | 到期或不可用回源；有界字节读取 |
| C10 | Redis 关闭客户端/静默连接 | 有界失败、不重试、正常回源 |
| C11 | 实际停止 Redis，期间写入，再启动 | API 不重启，列表回源，恢复见新值；browser |
| R01 | 并发超过额度 | Lua 精确接受上限，其余拒绝 |
| R02 | 不同用户/组织，窗口到期 | 配额隔离、到期恢复 |
| R03 | 非 owner/非法输入/撤权 | 配额前拒绝，不创建消息 |
| R04 | 配额用尽 | 429 RATE_LIMITED、Retry-After，未开始 SSE/模型 |
| R05 | Redis 不可用 | 503 RATE_LIMIT_UNAVAILABLE，未开始轮次/模型 |
| R06 | 前端 429/503 | 固定安全提示、输入保留、没有自动重试 |
| Q01 | 旧/新 SQL 筛选组合和分页 | 真实 repository 对照结果一致 |
| Q02 | 同 created_at、软删、空结果 | ID 打破平局，固定数据分页无重复 |
| Q03 | 两表各 12 万行与两种 plan 模式 | 原始 EXPLAIN ANALYZE BUFFERS、多次中位数 |
| Q04 | 深 OFFSET=80000 | 保存计划并明确限制，不宣传已解决 |
| G01 | W01..W08 全门禁 race | 认证、隔离、任务队列、SSE、原子确认回归 |
| G02 | frontend tests/type/build/lint/format、launcher | 现有 UI 和配置传递契约保持 |
| G03 | 日常运行 | 不透明备份后只 Up；API/前端 health 与 Worker 环境隔离 |

执行结果以 `evidence/week09/result.json`、`integration.log`、`checks.log`、`browser.json`、`sql-plans.json` 和 `runtime.json` 为准。普通 `go test ./...` 的门禁 Skip 不计为通过。SQL 计时不使用稳定毫秒阈值作 CI 断言，只验证结构性扫描改善和结果一致。

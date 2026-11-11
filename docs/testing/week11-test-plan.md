# 第十一周测试矩阵

| 范围 | 必须观察的结果 | 验证入口 |
|---|---|---|
| 关联ID | 服务端生成、header/error/日志相同，不信任客户端ID | httpserver与RAG单测、faults |
| 隐私与基数 | 无header/body/query/原URL/SQL参数；未知路由统一标签 | observe/httpserver测试 |
| 诊断 | 默认关闭，literal loopback，GET白名单，采样时间与并发限制，绑定失败/退出 | config/observe测试 |
| 并发 | registry安全，scrape不阻塞业务或读外部依赖，goroutine正常关闭 | Go race测试 |
| SSE | 原writer与ResponseController保留，取消/权限/人工确认不变 | 历史AI真实PG门禁、浏览器 |
| 缓存优化 | 外组织/null/损坏/无效数据回源，版本与实时授权不变 | cache测试、旧W09门禁 |
| 压测 | 两组织合成数据、实际认证、固定并发轮次，失败计数保留 | week11-performance.py |
| Profile | 实际负载采样，CPU/heap可解析，前后版本可追溯 | pprof与性能报告 |
| Redis故障 | 工单回源200，AI503无新消息/提议/模型调用，恢复后可用 | week11-faults.py |
| PG故障 | health200与业务失败同时成立，不从缓存绕过权限；恢复原数据 | 同上 |
| Worker | 优雅停止/重启；真实RUNNING被kill，真实租约过期后收敛SUCCEEDED且索引唯一 | 同上 |
| 全量回归 | 真实PG/Redis全部旧门禁，Go race无Skip；前端/Python/static通过 | week11-smoke.py、week11-checks.py |
| 浏览器 | 实际工单/文档/AI/组织切换/手机流程和无持久Token | week11-smoke.py --web-only |
| 数据保留 | 日常服务/数据未作为演练对象；旧证据/依赖锁/迁移不变 | preservation与source manifest |

证据位于 `docs/testing/evidence/week11/`。普通Go默认跳过真实数据库门禁，不能替代本周smoke。性能没有依赖机器速度的硬性门槛；差异、波动和闭环模型限制须披露。

本地确定性模型仅用于故障与浏览器流程，不产生本周真实LLM语义评测或云成本结论。所有容器停机操作必须经过随机项目和服务label核验；只清理脚本自己创建的资源。

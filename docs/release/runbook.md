# 日常维护手册

所有开发继续在原 NexusDesk 主目录。启动见[运行指南](quickstart.md)，接口见[索引](api-index.md)，代码分工见[架构](architecture.md)。W12 没有修改既有迁移、依赖锁或真实配置。

## 修改一项业务的顺序

先定位页面和 API 契约，再找 Handler → Service → Repository。输入格式在 Handler；权限、状态和事务在 Service；SQL 与组织条件在 Repository。先写能复现问题的行为测试，修复后运行相关检查。新增数据库变化追加迁移，不改已执行文件。修改 API 同步对应 docs/api 文档和前端类型，不在 UI 写一套替代后端的授权。

例如“允许谁指派”：查看 `internal/ticket/policy.go`、`service.go`，再看 `web/src/composables/ticket-management.ts` 按钮规则。前端隐藏只是提示，后端仍必须拒绝不允许的请求。不要把 JWT 中塞角色作为提速方案。

## 按改动选择验证

| 改动 | 应执行 |
|---|---|
| 文档 | 核对路径、命令、状态和事实；不声称重跑了产品测试 |
| Vue/TypeScript | `python3 scripts/week12-checks.py`；涉及流程再运行 `--web-only` |
| Go 业务、SQL、权限、缓存、任务 | checks + `python3 scripts/week12-smoke.py --backend-only`；涉及 UI 再跑 web |
| 初始化/打包 | `python3 -m unittest discover -s scripts -p 'test_*.py'`；临时配置和新版本源码包验证 |
| 性能/恢复策略 | 先真实库回归，再按 W11 报告重建对比与隔离故障实验 |

W12 smoke 复用 W11 的归属校验夹具，暂存于 `.local/week11-runs`，输出独立写入 `docs/testing/evidence/week12`；随机 Compose project/test DB 只清理自己的资源。普通 Go 测试和现有 CI 默认不启用集成门禁，Skip 不能当通过。

测试结果每次执行会更新 W12 同名输出；要长期比较，运行前保留一份带日期的副本。源码打包拒绝同名覆盖，下一次换版本标签。

## 定位一次错误

打开浏览器网络面板记录状态码和响应 `request_id`，在 API 终端 JSON 日志按同一 ID 查找。不要复制 Authorization、Cookie、正文或密钥。API/Worker 的 `/metrics` 分别在可选本机诊断端口；进程重启会清零计数。`/healthz` 不检查每个依赖，也不是任务成功指标。

| 现象 | 核对与处理 |
|---|---|
| JWT_ISSUER is required | 用 `dev.py api` 读根目录配置；不要删掉必填验证 |
| 注册 500 | 检查迁移与 DB 连接；查请求 ID，不能通过重复建库掩盖 |
| 看不到管理按钮 | 确认当前组织角色；编辑仅 OPEN 创建者/admin，指派仅 OPEN admin，删除仅 admin |
| 添加成员 404 | USER_NOT_FOUND 指目标账号不存在；统一 NOT_FOUND 则可能失去当前组织访问 |
| 编辑 409 | 数据已变化，界面刷新后重新打开表单确认；不自动重放旧写请求 |
| 刷新页面退出 | Token 只存内存，重新登录 |
| 上传后不处理 | 核对 Worker、Redis、模型和 Job 终态；不要手动修改 lease/token |
| AI 503 / 429 | 核对 Redis/模型和配额；等恢复后重新决定是否发送，不能自动重放工具写入 |
| 有问答却无可靠依据 | 先看召回原文、引用和索引版本；链路 smoke 不代表语义正确 |

## 停止、备份与更新

自己在终端启动的服务用 Ctrl+C 正常退出。端口冲突先确认进程归属，不根据端口号强杀未知进程。日常数据库和 Redis 不参与测试脚本的停机演练。

需要备份时同时考虑 PostgreSQL 和私有 `var/storage`，记录备份时间、迁移版本和源码版本；秘密与备份保持私有。当前没有跨数据库/文件系统原子快照和自动备份调度，不宣称仅 pg_dump 就覆盖全部文档对象。恢复先到独立环境验证，不对日常卷试错。Redis 缓存可丢，但队列恢复以 PG 任务状态为准，不应随意 FLUSHDB。

更新代码后：读变更 → 所需备份 → 查看迁移 status → 如有新迁移再 up → 重启自己管理的 API/Worker → 检查实际业务与前端。W12 是前端与交付工具变化，无需重建数据库。

Git 提交、推送、公开仓库及外部部署仍由你决定。先审阅差异与未跟踪文件，确认私有配置/上传/备份被忽略；本次没有自动提交，也没有将历史工作拆到其他项目目录。

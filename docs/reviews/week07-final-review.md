# W07 final whole-delivery review

日期：2026-11-11。范围：缺失第七周手册及 W06→W08 之间的完整异步文档任务交付。

审查依据为 `docs/superpowers/specs/2026-11-11-week07-design.md`（含 binding corrections / review rulings）、对应 implementation plan，以及 `.superpowers/sdd/2026-11-11-week07/final-review.patch` / `final-review-files.json`。已逐段阅读完整 68 文件、10,915 行快照差异；没有使用包含历史未提交工作的 HEAD diff 替代。审查期间只写本报告，没有运行服务、操作数据库/Redis、读取真实配置或私有数据、重新执行大测试集、派生审查者或提交 Git。

## Strengths

- PostgreSQL 的任务状态、预算和投递预留是业务事实；API 提交不接触 Redis，消息只含严格校验的三个标识。补投递与自动重试都受数据库状态、版本、时间和次数控制。
- 文档→任务的锁顺序贯穿领域发布、取消、人工重试与过期恢复。独立失败清理保留原 guard；结束执行的清理只修改匹配 token 的中间态，保护新执行和 READY 结果。第三次执行的已发布结果优先核验，避免误报失败或第四次模型调用。
- 五个 HTTP 端点保持组织、成员、创建者/admin 和软删除边界；固定错误码、最小 DTO 与日志脱敏一致。历史 schema 的同步路径保留不引用新列的兼容分支。
- 独立 Worker 无 JWT 配置依赖，队列及模型调用有时间预算。前端明确区分 202 与完成，轮询有界且不重叠；首个成功响应触发刷新，权限失效会取消正文请求并清理受保护内容。
- 新手册具备 Day0～7、36 项验收、20 道问题及 W08 交接；运行、API、ADR、维护和参考答案与实现相符，没有提前实现 W08 或代称个人掌握。

## Issues

### Critical (Must Fix)

无。

### Important (Should Fix)

无。

### Minor (Nice to Have)

**已解决：补投递时间被误写成上限。** 快照中 `docs/decisions/0007-durable-asynq-jobs.md:17` 写“最多 15 秒补投递延迟”，但 `internal/jobs/service.go` 的 15 秒是下一次预留资格，实际还受扫描周期、串行有界 I/O 和积压影响。这会让维护人员错误理解故障恢复时间。已通知主执行者，并核验当前第 17 行改为资格窗口及其额外延迟因素，不再承诺最大延迟；周报对应说明也已修正。无未解决 Minor 项。

## Verification evidence

本次审查读取并核对已保留的证据，没有把测试文件存在或普通 Skip 当作通过：

- `week07-go-race-green.log`：独立计数 182 顶层 + 779 子测试 PASS，0 FAIL、0 SKIP。周报保留了“Go 子进程通过、同轮后续 Python 失败”的区分。
- `week07-ui-fix-green.log`：101/101 通过；build 完成、format 全部匹配、mod verify 成功。lint 日志及主执行者记录支持其通过，vet/build 的退出状态由主执行者报告。
- `week07-process.json`：7 项 PASS、无清理失败，实际 SIGTERM/SIGKILL；调用间隔 6.028 / 31.322 秒，第三次发布后恢复保持 3 次尝试、1 次模型调用。报告明确列出 lease/attempt/触发器注入，未包装成无干预故障实验。
- `week07-browser-attempt1.log` 与四份浏览器结果：W04/W05/W06/W07 分别 11/10/8/7，共 36 项；真实 API、独立 Worker、Redis 和本机模型；同一异步入库文档的 Search/Ask 来源闭环通过。日志完成到专用资源清理；整体退出 0 由主执行者确认。
- `week07-baseline-comparison.json`：522 个基线文件无缺失，旧迁移和历史证据未变。旧 schema8/schema10、W01 真实回归由主执行者提供通过记录，本审查未重跑。
- 收尾补充：已读取 `week07-development-migration.log` 的不透明 0600 备份、现有 PG17.11 仅 Up10→11 记录，以及 `week07-development-redis.log` 的专用容器 Healthy 记录。主执行者另外确认回环 6380 映射及 PONG；审查者没有直接访问日常资源或备份内容。

以上是本机工程和契约证据，不扩展为新机器安装、远程 CI 或生产 SLA 证明。

## Recommendations

由主执行者将已完成的日常不透明备份、仅 Up11、专用 Redis6380 启动记录同步到最终周报及运行说明。保留现有失败日志和分阶段运行说明；这些文档收尾不要求重写已通过审查的业务代码。

## Declined to judge

- W08 SSE、会话、工具调用、写操作确认：明确属于下一周，未实现不构成本周缺陷。
- PDF 解析与历史无 task_token 的中间态自动恢复：本周明确排除；当前文档和失败行为如实说明边界。
- 历史成员管理及工单 PATCH/指派/删除的完整 UI：已有接口与回归保持，本周不承诺补齐全部历史界面。
- 公网部署、Redis Cluster、生产负载/SLA 和远程 CI：没有该任务范围或对应运行证据。
- 每一条 PostgreSQL 写入的提交响应丢失窗口：已检查当前错误/补偿设计与代表性真实强杀证据，但未对每个窗口独立故障注入，周报已披露这一测试边界。
- 日常备份内容和恢复演练：按只读及隐私约束未访问备份；升级和专用 Redis 启动以主执行者的实际操作记录为证，不声称本审查独立完成过恢复实验。
- 开发者独立掌握、语义标签人工复核、Git 提交：不由工程测试或 AI 审查代为认证。

## Assessment

**Ready to merge? Yes — 代码审查结论，不表示执行合并或提交。**

本周实现满足设计与前后端交付要求；没有发现未解决的 Critical、Important 或 Minor 问题，唯一文档问题已核验修正。日常运维收尾也已提供执行记录；主执行者同步最终文档后，可据实际证据完成本周交付。

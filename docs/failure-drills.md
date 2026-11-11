# 第十一周故障演练报告

2026-11-11在本机独立随机Compose项目上实测，原始记录见[故障数据](testing/evidence/week11/faults.json)，复现命令为 `python3 scripts/week11-faults.py`。所有账号、文档与工单为合成数据，模型为本地确定性HTTP夹具，没有真实云调用或日常数据库/Redis停机。

## 实际结果

| 演练 | 故障期间观测 | 恢复与结果 |
|---|---|---|
| Redis停止/启动 | 已预热工单缓存回源DB返回200；缓存failure指标增加；AI返回503 RATE_LIMIT_UNAVAILABLE，没有新增消息/提议或模型调用 | 恢复后真实SSE到message_end，消息表恰增2行；列表继续可读，PASS |
| PostgreSQL停止/启动 | healthz仍200，受保护工单读取500 INTERNAL_ERROR；没有从缓存绕过实时授权 | 原账号读取原工单成功，原记录保留，PASS |
| Worker优雅停止/重启 | SIGTERM退出码0；停止期间提交的任务持久化为PENDING | 重启后任务SUCCEEDED，总执行次数1，PASS |
| Worker运行中SIGKILL | 本地模型阻塞时，任务确认为RUNNING/INDEXING；Worker退出码-9 | 等待真实租约与重试，最终SUCCEEDED，总执行次数从1到2；当前索引READY且只有1个唯一块，PASS |

首次实测18:46:58～18:47:54（Asia/Shanghai）记录完整保留在[faults-initial](testing/evidence/week11/faults-initial/faults.json)。接入优化后于19:18:11～19:19:07复测，四项再次PASS；当前文件用UTC记录。复测Redis恢复约0.343秒，PG约0.230秒，Worker优雅退出约0.214秒。被杀时剩余租约29.824秒，约36.528秒后任务成功。依赖恢复时间从 `docker start` 返回后开始计算，不包含容器启动命令本身；这些是本机样本，不是可用性SLA。

## 如何证明没有假恢复

- Redis故障前先实际读取两次工单并确认cache hits>0；故障中断言failures增加，原工单仍存在。
- AI拒绝前后直接核对合成数据库中的消息和提议数量，且本地模型stream调用计数为0；恢复后确实完成SSE并持久化USER/ASSISTANT两行。
- PG故障前缓存已预热，但读取仍必须失败；这验证授权依赖DB，而不是有缓存就放行。
- Worker崩溃前从真实任务API确认运行阶段，从只读SQL读取剩余租约；没有修改lease_until、状态或执行次数。
- 新Worker启动后先断言任务仍属于未过期的旧执行，不能立即抢占；最终核对索引声明块数、实际块数、当前版本块数和唯一位置数全部相等。
- Redis与PG失败响应的request_id都在API JSON日志中找到。日志保存前检查不含测试登录Token或配置凭据。

## 资源与证据

脚本检查Compose容器的project和service标签，只能操作本次 `nexusdesk-week11-<随机12hex>` 的db/redis。Worker使用脚本创建的Popen进程对象；没有按名称批量杀进程。finally恢复依赖，随后清理自有容器和卷；日常6380/6379/PG均不作为演练对象。

- [API日志](testing/evidence/week11/faults-api.jsonl)
- [Worker日志](testing/evidence/week11/faults-worker.jsonl)
- [故障前指标](testing/evidence/week11/faults-metrics-before.txt)
- [故障后API指标](testing/evidence/week11/faults-metrics-after.txt)
- [恢复后Worker指标](testing/evidence/week11/faults-worker-metrics.txt)

测试说明：这证明当前单机、短时停止/启动及一次Worker中断的恢复机制。没有测试磁盘损坏、数据丢失、Redis持久文件损坏、网络分区或跨机高可用。重复投递可以继续发生，业务唯一性靠当前执行token/版本及数据库规则，不宣称队列“恰好一次”。

任务如果达到最大尝试次数，仍可能终止为FAILED，需要按既有手动重试权限处理。Worker函数nil返回不等同于业务成功；本演练额外查了任务与索引终态。

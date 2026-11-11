# 第七周验收与复现

本页将手册的 T01～T36 对应到实际检查。状态与最终次数以[周报](../weekly/week07.md)和保留日志为准，不把普通 Go 的 Skip 当作 SQL 通过。

## 入口

```bash
python3 scripts/week07-smoke.py --backend-only
python3 scripts/week07-smoke.py
```

随机专用 PostgreSQL/Redis/私有目录，使用虚构用户；验证库名前缀、回环地址与 Compose 所有权，只清理本次资源。默认普通 Go 会跳过需要专用库的入口。W07 wrapper 启用 W02～W07 全部门禁，不能手工把这些门禁指向日常库。

| 手册项 | 主要证据位置 | 验证内容 |
| --- | --- | --- |
| T01～T05 | TestWeek07SchemaAndHTTP | 五路由认证、组织隔离、禁用成员、创建者/admin、严格输入/22字段 |
| T06～T08 | SchemaAndHTTP、DedupPipelineAndRetry、process | 提交持久化、并发去重、CHECK/FK、Worker缺席时PENDING |
| T09～T14 | DedupPipelineAndRetry、RealRedisDelivery、process/browser | 独立无JWT Worker、真实提取/索引、重复与旧版本消息、租约竞争 |
| T15～T18、T28 | DedupPipelineAndRetry、PermanentFailures、upstream回归 | 5/30秒、max3、永久失败、人工版本和actor；早到消息不绕过预算 |
| T19～T20、T24～T25 | BlockedPublicationAndOldWorker | 模型阻塞时取消/重试、撤权/删除/依据改变，拒绝旧执行者迟到发布 |
| T21～T23 | week07-process.py、RecoveryStateInjection | 实际SIGTERM/SIGKILL重启、第三次已发布结果核验后成功 |
| T26～T27 | DispatchReservations、RealRedisDelivery、process | 提交与投递非原子间隙、发送失败、15秒补发、旧seq/run结果不能污染 |
| T29 | W05/W06事务回归、新任务受保护发布 | 故障不留下半份READY或部分Chunk |
| T30～T32 | jobs.spec.ts、week07-browser.mjs | 202不误报成功、轮询有界/无重叠、路由/会话/隐藏页取消、失败保留上传文档 |
| T33～T34 | process、模型故障API、payload单测 | Redis/模型故障不拖垮普通业务，消息仅三ID，日志无秘密/正文 |
| T35 | W01历史、W05/W06 schema回归、W04～W07浏览器 | 旧schema兼容、旧业务、真实BGE/Qwen/来源 |
| T36 | sharedrunner与日常升级日志 | 专用库10→11→10→11、重复Up；日常库只Up11 |

## 故障测试与真实模型的分工

Go 集成测试使用真实 PostgreSQL/Redis，模型边界使用可控返回/阻塞，以可重复地制造取消、撤权、租约到期等竞态。SQL 调整 due/lease/attempt 时间只代表确定性状态注入，不是假装进程真的崩溃。

进程脚本真的启动独立 Worker PID，并对自己创建的 PID 发送 SIGTERM/SIGKILL；Redis 故障也仅针对本次随机项目确认过的容器。强杀后的 lease 时间可加速，第三次“发布后确认前”窗口通过测试触发器和 attempts=2 注入扩大；报告会注明这些条件。假向量不能证明语义效果。

浏览器通过真实 API、Redis、Worker 和本机 bge-m3:567m 完成异步索引。历史 W06 浏览器继续验证真实 Qwen 问答及来源；固定20题语义标签和既有指标保持原样，不为本周重调。个人人工复核标签仍待完成。

## 维护原则

保留失败日志，修复后另存运行；不删断言以获得绿色。测试报告区分全套退出码、单包PASS、未启用Skip、未执行场景。本机依赖缓存与浏览器缓存被复用，不声称全新机器安装或远程CI已经通过。

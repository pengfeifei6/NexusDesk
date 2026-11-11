# W08 测试矩阵与复现入口

执行条件与最终统计见 [第八周记录](../weekly/week08.md)，实际输出保存在 [evidence/week08](evidence/week08)。普通Go测试默认跳过数据库门禁；正式验收脚本启动随机独立Compose项目、专用PostgreSQL/Redis与私有临时目录，只清理自己创建的资源。

```bash
python3 scripts/week08-smoke.py --backend-only
python3 scripts/week08-smoke.py --live
```

backend-only启用所有周次门禁执行完整 `go test -race -count=1 -v ./...`，包含真实PostgreSQL/Redis。live额外读取已授权本机DeepSeek配置，只将Key传给API，Worker与浏览器不接收Key；用合成文档执行BGE入库和真实模型工具调用，生成截图及3～5分钟演示。仅live产生云请求。失败退出非0，不能把测试文件生成或Skip当作验收。

| 编号 | 场景 | 自动化证据入口 |
|---|---|---|
| T01 | 未登录创建会话401 | TestWeek08HTTPPrivacyAndSSE |
| T02 | 同组织其他成员/admin不能看会话 | TestWeek08StoreIsolationDedupeRecovery、HTTPPrivacyAndSSE |
| T03 | 跨组织会话/提议404 | 上述真实PG测试、week08-browser.mjs |
| T04 | 重复request UUID不重复写或调模型 | StoreIsolationDedupeRecovery、ConcurrentStartAndProposalUniqueness、HTTPPrivacyAndSSE |
| T05 | 多个content_delta后message_end | TestStreamTextAndUsage、HTTPPrivacyAndSSE、真实浏览器 |
| T06 | 中途模型失败→error且FAILED | TestStreamRejectsBrokenResponses、HTTPStreamFailureAndServiceAvailability |
| T07 | 浏览器停止→上游取消 | TestStreamTimeoutCancelsUpstream、RealNetworkCancelAndProxyFlush、真实浏览器 |
| T08 | 超时有界结束 | TestStreamTimeoutCancelsUpstream、TestChatCanceledCleanupAndFailure、HTTP超时子例 |
| T09 | 无Flush writer在响应前拒绝 | TestSSERejectsUnsupportedWriterBeforeHeaders |
| T10 | 代理不等完整响应即转发 | TestWeek08RealNetworkCancelAndProxyFlush、Vite真实浏览器；不声称所有外部代理通用 |
| T11 | 不完整JSON参数不执行 | TestToolArgumentsRejectInvalidJSONAndTypes、HTTP bad_arguments |
| T12 | 未知工具名拒绝 | TestRunnerRejectsUnsafeToolsAndArguments |
| T13 | 未注册工具记录安全错误码 | 同上；固定AI tool rejected日志，代码校验不记录参数 |
| T14 | org/user伪造与未知/大小写字段 | RunnerRejectsUnsafeToolsAndArguments、ToolArgumentsRejectInvalidJSONAndTypes、HTTP spoof_org |
| T15 | search只返回本组织来源 | TestWeek08ToolsCurrentOrganizationAndSourceInvalidation/search、真实BGE+DeepSeek |
| T16 | 读取本组织工单 | 上述ticket子例、真实浏览器随机合成标题验证 |
| T17 | 外组织真实工单不泄露 | 上述foreign_ticket子例 |
| T18 | 轮数/输出/历史有界 | TestRunnerProposalOnlyAndFiniteLoop、CancellationOutputAndHistoryBounds |
| T19 | create_ticket不在模型白名单 | RunnerRejectsUnsafeToolsAndArguments、UntrustedToolResultCannotAddCapabilities、HTTP write_tool |
| T20 | 非法提议参数不保存 | TestNormalizeArguments、RunnerRejectsUnsafeToolsAndArguments |
| T21 | 合法提议PROPOSED | TestWeek08HTTPProposalReadToolsAndConfirmation |
| T22 | 不确认时tickets无新增 | 上述PG计数断言、真实浏览器确认前后列表计数 |
| T23 | 确认生成一张及事件 | TestWeek08ConfirmExactlyOnce、HTTPProposalReadToolsAndConfirmation |
| T24 | 重复确认同ticket_id | 上述测试、真实浏览器4次重复确认 |
| T25 | 并发确认至多一张 | TestWeek08ConfirmExactlyOnce真实PG8并发，不以race替代事务验证 |
| T26 | 过期拒绝执行 | TestWeek08ActionSafetyAndRollback |
| T27 | 拒绝后确认冲突 | 同上、真实浏览器 |
| T28 | 确认再次检查撤权且串行化 | 上述撤权测试、TestWeek08MembershipRevocationSerializesConfirm |
| T29 | 立即/COMMIT延迟失败完整回滚 | TestWeek08ActionSafetyAndRollback |
| T30 | 恶意文档/参数不能越权 | RunnerUntrustedToolResultCannotAddCapabilities、ToolsCurrentOrganizationAndSourceInvalidation、spoof_org |
| T31 | HTML脚本只作为文本 | web/src/lib/__tests__/ai.spec.ts安全渲染；后端转义SSE JSON |
| T32 | 日志/浏览器无凭据原文 | live脚本对捕获日志做Key/DB密码/JWT秘密匹配；浏览器无敏感env/存储；错误脱敏测试 |
| T33 | 模型不可用时普通工单可用 | HTTPStreamFailureAndServiceAvailability逐子例建单 |
| T34 | 工具结果不变成system指令/新能力 | RunnerToolLoopAndTrustedActor、UntrustedToolResultCannotAddCapabilities |
| T35 | 历史裁剪不无限增长 | RunnerCancellationOutputAndHistoryBounds、HistoryKeepsAnsweredTurnBoundaryWithoutResendingEvidence |

新增边界：SSE UTF8跨chunk、多个事件合并大chunk、缺少结束事件、畸形事件、FlushError穿过Gin、模型参数碎片、重复提议批次、成功确认后旧历史返回、组织切换与过期会话401、流中权限丢失清空、真实SQL完成和清理失败→恢复后遗留收尾。

数据库短暂不可用演练使用仅测试org命中的SQL UPDATE故障触发器，使完成/清理实际失败，再移除触发器、推进遗留时间验证恢复。这证明持久化失败边界，不假称已切断物理网络或重启整个数据库。模型断开/未知调用等故障通过可控HTTP/模型fixture注入；真实云可用性由live证据单独证明。

## 审查与已修复问题

三项专项审查为SSE/取消、Agent工具安全、确认事务。发现并修复：Gin隐藏底层FlushError；Go JSON大小写别名；测试夹具改变时间后按错位置断言；大网络chunk误触发缓冲上限；确认结果被较旧历史覆盖；组织切换退出AI页面。真实模型多轮测试还发现旧助手引用被整条移除导致重复处理历史请求，改为保留固定已处理标记，同时不重发旧片段。

每个问题有对应回归；最终审查结果及真实数值以week08.md和evidence结果为准。未测试或未实现的内容包括外部生产反向代理、横向多实例、自动断点续流、公网防滥用、模型正确性的统计评测；不能由本周合成演示推导生产准确率。

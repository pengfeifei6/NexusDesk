# W04 领取并发验收

运行 `python3 scripts/week04-smoke.py --backend-only`。随机独立 Compose Project、回环测试库沿用 `nexusdesk_w02d2_test_` 前缀；启用 W1/W2/W3/W4 共九个数据库入口。普通 `go test ./...` 会跳过这些入口，`-tags=integration` 不能替代环境开关。最新 migration 仍为 6，不改旧迁移，不访问日常 `.env` 或数据库。

## 数据库实验

[ticket_claim_test.go](../../tests/integration/ticket_claim_test.go)先同时借出20条连接并检查20个不同backend PID。20请求用同步起跑信号调用真实认证中间件/Handler/Service/PostgreSQL，不用进程业务锁、不串行化 HTTP 请求。第一轮20请求暂持工单行锁，查询 pg_stat_activity 确认多个真实 UPDATE 正在等锁，再释放；轮询只等待观测条件，不用 sleep 决定成功。

总计一轮2请求，以及20轮×20请求。每轮断言：恰好1个200，其余409 TICKET_CLAIM_CONFLICT；响应与最终assignee都等于获胜操作者；状态OPEN；version=1；只有一条TICKET_ASSIGNED，actor/version/before/after正确。两名active member轮换起跑顺序；不是20个不同用户或性能基准，不保证公平性或证明所有并发调度。

BEFORE INSERT和延迟COMMIT触发器分别注入事件失败，领取返回安全500，assignee仍NULL/version0/无领取事件；触发器只在专用库创建/删除，不改migration。持有真实行锁后取消请求，确认没有残留修改。另测无JWT/disabled/跨组织/删除/不存在/伪造身份/版本溢出/已指派/各非OPEN状态。

真实风险反证：两个独立事务先都SELECT到NULL，再分别无条件UPDATE，两次提交均成功、最终受理人被覆盖、version=2。该测试刻意绕过正式领取SQL，使用独立夹具，证明“先查再无条件改”风险；不是声称生产Claim出现过这一Bug。

首轮完整结果见 [PostgreSQL日志](../weekly/evidence/week04-postgres.log)：20个独立连接，实际观察18个并发锁等待，21轮全部满足断言；W1/W2/W3全部开启回归亦通过。最终版本结果在[week04](../weekly/week04.md)登记。

## C01～C20 对照

| 编号 | 场景 | 自动验证位置 |
| --- | --- | --- |
| C01 | active成员领取，actor来自身份 | ClaimCommits / 集成member_claim |
| C02 | 非成员/错组织404且无修改 | 集成isolation_identity_and_input |
| C03 | 无JWT401 | ClaimHTTPContract / 集成isolation |
| C04 | 非OPEN409 | 集成nonopen_assigned_stale_and_max |
| C05 | 软删除404 | 集成isolation |
| C06 | 已有受理人409 | 集成nonopen_assigned |
| C07 | 旧版本409 | 单元与集成nonopen_assigned_stale |
| C08 | 伪造assignee字段400 | ClaimHTTPContract / 集成isolation |
| C09 | 同人重复409 | 集成member_claim_repeat |
| C10 | 两人同时领取只有一成功 | simultaneous_claims round00 / 浏览器并发 |
| C11 | 20请求同时领取一成功 | simultaneous_claims round01～20 |
| C12 | 每轮version只加1 | simultaneous_claims最终行检查 |
| C13 | 每轮只有一领取事件 | simultaneous_claims事件内容和计数 |
| C14 | 事件失败回滚 | event_and_deferred_commit_failure_roll_back |
| C15 | 领取后OPEN | 单元/集成/浏览器 |
| C16 | 受理人开始处理、他人403 | member_claim_repeat_and_start；保留W03 admin代办权限 |
| C17 | 切组织清旧数据，拒绝迟到响应 | Vue组件测试 + browser C17 |
| C18 | 401/409/离线错误有区别 | HTTP单测 + browser C18、并发409 |
| C19 | 刷新失去Token，无旧身份请求 | browser C19、存储检查 |
| C20 | 登录/建单/领取真实API | browser C20及后续处理/评论/事件 |

浏览器结果和图片/视频见[G1演示](../demo/g1-demo-script.md)。Go `-race`验证执行路径内存竞争，数据库行和事件断言验证业务一致性；二者不能互相替代。

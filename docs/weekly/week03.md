# NexusDesk 第三周工作记录

- 日期：2026-11-10，Asia/Shanghai；W03工程任务完成，个人独立理解/演示、Git保存和远程CI另行记录。
- 起点：da47b6c 第二周开发任务完成；开始时W02 Day7清单58项全部一致，普通Go基线通过。
- AI按用户整周明确委托完成设计、实现、测试和资料；不把原手册本人先写/PLAN_ONLY当作重复确认要求，不冒充个人贡献。
- 没有读取/修改真实.env、访问日常库或自动stage/commit/push/部署；保留用户原有5份未跟踪后续手册。
- 依据：[第三周手册](../../项目开发文件/每周开发手册/06_NexusDesk_第三周详细开发手册.md)、[执行计划](../superpowers/plans/2026-11-10-week03.md)。

## 1. 每日任务与产物

| 阶段 | 实际交付 | 状态 |
| --- | --- | --- |
| Day1 | 用户故事、九API、状态/权限、ADR、W02差异和协作条款 | 文档完成 |
| Day2 | 00004～6、六表ER、复合FK/CHECK/索引、升级/反序回滚 | 真实库通过 |
| Day3 | 创建/列表筛选分页/详情，创建事件原子写入 | HTTP/DB通过 |
| Day4 | OPEN编辑、admin指派/null解除、expected_version、软删除 | HTTP/DB/TCP通过 |
| Day5 | 四合法状态边、评论/子列表、父锁及同tx事件 | 回滚/关闭竞争通过 |
| Day6 | T01～T26、旧版本真实并发、取消、Review及修复 | 全量通过 |
| Day7 | 新目录复现、六段TCP、复盘/回答/W04交接 | 工程完成，个人学习/Git另记 |

配套：[设计/ER](../ticket-design.md)、[API](../api/ticket.md)、[状态机](../ticket-state-machine.md)、[ADR](../decisions/0003-ticket-transaction.md)、[矩阵](../testing/ticket-test-matrix.md)、[请求](../../requests/week03.http)、[参考回答](week03-answers.md)、[演示](week03-demo.md)。

## 2. 冻结范围与设计差异

保留W02七个业务接口及健康检查，新增九个组织作用域工单接口，统一/api/v1/organizations/:orgID/tickets。不实现claim、support、前端、正式幂等键或公开部署。

| 旧蓝图/缺省项 | W03结果与理由 |
| --- | --- |
| member仅看本人 | 按本周手册，所有本组织active成员可读未删除工单/评论/事件，编辑仍按对象策略 |
| admin仅编辑本人 | 可编辑任意本组织OPEN并指派同组织active成员 |
| OPEN必须未分配 | 允许预指派；非OPEN必须有受理人，claim留W04 |
| priority例子方向相反 | 保留旧1低/2普通/3高，避免悄然反转语义 |
| 每版本唯一事件/评论不记事件 | 不设该唯一键；同PATCH两事件只加一次版本，评论事件共享父版本 |
| 评论/DELETE/解除指派 | CLOSED不可新增评论；DELETE无body但admin锁行增版；OPEN的assignee_id:null仅admin解除 |
| AGENTS.week03.append模板 | 仓库未找到，按手册直接追加，不伪称使用模板或覆盖秘密约束 |

以上为受托实现选择与代价，记录在计划/ADR，不冒充开发者逐项独立确认。同步当前需求/架构/数据库文档，原始手册及W1/W2历史记录保留。

## 3. 模块与数据库

[ticket/service.go](../../internal/ticket/service.go)显式管理Store.Begin/Transaction：同tx成员检查、父FOR UPDATE、对象权限/状态/version、SQL与事件、COMMIT。失败独立最多3秒Context回滚；普通查询3秒、事务整体5秒，父截止时间优先。

[ticket/repository.go](../../internal/ticket/repository.go)只执行参数化SQL：所有对象查询限定org及未删除父，更新含旧version/旧status。评论不加version但锁父工单。auth共享严格JSON增加可选字段/64KiB入口，W02仍原16KiB；组织模块共享pool/tx成员查询，未复制认证方式；main挂现有保护Group。

[00004](../../migrations/00004_create_tickets.sql)、[00005](../../migrations/00005_create_ticket_comments.sql)、[00006](../../migrations/00006_create_ticket_events.sql)落实工单/评论/事件。旧00001～3不变。真实验证默认值、非法priority/status/version、空白/超长、缺组织/成员、跨组织受理人及子表父/作者/操作者复合FK、事件类型/JSON对象CHECK、索引和EXPLAIN。

索引针对未删除工单组织/时间及组织/状态/时间、子列表组织/工单/时间；未加当前无查询用途的assignee索引，小数据EXPLAIN不当性能基准。仅在专用库3→6、重复Up、6→5→4→3再Up6，原组织哨兵保留，不在日常库实验。

## 4. 测试先行与真实问题

缺失能力红灯：[策略](evidence/week03-policy-red.log)、[Service](evidence/week03-service-red.log)、[HTTP](evidence/week03-http-red.log)、[Repository](evidence/week03-repository-red.log)。schema先在版本3运行明确报W03 schema missing，见[DB红灯](evidence/week03-schema-red.log)。实现后相应[策略](evidence/week03-policy-green.log)、[Service](evidence/week03-service-green.log)、[HTTP](evidence/week03-http-green.log)、[schema](evidence/week03-schema-green.log)通过，最终以全量回归为准。

| 类型/现象 | 根因与修复 | 证据 |
| --- | --- | --- |
| 业务：member启动未指派OPEN为403，契约409 | “本人受理人”判断早于有效受理人状态；先确认合法边的状态条件，保留角色403 | [首轮DB](evidence/week03-postgres-attempt1.log)、[最小红灯](evidence/week03-unassigned-red.log)，最终回归通过 |
| 审计：老事务晚写时间倒退/顺序异常 | NOW取事务开始；复现后应用SQL显式clock_timestamp，不改迁移默认或W02行为 | [真实红灯](evidence/week03-audit-time-red.log)、[最终绿灯](evidence/week03-clean-reproduction.log) |
| 验证夹具：超长标题误期望23514 | VARCHAR先返回22001，核对类型错误与CHECK区别 | [schema首次尝试](evidence/week03-schema-attempt1.log) |
| 验证夹具：DTO误计12字段 | 实际契约11字段，修正夹具，不添加无关响应字段 | [HTTP首次尝试](evidence/week03-http-attempt1.log) |

审计实验先BEGIN老tx，再让新tx完成写入，老tx随后更新/评论/事件，观察updated_at倒退。审查方提示时间边界但未判阻断；主代理根据真实用户可见复现决定修复，之后完整回归。该方案不承诺跨工单全局提交顺序或抵御时钟回拨。

所有问题由AI在本次工程验证中处置，不冒充开发者独立排障。没有降权限/删断言/用Skip替代通过，保留失败日志。

## 5. 最终验收与统计

| 检查 | 实际结果/证据 |
| --- | --- |
| 普通全量race | 8包，顶层65通过/0失败/8数据库入口跳过；[JSON事件](evidence/week03-go-test.jsonl) |
| 新目录真实库race | 8包，顶层73通过/0失败/0跳过；[最终日志](evidence/week03-clean-reproduction.log) |
| T01～T26 | 全部自动化，见[矩阵](../testing/ticket-test-matrix.md)与[业务集成](../../tests/integration/ticket_test.go)，schema独立测试 |
| 六段真实TCP | 创建/初始事件、编辑/指派、启动/评论/解决/关闭、跨组织、软删审计、事件失败回滚；同上 |
| W02回归 | 固定迁移3，七接口及Alice/Bob/过期演示通过；[日志](evidence/week03-week02-regression.log) |
| W1回归 | 固定迁移1，健康/信号退出/保留卷重启/连接失败通过；[日志](evidence/week03-week01-regression.log) |
| 工单JSON fuzz | 5s预算、2workers、13,117次通过；[日志](evidence/week03-ticket-fuzz.log) |
| 共用注册解码fuzz | 5s预算、2workers、19,275次通过；[日志](evidence/week03-auth-fuzz.log) |
| 静态/构建 | vet/build/mod verify/gofmt/diff通过；[质量日志](evidence/week03-quality.log) |

[统计摘要](evidence/week03-test-summary.json)区分顶层与子测试/fuzz seeds：普通373个通过节点/8跳过，真实库458通过节点/无跳过。单独-tags=integration也执行，但环境开关未启用的DB仍Skip，不能当作真实DB证据。race不能代替数据库事务实验。

额外覆盖：旧版本双请求一200一409且只一条修改事件；父锁等待取消无修改；关闭父事务后评论拒绝；disabled受理人拒绝、OPEN解除指派、最大version409；BEFORE/延迟COMMIT事件失败时创建/编辑/状态/评论/删除全部回滚；软删后的七个对象端点均404。重复POST无请求级幂等，未声称实现claim。

## 6. Review、复现与维护

[独立Review](../reviews/week03-review.md)静态核对源码/迁移/共用入口/SQL/测试，未发现阻断交付项，提示审计时间语义。主代理以红绿验证处理时间缺陷并执行全套，审查方未复审最终修复版本或运行测试。未执行漏洞扫描/渗透，不宣称生产安全认证。

最终在/var/tmp复制74个运行源码/示例配置/脚本/测试，不复制真实.env/.git/缓存，使用现有工具、Go缓存与本地postgres:17，不等于全新机器安装。见[快照清单](evidence/week03-reproduction-manifest.json)和[演示](week03-demo.md)。快照/容器/标记卷只清理本次归属，日常数据未访问。

项目根目录复现：

```bash
go test -race -count=1 ./...
go vet ./...
go build ./...
python3 scripts/week03-smoke.py
python3 scripts/week02-day2-smoke.py --closeout
python3 scripts/day5-smoke.py
```

本地最新API需迁移6，启动不自动迁移，十二环境变量由开发者维护。结构变化继续追加migration，契约/权限变化同步测试，保留历史证据。[源码清单](evidence/week03-source-baseline.json)、[交付检查](evidence/week03-delivery-check.log)标识本次未提交工作区，不是新发布。

## 7. AI参与、个人学习和Git

本周设计、迁移、业务逻辑、测试、复现、审查协调与文档由AI按用户整周授权完成。个人闭卷解释、独立实现/演示、Review阅读未代填。[20道参考回答](week03-answers.md)有十题导航、真实问题和排障，供后续自己理解org_id/父锁/version/同tx事件。未录个人演示视频。

当前HEAD仍da47b6c，W03未自动stage/commit/push；按功能检查Diff及未跟踪文件后由开发者保存，远程CI以实际执行为准。延续现工作区、不创建额外分支，保留用户的后续手册。选择与代价集中在计划/ADR，不悄悄扩大角色/API或宣称幂等/并发撤权全序保证。

## 8. W04交接

1. 增加POST /api/v1/organizations/:orgID/tickets/:ticketID/claim，限定active成员、同组织未删除OPEN且assignee为空，用条件更新/锁与影响行数保证至多一成功，不能先查后无条件改。
2. 同tx写领取及TICKET_ASSIGNED/状态事件，明确version/状态变化组合；真并发两成员一成功其余冲突，事件失败回滚领取，不产生重复事件。
3. 引入Vue3登录/列表/详情/处理页面，按W03当前可见范围和version契约联调；评估BIGINT超JS安全整数的编码，不能静默丢精度。
4. 如新增未分配/assignee筛选，基于真实SQL评估索引，不宣称当前索引覆盖未知查询；不提前引入Redis。
5. 保留W1/W2/W3隔离回归，support/角色状态变更如需增加必须增量迁移和授权测试，不能改00003。

可靠限流、HTTPS/凭据管理、邮箱所有权/弱密码、Token撤销/轮换、审计及漏洞评估仍是公开部署待办；无正式请求幂等或并发撤权完全线性化。工单/事件/权限稳定后再进入claim，不把前端、AI或上线能力写成本周完成项。

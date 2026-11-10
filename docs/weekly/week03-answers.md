# NexusDesk 第三周：问题与参考回答

2026-11-10，AI按整周委托结合当前实现整理，供复习，不代表开发者独立作答。原始[第三周手册](../../项目开发文件/每周开发手册/06_NexusDesk_第三周详细开发手册.md)中的十题映射如下。

| 手册自测题 | 对应答案 |
| --- | --- |
| 1 分层与事务 | Q01 |
| 2 URL组织与SQL范围 | Q02 |
| 3 复合键/外键 | Q03 |
| 4 CHECK与状态机 | Q04 |
| 5 同事务与回滚证明 | Q05 |
| 6 version乐观锁边界 | Q06 |
| 7 软删除过滤 | Q07 |
| 8 race能否证明DB正确 | Q08 |
| 9 RESOLVED重开 | Q11 |
| 10 W04抢单不能先查后改 | Q18 |

## Day1～2：设计与数据库

### Q01：Handler、Service、Repository怎样分工，事务在哪一层？

Handler做认证上下文、严格HTTP输入/DTO和错误映射；Service决定对象权限、状态、版本以及哪些动作必须原子完成；Repository只执行SQL。W03由Service调用Store.Begin取得Transaction，在同tx成员检查、父行锁、更新及事件，确认COMMIT后返回。它与W02专用原子Repository方法不同，是按跨多个工单动作的需求扩展，不是套通用框架。

### Q02：URL带orgID为什么还要查成员，并在每条SQL限定？

URL只是客户端选择的目标。Token验证当前用户，DB成员关系证明属于目标组织，SQL的org_id防止真实ticketID被换到另一组织访问。三个层次都需要；不能只用ticket_id取对象，再相信前端过滤。

本周同组织所有active成员可读工单/评论/事件，编辑仍检查创建者或admin。这个W03规则覆盖旧蓝图的member仅看本人，不能把旧文档当作新的访问限制。

### Q03：UNIQUE(org_id,id)与复合外键有什么帮助？

给父表提供可引用的同组织键，评论/事件的(org_id,ticket_id)必须指向同一张工单，作者/创建者/受理人/操作者也引用对应(org_id,user_id)成员关系。它阻止跨组织关联错误，但不证明成员仍active或动作有权限，后者要由Service查询。

### Q04：为什么CHECK和状态机都要有？

CHECK约束当前记录的合法形状：priority1..3、四个status、version非负、非OPEN必须有受理人。状态机判断从哪个状态到哪个状态、由谁操作、当前受理人是否active。合法字段组合并不意味着转换合法，例如OPEN直接CLOSED即使字段能过CHECK，业务仍409。

### Q05：怎样证明两次写入原子，而不只是HTTP成功？

在隔离库让ticket_events的BEFORE触发器失败或延迟到COMMIT失败，分别调用创建、PATCH、transition、评论和删除；检查工单/版本/评论/删除标记与事件数量都未变化。W03实际运行了这些实验，见[周报](week03.md)。不能把事件写到pool上脱离tx，也不能只检查500而不查数据库。

## Day3～4：查询、编辑与版本

### Q06：version、行锁、条件UPDATE分别解决什么？

客户端expected_version表达“基于哪个版本修改”，旧版本409，避免覆盖已知的新修改。FOR UPDATE使同工单写入在数据库中串行，拿锁后再判断当前状态与版本；UPDATE仍含org/id/旧version/旧status/deleted条件。测试两请求带同一旧版本，一200一409，仅多一版本和事件。

version不是请求幂等键，不自动阻止重复POST，也不解决跨工单规则、所有隔离异常或权限撤销竞争。评论不改version但锁父工单，与关闭/删除确定先后。

### Q07：软删除为何要过滤详情、评论和事件？

只从列表隐藏仍能通过已知ticketID访问子资源。普通父/子查询必须匹配org_id及父deleted_at IS NULL；软删后七个面向该父对象的端点都404，集合列表不含它，但DB保留工单、评论、事件。没有恢复/审计绕过API，重复删除404。

### Q08：race通过是否表示没有数据库并发问题？

race检查Go共享内存竞争，不能证明唯一约束、行锁、事务回滚或跨租户授权。需要真实PostgreSQL并发修改、锁等待取消、父关闭与评论、复合FK和事件失败实验。普通测试的数据库Skip不是Pass，standalone integration tag也不会自动开启本项目环境开关。

### Q09：PATCH为什么不能传status？为什么admin指派允许null？

status只能走/transition，否则会绕过源状态、受理人和动作权限。PATCH只改OPEN基础字段，member限创建者，admin可编辑任意OPEN和指派同组织active成员；null是明确解除OPEN指派，不是改角色。字段与指派都有实际变化时写两条事件，但version只加一次；无变化400。

### Q10：分页和优先级当前是什么约定？

默认1/20、单页最大100，无total；工单created_at/id降序，评论/事件升序。过滤与软删/组织范围在LIMIT前完成。重复、未知query、非法状态/priority、巨大偏移溢出400。priority沿用已有1低2普通3高，手册中的1高只是示例，已在当前契约明确。

## Day5：状态、评论和事件

### Q11：RESOLVED→IN_PROGRESS为什么只允许admin？

重开代表已解决事项需要继续处理，要明确授权和保留审计。当前只有active admin能重开，必须有同组织active受理人；写状态和版本同时追加TICKET_STATUS_CHANGED，before/after可追溯。CLOSED不能重开。启动/解决则允许active assignee本人或admin，关闭RESOLVED只允许admin。

### Q12：评论为什么不增加version，又为什么锁工单？

讨论内容不是工单基础字段或状态修改，不应让用户读到的expected_version因普通评论频繁过期。但评论必须与COMMENTED事件原子，并在父工单仍可评论时完成。锁同一父行后重查状态，能阻止一个等待中的评论在工单已关闭后提交。

### Q13：事件应保存什么，为什么不设每版本唯一？

保存安全结构化变化，如title/priority/assignee/status/version；body变更只标记变化，评论事件只记comment_id。不复制Token、密码、请求头、评论全文或数据库错误。一次PATCH可有UPDATED/ASSIGNED两事件，多个评论也共享工单版本，因此不能UNIQUE(org_id,ticket_id,ticket_version)。事件无单独修改/删除接口。

### Q14：为什么审计时间没有直接沿用NOW()？

NOW()代表事务开始；老事务晚拿锁后写入，可能把较新版本updated_at写成更早时间，迟插入的评论/事件也会排到先插入者前。真实实验先BEGIN旧事务，再让新事务完成写入，最后旧事务写入，已复现该问题。

应用SQL改为写入时clock_timestamp()，不改迁移默认值或W02行为。它解决这个事务开始时间回退问题，不提供跨工单全局提交顺序，也不抵御数据库主机时钟回拨。参见[PostgreSQL时间函数](https://www.postgresql.org/docs/17/functions-datetime.html)与[审查处置](../reviews/week03-review.md)。

## Day6～7：故障、验收和W04

### Q15：本周实际修复和验证问题有哪些？

| 类型 | 现象与原因 | 修正 |
| --- | --- | --- |
| 业务错误分类 | member启动未指派OPEN返回403；状态条件晚于角色判断 | 未指派/受理人失效先返回409，补失败单测再修复 |
| 数据库审计时间 | 老事务晚写导致updated_at/审计时间倒退 | 真实失败实验后使用显式写入取时并回归 |
| 测试期望问题 | VARCHAR超长应22001，夹具误期望CHECK的23514 | 区分错误类别，保留首次日志，不改数据库限制 |
| 测试期望问题 | 工单DTO实际11字段，断言误计12 | 按契约逐项核对，保留失败记录，不新增无关字段 |

这些由AI按委托实现、验证或处理审查意见，不能记录成开发者独立排障。证据链接均在week03。

### Q16：怎样排查403、404、409、500？

401先查Bearer/签名/Claim/用户存在；404查目标组织active关系、ticket所属组织及软删；403查创建者/admin/受理人动作；409查当前状态、assignee有效性及expected_version；500查静态存储错误和事务/事件失败。使用隔离虚构数据复现，不打印真实.env、Token或DSN，不能靠跳过鉴权/删断言处理。

### Q17：重复POST、取消和提交断线有什么边界？

当前没有Idempotency-Key持久化去重，重复创建可得到不同ID。取消会向查询和事务传播，独立最多3秒Context清理；已经开始的DB操作要等驱动取消完成，不能假定断开前台就没有副作用。提交时网络中断可能结果不确定，应用不会自动重试整个创建请求并宣称恰好一次。

### Q18：W04抢单为什么不能先SELECT未领取再UPDATE？

两名成员可能同时读到未领取，再分别覆盖处理人。W04要用数据库条件更新或锁，条件同时含org/id、未删除、OPEN、assignee为空及所需版本，确认恰好一条成功；同tx写分配/状态事件，真并发验证一成功其余冲突。W03的admin PATCH指派和状态机不等于已经实现/claim。

### Q19：已有成果以后怎样维护？

接口改动同步[API](../api/ticket.md)，状态/角色改动同步[规则](../ticket-state-machine.md)与测试，结构变化新增迁移；周报记实际问题/证据，不重写历史验收。提交前普通race/vet/build/diff，加脚本week03-smoke的真实库验证，并保留W02 --closeout/W1历史回归。源码清单标识未提交内容，不替代开发者Git保存或远程CI。

### Q20：完成代码与独立掌握有什么区别？

本周业务实现、测试和文档由AI按明确授权完成。个人仍需闭卷解释十题、亲自运行虚构数据流程，尤其指出每个tx与org_id过滤的位置。未录个人演示视频，未代填独立贡献或掌握结论，未自动commit/push/部署。参考材料见[week03](week03.md)、[演示](week03-demo.md)。

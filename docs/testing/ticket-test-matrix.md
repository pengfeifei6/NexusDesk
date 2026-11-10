# W03 测试矩阵

T01～T26取自第三周手册，T01～T26均已通过真实库自动化验证；最终状态与日志见[week03](../weekly/week03.md)。

| 编号 | 测试场景 | 核心预期 | 实现位置 |
| --- | --- | --- | --- |
| T01 | active 成员创建工单 | 201，OPEN，版本 0，creator 正确 | tests/integration/ticket_test.go |
| T02 | 创建请求伪造 `creator_id`/`org_id` | 无法以其他身份或组织创建 | tests/integration/ticket_test.go |
| T03 | 非成员向目标组织创建工单 | 拒绝，不新增数据 | tests/integration/ticket_test.go |
| T04 | 缺失/过期 JWT 访问工单 | 401 | tests/integration/ticket_test.go |
| T05 | 列表按 `org_id`、状态、优先级分页 | 无跨组织数据，排序稳定 | tests/integration/ticket_test.go |
| T06 | 跨组织读取工单详情 | 404，不泄漏对象信息 | tests/integration/ticket_test.go |
| T07 | 使用错误 `orgID` 与真实 `ticketID` | 404 | tests/integration/ticket_test.go |
| T08 | 创建时标题为空/优先级非法 | 400 或数据库校验失败，不能创建 | tests/integration/ticket_test.go |
| T09 | 创建事件失败 | 工单创建事务回滚 | tests/integration/ticket_test.go |
| T10 | 创建人编辑自己 OPEN 工单 | 成功，version 增加 | tests/integration/ticket_test.go |
| T11 | member 编辑别人 OPEN 工单 | 403 | tests/integration/ticket_test.go |
| T12 | member 为工单指派 assignee | 403 | tests/integration/ticket_test.go |
| T13 | admin 指派外组织或 disabled 用户 | 被拒绝 | tests/integration/ticket_test.go |
| T14 | 旧 `expected_version` 修改 | 409，数据不被覆盖 | tests/integration/ticket_test.go |
| T15 | OPEN 未分配 assignee 直接流转 | 拒绝 | tests/integration/ticket_test.go |
| T16 | 指派后 OPEN→IN_PROGRESS | 允许并记录事件 | tests/integration/ticket_test.go |
| T17 | IN_PROGRESS→RESOLVED→CLOSED | 符合角色策略时成功 | tests/integration/ticket_test.go |
| T18 | OPEN→CLOSED / CLOSED→OPEN | 拒绝 | tests/integration/ticket_test.go |
| T19 | RESOLVED→IN_PROGRESS | 仅 active admin 可重开 | tests/integration/ticket_test.go |
| T20 | 状态事件失败 | 状态与 version 均回滚 | tests/integration/ticket_test.go |
| T21 | 本组织 active 成员新增评论 | 201，评论和事件均存在 | tests/integration/ticket_test.go |
| T22 | 评论事件失败 | 评论新增回滚 | tests/integration/ticket_test.go |
| T23 | 非成员读取/新增他组织评论或事件 | 拒绝 | tests/integration/ticket_test.go |
| T24 | admin 软删除工单 | 204，普通查询不可见，事件留存 | tests/integration/ticket_test.go |
| T25 | member 删除 / 重复删除 | 拒绝或明确约定的不可见响应 | tests/integration/ticket_test.go |
| T26 | 数据库连接异常 | 安全错误，不暴露 SQL/DSN | tests/integration/ticket_test.go |

补充：schema真实约束/索引、严格JSON/Unicode/整数、所有事件失败及提交失败回滚、两个旧版本并发修改、软删全九接口、关闭/评论锁竞争、重复POST边界与旧W02/W1回归。纯状态机表驱动在state_machine_test.go，HTTP输入在handler_test.go，Service事务错误在service_test.go。

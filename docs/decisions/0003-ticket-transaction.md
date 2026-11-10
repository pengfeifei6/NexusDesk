# ADR-0003：W03 工单事务、版本与审计

2026-11-10，AI按整周委托作出实现选择，个人独立理解待开发者记录。

采用Service显式开始/提交Transaction小接口，Repository执行tx SQL；对比Repository封装整个业务或通用UnitOfWork，此方案让权限/状态/事件原子性集中可读，代价是需定义有限事务方法。

对已有工单先SELECT FOR UPDATE，再校验当前状态/版本，以org_id/id/version/status/deleted条件UPDATE；相比仅内存判断，这能在数据库串行化冲突，且保持乐观版本对客户端的409语义。创建工单及事件、编辑/指派/状态/删除及事件、评论及事件全部同tx。COMMIT后返回，独立有界Context回滚。

评论不加version但锁父工单，与关闭/删除串行；CLOSED不可评论。一次PATCH可生成UPDATED/ASSIGNED两事件但只加一次version。事件追加，无修改/删除API，结构化快照不包含密码/Token/请求头/数据库错误或评论全文。

软删除保留全部记录，所有普通父/子API隐藏该工单；不额外创建恢复/审计绕过接口。列表无total和跨请求快照保证。重复POST无Idempotency-Key去重。本周不实现/claim；W04必须条件更新+同tx事件+真实并发验证。

W03手册覆盖旧蓝图的组织可读范围、admin编辑和OPEN指派；priority保留旧1低2普通3高（手册仅举例），不引入support。状态/权限依据见[状态机](../ticket-state-machine.md)，验收见[矩阵](../testing/ticket-test-matrix.md)。

审查提出NOW()是事务开始时间的边界，审查方未将其判为阻断项；主代理用真实“老事务晚写”复现updated_at倒退与子记录排序倒置，决定修复。应用SQL在写入时显式clock_timestamp()，不改已有迁移默认值/旧W02行为，时间仍由数据库提供。测试证明迟到的写入不会因旧事务开始时间排到前面；这不承诺跨工单全局提交顺序或抵御主机时钟回拨。依据：[PostgreSQL时间函数](https://www.postgresql.org/docs/17/functions-datetime.html)。

# Day 5：可信身份与创建组织事务

- 日期：2026-11-10；AI 按开发者委托整理的实现说明，供复习，不代表个人已独立掌握。
- 代码：[认证中间件](../internal/auth/middleware.go)、[本人资料](../internal/auth/me.go)、[组织服务](../internal/organization/service.go)、[事务与列表 SQL](../internal/organization/repository.go)。

## 1. 请求的操作者从哪里来

客户端的 owner_id 或 user_id 只是外部输入，不能证明它属于当前操作者。中间件严格取唯一 Authorization，验证 HS256 Token 的签名、Claim 和时效，再查询该用户仍存在，才把 int64 用户 ID 放进 Gin Context。缺失/畸形/无效凭据或用户已删除为 401；数据库故障是 500，不能伪装成用户不存在。

组织 Handler 从 CurrentUserID 取操作者，仅接受 JSON name；前端额外传 owner_id、admin_user_id、user_id、role 或 status 都返回 400，不会写入。角色 admin 和状态 active 由后端固定。JWT 不携带组织权限。

本次接受 scheme 大小写变化，但 Bearer 与 Token 间固定一个 ASCII 空格，拒绝重复头、逗号、多凭据、空 Token 和多余空白。上下文不保存 Token、密码、密钥或用户整条记录。

## 2. 为什么两次插入必须用同一个事务

业务承诺是“创建可由当前用户管理的组织”。如果组织写入成功、管理员成员写入失败后仍保留第一条，就会出现没有管理员的孤立组织。

Repository 的实际顺序为：

1. 从请求 Context 派生最多 5 秒的事务 Context，BEGIN。
2. 用 tx.QueryRow 插入 organizations，获取数据库生成的 ID。
3. 用同一个 tx.Exec 插入 organization_members，固定 admin/active。
4. 两步成功后 COMMIT，确认成功再返回 201。
5. 任意失败返回安全错误，defer 用独立、最多 3 秒的清理 Context 执行 ROLLBACK；提交成功后的再次回滚是无害的结束状态。

不能在第二步改用 pool.Exec，否则它可能在另一条连接执行，脱离本事务。也不能把“写完两条 SQL”当作成功，COMMIT 本身仍可能失败。pgx 的事务 Context 不会自动代替显式回滚，说明见 [pgx Tx 文档](https://pkg.go.dev/github.com/jackc/pgx/v5#Tx)。

父 Context 已取消时，用它清理可能立即失败，因此回滚使用独立且有上限的 Context。回滚保证事务修改不生效，不保证 Identity 序号连续。网络在提交时断开可能造成结果不确定，当前没有幂等键，应用不会自动重试整个创建请求。

## 3. 本次如何验证回滚

仅在运行器创建的可丢弃数据库中安装临时测试触发器，不改变迁移或日常数据库：

- BEFORE INSERT 在成员写入时故意失败：Repository 无半成品返回，HTTP 为 500，按名称查询组织为零条。
- 延迟约束触发器直到 COMMIT 才失败：证明提交错误也不会返回 201，组织同样不存在。
- 锁住成员表，让第二次写入等待并取消请求：返回截止时间错误、没有遗留组织，随后连接仍可用。
- 使用不存在的创建者：真实外键失败使整个动作回滚，服务对创建者消失返回 401。

延迟约束触发器的机制见 [PostgreSQL 17 CREATE TRIGGER](https://www.postgresql.org/docs/17/sql-createtrigger.html)。触发器、测试数据和卷只存在于隔离测试项目；这些不是部署所需的数据库对象。

## 4. 本人组织列表为什么先过滤再分页

查询用可信 user_id 关联 organization_members 与 organizations，先限定 user_id 和 status='active'，再按组织 ID 升序 LIMIT/OFFSET。若先分页再过滤，他人或 disabled 的记录会占页槽，可能返回错误的空页，也可能泄露不应返回的组织。

page/page_size 默认 1/20，单页最多 100。重复/未知参数、非数字和乘法溢出都返回 400；合法但远超结果的页返回空数组。my_role 来自当前数据库成员关系，无 total。列表不自动认领 W1 旧组织，也不证明用户可以管理列表中的所有组织。

组织 disabled 只撤去对应组织的列表资格，不删除账号；原 Token 仍可访问 /me 和其他有效组织。Day 5当时未实现的成员列表、添加成员与跨组织404/角色不足403现已随Day 6完成，详见[成员权限说明](member-authorization-guide.md)。

运行方式与真实结果见 [week02](weekly/week02.md)，虚构输入见 [请求示例](../requests/week02.http)。

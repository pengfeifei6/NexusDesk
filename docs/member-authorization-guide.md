# Day 6：成员身份、组织角色与权限验证

- 日期：2026-11-10；AI按开发者委托整理，供后续复习，个人独立作答待开发者记录。
- 实现：[策略](../internal/organization/policy.go)、[成员Service](../internal/organization/member_service.go)、[成员Handler](../internal/organization/member_handler.go)、[成员SQL](../internal/organization/member_repository.go)。

## 1. 权限不是用户的全局属性

users描述账号，organization_members描述账号在某组织中的关系。Bob可以在A组织是member，在B组织是admin。登录Token只确认Bob是谁，不声明他能管理所有组织。

| 当前请求者在目标组织的状态 | 读取成员 | 添加已注册成员 |
| --- | --- | --- |
| 无有效账号凭据 | 401 | 401 |
| 非成员 / disabled / 不存在组织 | 404 NOT_FOUND | 404 NOT_FOUND |
| active member | 200 | 403 FORBIDDEN |
| active admin | 200 | 允许，成功201 |

这是 [权限矩阵](authorization-matrix.md) 的可执行实现，不接受前端或JWT携带的角色替代数据库事实。

## 2. 为什么先授权，再判断目标用户

请求按认证、orgID格式、当前active关系、操作角色、业务输入、SQL的顺序执行。如果先检查目标用户是否存在，非成员或普通member就能通过不同错误探测全局账号。

因此无论请求体是合法JSON、非法JSON还是指向不存在用户，非成员都返回同一个404，active member添加都返回403。只有目标组织admin才能看到USER_NOT_FOUND。

Handler先调用Authorize保护上述错误顺序，Service的List/Add入口再次授权，避免绕过Handler直接调用Service就跳过权限。合法请求多一次关系查询。这不保证撤权操作与正在执行的请求完全线性化；本周没有角色修改/停用接口，未来增加时需要明确事务与锁定语义。

## 3. 操作者与目标user_id的区别

操作者ID只取自RequireUser写入的可信Context；URL中的orgID选择目标组织；JSON里的user_id是将要加入的现有用户。这三个值用途不同。

添加仅允许user_id和role。user_id必须是1..9223372036854775807的JSON整数，拒绝字符串、小数、指数、null及溢出；role只能是精确小写member或admin。org_id、actor_id、owner_id和status等额外字段会被拒绝，不能用它们更换组织或操作者。status由后端固定active。

数字与字符串字段共用原有16KiB严格对象解码：拒绝未知/重复字段（包括转义后重复的名称）、非法UTF-8、未配对Unicode转义和尾随JSON，不复制另一套宽松解析器。注册与登录回归继续通过。

## 4. 查询为什么始终限定组织

FindActiveMembership同时限定org_id、当前user_id和active状态；ListMembers只取已授权org_id中的关系，按user_id升序分页，允许看到该组织中的active/disabled成员。

响应只含user_id、display_name、role、status、created_at。邮箱、密码散列、组织外用户和独立member_id都不返回。空列表为[]，时间格式为UTC RFC3339，分页沿用默认1/20、最大100和偏移溢出保护。

## 5. 重复关系与事务

AddMember在同一个事务中读取目标显示名、插入关系、读取插入结果并提交；提交成功后才返回成员DTO。任一步失败均回滚，错误不能返回虚假的201。普通查询最多3秒，添加事务最多5秒，回滚用独立最多3秒的清理Context。

成员联合主键是并发保护的最终依据；两个admin同时添加，真实测试结果一个201、一个409，仅一条关系。已有disabled关系同样409，不能用UPSERT隐式恢复状态或改角色。添加admin本人也返回409。

只有SQLSTATE 23505且约束名为organization_members_pkey才映射成员重复；指定用户/组织外键分别映射目标不存在或资源不存在，其他异常保持安全500。依据见 [PostgreSQL错误码说明](https://www.postgresql.org/docs/17/errcodes-appendix.html)，不通过匹配数据库错误文本判断业务错误。

## 6. 旧Token为什么无法绕过disabled

测试先签发Token，再直接在专用测试库把一条关系置为disabled，使用原Token再次读/写该组织均404。同一个用户在另一个组织的有效成员资格继续工作。角色从admin改为member后，原Token写操作403，读操作仍200。

这些UPDATE仅为隔离测试夹具，本次没有新增状态/角色修改API。签名确认身份与每请求查询成员关系共同构成授权路径，Token过期、删除用户和组织禁用是不同层面的规则。

真实库还覆盖提交时触发器失败、等待中取消、数据库关闭和安全错误；触发器只存在于可丢弃测试库，不加入迁移。运行 `python3 scripts/week02-day2-smoke.py --members` 可复现，结果见 [week02](weekly/week02.md)，请求见 [week02.http](../requests/week02.http)。

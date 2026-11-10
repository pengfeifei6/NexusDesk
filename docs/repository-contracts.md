# W02 Repository 契约与事务设计

- 日期：2026-11-10；阶段：Day 2。
- 状态：Day 6已实现三个用户方法与五个组织/成员方法，真实库验证通过。
- 编写方式：AI 按开发者委托整理；已实现方法与事务归属如下，实际验证见week02。
- 依据：[API 契约](api/auth-organization.md)、[权限矩阵](authorization-matrix.md)、[数据库](database.md)、[认证设计](auth-design.md)。

## 1. 职责与依赖

Day 3 用户存储已落在 [internal/auth/repository.go](../internal/auth/repository.go)，复用既有 pgxpool；组织创建与本人列表已在 [internal/organization/repository.go](../internal/organization/repository.go) 实现，成员三方法落在 [member_repository.go](../internal/organization/member_repository.go)。

Handler 解析 HTTP；Service 规范化邮箱、校验密码与名称、验证当前身份和组织权限；Repository 执行参数化 SQL并分类存储结果。Repository 返回领域数据，不返回 Gin Context、HTTP 状态码、SQL 文本或 DSN。

每个方法第一个参数为标准 `context.Context`，调用链传递请求 Context。普通 SQL 初始上限 3 秒，组织创建事务整体上限 5 秒，父 Context 更早结束时服从父级；这些上限已在用户/组织方法实现，不是实测性能保证。检查取消与截止时间，及时关闭 rows，遍历后检查 rows.Err。

## 2. 拟用数据类型

用户类型已在 auth/model.go 实现；组织/分页类型已在Day 5实现，Membership/Member已在Day 6实现。DTO 与持久化记录分开，不能直接把含散列的记录交给 JSON 编码。

```go
// internal/auth/model.go — Day 3 已实现
type User struct {
    ID int64
    Email string
    DisplayName string
    CreatedAt time.Time
    UpdatedAt time.Time
}
type Credential struct {
    User User
    PasswordHash string // 实现时使用 json:"-"，也不得直接作为响应
}
type CreateUserParams struct {
    Email string       // Service 已规范化
    DisplayName string // Service 已去首尾空白
    PasswordHash string // 已生成的 Argon2id 编码，绝非明文
}
```

```go
// internal/organization/model.go — Day 6 以下类型均已实现
type Role string // 只允许 "admin"、"member"
type MembershipStatus string // 只允许 "active"、"disabled"
type PageQuery struct {
    Limit int32 // 1..100
    Offset int64 // >=0；页码转换已做溢出检查
}
type Organization struct {
    ID int64
    Name string
    CreatedAt time.Time
}
type OrganizationView struct {
    Organization Organization
    MyRole Role
}
type Membership struct {
    OrgID int64
    UserID int64
    Role Role
    Status MembershipStatus
    CreatedAt time.Time
}
type Member struct {
    Membership Membership
    DisplayName string
}
```

HTTP DTO 根据 Day 1 契约挑选字段，例如成员不返回邮箱和散列，组织响应不必暴露所有存储字段。列表空结果返回非 nil 的空切片，Handler 编码成 `[]`。

## 3. 用户 Repository 三个方法（Day 3 已实现）

以下方法属于已实现的 auth.PostgresRepository；真实注册集成覆盖写入、查询、唯一冲突、在途取消与数据库失败，见 [第二周记录](weekly/week02.md)。

```go
func (r *PostgresRepository) CreateUser(ctx context.Context, p CreateUserParams) (User, error)
func (r *PostgresRepository) FindUserByEmail(ctx context.Context, normalizedEmail string) (Credential, error)
func (r *PostgresRepository) FindUserByID(ctx context.Context, userID int64) (User, error)
```

| 方法 | 输入与查询范围 | 返回与错误 |
| --- | --- | --- |
| CreateUser | INSERT users 的 email/display_name/password_hash；不接收角色或 org_id；ID 和时间由数据库默认生成 | RETURNING 公开用户字段；只有 SQLSTATE 23505 且 ConstraintName=users_email_ci_unique 映射 ErrEmailExists；其他异常安全分类 |
| FindUserByEmail | 邮箱由 Service 去首尾空白、小写化；WHERE lower(email)=lower($1)，参数化精确匹配，不用 LIKE | 找到返回 User 与 PasswordHash 供登录验证；pgx.ErrNoRows → ErrNotFound；损坏散列留给 Password 封装处理 |
| FindUserByID | 正 userID；WHERE id=$1，只查询需要的公开字段，不取 password_hash | User；未找到 ErrNotFound，认证层将已消失账号映射为 401 |

CreateUser 不先依赖“查询不存在”作为并发保护，不写组织或成员表；正常注册仅一条写入，不需要跨表事务。FindUserByEmail 是本地密码验证能力，不提供全局用户搜索接口。

## 4. 组织 Repository 五个方法

以下方法属于 `organization.PostgresRepository`。Day 5实现前两个方法，Day 6实现后三个方法；同名接收者分别位于两个包，不是一个通用 Repository。

```go
func (r *PostgresRepository) CreateOrganizationWithAdmin(ctx context.Context, creatorID int64, name string) (OrganizationView, error)
func (r *PostgresRepository) ListOrganizationsByUser(ctx context.Context, currentUserID int64, page PageQuery) ([]OrganizationView, error)
func (r *PostgresRepository) FindActiveMembership(ctx context.Context, orgID, userID int64) (Membership, error)
func (r *PostgresRepository) ListMembers(ctx context.Context, orgID int64, page PageQuery) ([]Member, error)
func (r *PostgresRepository) AddMember(ctx context.Context, orgID, targetUserID int64, role Role) (Member, error)
```

| 方法 | 前提、SQL 范围和事务 | 返回与错误 |
| --- | --- | --- |
| CreateOrganizationWithAdmin | creatorID 来自认证上下文；name 已经 Service 校验；方法内部开始一个事务，INSERT organizations，再用同一 tx INSERT admin/active 成员，最后 COMMIT | 只在提交成功后返回 OrganizationView；创建者 FK 失效 → ErrUserNotFound；失败回滚，不返回半成品 |
| ListOrganizationsByUser | currentUserID 来自认证；JOIN organizations / organization_members；WHERE user_id=$1 AND status='active'；过滤后按组织 id ASC，LIMIT/OFFSET | 本人组织和 MyRole；无成员返回空列表，不返回 ErrNotFound |
| FindActiveMembership | WHERE org_id=$1 AND user_id=$2 AND status='active'；匹配后再按 role 决定动作 | Membership；不存在和 disabled 都 ErrNotFound，不区分对外原因 |
| ListMembers | Service 已通过 FindActiveMembership；WHERE m.org_id=$1，JOIN users 只取显示名；包含 active/disabled，按 user_id ASC 分页 | Member 列表；SQL 始终限定 org_id；方法本身不替代成员授权 |
| AddMember | Service 已证明操作者为目标组织 active admin，且 role 合法；本方法参数中的 user_id 是目标用户；只 INSERT 新关系，不做 UPSERT | 返回插入关系与显示名；23505 + organization_members_pkey → ErrMemberExists；用户 FK 不存在 → ErrUserNotFound；组织 FK 不存在 → ErrNotFound |

AddMember 的角色只允许 admin/member，status 由后端固定 active。已存在 disabled 关系也由联合主键拒绝，不修改其角色或状态。Service 先授权，再判断目标用户是否存在，防止非成员或 member 通过错误探测用户。

Day 6的AddMember采用同一事务读取目标显示名、INSERT/RETURNING成员关系，再COMMIT后返回；独立最多3秒Context回滚，事务整体最多5秒。提交失败与取消均验证不遗留关系，不引入通用事务管理器。Service每请求查当前关系，Handler先授权以保护错误顺序，Service公开入口再授权；合法HTTP有两次关系查询，不承诺并发撤权线性化。

## 5. 事务归属与失败语义

Service 决定哪个业务动作必须原子执行。本周 `CreateOrganizationWithAdmin` 是一个专用原子存储方法，Repository 内部持有同一个 pgx.Tx，Service 不再嵌套开启第二个事务。以后工单跨多个 Repository 的事务可由 Service 显式传递共同执行器，届时再增加小接口。

创建组织的约定顺序：

1. 从请求 Context 派生事务上限，BEGIN；尚未成功创建事务则直接返回安全错误。
2. 插入组织，获得 ID；同一个 tx 插入 creatorID 的 admin/active 关系。
3. 任一错误返回前 ROLLBACK，清理使用独立且最多 3 秒的 Context，避免父 Context 已取消导致清理失效；不隐藏原始业务失败类别。
4. 两步成功后 COMMIT；提交报错不能返回 201，不自动重试整个创建请求。

BEGIN 开始事务，COMMIT 使其中修改共同生效，ROLLBACK 撤销未提交的事务修改。回滚不保证 Identity 序号连续。提交时连接中断有可能使客户端无法确定结果，本周不承诺请求恰好执行一次，幂等键后续设计。

Day 2 的数据库测试证明迁移约束和数据库并发唯一性；它还没有调用这个未来方法，因此不能把 A10/A11 的 Service 事务测试标成通过。

## 6. 错误分类合同

| 层内类别 | 后续 Go 约定 | Service / HTTP 映射 |
| --- | --- | --- |
| 无记录或不可见成员 | 对应包 ErrNotFound，errors.Is 可识别 | 登录用通用失败；保护接口用户失效为 401；组织不可见为 404 |
| 邮箱唯一冲突 | auth.ErrEmailExists，只匹配指定索引 | 409 EMAIL_ALREADY_EXISTS |
| 成员唯一冲突 | organization.ErrMemberExists，只匹配指定联合主键 | 409 MEMBER_ALREADY_EXISTS |
| 目标用户已不存在 | organization.ErrUserNotFound，匹配 user_id 外键 | 仅已授权 admin 返回 404 USER_NOT_FOUND；创建者身份失效由 Service 返回 401 |
| 调用参数明显错误 | 对应包 ErrInvalidArgument；正 ID、合法 role、分页范围等 | Service 对客户端做 400；内部调用错误不得伪装成成功 |
| Context 取消 / 超时 | 保留 errors.Is(context.Canceled / DeadlineExceeded) | 调用链停止；若客户端仍连接，按已有错误策略安全返回，不输出底层信息 |
| 其他数据库或提交异常 | 对应包 ErrStorage；对外 Error 文本为固定摘要 | 500 INTERNAL_ERROR，不返回 SQL、参数、DSN、密码和散列 |

23502（非空）、23514（CHECK）、22001（字段超长）表示校验遗漏或内部数据问题，不一律当成重复。23503 需要结合命名外键分类：`organization_members_user_id_fkey` 与 `organization_members_org_id_fkey`，未知约束走安全内部错误。

日志如需诊断只保留操作名、SQLSTATE 和约束名；不能原样打印 pgconn.PgError，它的 Detail 可能包含邮箱或散列。真实密码和 Token 不属于 Repository 参数；注册传入的只能是编码散列。

## 7. 后续实现的验收清单

- 用户存储：规范化邮箱查询、指定唯一索引冲突、缺失用户、Context 取消与失败脱敏；查 ID 不返回散列。
- 组织存储：本人 active 组织过滤、成员读取限定 org_id、空列表、稳定分页、重复与 disabled 关系不被覆盖。
- 原子操作：创建组织第二步故意失败时无新组织；提交失败不报成功；添加成员返回数据失败不能留下已提交关系。
- 授权：Service 在存储调用前检查身份和成员；在未来加入角色/状态更新时重新设计并发撤权，不把本周静态角色模型扩大为完整保证。

数据库唯一索引与外键原理参见 [PostgreSQL 17 表达式索引](https://www.postgresql.org/docs/17/indexes-expressional.html) 和 [约束](https://www.postgresql.org/docs/17/ddl-constraints.html)。用户三方法已随 Day 3 实现并审查，组织方法仍待后续，不把用户存储通过等同于组织授权通过。

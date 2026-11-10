# NexusDesk W02 授权与错误矩阵

- 日期：2026-11-10；状态：Day 1 已冻结本周设计，代码待实现。
- 依据：[认证设计](auth-design.md)、[API 契约](api/auth-organization.md)、[第二周手册](../项目开发文件/每周开发手册/05_NexusDesk_第二周详细开发手册.md)。
- 编写方式：AI 受托补齐。本周只存在 admin/member、active/disabled；后续 support 与角色变更不在本矩阵范围。

## 1. 按请求者身份的动作矩阵

下表中的“有效 Token”还要求对应用户存在；数据库异常应返回内部错误，不能当作用户或成员不存在。

| 动作 | 匿名或无效 Token | 已登录但非目标组织成员 | disabled 成员 | active member | active admin |
| --- | --- | --- | --- | --- | --- |
| GET /healthz | 200 | 200 | 200 | 200 | 200 |
| POST /api/v1/auth/register | 公开接口 | 公开接口 | 公开接口 | 公开接口 | 公开接口 |
| POST /api/v1/auth/login | 公开接口 | 公开接口 | 公开接口 | 公开接口 | 公开接口 |
| GET /api/v1/me | 401 | 200 | 200 | 200 | 200 |
| POST /api/v1/organizations | 401 | 允许 | 允许创建新组织 | 允许 | 允许 |
| GET /api/v1/organizations | 401 | 200，仅本人 active 组织 | 不包含该组织 | 包含该组织 | 包含该组织 |
| GET /api/v1/organizations/:orgID/members | 401 | 404 | 404 | 200 | 200 |
| POST /api/v1/organizations/:orgID/members | 401 | 404 | 404 | 403 | 允许，成功 201 |
| 修改角色、停用、删除、邀请 | 无接口 | 无接口 | 无接口 | 无接口 | 无接口 |

公开接口不以 Authorization 为身份依据，也不因附带 Token 而切换账号；受保护接口不能漏挂认证。成员停用仅影响对应组织，不禁止账号访问 `/me` 或创建新的组织。

## 2. 服务端校验顺序

1. 受保护接口先认证：缺失、畸形、篡改、过期凭据 → 401；不得先查组织或目标用户。
2. 解析 orgID：正十进制 int64，不合法 → 400；无组织路径的接口跳过此步。
3. 查询 `(org_id, 当前用户 ID)` 的成员关系并确认 active。未找到、组织不存在或 disabled → 同样的 404 `NOT_FOUND`；不向外披露具体原因。
4. 检查动作权限。添加成员仅 admin 可用，active member → 403 `FORBIDDEN`，不查询目标用户是否存在。
5. 再校验动作请求体、分页参数和业务对象，执行参数化 SQL。目标用户不存在 → 404 `USER_NOT_FOUND`，仅已经通过第 4 步的 admin 能看到该信息。
6. 重复邮箱或成员 → 409；数据库异常或提交失败 → 500，不能把内部错误转成“未找到”后继续执行。

资源大小保护可在入口先执行；已通过资源上限的请求，按上述顺序检查。路径不合法的 400 只说明路径格式，不说明组织是否存在。

## 3. 状态码与错误码

| HTTP | 错误码 | 适用情形 |
| --- | --- | --- |
| 400 | INVALID_REQUEST | JSON、未知字段、ID、分页或必填字段不合法 |
| 400 | INVALID_EMAIL / INVALID_PASSWORD | 已解析的注册或登录输入不符合指定格式或边界 |
| 401 | UNAUTHENTICATED | 受保护接口凭据不被接受，或凭据用户已不存在 |
| 401 | INVALID_CREDENTIALS | 登录邮箱不存在或密码不匹配，固定相同消息 |
| 403 | FORBIDDEN | 有效成员尝试 admin 动作 |
| 404 | NOT_FOUND | 目标组织不存在、非成员、disabled，统一消息 |
| 404 | USER_NOT_FOUND | 已授权的 admin 添加不存在用户 |
| 409 | EMAIL_ALREADY_EXISTS | 邮箱大小写不敏感唯一冲突 |
| 409 | MEMBER_ALREADY_EXISTS | 同组织已有关系，包含 disabled，不更新它 |
| 413 | REQUEST_TOO_LARGE | 请求体超过 16 KiB |
| 415 | UNSUPPORTED_MEDIA_TYPE | JSON 写接口不使用 application/json |
| 503 | AUTH_BUSY | 密码计算并发名额已满 |
| 500 | INTERNAL_ERROR | 查询、事务或其他未预期内部失败，响应脱敏 |

429 预留给未来频率限制，本周未实现，不把 AUTH_BUSY 伪装成“登录限流已完成”。错误 JSON 的格式和固定消息以 API 契约为准。

## 4. 必须验证的越权场景

| 手册编号 | 场景与期望 |
| --- | --- |
| A07～A09 | 缺失、过期、错误签名、算法/issuer/audience 错误均不能访问受保护接口，返回 401 |
| A10～A11 | 创建者从可信身份取得；组织和首名管理员共同提交，任一步失败回滚 |
| A12 | Bob 仅属于 B，查询或添加 A 的成员均为统一 404；A 不存在时响应相同 |
| A13 | Bob 加入 A 为 member 后，读取成功，添加成员仍为 403 |
| A14～A15 | Alice 在 A 为 admin，可添加已注册用户；重复与并发重复添加不更新旧关系，冲突返回 409 |
| A16 | 夹具禁用 Bob 在 A 的关系，原 Token 访问 A 为 404，本人组织列表不含 A；/me 仍成功 |
| A17 | 给创建组织请求伪造 owner_id、admin_user_id、user_id，返回 400 且不写数据；给添加成员请求伪造操作者或 status 同样拒绝 |
| A18 | DB 错误返回安全内部错误，不泄露 SQL、DSN、密码或 Token |

伪造 ID 的两个含义要区分：添加成员的 `user_id` 是管理员选中的目标用户，属于合法输入；操作者身份永远不能通过该字段指定。

成员列表可列出本组织 active 与 disabled 关系，字段为 user_id、display_name、role、status、created_at，不返回邮箱、密码散列或认证信息。组织列表只返回本人 active 关系，不通过分页前的未过滤结果泄露其他组织。

默认拒绝、逐次请求检查权限的原则参考 [OWASP 授权指南](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)。本周无成员角色/状态变更接口；未来新增时还须处理并发撤权，不能只加一个路由跳过事务设计。

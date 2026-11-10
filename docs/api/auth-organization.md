# NexusDesk W02 身份与组织 API 契约

- 日期：2026-11-10；版本：v0.1；状态：Day 1 冻结设计，尚未实现。
- 本周业务接口：下面列出的 7 个；统一前缀 `/api/v1`。原有 `GET /healthz` 保持不变。
- 编写方式：AI 根据开发者委托补齐手册中的输入、响应、错误和边界。
- 配套：[认证设计](../auth-design.md)、[权限矩阵](../authorization-matrix.md)、[数据库设计](../database.md)。

## 1. 公共约定

写接口使用 `Content-Type: application/json`，允许 charset 参数；只接收一个 JSON 对象，拒绝未知字段、尾随第二个 JSON 值、类型错误和缺失必填项。请求体最多 16 KiB，超过返回 413。GET 无请求体，列表只接受 page/page_size 两个可选查询参数，其他参数返回 400。

所有 API 响应为 JSON，带 `Cache-Control: no-store`。成功统一 `{"data":...}`；错误统一以下结构，request_id 当前允许空字符串，不为此增加观测平台。

```json
{"error":{"code":"FORBIDDEN","message":"permission denied","request_id":""}}
```

响应 ID 为 JSON 整数；请求 user_id 必须是 JSON 整数，路径 orgID 必须是十进制数字字符串，两者均在 1～9223372036854775807 内，拒绝浮点、负数、0 和溢出。组织和用户不是同一种 ID，不能混用；当前没有独立 member_id，成员由 `(org_id,user_id)` 标识。未来接前端时须再次评估超过 JavaScript 安全整数的编码方式，本周不改变 BIGINT 方案。

名称按有效 UTF-8 的 Unicode 码点计数，字节上限另有说明；时间字符串使用 UTC RFC3339。示例账号、ID、时间和 `<ACCESS_TOKEN>` 均为虚构或占位内容，不是运行结果或可用凭据。

### 1.1 身份与错误顺序

受保护接口只从 `Authorization: Bearer <ACCESS_TOKEN>` 获取凭据，不从请求体取操作者。scheme 大小写不敏感，只允许一个非空 Token；缺失、畸形和重复 Authorization 返回 401。受保护接口的 401 响应带 `WWW-Authenticate: Bearer`，不暴露验签细节。

除入口请求体大小保护外，校验顺序为：认证及用户存在性 → orgID 格式 → active 成员关系 → 操作角色 → 业务输入与目标用户 → SQL / 事务。公开注册和登录不依赖 Authorization。具体分支见 [权限矩阵](../authorization-matrix.md)。

### 1.2 字段规则

| 字段 | 契约 |
| --- | --- |
| email | 去首尾空白后 3～254 ASCII 字节；使用普通 local@domain 地址，不含显示名、列表、内部空白或国际化字符；以 net/mail 解析且解析后的 Address 必须等于原输入；ASCII 小写存储与查找；不改写点号或 +tag |
| display_name | 去首尾空白后 1～80 个 Unicode 码点 |
| password | 有效 UTF-8，15～128 个 Unicode 码点，最多 512 UTF-8 字节，非全空白；保持原文，不裁剪或截断；注册与登录边界相同 |
| name | 去首尾空白后 1～100 个 Unicode 码点；组织名不要求全局唯一 |
| role | 精确小写 member 或 admin，添加成员时必填；不接受 support 和大小写别名 |
| status | 服务端设置 active；本周任何写接口均不接收此字段 |
| page / page_size | 正十进制整数，默认 1 / 20；page_size 最大 100；不接受重复参数、非数字、溢出，分页偏移计算也必须检查溢出 |

### 1.3 错误字典

| HTTP | code | 固定 message |
| --- | --- | --- |
| 400 | INVALID_REQUEST | invalid request |
| 400 | INVALID_EMAIL | invalid email |
| 400 | INVALID_PASSWORD | invalid password |
| 401 | UNAUTHENTICATED | authentication required |
| 401 | INVALID_CREDENTIALS | invalid email or password |
| 403 | FORBIDDEN | permission denied |
| 404 | NOT_FOUND | resource not found |
| 404 | USER_NOT_FOUND | user not found |
| 409 | EMAIL_ALREADY_EXISTS | email already registered |
| 409 | MEMBER_ALREADY_EXISTS | membership already exists |
| 413 | REQUEST_TOO_LARGE | request body too large |
| 415 | UNSUPPORTED_MEDIA_TYPE | application/json required |
| 503 | AUTH_BUSY | authentication service busy |
| 500 | INTERNAL_ERROR | internal server error |

格式或必填错误优先 INVALID_REQUEST；字段已解析但邮箱格式不符或密码长度不符，分别用 INVALID_EMAIL / INVALID_PASSWORD。登录缺失或空 password 为 INVALID_REQUEST；已解析的非空密码超出边界为 INVALID_PASSWORD。已满足输入规则但邮箱不存在或密码错误统一 INVALID_CREDENTIALS。

所有数据库错误均先分类；只将已识别的邮箱/成员唯一冲突映射为 409。其他失败不得假装成功或泄露原始错误。未来限流可使用 429，本周尚无频率限制，不在七个接口中宣称已经实现。

## 2. 七个接口总表

| 方法 | 路径 | 条件 | 成功 |
| --- | --- | --- | --- |
| POST | /api/v1/auth/register | 匿名 | 201 |
| POST | /api/v1/auth/login | 匿名 | 200 |
| GET | /api/v1/me | 有效用户身份 | 200 |
| POST | /api/v1/organizations | 有效用户身份 | 201 |
| GET | /api/v1/organizations | 有效用户身份 | 200 |
| GET | /api/v1/organizations/:orgID/members | 当前组织 active 成员 | 200 |
| POST | /api/v1/organizations/:orgID/members | 当前组织 active admin | 201 |

## 3. 注册

`POST /api/v1/auth/register`，请求体仅允许 email、display_name、password。

```json
{"email":"Alice@example.test","display_name":"Alice","password":"ExampleOnly-ChooseYourOwnLongPassword"}
```

成功 201：

```json
{"data":{"id":101,"email":"alice@example.test","display_name":"Alice"}}
```

只新增 users，不产生组织、成员或 Token。持久化密码散列，不返回或记录 password/password_hash。邮箱唯一性以数据库 `users_email_ci_unique` 为最终依据，大小写变化和并发重复不能创建两个用户。

失败：400 输入/邮箱/密码不合法；409 EMAIL_ALREADY_EXISTS；413 / 415 协议问题；503 AUTH_BUSY；500 散列、数据库等内部错误。注册的 409 会暴露邮箱已注册这一事实，为本地演示的已知取舍，不声称具有完整账号枚举防护。

## 4. 登录

`POST /api/v1/auth/login`，仅允许 email、password。

```json
{"email":"alice@example.test","password":"ExampleOnly-ChooseYourOwnLongPassword"}
```

成功 200：

```json
{"data":{"access_token":"<ACCESS_TOKEN>","token_type":"Bearer","expires_in":900}}
```

邮箱规范化与注册一致。验证密码后才签发 Token；不因成功登录而自动加入组织。Token 固定 HS256、15 分钟，必需 sub/iss/aud/iat/exp，无组织或角色；完整规则见 [认证设计](../auth-design.md)。

失败：400 非法或缺失输入；401 INVALID_CREDENTIALS（邮箱不存在与密码错误完全相同）；413 / 415；503 AUTH_BUSY；500 查询、散列编码或签发错误。不存在账号时做同成本 dummy 验证，不能声称时序绝对相同。

## 5. 当前用户

`GET /api/v1/me`，必须通过认证，不接收 user_id 查询参数或请求体身份。

成功 200：

```json
{"data":{"id":101,"email":"alice@example.test","display_name":"Alice"}}
```

仅返回当前用户公开 DTO，不包含其他用户、散列和组织角色。用户不存在或 Token 无效 → 401 UNAUTHENTICATED；查询异常 → 500。用户没有组织或在某组织 disabled，不影响其访问本人资料。

## 6. 创建组织

`POST /api/v1/organizations`，仅允许 name。

```json
{"name":"Engineering Support"}
```

成功 201：

```json
{"data":{"id":201,"name":"Engineering Support","my_role":"admin"}}
```

同一事务插入组织和 `(org_id, 当前用户 ID, admin, active)` 成员关系，确认 COMMIT 成功后才返回。名称允许重复，用户可以创建多个组织；重复发送会创建不同组织，本周没有幂等键，不对失败请求自动重试。

未知的 owner_id、admin_user_id、user_id、role、status 都返回 400，并且不写入。失败：400；401；413 / 415；500。第二步写入或提交失败必须回滚，不留下孤立组织。

## 7. 本人组织列表

`GET /api/v1/organizations?page=1&page_size=20`，必须认证，无请求体。

成功 200：

```json
{"data":{"items":[{"id":201,"name":"Engineering Support","my_role":"admin"}],"page":1,"page_size":20}}
```

只查询当前用户 status=active 的成员关系并关联组织，先过滤再分页；按组织 id 升序。无结果时 items 为 `[]`，不返回 null；本周不提供 total。每页是单次查询的结果，不承诺跨请求快照一致。

不接受 user_id、org_id 等指定别人的筛选条件，未知参数 → 400。其他失败：401、500。disabled 关系不出现在本人列表，即使 Token 仍有效。

## 8. 组织成员列表

`GET /api/v1/organizations/:orgID/members?page=1&page_size=20`。

成功 200：

```json
{"data":{"items":[{"user_id":101,"display_name":"Alice","role":"admin","status":"active","created_at":"2026-11-10T02:00:00Z"}],"page":1,"page_size":20}}
```

当前组织 active member/admin 都可读，列表包含该组织 active 和 disabled 关系；按 user_id 升序分页。字段严格限于示例中的五项，不含邮箱、密码散列、其他组织信息或独立 member_id。

失败：401；orgID 或已授权后的分页格式错误为 400；组织不存在、非成员、disabled 均为相同 404 NOT_FOUND；数据库异常为 500。不得先查询全局成员再用前端过滤。

## 9. 添加已注册成员

`POST /api/v1/organizations/:orgID/members`，仅允许目标 user_id 与 role，二者必填。

```json
{"user_id":102,"role":"member"}
```

成功 201：

```json
{"data":{"user_id":102,"display_name":"Bob","role":"member","status":"active","created_at":"2026-11-10T02:05:00Z"}}
```

操作人必须为目标组织 active admin。admin 可添加 member 或 admin；目标用户必须已注册。status 永远由后端写 active，组织来自 URL，操作者来自认证上下文。

| 情况 | 响应及副作用 |
| --- | --- |
| Token 无效 | 401，不查询目标用户 |
| 组织不存在、操作者非成员或 disabled | 404 NOT_FOUND，不暴露目标用户或组织信息 |
| 操作者为 active member | 403，不查询目标用户 |
| orgID、user_id、role 或未知字段不合法 | 授权顺序允许后返回 400，不写入 |
| 目标用户不存在 | 404 USER_NOT_FOUND，仅已授权 admin 能看到 |
| 已有同组织关系，包括 disabled | 409 MEMBER_ALREADY_EXISTS，不更新原角色或状态 |
| 两个 admin 并发添加同一用户 | 一个 201、另一个 409，依靠联合主键，不产生重复关系 |
| 数据库失败 | 500，不返回虚假成员 |

添加接口不支持“加入自己”、恢复 disabled 或修改既有关系等额外语义；当前 admin 自己已有成员关系，再添加自己返回 409。HTTP 通用错误还有 413 / 415。

## 10. 冻结范围与后续验收

本周不新增角色修改、停用、组织详情/删除、用户搜索、邀请、刷新或撤销接口。第一周健康检查不纳入上述七个接口计数。

上述设计对应手册 A01～A18；Day 3～Day 6 还需覆盖严格输入、大小与长度边界、JWT 必需 Claim、密码计算门限、分页过滤和空列表。所有示例当前只作为合同，不是 curl 结果。设计调整须同步认证、权限、数据库文档与周报，并增加对应验证。

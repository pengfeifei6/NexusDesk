# NexusDesk W02 认证与会话设计

- 日期：2026-11-10；状态：Day 1 设计完成，代码待 Day 2～Day 6 实现与验证。
- 依据：[第二周手册](../项目开发文件/每周开发手册/05_NexusDesk_第二周详细开发手册.md)。
- 编写方式：AI 按开发者“全部直接完成”的委托补齐；下文具体参数是受托设计选择，不是个人掌握记录。
- 配套：[API 契约](api/auth-organization.md)、[授权矩阵](authorization-matrix.md)、[ADR-0002](decisions/0002-auth-security.md)、[第二周记录](weekly/week02.md)。

## 1. 范围与已有基础

沿用现有 Go Module、配置加载、Gin Router、pgxpool、PostgreSQL 17 和首条 organizations 迁移。第一周目前只有公开的 `GET /healthz`；下文七个业务接口均未实现。

第二周增加注册、登录、当前用户、组织创建、本人组织列表、组织成员列表、管理员添加已注册成员。注册只创建账号；一个账号可加入多个组织，在不同组织拥有不同角色。

本周不实现 Refresh Token、注销即撤销、找回密码、邮箱验证、OAuth、邀请、修改角色、停用接口、工单和前端。运行目标为本机回环地址演示；可靠登录限流等发布条件见第 7 节。

## 2. 身份与权限边界

| 步骤 | 解决的问题 | 可信依据 |
| --- | --- | --- |
| Token 解析 | 能否读出结构与字段 | 解析结果仍是不可信输入 |
| 身份认证 | 此请求是否持有本 API 接受的凭据 | JWT 签名、算法、Claims 与仍存在的用户 |
| 成员检查 | 当前用户是否属于目标组织 | organization_members 中对应关系及 active 状态 |
| 角色授权 | 允许执行这项操作吗 | 该组织内 admin/member 角色与动作矩阵 |

用户 ID 来自通过验证的 `sub`；组织 ID 只是请求者选中的目标。每个组织请求重新查询成员，不把前端角色或 JWT 中的角色作为事实来源。非成员和 disabled 成员统一隐藏组织资源，返回 404；有效 member 尝试添加成员返回 403。按 [授权矩阵](authorization-matrix.md) 执行校验顺序。

## 3. 注册与密码处理

### 3.1 输入规则

- 邮箱：去首尾空白、ASCII 小写化；接受普通 ASCII `local@domain` 地址，最长 254 字节；不接受显示名、列表、内部空白和国际化邮箱。完整格式要求见 API 契约；不自行删除点号或 `+tag`。
- 显示名：去首尾空白后 1～80 个 Unicode 码点，允许中文，必须是有效 UTF-8。
- 密码：有效 UTF-8，15～128 个 Unicode 码点且不超过 512 UTF-8 字节，拒绝全空白；不裁剪、不截断、不规范化，不强制字符组合。注册与登录使用同一编码和长度边界。
- 请求体上限 16 KiB；先做格式与长度校验，再计算散列。完整错误语义见 API 契约。

最小长度参考无 MFA 场景的 [OWASP 认证建议](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)；字符和字节上限为本项目的资源边界，不能据此宣称已经阻止所有弱密码。

### 3.2 散列选择和成本

使用 `github.com/alexedwards/argon2id` 封装，显式配置 Argon2id：Memory=19456 KiB、Iterations=2、Parallelism=1、SaltLength=16 字节、KeyLength=32 字节。每次散列使用安全随机盐，存储带算法、版本、参数、盐和散列的编码字符串。参数参考 [OWASP 密码存储建议](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)，封装方式参考 [库文档](https://github.com/alexedwards/argon2id)。

业务通过 `HashPassword` / `VerifyPassword` 的小范围封装调用库，不发明算法，不直接使用 SHA-256 或可逆加密保存密码。不盲用库默认参数；准确依赖版本在 Day 3 审查兼容性后锁定，本次不安装依赖。

注册和登录共用一个进程级计算门限，初始最多同时进行 2 次散列或验证。无空闲名额立即返回 503 `AUTH_BUSY`，不无限排队；请求取消时不再启动新的计算。已经执行的库函数不假设可以被 Context 强制中止，结束后释放名额，取消后不继续写库。

验证存储散列前检查编码和参数符合本周支持的版本与成本，拒绝异常超大参数，不把数据中的任意成本直接交给库分配资源。具体校验由封装实现并测试，库错误不原样返回客户端。

Day 3 必须记录本机单次与并发计算耗时、参数和资源观察，当前没有性能数据。“2 个并发名额”只限制密码计算资源，不能替代登录防暴力破解限流。

### 3.3 注册时序

```mermaid
sequenceDiagram
    participant C as 客户端
    participant H as Auth Handler
    participant S as Auth Service
    participant P as Password 封装
    participant R as User Repository
    participant D as PostgreSQL
    C->>H: POST /api/v1/auth/register
    H->>S: 已解析的 email、display_name、password
    S->>S: 规范化与长度校验
    S->>P: 获取计算名额并生成带随机盐的散列
    P-->>S: 编码后的 password_hash
    S->>R: CreateUser
    R->>D: INSERT users
    alt 插入成功
        D-->>R: 用户公开字段
        R-->>S: 用户
        S-->>H: 公开用户 DTO
        H-->>C: 201，不返回散列
    else 邮箱唯一冲突
        D-->>R: 指定唯一索引冲突
        R-->>S: EMAIL_ALREADY_EXISTS
        S-->>H: 业务冲突
        H-->>C: 409
    else 数据库异常
        R-->>S: 内部错误
        S-->>H: 安全错误
        H-->>C: 500，不泄露 SQL 或连接信息
    end
```

规范化后直接插入，以 `lower(email)` 唯一索引作为并发最终防线；即使优化增加预查，也不能用预查代替约束。只映射对应邮箱唯一冲突，不能把所有 SQL 错误当作重复注册。注册不写 organizations 或 organization_members，也不自动登录。

## 4. 登录流程与账号枚举边界

```mermaid
sequenceDiagram
    participant C as 客户端
    participant H as Auth Handler
    participant S as Auth Service
    participant D as User Repository / PostgreSQL
    participant P as Password 封装
    participant T as Token 服务
    C->>H: POST /api/v1/auth/login
    H->>S: email、password
    S->>S: 规范化与输入校验，取得计算名额
    S->>D: FindUserByEmail
    D-->>S: 用户、未找到或内部错误
    alt 用户存在
        S->>P: 验证已存储散列
    else 用户不存在
        S->>P: 对同成本 dummy hash 做验证
    end
    P-->>S: 比对结果
    alt 用户存在且验证成功
        S->>T: 以用户 ID 签发 15 分钟 JWT
        T-->>S: Access Token
        S-->>H: Token、Bearer、900 秒
        H-->>C: 200，Cache-Control no-store
    else 凭据不匹配
        S-->>H: INVALID_CREDENTIALS
        H-->>C: 401，相同通用消息
    end
```

不存在邮箱和错误密码均返回 401 `INVALID_CREDENTIALS`，消息固定为 `invalid email or password`。不存在用户仍做同成本 dummy 验证，dummy hash 在进程启动时用虚构输入生成，不从真实账号复制，也不在每次失败时重新生成。

数据库异常、损坏散列或签发内部失败返回安全的 500，不伪装成成功。dummy 路径旨在减小明显的耗时差异，不承诺网络响应时间完全一致。注册保留手册要求的 409 重复邮箱反馈，因此本地版本仍可能通过注册探测邮箱；公开部署前需要重新评估。

## 5. JWT 签发、验证与会话

### 5.1 配置计划

| 变量 | 本周契约 |
| --- | --- |
| JWT_ISSUER | nexusdesk，非空，验证时精确匹配 |
| JWT_AUDIENCE | nexusdesk-api，非空，验证时包含该受众 |
| JWT_TTL_MINUTES | 15；本周固定 15，其他值启动报错 |
| JWT_SECRET_BASE64 | 标准 Base64 解码后至少 32 字节的随机密钥；只在本地填写 |

以上变量到 Day 4 才加入示例模板和配置实现。当前 `.env.example` 与真实 `.env` 均不改；本文没有生成密钥。HMAC 使用解码后的字节，不能直接使用 Base64 文本。配置错误只指出字段与规则，不输出值。

### 5.2 签发与验证规则

- 使用 `github.com/golang-jwt/jwt/v5`；只允许 HS256，不根据传入 alg 自动选择策略，不使用未验证 Claims。
- 必需 Claims：sub 为正的十进制 int64 用户 ID 字符串；iss/aud 匹配配置；iat 和 exp 必须存在。
- 签发 exp=iat+900 秒；验证 iat 不晚于当前时间、exp 大于当前时间且大于 iat、exp-iat 不超过 900 秒。本周不增加时钟宽限；测试使用可控时钟。
- nbf 不主动签发；出现时同样校验，未到生效时间拒绝。缺失必需 Claim、错误签名、未知算法或畸形结构均拒绝。
- Payload 不放组织、角色、密码、散列、密钥或无关资料。签名 JWT 的 Payload 可以解码，签名提供完整性验证，不提供内容加密。

实现时显式使用算法、issuer、audience、exp 和 iat 校验能力；不能假设解析库默认要求所有字段。`iat` 的存在性及 sub/lifetime 规则需要额外验证。参见 [golang-jwt 解析说明](https://golang-jwt.github.io/jwt/usage/parse/) 和 [v5 API](https://pkg.go.dev/github.com/golang-jwt/jwt/v5)。

### 5.3 受保护请求

Authorization 只接受一个 Bearer 凭据；scheme 按大小写不敏感比较，拒绝空 Token、多凭据、重复 Authorization 和错误格式。不从 URL 或 JSON 取 Token。验证完成后再确认用户仍存在，数据库故障返回 500，用户已不存在返回 401。

Gin Context 只存可信用户 ID，不存完整 Token、密码或密钥。Service 使用标准 Context 和可信身份参数。组织操作继续查成员关系，不因 `/me` 成功就直接允许组织访问。

没有 Refresh Token 和服务端撤销表；过期重新登录。成员 disabled 后，旧 Token 在该组织的后续请求返回 404，但仍可用于 `/me` 和其他有效组织。过期只表示凭据失效，不删除账号。客户端删除 Token 也不会让别处保存的副本立即失效。

## 6. 组织事务与模块职责

组织创建只接受 name，从可信身份取得创建者。Service 协调同一事务内的两次写入：organizations → organization_members(admin, active) → COMMIT。第二步或提交失败不返回成功；错误回滚，不留下孤立组织。第一周演示库已有的无成员组织不自动归属给任何账号。

添加成员前检查操作者在目标组织为 active admin，再检查目标用户和角色。已有关系（包括 disabled）返回 409，不通过添加接口修改角色或重新启用。成员列表与个人组织列表必须使用认证身份和组织过滤。

Handler 负责协议、Service 负责规则与事务、Repository 执行参数化 SQL。创建组织与首名管理员可集中为一个专用事务方法，不建立通用仓储框架。Day 2 再定义各方法的精确签名与错误分类。

本周没有角色或状态修改接口；A16 在专用测试库通过夹具把成员置为 disabled，随后使用原 Token 验证拒绝。若未来加入角色/状态变更，要补事务一致性与并发测试，不能把本周查询成员关系当作已解决全部并发撤权问题。

## 7. 本地边界与待上线工作

本周选择手册允许的回环地址方案：后续认证接入后，限制 HTTP_ADDR 的主机为显式回环 IP，拒绝空主机、0.0.0.0 和公网监听；本周暂不实现可靠登录频率限制。Day 4 实施并验证该限制，本次只是设计。密码计算并发门限仍必须实现。

公开部署前至少补齐 HTTPS、可靠登录/注册防滥用、常见及泄露密码检查、真实凭据管理、日志审查和会话撤销策略。当前没有邮箱所有权验证；user_id 添加成员只面向已注册账号，本周不开放全局用户搜索。这些限制记录为未完成事项，不声称具有生产安全保障。

所有 API 的认证输入、Authorization、access_token、password_hash 和 DSN 不进入应用日志或测试快照；成功与错误响应采用公开 DTO，响应加 Cache-Control: no-store，未预期错误只给固定摘要。详细契约及负向验证见配套文档。

## 8. Day 1 五个问题的参考回答

1. **JWT 为什么不是角色的最终来源？** 签发后角色与成员状态可能变化，且角色属于具体组织；服务端每次组织请求查询当前成员关系。
2. **为什么注册与创建组织分开？** 注册建立身份，组织创建建立租户和首名管理员；允许用户先无组织，之后创建或加入多个组织。
3. **为什么必须查 organization_members？** 用户存在只证明账号存在；关联记录才说明其在目标组织是否有效及能执行哪些动作。
4. **为什么密码用慢散列？** 数据库泄露后，攻击者会离线猜测；带随机盐的内存困难算法提高每次猜测成本，不能用快速摘要代替。
5. **Token 失效为什么不等于账号删除？** Token 是有时限的访问凭据，账号是持久化身份；到期只要求重新认证，不修改用户记录或成员关系。

以上为 AI 参考回答；开发者的独立复述尚未记录。当前只完成设计，不代表 A01～A18 已运行通过。

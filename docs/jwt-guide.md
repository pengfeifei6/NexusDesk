# W02 Day 4：JWT 结构、登录与验证说明

- 日期：2026-11-10；用途：解释本次实现，供开发者后续阅读，不冒充独立作答记录。
- 代码：[TokenManager](../internal/auth/token.go)、[LoginService](../internal/auth/service.go)、[LoginHandler](../internal/auth/handler.go)。

## 1. 当前可演示的行为

注册只创建用户。使用相同邮箱和原密码请求 POST /api/v1/auth/login，后端验证密码后返回一个 15 分钟 Access Token。错误密码和不存在邮箱返回完全相同的 401 响应；空密码属于输入错误，返回 400。

Day 4 的 Verify 只验证 Token 的签名和内容，返回用户 ID；它不查询成员表，也不代表用户拥有某组织权限。Day 5 已接入认证中间件、/me 和用户存在性检查，见 [可信身份与事务说明](organization-transaction-guide.md)。Day 6已接入每请求查询的组织内成员授权，见[成员权限说明](member-authorization-guide.md)。

## 2. 三段结构分别是什么

```text
Base64URL(Header) . Base64URL(Payload) . Base64URL(Signature)
```

Header 声明算法与类型。本项目签发时固定为：

```json
{"alg":"HS256","typ":"JWT"}
```

Payload 保存声明。以下是虚构的内容示例，没有实际签名或可用凭据：

```json
{"sub":"101","iss":"nexusdesk","aud":["nexusdesk-api"],"iat":1794304800,"exp":1794305700}
```

| 字段 | 本项目含义与校验 |
| --- | --- |
| sub | 用户 ID 字符串，规范正十进制 int64；拒绝 0、负数、前导零和溢出 |
| iss | 发行方，必须匹配配置 |
| aud | 接收方，必须包含配置的 API 受众 |
| iat | 签发时间，必需且不能晚于验证时刻 |
| exp | 到期时间，必需且尚未到期；大于 iat，二者相差最多 900 秒 |
| nbf | 本项目不主动签发；出现时必须是合法时间且已经生效 |

时间字段只接受 int64 范围的 JSON 十进制整数秒，不接受小数、指数、字符串或 null。Issuer / audience 是有效 UTF-8，各最多 256 字节。

签名通过 HMAC-SHA256 对前两段及中间点号计算。验证方使用同一密钥重新验证签名；只有签名与内容检查全部通过，才接受 sub。签发方和受众检查限制 Token 被哪个系统接受，过期检查限制使用时间，它们不能互相替代。

## 3. 为什么解码出来不代表可信

Base64URL 是编码，任何持有 Token 的人都能解出 Payload。修改 sub 再重新编码也很容易，但修改后无法在不知道签名密钥的情况下生成有效签名。

因此本项目不把密码、散列、密钥或组织角色放入 Payload，也不会只解码后就信任用户 ID。后端只允许 HS256，不根据客户端 Header 任意选择算法；none、HS384、错误密钥和篡改都被拒绝。官方选项说明见 [golang-jwt 解析与验证](https://golang-jwt.github.io/jwt/usage/parse/)。

## 4. 配置和密钥如何使用

| 环境变量 | 当前要求 |
| --- | --- |
| JWT_ISSUER | 非空，默认示例 nexusdesk |
| JWT_AUDIENCE | 非空，默认示例 nexusdesk-api |
| JWT_TTL_MINUTES | 本周固定 15 |
| JWT_SECRET_BASE64 | 标准 Base64 规范编码，解码后至少 32 字节；不接受多余空白或换行 |

应用先解码 Base64，再把字节作为 HMAC 密钥；不能直接拿 Base64 文本替代字节。TokenManager 复制密钥，调用方之后修改原切片不会改变签名行为。

真实密钥由开发者本机配置，不保存到文档、请求示例或测试结果。测试运行器只生成自己临时环境的虚构测试密钥，并对输出脱敏。

## 5. dummy hash 与共享计算门限

若邮箱不存在就立即退出，和实际验证密码的耗时差异会很大。登录服务在启动时准备一个同参数的虚构散列，不存在账号时也验证一次密码。即使输入恰好匹配 dummy 的公开虚构原文，也不会登录，因为必须同时确认用户存在。

注册与登录复用同一个 PasswordHasher，Hash 和 Verify 共用两个计算名额；超额返回 503。dummy 不在每次失败时重新生成。它们限制成本、减小明显耗时差异，不保证网络响应恒定时间，也不替代可靠登录频率限制。

## 6. 本次审查处理的两个细节

golang-jwt 默认按秒精度解析 NumericDate，会截掉小数部分。本项目在库解析日期前检查原始 JSON，拒绝小数等非整数表示，保持零宽限与最长 900 秒的规则，不改全局时间精度。

合法发行方和受众在 JSON 中可能发生 HTML/控制字符转义，输出会变长。原先 4096 字节的解析预算会拒绝部分自己签发的 Token；当前使用 8 KiB 上限，覆盖两个 256 字节标签的最坏转义、用户 ID 与签名，并在签发端检查长度。边界配置有自签发、自验证回归；超长但签名正确的 Token 也拒绝。

库版本固定 v5.3.1，参考 [官方发布记录](https://github.com/golang-jwt/jwt/releases/tag/v5.3.1)。完整发现和验证见 [Day 4 审查](reviews/week02-day4-review.md) 与 [本周记录](weekly/week02.md)。

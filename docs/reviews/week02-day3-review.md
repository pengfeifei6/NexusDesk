# W02 Day 3 注册与密码代码审查

- 日期：2026-11-10；范围：auth 模块、注册集成、路由/启动/配置、隔离运行器与依赖锁定。
- 方式：独立代理只读检查工作区和已有证据，主代理核对后修复并实际验证。
- 结果：发现两项 P2 输入问题，均已补红灯、修复并通过相关单元与最终真实数据库回归；没有遗留的本次实质修复项。

## 1. 发现与修正

| 编号 | 问题与触发 | 本次修正 | 验证 |
| --- | --- | --- | --- |
| W02-D3-R01 / P2 | ASCII 形式的未配对高/低代理项会被 encoding/json 替换成 U+FFFD，原始字节 UTF-8 检查无法发现，不同无效密码可能变成同一字符串 | [json_input.go](../../internal/auth/json_input.go) 在解码前检查 UTF-16 转义，拒绝不成对代理项；JSON 语法仍由标准解码器验证 | HTTP 用例先失败，再确认 400 且不执行散列/存储；合法代理对、显式 U+FFFD、字面反斜杠与密码 NUL 正常；额外短时 fuzz 通过 |
| W02-D3-R02 / P2 | 显示名 A + U+0000 + B 通过长度与 UTF-8 校验，先计算散列后才被 PostgreSQL 文本类型拒绝，导致输入错误变成 500 | [service.go](../../internal/auth/service.go) 在散列前拒绝显示名 NUL，按 INVALID_REQUEST 返回 400，并同步 API 契约 | Service/HTTP 红灯后通过；真实数据库链路返回 400 且查不到新增用户；密码 NUL 保留，未一并禁止 |

失败证据：[review-red.log](../weekly/evidence/week02-day3-review-red.log)；修复单元结果：[review-green.log](../weekly/evidence/week02-day3-review-green.log)；最终真实链路：[postgres.log](../weekly/evidence/week02-day3-postgres.log)；模糊测试：[json-fuzz.log](../weekly/evidence/week02-day3-json-fuzz.log)。

## 2. 其余核对范围

审查方在以下范围未发现其他实质问题：固定 Argon2id 参数及编码成本限制、共享两个计算名额、取消后停止写库、指定邮箱唯一索引映射、参数化查询、公开 DTO、16 KiB 边界、显式回环监听，以及主入口依赖组装。

注册以数据库唯一索引为并发防线，返回前确认单条写入成功；组织创建事务、角色权限和 JWT 不在本日实现范围，不能据注册审查宣称这些已经正确。

## 3. 验证归属与限制

独立审查代理没有编辑文件、运行测试、启动数据库或读取真实 `.env`。两项发现由主代理分别核对并修正；修复后的测试由主代理运行，没有声称独立审查方重新审查或运行最终版本。

最终结果包含普通 race、真实 PostgreSQL/race、进程 HTTP、W1 回归、vet/build/module verify 与格式检查，入口见 [Day 3 统计](../weekly/evidence/week02-day3-test-summary.json) 和 [周报](../weekly/week02.md)。公开部署防护、依赖漏洞数据库审计、登录/JWT 与组织授权未因此完成。

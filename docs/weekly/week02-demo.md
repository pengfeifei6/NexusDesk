# 第二周可复现演示记录

- 日期：2026-11-10，Asia/Shanghai。
- 执行者：AI根据开发者委托执行；真实终端/TCP演示，未录视频，不代表开发者已独立演示。
- 入口：[week02](week02.md)、[完整新目录日志](evidence/week02-day7-clean-reproduction.log)、[统计摘要](evidence/week02-day7-test-summary.json)。

## 1. 本次验证环境

在/var/tmp创建本次专属项目快照，按清单复制58个运行源码/示例配置/测试/脚本文件，不复制真实.env、.git或生成缓存。先记录工具版本、go mod download，再执行python3 scripts/week02-day2-smoke.py --closeout。

本机Go1.26.1、Docker26.1.3、Compose5.6.0、Goose3.29.0和本地postgres:17参与验证；现有模块缓存/主机工具被复用，不等同全新机器安装。复制内容见 [复现清单](evidence/week02-day7-reproduction-manifest.json)，本次快照已自动清理。

运行器使用独立随机Compose Project、临时回环端口、专用测试库和临时虚构凭据，Goose禁止自动加载.env；只删除本次核对归属的容器/测试卷。

## 2. 手册八步演示与数据库效果

| 顺序 | 动作与实际结果 | 核对依据 |
| --- | --- | --- |
| 1 | Alice注册201、登录200 | 公开DTO；Python独立HMAC验证签名、身份Claim和900秒有效期 |
| 2 | Alice创建A组织201 | 真实数据库组织对应当前Alice的admin/active关系 |
| 3 | Bob注册201、登录200 | 真实API登录结果仅保留测试内存 |
| 4 | Bob创建B组织201 | 数据库Bob在B的admin/active关系 |
| 5 | Bob请求A成员列表404 | 数据库Bob在A的关系数为0；A/B之间资源隐藏一致 |
| 6 | Alice添加Bob到A返回201 | 数据库A/Bob关系为member/active，五字段DTO |
| 7 | Bob读取A成员200，添加403 | 成员与管理员动作权限分开；不能因Bob在B是admin而管理A |
| 8 | 过期凭据访问/me及A成员均401 | 用本次虚构密钥在内存签名，iat/exp差900秒且已过期；未改生产时钟 |

完整日志中DEMO step 1～8逐项对应。之后继续验证重复409、禁用后旧Token404、另一组织仍可访问、禁用关系不会被重复添加恢复；数据库状态和应用日志脱敏均通过。

过期Token、正常Token、密码和密钥均不写入演示文档或归档输出。不是通过等15分钟、跳过签名或修改生产时钟模拟拒绝。

## 3. 真实测试与回归

新目录--closeout包含schema、注册、登录、组织和成员全部测试，7包、64个顶层通过、无失败/跳过；包含子测试与fuzz seeds为371通过节点。普通race为58顶层通过、6个真实数据库入口跳过，不能将其当作数据库已验证。

组织第二步/提交失败回滚、成员提交失败、锁等待取消、并发唯一、SQL范围与当前角色验证都实际运行。独立的 [W1回归](evidence/week02-day7-week01-regression.log) 固定迁移版本1，验证迁移/保留卷重启、健康检查、SIGINT/SIGTERM和数据库连接失败；不把该历史模式当作最新schema验收。

## 4. 自己复现时的顺序

项目根目录可直接执行，不加载真实.env：

```bash
python3 scripts/week02-day2-smoke.py --closeout
```

前置条件是README中已说明的Go、Docker/Compose、Goose和本地postgres:17镜像。脚本自己准备临时测试配置；失败时保存安全错误与退出状态，不能手工把开关指向日常开发库。

若使用自己的开发环境，先按 [README](../../README.md) 准备十二变量配置、启动数据库、迁移到版本3并运行API，再在本地客户端使用 [虚构请求示例](../../requests/week02.http)。实际账号、组织ID和Token由本地客户端提供，不回填到仓库。

## 5. 演示范围与交接

本周可以展示七个身份/组织业务接口、密码验证、短期JWT、组织/admin事务、成员RBAC、跨组织拒绝和真实回归。当前没有工单、前端、角色修改、停用/邀请API或生产部署；不把参考设计和安全待办写成已上线功能。

复盘和第三周风险见 [week02](week02.md)，按天学习与十题答案见 [week02-answers](week02-answers.md)。

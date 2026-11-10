# W03 工单可复现演示

2026-11-10，AI按整周授权实际执行，未录个人视频，不代表开发者已独立演示。[日志](evidence/week03-clean-reproduction.log)包含全量race/数据库与W03 DEMO 1～6。

## 已执行的流程

| 步骤 | 实际行为与核对 |
| --- | --- |
| 1 | Alice登录A，创建OPEN/version0，详情/过滤列表200，创建事件1条 |
| 2 | admin更新优先级、指派同组织Bob，各加一次version、各一事件，总事件3条 |
| 3 | Bob启动处理、评论（version不变）、解决，admin关闭；CLOSED重开409 |
| 4 | 只有B资格的Visitor无法读写A工单，详情/评论/事件404；B URL+A真实ticketID在集成中同样404 |
| 5 | admin软删204，普通详情/子资源404，数据库保留TICKET_DELETED |
| 6 | 临时事件触发器失败，transition/comment500，父状态OPEN/version1和空评论保持原样，恢复触发器后正常可用 |

集成还验证T01～T26、旧版本双请求一200一409、迟到旧事务审计时间、锁等待取消、关闭父工单与评论顺序、所有写入BEFORE/延迟COMMIT事件失败回滚，以及软删后的七个对象端点不可见。

## 本地复现

项目根目录执行：

```bash
python3 scripts/week03-smoke.py
```

这个入口复用已验证的隔离运行器并开启--tickets，包含W02全部测试和八步演示，迁移升到6，重复Up、反序回滚至3再升到6，仅在随机独立项目运行，保留旧组织哨兵；回环地址、专用库前缀及显式开关阻止误用日常库。Token/密钥仅留测试内存，日志核对无凭据。

--ticket-schema-baseline是预期失败实验：只应用到W02版本3后检查新表，应该非零退出，不是通过命令。

## 新目录与运行边界

最终从/var/tmp中新项目复制74个运行源码/示例配置/脚本/测试文件，不复制真实.env/.git/生成缓存；使用现有主机Go/Docker/Compose/Goose、缓存及本地postgres:17，不等于全新机器安装。复制内容见[复现清单](evidence/week03-reproduction-manifest.json)，临时快照/容器/卷已清理。

若用自己维护的开发库，按[README](../../README.md)准备十二变量、迁移到6，再用[虚构请求](../../requests/week03.http)在客户端私有变量中提供凭据和实际ID。数据库迁移不随API启动自动运行；不能为复现回滚或删掉日常卷。

本周工单能力不是完整G1前端产品；没有claim、support、Vue或请求级幂等去重。W04交接和真实统计见[week03](week03.md)，学习参考见[回答文档](week03-answers.md)。

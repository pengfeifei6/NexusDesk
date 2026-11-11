# 工具注册与权限

| 工具 | 风险类别 | 参数 | 执行入口 | 结果 |
|---|---|---|---|---|
| search_knowledge | READ_ONLY | query1～1000字符/4000字节；top_k1～5默认5 | RAG.SearchWithValidation，与Search共用retrieve | 本组织真实片段、来源及私有校验闭包 |
| get_ticket | READ_ONLY | ticket_id规范正int64十进制字符串 | TicketService.Get | id/title/body摘要/priority/status/version |
| propose_create_ticket | WRITE_PROPOSAL | title1～200、body1～10000字符，priority1/2/3 | Store.Propose | 持久化提议，未创建工单 |

`create_ticket`、确认、SQL、Shell、URL访问、文件读取不在注册表中。模型无权更改注册表，工具结果也不能动态添加工具。

`Actor{UserID,OrgID}` 在Handler从RequireUser及授权后的路径构造。工具参数不含这些字段；多传org_id/user_id、大小写别名、未知字段或重复字段直接拒绝。并不是模型说“我是管理员”后再去相信它的角色。

模型JSON Schema只是提示。Go再次检查JSON整体语法、UTF8与surrogate、重复键、精确字段名、非null、类型、字符串长度、枚举、ID规范。合法JSON依然可能包含错误对象或恶意指令，因此还要查真实权限。

查询工单只通过已有组织范围Service，不全局按ID查对象。检索返回的文档标题和正文都视为数据；返回模型时是tool角色JSON，不插入system提示。来源由后端检索集合构造。发布新文字之前再次验证成员关系、工单版本、文档可见性和索引hash/version；已流出的文字不能被网络“撤回”，因此发生变化后停止后续输出并标失败。

系统提示可以帮助模型少犯错，不能赋予或撤销数据库权限。即使模型完全听从恶意文档，工具层仍拒绝未注册能力、伪造租户及直接写入。模型自然语言也不作为“操作成功”的凭据，成功状态只看持久化卡片与工单ID。

工具未知/参数非法会终止本轮并只记录固定错误码；对象不可见可作为 `{"error":"NOT_FOUND"}` 回传模型解释。日志不包含参数、文档、Token或原始供应商响应。错误码只从白名单输出。

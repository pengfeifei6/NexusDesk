# 第八周运行与演示

在项目根目录执行。已有真实 `.env` 继续使用，DeepSeek Key只由API进程读取，不要复制到前端。

```bash
cd /media/yjz/pzp/Go/projects/NexusDesk
python3 scripts/dev.py status
```

没有运行时，分别在三个终端启动（已经运行就不要重复占用端口）：

```bash
python3 scripts/dev.py api
```

```bash
python3 scripts/dev.py worker
```

```bash
python3 scripts/dev.py web
```

打开 <http://127.0.0.1:5173>；注册或登录 → 选择/创建组织 → 点击“AI 对话” → 输入对话标题并新建 → 输入消息发送。前端通过同源Vite代理访问8080。远程浏览器访问需使用你既有的本机端口转发，API继续只监听回环。

会话和消息保存在PostgreSQL，登录Token仍只在内存，刷新页面需重新登录。重新打开同一组织的AI对话列表可以读取已保存记录。新页面与旧“检索问答”同时保留；旧页面使用非流式Ask，新页面可以多轮聊天和受控工具调用。

## 准备知识库

PostgreSQL、独立Redis6380、Ollama11434须启动；上传TXT/MD后点击“上传并异步处理”，等任务SUCCEEDED且索引READY。如果BGE冷启动耗时，先执行：

```bash
python3 scripts/dev.py warmup
```

DeepSeek只负责文本生成，Embedding仍本地BGE-M3。Worker不需要JWT或DeepSeek Key。已经READY的知识库与工单查询不要求Redis在线；新异步入库仍需要Worker/Redis。

## 3～5分钟演示顺序

1. 新建个人会话，问“请先查询知识库：数据库连接超时首先检查什么？”观察逐段文字及真实来源。
2. 用真实本组织ID问“查询工单42的完整标题、状态和优先级”。其他组织工单不会返回内容。
3. 提问“请提出一个工单：标题检查索引失败，正文检查Worker和Redis连接，优先级高”。完整确认卡片出现后，先去工单列表核对尚未新增。
4. 回到卡片点击“确认创建工单”，观察EXECUTED与工单链接；打开工单事件，TICKET_CREATED的after_data含ai_proposal_id。重复确认返回同一ID。
5. 新建另一会话提出另一个工单，点击拒绝；拒绝后不能确认，工单数不增加。
6. 提问较长文本，点击“停止生成”，重新读取历史看到已中断。切换组织后看不到上一组织会话。

只有数据库持久化卡片代表实际提议，只有EXECUTED/ticket_id代表已建单。模型自然语言可能错误声称“已保存”，没有卡片时不能据此认为已提出或已执行；可以明确要求实际调用工具或新建独立会话。安全边界由工具和事务保证，不依赖模型语言承诺。

## 故障与维护

| 现象 | 检查/处理 |
|---|---|
| 找不到AI入口 | 重启新版API/前端，登录后选择组织 |
| 创建对话500 | 确认数据库已Up到迁移12；查看固定安全错误，勿在日常库Down |
| 429/生成繁忙 | 同一会话只允许一轮，实例最多两轮；等待或停止后读取历史 |
| 生成失败/模型超时 | 查DeepSeek网络/配置或Ollama；已有工单仍可独立工作 |
| 未检索到来源 | 文档与索引须READY，确认当前组织和阈值；不要编造引用 |
| 停止后仍显示生成中 | 先重新读历史；数据库清理失败/进程崩溃时，超过120秒再读会收敛中断 |
| 提议过期 | 15分钟后不可执行，需要新的提议；不能改旧参数重新确认 |
| 只见文字“已提出”无卡片 | 模型没有成功保存提议，按卡片状态判断，不当作实际操作 |

本机本次已在不透明0600备份后只Up11→12；原有数据和迁移保留。详情见 [runtime.json](../testing/evidence/week08/runtime.json)。测试数据库的Up/Down实验只属于独立烟测，不用于维护日常数据。

录像及截图：[实际浏览器结果](../testing/evidence/week08/browser.json) · [演示录像](../testing/evidence/week08/week08-demo.webm)。接口手工请求见 [requests/week08.http](../../requests/week08.http)。复习见 [22道参考问题](../weekly/week08-answers.md)。

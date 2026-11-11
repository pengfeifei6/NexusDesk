# ADR-0008：原生流式函数调用与显式人工确认

状态：实施。日期：2026-11-11。依据：用户委托第八周整周开发；手册模板按现有项目契约调整。

现有系统已经有组织授权、工单Service、知识库检索、后台入库以及DeepSeek非流式问答。第八周需要可解释的小型工具循环与流式交互，不需要增加另一套业务写入入口。

决定采用自有ChatModel接口与原生HTTP适配器，供应商细节集中在llm/stream.go。工具注册表固定三个条目；模型只能查资料、查工单或提议建单。Runner有严格轮数和预算。所有组织身份来自后端，供应商JSON Schema之外再做Go验证。

写操作通过持久化提议和独立confirm接口。TicketService提供同事务创建入口，保留既有验证与事件。UUID会话配bigint既有业务ID，不强行重写历史协议。路由沿用组织前缀，减少隐藏的租户选择；同组织内owner私有。

收益是可审查、可测试，失败与权限边界清晰，沿用已有模块。成本是需要维护两种供应商流协议、SSE状态机及历史预算。未来若引入框架，仍不能把租户权限或确认事务交给模型/框架。

本周不引入MCP、Eino/LangGraph、多Agent规划器、任意工具执行、自动建单、流式断点续传或公网发布。下一步若多实例部署，应增加共享速率限制、持久运行租约、完善历史分页/归档以及运维监控，再讨论更复杂编排。

依据：[DeepSeek Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)、[DeepSeek Tool Calls](https://api-docs.deepseek.com/guides/tool_calls/)、[Ollama Chat](https://docs.ollama.com/api/chat)。字段在2026-11-11核对；无新供应商SDK依赖。

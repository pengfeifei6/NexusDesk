# 第八周开发与验收记录

任务：完成用户委托的第八周SSE、会话、原生工具调用与人工确认。工作从2026-11-11开始；实现由AI协助，开发者个人掌握程度、独立复盘和最终Git提交须自行完成。

## 当前交付

| 日次 | 交付 |
|---|---|
| Day1 | 现状核对、设计、内部与HTTP契约、ADR0008 |
| Day2 | 迁移00012：会话/消息/提议3表，owner+org隔离，去重与遗留状态回收 |
| Day3 | DeepSeek/Ollama适配器、POST SSE、心跳/取消/失败收尾、Mock与真实HTTP测试 |
| Day4 | 三项固定工具定义、两个只读工具、严格参数、有限循环与来源复查 |
| Day5 | 建单提议、确认/拒绝、摘要/有效期、同事务工单事件与EXECUTED、重复确认 |
| Day6 | 第九页前端、来源/确认卡片、停止/空态/错误、35项矩阵和故障注入 |
| Day7 | 三轮专项审查及回归、运行演示、录像与截图、参考答案和维护说明 |

本周新增6个业务接口，当前39个业务API+healthz、14张业务表、12条迁移、9个主页面。只追加00012，旧迁移与W01～W07历史证据保留。go.mod中原有google/uuid v1.6.0改为直接依赖；无新增SDK、依赖升级或框架。

## 实际验证

全量隔离Go race检查已运行：1051个PASS节点（227个顶层），0FAIL/0SKIP，包含真实PostgreSQL与Redis及旧周回归。节点包含顶层与子测试，不等于1051个独立API。完整日志见 [integration.log](../testing/evidence/week08/integration.log)。Go vet/build已通过，最终命令汇总随交付证据记录。

前端当前130项测试通过（29项W08），type-check、production build、lint、format:check通过。启动脚本5项Python测试通过。

真实DeepSeek+BGE浏览器完整链路PASS：知识引用、随机合成工单查询、未确认无新增、确认与事件、4次并发重复确认同ID、独立提议拒绝、跨组织404、停止持久化INTERRUPTED、移动端及跳转既有工单。3分钟录像和截图已生成。结果见 [result.json](../testing/evidence/week08/result.json)、[browser.json](../testing/evidence/week08/browser.json)，完整命令见 [checks.log](../testing/evidence/week08/checks.log)。全分支审查无阻塞问题；最后一项实时引用数组共享的轻微显示问题也已修复，并有生成尚未结束时的引用归属回归；审查摘要见下方。

独立数据库验证00012 Up→Down→Up；日常数据库另做不透明0600备份后仅Up11→12，无Down、删卷或重建。现有DeepSeek `.env` 保留，Key未写入代码、浏览器、文档或证据。运行状态见 [runtime.json](../testing/evidence/week08/runtime.json)。

## 发现与修复

数据库真实测试暴露测试夹具改变消息时间后位置断言错误，已按message_id验证。静态审查和回归修复Gin FlushError丢失、参数大小写别名、合法SSE合并chunk误拒绝、确认后旧历史覆盖和切组织离开AI页。真实模型多轮演示发现删除旧助手引用消息会使历史用户请求看起来尚未处理，改为保留固定结束标记，不重发旧引用正文。

模型仍可能只用自然语言声称“已保存提议”却没有实际调用工具；工具层不会因此写入。卡片是持久化事实来源，演示分别检查真实提议、未确认无新增、确认后有审计。此行为属于模型输出可靠性边界，不能用放宽预算或自动创建来掩盖。

## 已知范围

会话只有owner可见；历史保留旧文字，不因源文档删除自动擦除。后续模型不重发旧引用内容，本轮输出前复核当前依据。最新50会话/100消息/100提议无全量分页；未提供归档、断流续传、多实例共享额度或公网防滥用。PDF仍仅上传/下载；成员管理、工单编辑/指派/删除的完整UI仍属历史未完成范围。

当前完成的是本机可运行工程，不代表生产部署、模型准确率保证或开发者个人已掌握。没有自动commit/push/merge、公开部署或修改原第八周教学手册。

## 阅读入口

[运行演示](../demo/week08-demo.md) · [API](../api/ai-conversation.md) · [Agent流程](../ai/agent-flow.md) · [工具边界](../ai/tool-registry.md) · [SSE](../ai/sse-protocol.md) · [确认事务](../ai/approval-security.md) · [测试矩阵](../testing/week08-test-plan.md) · [问题参考答案](week08-answers.md)。

## 最终审查摘要

确认事务、SSE/取消、工具安全、前端与全分支审查均完成。全部发现已修复，无遗留阻塞项；最后的引用数组问题通过新增生成中状态测试验证。完整浏览器录屏生成在该孤立显示修复前，完成态/接口/后台代码相同；修复后完整130项前端测试与构建通过，没有为此重复付费云调用。统计汇总见 [summary.json](../testing/evidence/week08/summary.json)。
